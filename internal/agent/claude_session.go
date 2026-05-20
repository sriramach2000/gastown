package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// claudeSession implements Session for the Claude tmux rail.
//
// In M1 this is the only concrete implementation. It wraps a tmux subprocess
// for process management and streams output by polling tmux capture-pane.
//
// The full Claude Agent SDK integration (partio-io/claude-agent-sdk-go) is
// deferred to a follow-up milestone. This implementation preserves the
// byte-identical behaviour of the existing session_manager.go tmux path while
// putting it behind the Session interface so the router (M3) can dispatch to it.
type claudeSession struct {
	// rail is always RailClaude; stored at construction and never mutated.
	rail Rail

	// opts is the StartOptions provided at construction time.
	opts StartOptions

	// sessionName is the tmux session name set at Start time.
	sessionName string

	// started is 1 after Start succeeds, 0 otherwise.
	started atomic.Int32

	// stopped is 1 after Stop has been called.
	stopped atomic.Int32

	// mu guards mutable fields below.
	mu sync.Mutex

	// summary is populated by Stop.
	summary *Summary

	// turns counts the number of Send calls since Start.
	turns int
}

// newClaudeSession constructs a claudeSession. Rail is fixed to RailClaude.
// Start must be called before Send/Stream/Stop.
func newClaudeSession(opts StartOptions) *claudeSession {
	return &claudeSession{
		rail: RailClaude,
		opts: opts,
	}
}

// Rail returns RailClaude. Immutable for the lifetime of the session.
func (s *claudeSession) Rail() Rail {
	return s.rail
}

// Start launches the agent subprocess via tmux and blocks until it is ready
// or the context is cancelled.
//
// For M1 this delegates to the `tmux new-session` subprocess directly.
// The full SDK handshake (ResultMessage, BudgetGuardHook) is wired in M0/M3.
func (s *claudeSession) Start(ctx context.Context, opts StartOptions) (SessionID, error) {
	if s.started.Swap(1) == 1 {
		return "", ErrSessionAlreadyStarted
	}

	// Merge caller opts into construction opts (caller takes precedence for non-zero fields).
	merged := s.mergeOpts(opts)

	if merged.WorkDir == "" {
		s.started.Store(0) // allow retry
		return "", fmt.Errorf("claudeSession.Start: WorkDir is required")
	}

	// Build a deterministic session name from polecat name.
	sessionName := buildSessionName(merged)

	s.mu.Lock()
	s.sessionName = sessionName
	s.opts = merged
	s.mu.Unlock()

	// Spawn the tmux session carrying the claude process.
	if err := spawnTmuxSession(ctx, sessionName, merged); err != nil {
		// Reset started flag so callers can attempt recovery.
		s.started.Store(0)
		return "", fmt.Errorf("claudeSession.Start: spawn tmux session %q: %w", sessionName, err)
	}

	return SessionID(sessionName), nil
}

// Send delivers a prompt to the agent via tmux send-keys.
// Returns ErrSessionNotStarted if Start has not been called.
func (s *claudeSession) Send(_ context.Context, msg string) error {
	if s.started.Load() == 0 {
		return ErrSessionNotStarted
	}

	s.mu.Lock()
	sessionName := s.sessionName
	s.turns++
	s.mu.Unlock()

	return tmuxSendKeys(sessionName, msg)
}

// Stream returns a channel of Events. The channel is closed after an EventDone
// is emitted or the context is cancelled.
//
// For M1 the implementation polls tmux capture-pane at a 200ms interval, emitting
// text deltas as EventText events and closing with EventDone when the prompt
// returns to the idle state. This matches the existing polecat visibility model.
// The SDK streaming surface (NDJSON callbacks) is wired in M0/M3.
//
// Returns ErrSessionNotStarted if Start has not been called.
func (s *claudeSession) Stream(ctx context.Context) (<-chan Event, error) {
	if s.started.Load() == 0 {
		return nil, ErrSessionNotStarted
	}

	s.mu.Lock()
	sessionName := s.sessionName
	s.mu.Unlock()

	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		var lastOutput string

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		// Poll for up to 5 minutes; the witness / budget guard will terminate
		// long-running sessions from the outside if needed.
		deadline := time.After(5 * time.Minute)

		for {
			select {
			case <-ctx.Done():
				ch <- Event{Kind: EventError, Err: ctx.Err()}
				return
			case <-deadline:
				ch <- Event{Kind: EventDone}
				return
			case <-ticker.C:
				output, err := tmuxCapturePane(sessionName, 200)
				if err != nil {
					// Session may have died — emit error and stop.
					ch <- Event{Kind: EventError, Err: fmt.Errorf("capture-pane: %w", err)}
					return
				}

				// Emit only the new lines since last poll.
				delta := diffOutput(lastOutput, output)
				if delta != "" {
					ch <- Event{Kind: EventText, Text: delta}
				}
				lastOutput = output

				// Detect idle prompt ("❯ " at end of pane) as done signal.
				if strings.HasSuffix(strings.TrimRight(output, " \t"), "❯") {
					ch <- Event{Kind: EventDone}
					return
				}
			}
		}
	}()

	return ch, nil
}

// Stop terminates the tmux session. Idempotent.
// If force is false a Ctrl-C is sent first and the process is given a short
// grace period before a hard kill.
func (s *claudeSession) Stop(ctx context.Context, force bool) error {
	if s.stopped.Swap(1) == 1 {
		// Idempotent: already stopped.
		return nil
	}

	s.mu.Lock()
	sessionName := s.sessionName
	turns := s.turns
	hooks := s.opts.Hooks
	s.mu.Unlock()

	if sessionName == "" {
		// Start was never called or failed; nothing to tear down.
		s.mu.Lock()
		s.summary = &Summary{Rail: RailClaude, Status: "not-started"}
		s.mu.Unlock()
		return nil
	}

	if !force {
		// Best-effort graceful interrupt; ignore errors (session may already be gone).
		_ = tmuxSendKeysRaw(sessionName, "C-c")
		// Wait briefly for the process to exit cleanly.
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}

	err := tmuxKillSession(sessionName)

	summary := &Summary{
		Rail:   RailClaude,
		Turns:  turns,
		Status: "stopped",
	}
	s.mu.Lock()
	s.summary = summary
	s.mu.Unlock()

	if hooks.OnStop != nil {
		hooks.OnStop(summary)
	}

	return err
}

// ResultSummary returns the session summary. Returns nil before Stop is called.
func (s *claudeSession) ResultSummary() *Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summary
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// mergeOpts returns a copy of s.opts overlaid with any non-zero fields in
// the caller-provided opts. Construction opts are the defaults; Start opts
// are the override (matches ADR pattern where StartOptions is passed at spawn
// time with bead-specific fields).
func (s *claudeSession) mergeOpts(caller StartOptions) StartOptions {
	base := s.opts

	if caller.PolecatName != "" {
		base.PolecatName = caller.PolecatName
	}
	if caller.Rig != "" {
		base.Rig = caller.Rig
	}
	if caller.WorkDir != "" {
		base.WorkDir = caller.WorkDir
	}
	if caller.Beacon != "" {
		base.Beacon = caller.Beacon
	}
	if caller.BeadID != "" {
		base.BeadID = caller.BeadID
	}
	if caller.BeadSeverity != "" {
		base.BeadSeverity = caller.BeadSeverity
	}
	if len(caller.BeadLabels) > 0 {
		base.BeadLabels = caller.BeadLabels
	}
	if caller.MaxTurns > 0 {
		base.MaxTurns = caller.MaxTurns
	}
	if caller.MaxBudgetUSD > 0 {
		base.MaxBudgetUSD = caller.MaxBudgetUSD
	}
	if caller.ExtendedThinking {
		base.ExtendedThinking = true
	}
	// Merge env maps (caller wins on conflicts).
	if len(caller.Env) > 0 {
		if base.Env == nil {
			base.Env = make(map[string]string, len(caller.Env))
		}
		for k, v := range caller.Env {
			base.Env[k] = v
		}
	}
	// Hooks from caller override (non-nil fields win).
	if caller.Hooks.OnEvent != nil {
		base.Hooks.OnEvent = caller.Hooks.OnEvent
	}
	if caller.Hooks.OnStop != nil {
		base.Hooks.OnStop = caller.Hooks.OnStop
	}
	return base
}

// buildSessionName produces a tmux-safe session name from StartOptions.
// Format: gt-<polecat> (mirrors session_manager.go SessionName convention).
func buildSessionName(opts StartOptions) string {
	name := opts.PolecatName
	if name == "" {
		name = "polecat"
	}
	return "gt-" + name
}

// ---------------------------------------------------------------------------
// Thin subprocess shims (tmux CLI calls).
// These are package-level variables so tests can replace them with stubs
// without spawning real tmux processes.
// ---------------------------------------------------------------------------

// spawnTmuxSession creates a new tmux session running the claude CLI.
var spawnTmuxSession = func(ctx context.Context, sessionName string, opts StartOptions) error {
	args := []string{
		"new-session", "-d",
		"-s", sessionName,
	}
	if opts.WorkDir != "" {
		args = append(args, "-c", opts.WorkDir)
	}

	// Build the claude command with the beacon prompt.
	command := "claude --dangerously-skip-permissions"
	if opts.Beacon != "" {
		// Beacon is passed as the initial prompt argument.
		escaped := strings.ReplaceAll(opts.Beacon, `"`, `\"`)
		command = fmt.Sprintf(`claude --dangerously-skip-permissions "%s"`, escaped)
	}
	args = append(args, command)

	cmd := exec.CommandContext(ctx, "tmux", args...) //nolint:gosec // G204: tmux is a trusted internal tool
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxSendKeys delivers a text message to a running tmux session.
var tmuxSendKeys = func(sessionName, msg string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, msg, "Enter") //nolint:gosec // G204
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxSendKeysRaw delivers a raw key sequence (e.g. "C-c") to a session.
var tmuxSendKeysRaw = func(sessionName, keys string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, keys) //nolint:gosec // G204
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys-raw: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxCapturePane returns the last n lines of a session's pane as a string.
var tmuxCapturePane = func(sessionName string, lines int) (string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-p", "-t", sessionName, //nolint:gosec // G204
		fmt.Sprintf("-S-%d", lines))
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}
	return string(out), nil
}

// tmuxKillSession terminates a tmux session.
var tmuxKillSession = func(sessionName string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName) //nolint:gosec // G204
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Session already gone — treat as success.
		if strings.Contains(string(out), "no server running") ||
			strings.Contains(string(out), "can't find session") {
			return nil
		}
		return fmt.Errorf("tmux kill-session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// diffOutput returns the text in next that is not a prefix of prev.
// Used to produce incremental deltas from capture-pane polling.
func diffOutput(prev, next string) string {
	if strings.HasPrefix(next, prev) {
		return next[len(prev):]
	}
	// Full refresh (pane scrolled or cleared).
	return next
}

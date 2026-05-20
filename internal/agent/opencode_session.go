// Package agent provides shared types and utilities for Gas Town agents.
package agent

// opencode_session.go — OpenCode rail wrapper for the dual-SDK runtime router (M2).
//
// Implements the canonical Session interface (defined by M1 in session.go).
// This file is M2-exclusive and adds OpenCode support to the New() factory.
//
// Token-bloat firewall (ADR §2.4): no gastown gt adapter tools are registered
// as MCP tools here. OpenCode invokes gt via its native bash/shell tool surface.
// This file contains zero MCP tool registry calls.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// openCodeBinary is the default path to the OpenCode binary.
// Confirmed installed at v1.14.48 per M2 task brief.
const openCodeBinary = "/Users/ryanl/.opencode/bin/opencode"

// openCodeGracefulShutdownTimeout is how long to wait for OpenCode to exit cleanly
// after SIGTERM before escalating to SIGKILL.
const openCodeGracefulShutdownTimeout = 10 * time.Second

// openCodeSession implements the canonical Session interface on the OpenCode rail.
//
// Lifecycle:
//
//	Start(ctx, opts) → spawns opencode TUI in a detached tmux session (viewer pane).
//	Send(ctx, msg)   → delivers a prompt via `opencode run --continue <msg>`.
//	Stream(ctx)      → returns a channel that emits EventDone immediately (M2 stub;
//	                   full NDJSON streaming is TODO M2.5).
//	Stop(ctx, force) → SIGTERM → graceful wait → SIGKILL on the tmux session.
//
// The struct is safe to use from a single goroutine; polecat sessions are
// single-threaded by design.
type openCodeSession struct {
	// rail is always RailOpenCode; set at construction and never mutated.
	rail Rail

	// opts holds the StartOptions passed at construction time (merged with Start opts).
	opts StartOptions

	// binary is the resolved path to the opencode executable.
	// Defaults to openCodeBinary; can be overridden in tests.
	binary string

	// tmuxSession is the tmux session name created by Start.
	tmuxSession string

	// started is 1 after Start succeeds.
	started atomic.Int32

	// stopped is 1 after Stop has been called.
	stopped atomic.Int32

	// mu guards mutable fields (summary, turns, tmuxSession, opts).
	mu sync.Mutex

	// summary is populated by Stop.
	summary *Summary

	// turns counts Send calls since Start.
	turns int
}

// Compile-time assertion: openCodeSession must implement Session.
var _ Session = (*openCodeSession)(nil)

// newOpenCodeSession constructs an openCodeSession for the given StartOptions.
// binary defaults to openCodeBinary when empty (allows test injection).
func newOpenCodeSession(opts StartOptions, binary string) *openCodeSession {
	if binary == "" {
		binary = openCodeBinary
	}
	return &openCodeSession{
		rail:   RailOpenCode,
		opts:   opts,
		binary: binary,
	}
}

// Rail returns RailOpenCode. Immutable for the lifetime of this session.
func (s *openCodeSession) Rail() Rail {
	return s.rail
}

// Start launches the OpenCode TUI inside a detached tmux session.
//
// The tmux session name is derived as "oc-<polecatName>" to namespace OpenCode
// sessions distinctly from Claude sessions ("gt-<rig>-<name>") in the operator's
// tmux list. Operators can attach with `tmux attach -t oc-<polecatName>`.
//
// OpenCode reads its provider credentials and model config from .opencode/
// relative to WorkDir (matching AgentOpenCode preset in config/agents.go:374).
//
// Start is not idempotent: calling it twice returns ErrSessionAlreadyStarted.
func (s *openCodeSession) Start(ctx context.Context, opts StartOptions) (SessionID, error) {
	if s.started.Swap(1) == 1 {
		return "", ErrSessionAlreadyStarted
	}

	// Merge caller opts into construction opts (caller wins on non-zero fields).
	merged := s.mergeOpts(opts)

	if merged.WorkDir == "" {
		s.started.Store(0)
		return "", fmt.Errorf("openCodeSession.Start: WorkDir is required")
	}

	tmuxName := s.deriveSessionName(merged)

	if err := s.spawnTmuxSession(ctx, tmuxName, merged); err != nil {
		s.started.Store(0)
		return "", fmt.Errorf("openCodeSession.Start: spawn tmux session %q: %w", tmuxName, err)
	}

	s.mu.Lock()
	s.tmuxSession = tmuxName
	s.opts = merged
	s.mu.Unlock()

	return SessionID(tmuxName), nil
}

// Send delivers a prompt to OpenCode using `opencode run --continue <msg>`.
//
// `opencode run` is the non-interactive subcommand (ADR §4.1, NonInteractive.Subcommand).
// The --continue flag resumes the last session created by Start, preserving context
// across multiple Send calls (OpenCode stores session state under .opencode/).
//
// Send blocks until opencode run exits. For M2, output is streamed to os.Stdout/Stderr
// so operators can see progress in the terminal. Use Stream() to receive structured
// events (currently stubbed — see Stream docs).
//
// TODO: M2.5 — add --format json and parse NDJSON events for cost telemetry
// (ResultSummary.TotalCostUSD) and turn-level streaming.
func (s *openCodeSession) Send(ctx context.Context, msg string) error {
	if s.started.Load() == 0 {
		return ErrSessionNotStarted
	}
	if strings.TrimSpace(msg) == "" {
		return fmt.Errorf("openCodeSession.Send: msg must not be empty")
	}

	s.mu.Lock()
	workDir := s.opts.WorkDir
	envMap := s.opts.Env
	model := ""
	// Extract model from env if present (no dedicated StartOptions field for model
	// in the canonical StartOptions; callers may pass via Env["OPENCODE_MODEL"]).
	if m, ok := envMap["OPENCODE_MODEL"]; ok {
		model = m
	}
	s.turns++
	s.mu.Unlock()

	// Build `opencode run --continue [--model X] <message..>` args.
	// --continue resumes the last session without an interactive picker.
	args := []string{"run", "--continue"}
	if model != "" {
		args = append(args, "--model", model)
	}
	// Append message words as positional args. OpenCode's CLI accepts
	// `run [message..]` where message is an array of words.
	// TODO: M2.5 — use stdin pipe for prompts with shell metacharacters.
	args = append(args, strings.Fields(msg)...)

	cmd := exec.CommandContext(ctx, s.binary, args...) //nolint:gosec // G204: binary validated at construction
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Inherit environment; add OpenCode permission bypass (agents.go:358-360)
	// and any session-specific overrides.
	cmd.Env = append(os.Environ(), `OPENCODE_PERMISSION={"*":"allow"}`)
	for k, v := range envMap {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openCodeSession.Send: opencode run: %w", err)
	}
	return nil
}

// Stream returns a channel of Events for the current turn.
//
// For M2, the implementation immediately emits EventDone and closes the channel.
// This is a safe stub: callers that wait for EventDone will proceed immediately,
// and callers that expect EventText or EventToolUse events during the turn should
// either poll tmux capture-pane (as claudeSession does) or wait for M2.5.
//
// TODO: M2.5 — implement full NDJSON event streaming via `opencode run --format json`,
// emitting EventText, EventToolUse, and EventDone from the parsed event stream.
func (s *openCodeSession) Stream(ctx context.Context) (<-chan Event, error) {
	if s.started.Load() == 0 {
		return nil, ErrSessionNotStarted
	}

	ch := make(chan Event, 1)
	// Immediately signal done; real streaming is M2.5.
	ch <- Event{Kind: EventDone}
	close(ch)
	return ch, nil
}

// Stop terminates the OpenCode TUI session.
//
// Termination sequence mirrors claudeSession.Stop and session_manager.go Stop():
//  1. Send Ctrl-C to the tmux pane (graceful signal) unless force=true.
//  2. Wait up to openCodeGracefulShutdownTimeout for clean exit.
//  3. Kill the tmux session's process group (SIGKILL) and the session itself.
//
// Stop is idempotent: calling it on an already-stopped session is a no-op.
func (s *openCodeSession) Stop(ctx context.Context, force bool) error {
	if s.stopped.Swap(1) == 1 {
		return nil // already stopped
	}

	s.mu.Lock()
	tmuxName := s.tmuxSession
	turns := s.turns
	hooks := s.opts.Hooks
	s.mu.Unlock()

	if tmuxName == "" {
		// Start was never called or failed.
		s.mu.Lock()
		s.summary = &Summary{Rail: RailOpenCode, Status: "not-started"}
		s.mu.Unlock()
		return nil
	}

	var stopErr error

	if !force {
		// Graceful: send Ctrl-C and wait for the session to exit.
		_ = tmuxSendCtrlC(tmuxName) // best-effort; ignore errors

		deadline := time.Now().Add(openCodeGracefulShutdownTimeout)
		for time.Now().Before(deadline) {
			if !tmuxSessionExists(tmuxName) {
				goto buildSummary
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	// Forceful: kill the pane's process group then the tmux session.
	_ = openCodeKillProcesses(tmuxName)
	stopErr = tmuxKillSession(tmuxName)

buildSummary:
	summary := &Summary{
		Rail:   RailOpenCode,
		Turns:  turns,
		Status: "stopped",
	}
	s.mu.Lock()
	s.summary = summary
	s.mu.Unlock()

	if hooks.OnStop != nil {
		hooks.OnStop(summary)
	}

	return stopErr
}

// ResultSummary returns the post-session summary populated by Stop.
// Returns nil if Stop has not been called yet.
func (s *openCodeSession) ResultSummary() *Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summary
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// mergeOpts returns a copy of s.opts overlaid with any non-zero fields in caller.
// Construction opts are the defaults; Start opts are the override.
func (s *openCodeSession) mergeOpts(caller StartOptions) StartOptions {
	s.mu.Lock()
	base := s.opts
	s.mu.Unlock()

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
	// ExtendedThinking is silently ignored on the OpenCode rail (ADR §4.1).
	if len(caller.Env) > 0 {
		if base.Env == nil {
			base.Env = make(map[string]string, len(caller.Env))
		}
		for k, v := range caller.Env {
			base.Env[k] = v
		}
	}
	if caller.Hooks.OnEvent != nil {
		base.Hooks.OnEvent = caller.Hooks.OnEvent
	}
	if caller.Hooks.OnStop != nil {
		base.Hooks.OnStop = caller.Hooks.OnStop
	}
	return base
}

// deriveSessionName returns a tmux session name for this OpenCode session.
// Format "oc-<polecatName>" namespaces OpenCode sessions distinctly from
// Claude sessions ("gt-<rig>-<name>") in the operator's tmux list.
func (s *openCodeSession) deriveSessionName(opts StartOptions) string {
	name := opts.PolecatName
	if name == "" {
		name = "default"
	}
	return "oc-" + name
}

// spawnTmuxSession creates a detached tmux session running the opencode TUI.
// Passes env vars via -e flags so the session and its subprocesses inherit them
// (same approach as session_manager.go:503).
func (s *openCodeSession) spawnTmuxSession(ctx context.Context, tmuxName string, opts StartOptions) error {
	// Build the binary command string.
	cmdStr := s.binary

	// Build tmux new-session arguments.
	tmuxArgs := []string{
		"new-session", "-d",
		"-s", tmuxName,
		"-c", opts.WorkDir,
	}
	// OpenCode permission bypass (AgentOpenCode preset: OPENCODE_PERMISSION={"*":"allow"}).
	tmuxArgs = append(tmuxArgs, "-e", `OPENCODE_PERMISSION={"*":"allow"}`)
	// Inject caller-supplied env.
	for k, v := range opts.Env {
		tmuxArgs = append(tmuxArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	tmuxArgs = append(tmuxArgs, cmdStr)

	cmd := exec.CommandContext(ctx, "tmux", tmuxArgs...) //nolint:gosec // G204: tmux is trusted infra
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// tmuxSendCtrlC sends Ctrl-C to the active pane of a tmux session.
func tmuxSendCtrlC(tmuxSession string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", tmuxSession, "C-c", "") //nolint:gosec
	return cmd.Run()
}

// tmuxSessionExists returns true if the named tmux session is currently alive.
func tmuxSessionExists(tmuxSession string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", tmuxSession) //nolint:gosec
	return cmd.Run() == nil
}

// openCodeKillProcesses sends SIGKILL to the process group rooted at the tmux
// pane's PID. This is the OpenCode-rail equivalent of tmux.KillSessionWithProcesses.
//
// Named openCodeKillProcesses to avoid collision with tmux package helpers.
// TODO: M2.5 — replace with tmux.Tmux.KillSessionWithProcesses() in the M3
// wiring wave when the orchestrator passes a *tmux.Tmux instance.
func openCodeKillProcesses(tmuxSession string) error {
	pidCmd := exec.Command("tmux", "display-message", "-p", "-t", tmuxSession, "#{pane_pid}") //nolint:gosec
	pidOut, err := pidCmd.Output()
	if err != nil {
		return fmt.Errorf("openCodeKillProcesses: get pane_pid: %w", err)
	}

	pidStr := strings.TrimSpace(string(pidOut))
	if pidStr == "" {
		return nil
	}

	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return fmt.Errorf("openCodeKillProcesses: parse pid %q: %w", pidStr, err)
	}
	if pid <= 1 {
		return nil // safety: never kill init/systemd
	}

	// Negative PID sends SIGKILL to the entire process group (POSIX).
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		// Non-fatal: the process group may have already exited cleanly.
		_ = err
	}
	return nil
}

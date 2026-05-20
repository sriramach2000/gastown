// Package agent defines the rail-agnostic Session interface and the Config type
// used to create sessions. Rail is immutable for the lifetime of a Session — see
// ADR 2026-05-19-dual-sdk-runtime-router.md §6 failure-mode F1.
package agent

import "context"

// Rail identifies which SDK rail a session is using.
type Rail string

const (
	// RailClaude selects the Claude Agent SDK rail (extended thinking available).
	RailClaude Rail = "claude"

	// RailOpenCode selects the OpenCode CLI rail (leaner, multi-provider default).
	RailOpenCode Rail = "opencode"
)

// Hooks bundles lifecycle callbacks that callers can attach to a session.
// Fields are optional; nil values are silently skipped.
type Hooks struct {
	// OnEvent is called for each event emitted during a turn.
	OnEvent func(Event)

	// OnStop is called when the session is stopped (before Stop returns).
	OnStop func(summary *Summary)
}

// StartOptions carries all per-spawn parameters. It is passed to Session.Start.
type StartOptions struct {
	// PolecatName is the agent name for telemetry and beacon addressing.
	PolecatName string

	// Rig is the rig name (used for environment wiring and beacon routing).
	Rig string

	// WorkDir is the git worktree directory the agent should start in.
	WorkDir string

	// Beacon is the initial prompt delivered to the agent at spawn time.
	Beacon string

	// Env contains extra environment variables to inject into the session.
	Env map[string]string

	// BeadID is the bead being worked on; passed to the classifier for rail selection.
	BeadID string

	// BeadSeverity is the triage severity of the bead (e.g. "P1", "P2").
	BeadSeverity string

	// BeadLabels are the taxonomy labels on the bead (e.g. "architecture", "security").
	BeadLabels []string

	// Hooks are the lifecycle callbacks for this session (all optional).
	Hooks Hooks

	// MaxTurns caps the number of agent turns (0 = use rail default).
	MaxTurns int

	// MaxBudgetUSD caps the USD cost per session (0 = no cap).
	MaxBudgetUSD float64

	// ExtendedThinking requests extended thinking mode.
	// Only honoured on RailClaude; silently ignored on RailOpenCode.
	ExtendedThinking bool
}

// SessionID is an opaque identifier assigned to a session at Start time.
type SessionID string

// EventKind classifies an Event.
type EventKind string

const (
	// EventText is a text-output chunk from the agent.
	EventText EventKind = "text"

	// EventToolUse signals that the agent is invoking a tool.
	EventToolUse EventKind = "tool_use"

	// EventError signals a non-fatal error from the agent stream.
	EventError EventKind = "error"

	// EventDone signals that the agent has finished its turn.
	EventDone EventKind = "done"
)

// Event is a single item emitted by the agent during a turn.
type Event struct {
	// Kind classifies the event.
	Kind EventKind

	// Text is the text payload for EventText events.
	Text string

	// ToolName is the tool being invoked for EventToolUse events.
	ToolName string

	// Err carries the error payload for EventError events.
	Err error
}

// Summary holds post-session telemetry populated when Stop is called.
type Summary struct {
	// Rail is the rail that served this session.
	Rail Rail

	// Turns is the number of agent turns completed.
	Turns int

	// TotalCostUSD is the total cost of the session in USD (may be 0 if not tracked).
	TotalCostUSD float64

	// Status is a human-readable outcome string (e.g. "done", "deferred", "error").
	Status string
}

// Session is the rail-agnostic interface for a single polecat agent session.
//
// Invariants:
//   - Rail() returns the same value for the entire lifetime of the session.
//   - There is no Switch() method. Rail selection is fixed at New() time
//     to prevent mid-session context corruption (ADR §6, failure-mode F1).
//   - Calling Start on an already-started session must return an error.
//   - Calling Send/Stream before Start must return an error.
//   - Stop is idempotent; calling it on an already-stopped session is a no-op.
type Session interface {
	// Start initialises the session and blocks until the agent is ready to
	// receive prompts, or returns an error.
	Start(ctx context.Context, opts StartOptions) (SessionID, error)

	// Send delivers a single prompt to the agent. Send does not block for the
	// agent to respond; use Stream to consume the response.
	Send(ctx context.Context, msg string) error

	// Stream returns a channel of Events for the current turn. The channel is
	// closed when the agent signals EventDone or when the context is cancelled.
	// A new channel is returned for each call; callers must drain the previous
	// channel before calling Stream again.
	Stream(ctx context.Context) (<-chan Event, error)

	// Stop terminates the session. If force is false, a graceful shutdown is
	// attempted first (e.g. sending an interrupt) before forcibly killing the
	// underlying process.
	Stop(ctx context.Context, force bool) error

	// Rail returns the rail identifier for this session. Immutable.
	Rail() Rail

	// ResultSummary returns the session summary populated by Stop.
	// Returns nil if Stop has not been called yet.
	ResultSummary() *Summary
}

// New constructs a Session for the given rail. The rail value is written into
// the session at construction time and cannot be changed afterwards.
//
// Returns ErrUnknownRail if rail is not RailClaude or RailOpenCode.
// Returns ErrRailNotImplemented for RailOpenCode until M2 lands.
func New(rail Rail, opts StartOptions) (Session, error) {
	switch rail {
	case RailClaude:
		return newClaudeSession(opts), nil
	case RailOpenCode:
		return newOpenCodeSession(opts, ""), nil
	default:
		return nil, ErrUnknownRail
	}
}

// Sentinel errors returned by New and session methods.
var (
	// ErrUnknownRail is returned when an unrecognised rail string is provided.
	ErrUnknownRail = sessionError("unknown rail")

	// ErrRailNotImplemented is returned for rails that are wired but not yet
	// implemented (e.g. RailOpenCode before M2 lands).
	ErrRailNotImplemented = sessionError("rail not implemented")

	// ErrSessionNotStarted is returned when Send or Stream is called before Start.
	ErrSessionNotStarted = sessionError("session not started")

	// ErrSessionAlreadyStarted is returned when Start is called more than once.
	ErrSessionAlreadyStarted = sessionError("session already started")
)

// sessionError is a string-typed sentinel error.
type sessionError string

func (e sessionError) Error() string { return string(e) }

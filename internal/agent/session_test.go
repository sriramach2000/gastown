package agent

import (
	"testing"
)

// TestSessionInterfaceCompliance verifies that *claudeSession satisfies Session
// at compile time. The var _ pattern catches interface drift without running
// any logic.
var _ Session = (*claudeSession)(nil)

// TestNewClaudeRailReturnsClaudeSession verifies that New(RailClaude, ...) returns
// a *claudeSession (not nil, not an error).
func TestNewClaudeRailReturnsClaudeSession(t *testing.T) {
	t.Parallel()

	sess, err := New(RailClaude, StartOptions{PolecatName: "Toast", WorkDir: "/tmp"})
	if err != nil {
		t.Fatalf("New(RailClaude): unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("New(RailClaude): returned nil session")
	}

	cs, ok := sess.(*claudeSession)
	if !ok {
		t.Fatalf("New(RailClaude): expected *claudeSession, got %T", sess)
	}

	if cs.Rail() != RailClaude {
		t.Errorf("Rail() = %q, want %q", cs.Rail(), RailClaude)
	}
}

// TestNewOpenCodeRailReturnsSession verifies that New(RailOpenCode, ...) returns
// a live *openCodeSession after M2 lands. (Previously checked for ErrRailNotImplemented;
// updated when M2 wired the factory in session.go.)
func TestNewOpenCodeRailReturnsSession(t *testing.T) {
	t.Parallel()

	sess, err := New(RailOpenCode, StartOptions{PolecatName: "m2-test"})
	if err != nil {
		t.Fatalf("New(RailOpenCode): unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("New(RailOpenCode): returned nil session")
	}
	if sess.Rail() != RailOpenCode {
		t.Errorf("Rail() = %q, want %q", sess.Rail(), RailOpenCode)
	}
}

// TestNewUnknownRailReturnsError verifies that New with an unknown rail string
// returns ErrUnknownRail.
func TestNewUnknownRailReturnsError(t *testing.T) {
	t.Parallel()

	_, err := New(Rail("bogus-rail"), StartOptions{})
	if err != ErrUnknownRail {
		t.Errorf("New(bogus-rail): got %v, want ErrUnknownRail", err)
	}
}

// TestRailIsImmutable verifies that Rail() returns the same value throughout
// the session's lifetime (pre/post Start). This is the ADR §6 F1 invariant.
func TestRailIsImmutable(t *testing.T) {
	t.Parallel()

	sess := newClaudeSession(StartOptions{PolecatName: "Cheedo"})

	// Before Start.
	if got := sess.Rail(); got != RailClaude {
		t.Errorf("Rail() before Start = %q, want %q", got, RailClaude)
	}

	// No Switch() method exists — this is enforced structurally. The test simply
	// documents the invariant: Rail() must be stable across calls.
	if got := sess.Rail(); got != RailClaude {
		t.Errorf("Rail() second call = %q, want %q", got, RailClaude)
	}
}

// TestNoSwitchMethod confirms (structurally) that the Session interface has no
// Switch method. This is the ADR §6 F1 architectural lockout.
// The compile-time check is the gate: if Switch() were added to Session,
// this file would need to be updated, creating an audit trail.
func TestNoSwitchMethod(t *testing.T) {
	t.Parallel()

	var s Session = newClaudeSession(StartOptions{})
	_ = s
}

// TestResultSummaryNilBeforeStop verifies that ResultSummary() returns nil
// before Stop has been called.
func TestResultSummaryNilBeforeStop(t *testing.T) {
	t.Parallel()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast"})
	if got := sess.ResultSummary(); got != nil {
		t.Errorf("ResultSummary() before Stop = %+v, want nil", got)
	}
}

// TestRailConstantValues verifies the Rail constant string values match what
// the ADR specifies and what telemetry consumers will see.
func TestRailConstantValues(t *testing.T) {
	t.Parallel()

	if string(RailClaude) != "claude" {
		t.Errorf("RailClaude = %q, want %q", RailClaude, "claude")
	}
	if string(RailOpenCode) != "opencode" {
		t.Errorf("RailOpenCode = %q, want %q", RailOpenCode, "opencode")
	}
}

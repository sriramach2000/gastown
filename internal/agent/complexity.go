package agent

import (
	"fmt"
	"strings"
)

// Bead is the minimal slice of bead (issue) data the router needs.
// Callers convert from their native bead representation before passing here.
type Bead struct {
	// ID is the unique identifier for the bead (e.g. "pp-bzs").
	ID string

	// Labels are the taxonomy labels attached to the bead (e.g. "architecture", "p1").
	Labels []string

	// Severity is the triage severity string: "CRITICAL", "P0", "P1", "P2", "P3", etc.
	Severity string

	// DeferredCount is how many times this bead has been marked DEFERRED.
	DeferredCount int

	// EstLOC is the estimated lines-of-code change. 0 means unknown.
	EstLOC int

	// PolecatRailRequest is set to "claude" when a polecat has self-requested the
	// Claude rail via `gt rail-request claude`. Empty otherwise.
	// TODO: M5 wire gt rail-request
	PolecatRailRequest string
}

// Complexity describes the routing decision made for a bead.
// The Rule field identifies which of the 7 classifier rules matched first.
type Complexity struct {
	// Rail is the selected rail.
	Rail Rail

	// Rule is the 1-based rule number that matched (1..7).
	Rule int

	// Reason is a human-readable explanation of why this rule matched.
	Reason string
}

// highSeverities is the set of severity strings that trigger Rule 2.
var highSeverities = map[string]bool{
	"CRITICAL": true,
	"P0":       true,
	"P1":       true,
}

// claudeLabels is the set of labels that trigger Rule 5.
var claudeLabels = map[string]bool{
	"architecture": true,
	"security":     true,
	"audit":        true,
}

// thinkingLabels is the set of labels that trigger Rule 1.
var thinkingLabels = map[string]bool{
	"needs-thinking":    true,
	"extended-thinking": true,
}

// hasLabel returns the first matching label and true if any element of labels
// is in the targets set. Returns "", false if none match.
func hasLabel(labels []string, targets map[string]bool) (string, bool) {
	for _, l := range labels {
		if targets[l] {
			return l, true
		}
	}
	return "", false
}

// Classify applies the 7 ADR §5.2 rules in order and returns the first match.
// Rules are evaluated in order; the first-matching rule determines the rail.
// Rule 7 (default OpenCode) always matches if no earlier rule fires.
func Classify(b Bead) Complexity {
	// Rule 1: explicit operator opt-in via label.
	if label, ok := hasLabel(b.Labels, thinkingLabels); ok {
		return Complexity{
			Rail:   RailClaude,
			Rule:   1,
			Reason: fmt.Sprintf("label %q requests extended thinking", label),
		}
	}

	// Rule 2: high-severity bead (CRITICAL, P0, P1).
	if highSeverities[strings.ToUpper(b.Severity)] {
		return Complexity{
			Rail:   RailClaude,
			Rule:   2,
			Reason: fmt.Sprintf("severity %q triggers Claude rail (CRITICAL/P0/P1 reward extended thinking)", b.Severity),
		}
	}

	// Rule 3: bead has been DEFERRED at least once — escalation.
	if b.DeferredCount >= 1 {
		return Complexity{
			Rail:   RailClaude,
			Rule:   3,
			Reason: fmt.Sprintf("bead deferred %d time(s); escalating to Claude rail", b.DeferredCount),
		}
	}

	// Rule 4: large estimated change (>200 LOC).
	if b.EstLOC > 200 {
		return Complexity{
			Rail:   RailClaude,
			Rule:   4,
			Reason: fmt.Sprintf("est_loc %d > 200; multi-file refactor benefits from extended thinking", b.EstLOC),
		}
	}

	// Rule 5: architectural, security, or audit label.
	if label, ok := hasLabel(b.Labels, claudeLabels); ok {
		return Complexity{
			Rail:   RailClaude,
			Rule:   5,
			Reason: fmt.Sprintf("label %q requires Claude rail (architecture/security/audit reasoning)", label),
		}
	}

	// Rule 6: polecat self-request (informational hook point; gt rail-request not yet wired).
	// TODO: M5 wire gt rail-request — when PolecatRailRequest is populated by the
	// polecat's own `gt rail-request claude` invocation during prime, this rule fires.
	if strings.EqualFold(b.PolecatRailRequest, "claude") {
		return Complexity{
			Rail:   RailClaude,
			Rule:   6,
			Reason: "polecat self-requested Claude rail via gt rail-request",
		}
	}

	// Rule 7: default — OpenCode handles the long tail of routine fixes.
	return Complexity{
		Rail:   RailOpenCode,
		Rule:   7,
		Reason: "default: routine bead routed to OpenCode rail",
	}
}

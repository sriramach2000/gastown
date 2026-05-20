package agent

import (
	"testing"
)

// TestRouteFor_Rule1_NeedsThinkingLabel verifies that the "needs-thinking" and
// "extended-thinking" labels route to the Claude rail as Rule 1.
func TestRouteFor_Rule1_NeedsThinkingLabel(t *testing.T) {
	cases := []struct {
		label string
	}{
		{"needs-thinking"},
		{"extended-thinking"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			b := Bead{ID: "test", Labels: []string{tc.label}}
			rail, rule := RouteFor(b)
			if rail != RailClaude {
				t.Errorf("label %q: got rail %q, want %q", tc.label, rail, RailClaude)
			}
			if rule != 1 {
				t.Errorf("label %q: got rule %d, want 1", tc.label, rule)
			}
		})
	}
}

// TestRouteFor_Rule2_HighSeverity verifies that CRITICAL, P0, and P1 severities
// route to the Claude rail as Rule 2.
func TestRouteFor_Rule2_HighSeverity(t *testing.T) {
	for _, sev := range []string{"CRITICAL", "P0", "P1"} {
		t.Run(sev, func(t *testing.T) {
			b := Bead{ID: "test", Severity: sev}
			rail, rule := RouteFor(b)
			if rail != RailClaude {
				t.Errorf("severity %q: got rail %q, want %q", sev, rail, RailClaude)
			}
			if rule != 2 {
				t.Errorf("severity %q: got rule %d, want 2", sev, rule)
			}
		})
	}
}

// TestRouteFor_Rule3_DeferredEscalation verifies that a bead with DeferredCount >= 1
// is escalated to the Claude rail as Rule 3.
func TestRouteFor_Rule3_DeferredEscalation(t *testing.T) {
	for _, count := range []int{1, 2, 5} {
		b := Bead{ID: "test", DeferredCount: count}
		rail, rule := RouteFor(b)
		if rail != RailClaude {
			t.Errorf("DeferredCount=%d: got rail %q, want %q", count, rail, RailClaude)
		}
		if rule != 3 {
			t.Errorf("DeferredCount=%d: got rule %d, want 3", count, rule)
		}
	}
}

// TestRouteFor_Rule4_LargeLOC verifies that EstLOC > 200 routes to Claude as Rule 4.
func TestRouteFor_Rule4_LargeLOC(t *testing.T) {
	cases := []struct {
		loc      int
		wantRule int
		wantRail Rail
	}{
		{loc: 200, wantRule: 7, wantRail: RailOpenCode}, // boundary: 200 is NOT > 200
		{loc: 201, wantRule: 4, wantRail: RailClaude},
		{loc: 500, wantRule: 4, wantRail: RailClaude},
	}
	for _, tc := range cases {
		b := Bead{ID: "test", EstLOC: tc.loc}
		rail, rule := RouteFor(b)
		if rail != tc.wantRail {
			t.Errorf("EstLOC=%d: got rail %q, want %q", tc.loc, rail, tc.wantRail)
		}
		if rule != tc.wantRule {
			t.Errorf("EstLOC=%d: got rule %d, want %d", tc.loc, rule, tc.wantRule)
		}
	}
}

// TestRouteFor_Rule5_ArchSecurityAudit verifies that "architecture", "security",
// and "audit" labels route to the Claude rail as Rule 5.
func TestRouteFor_Rule5_ArchSecurityAudit(t *testing.T) {
	for _, label := range []string{"architecture", "security", "audit"} {
		t.Run(label, func(t *testing.T) {
			b := Bead{ID: "test", Labels: []string{label}}
			rail, rule := RouteFor(b)
			if rail != RailClaude {
				t.Errorf("label %q: got rail %q, want %q", label, rail, RailClaude)
			}
			if rule != 5 {
				t.Errorf("label %q: got rule %d, want 5", label, rule)
			}
		})
	}
}

// TestRouteFor_Rule6_SelfRequest verifies that PolecatRailRequest == "claude"
// routes to the Claude rail as Rule 6 (M5 hook point).
func TestRouteFor_Rule6_SelfRequest(t *testing.T) {
	b := Bead{ID: "test", PolecatRailRequest: "claude"}
	rail, rule := RouteFor(b)
	if rail != RailClaude {
		t.Errorf("got rail %q, want %q", rail, RailClaude)
	}
	if rule != 6 {
		t.Errorf("got rule %d, want 6", rule)
	}
}

// TestRouteFor_Rule7_DefaultOpenCode verifies that a plain bead with no triggers
// routes to the OpenCode rail as Rule 7.
func TestRouteFor_Rule7_DefaultOpenCode(t *testing.T) {
	cases := []Bead{
		{ID: "plain"},
		{ID: "low-sev", Severity: "P2"},
		{ID: "low-sev-p3", Severity: "P3"},
		{ID: "small-loc", EstLOC: 50},
		{ID: "zero-deferred", DeferredCount: 0},
	}
	for _, b := range cases {
		rail, rule := RouteFor(b)
		if rail != RailOpenCode {
			t.Errorf("bead %q: got rail %q, want %q", b.ID, rail, RailOpenCode)
		}
		if rule != 7 {
			t.Errorf("bead %q: got rule %d, want 7", b.ID, rule)
		}
	}
}

// TestRouteFor_FirstMatchWins is a table-driven test verifying that when a bead
// satisfies multiple rules, only the lowest-numbered rule fires.
func TestRouteFor_FirstMatchWins(t *testing.T) {
	cases := []struct {
		name     string
		bead     Bead
		wantRail Rail
		wantRule int
	}{
		{
			// Rule 1 beats Rule 2 (thinking label + high severity)
			name: "rule1_beats_rule2",
			bead: Bead{
				ID:       "test",
				Labels:   []string{"needs-thinking"},
				Severity: "P0",
			},
			wantRail: RailClaude,
			wantRule: 1,
		},
		{
			// Rule 1 beats Rule 5 (thinking label + architecture label)
			name: "rule1_beats_rule5",
			bead: Bead{
				ID:     "test",
				Labels: []string{"needs-thinking", "architecture"},
			},
			wantRail: RailClaude,
			wantRule: 1,
		},
		{
			// Rule 2 beats Rule 3 (high severity + deferred)
			name: "rule2_beats_rule3",
			bead: Bead{
				ID:            "test",
				Severity:      "CRITICAL",
				DeferredCount: 2,
			},
			wantRail: RailClaude,
			wantRule: 2,
		},
		{
			// Rule 2 beats Rule 4 (high severity + large LOC)
			name: "rule2_beats_rule4",
			bead: Bead{
				ID:       "test",
				Severity: "P1",
				EstLOC:   500,
			},
			wantRail: RailClaude,
			wantRule: 2,
		},
		{
			// Rule 3 beats Rule 4 (deferred + large LOC)
			name: "rule3_beats_rule4",
			bead: Bead{
				ID:            "test",
				DeferredCount: 1,
				EstLOC:        300,
			},
			wantRail: RailClaude,
			wantRule: 3,
		},
		{
			// Rule 3 beats Rule 5 (deferred + security label)
			name: "rule3_beats_rule5",
			bead: Bead{
				ID:            "test",
				DeferredCount: 1,
				Labels:        []string{"security"},
			},
			wantRail: RailClaude,
			wantRule: 3,
		},
		{
			// Rule 4 beats Rule 5 (large LOC + architecture label)
			name: "rule4_beats_rule5",
			bead: Bead{
				ID:     "test",
				EstLOC: 250,
				Labels: []string{"architecture"},
			},
			wantRail: RailClaude,
			wantRule: 4,
		},
		{
			// Rule 5 beats Rule 6 (audit label + self-request)
			name: "rule5_beats_rule6",
			bead: Bead{
				ID:                 "test",
				Labels:             []string{"audit"},
				PolecatRailRequest: "claude",
			},
			wantRail: RailClaude,
			wantRule: 5,
		},
		{
			// All rules 1-6 active: Rule 1 wins
			name: "all_rules_active_rule1_wins",
			bead: Bead{
				ID:                 "test",
				Labels:             []string{"needs-thinking", "architecture", "security"},
				Severity:           "CRITICAL",
				DeferredCount:      3,
				EstLOC:             999,
				PolecatRailRequest: "claude",
			},
			wantRail: RailClaude,
			wantRule: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rail, rule := RouteFor(tc.bead)
			if rail != tc.wantRail {
				t.Errorf("got rail %q, want %q", rail, tc.wantRail)
			}
			if rule != tc.wantRule {
				t.Errorf("got rule %d, want %d", rule, tc.wantRule)
			}
		})
	}
}

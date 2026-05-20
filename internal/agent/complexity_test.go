package agent

import (
	"strings"
	"testing"
)

// TestClassify_ReasonString verifies that the Reason field is non-empty,
// human-readable, and mentions the key condition that triggered the rule.
func TestClassify_ReasonString(t *testing.T) {
	cases := []struct {
		name         string
		bead         Bead
		wantRule     int
		reasonSubstr string // a word or phrase that should appear in the reason
	}{
		{
			name:         "rule1_needs_thinking",
			bead:         Bead{Labels: []string{"needs-thinking"}},
			wantRule:     1,
			reasonSubstr: "needs-thinking",
		},
		{
			name:         "rule1_extended_thinking",
			bead:         Bead{Labels: []string{"extended-thinking"}},
			wantRule:     1,
			reasonSubstr: "extended-thinking",
		},
		{
			name:         "rule2_critical",
			bead:         Bead{Severity: "CRITICAL"},
			wantRule:     2,
			reasonSubstr: "CRITICAL",
		},
		{
			name:         "rule2_p0",
			bead:         Bead{Severity: "P0"},
			wantRule:     2,
			reasonSubstr: "P0",
		},
		{
			name:         "rule2_p1",
			bead:         Bead{Severity: "P1"},
			wantRule:     2,
			reasonSubstr: "P1",
		},
		{
			name:         "rule3_deferred",
			bead:         Bead{DeferredCount: 2},
			wantRule:     3,
			reasonSubstr: "deferred",
		},
		{
			name:         "rule4_large_loc",
			bead:         Bead{EstLOC: 300},
			wantRule:     4,
			reasonSubstr: "300",
		},
		{
			name:         "rule5_architecture",
			bead:         Bead{Labels: []string{"architecture"}},
			wantRule:     5,
			reasonSubstr: "architecture",
		},
		{
			name:         "rule5_security",
			bead:         Bead{Labels: []string{"security"}},
			wantRule:     5,
			reasonSubstr: "security",
		},
		{
			name:         "rule5_audit",
			bead:         Bead{Labels: []string{"audit"}},
			wantRule:     5,
			reasonSubstr: "audit",
		},
		{
			name:         "rule6_self_request",
			bead:         Bead{PolecatRailRequest: "claude"},
			wantRule:     6,
			reasonSubstr: "rail-request",
		},
		{
			name:         "rule7_default",
			bead:         Bead{},
			wantRule:     7,
			reasonSubstr: "default",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Classify(tc.bead)
			if c.Rule != tc.wantRule {
				t.Errorf("got rule %d, want %d", c.Rule, tc.wantRule)
			}
			if c.Reason == "" {
				t.Error("Reason is empty; must be a non-empty human-readable string")
			}
			if !strings.Contains(c.Reason, tc.reasonSubstr) {
				t.Errorf("Reason %q does not contain expected substring %q", c.Reason, tc.reasonSubstr)
			}
		})
	}
}

// TestClassify_RailMatches verifies that Classify().Rail is always consistent
// with RouteFor() for the same bead.
func TestClassify_RailMatches(t *testing.T) {
	beads := []Bead{
		{},
		{Labels: []string{"needs-thinking"}},
		{Labels: []string{"extended-thinking"}},
		{Severity: "CRITICAL"},
		{Severity: "P0"},
		{Severity: "P1"},
		{Severity: "P2"},
		{DeferredCount: 1},
		{EstLOC: 201},
		{Labels: []string{"architecture"}},
		{Labels: []string{"security"}},
		{Labels: []string{"audit"}},
		{PolecatRailRequest: "claude"},
		{Labels: []string{"needs-thinking"}, Severity: "P0", DeferredCount: 2},
	}

	for _, b := range beads {
		c := Classify(b)
		routeRail, routeRule := RouteFor(b)

		if c.Rail != routeRail {
			t.Errorf("bead %+v: Classify().Rail=%q but RouteFor()=%q", b, c.Rail, routeRail)
		}
		if c.Rule != routeRule {
			t.Errorf("bead %+v: Classify().Rule=%d but RouteFor() rule=%d", b, c.Rule, routeRule)
		}
	}
}

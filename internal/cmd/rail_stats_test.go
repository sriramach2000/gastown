package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// TestRailStats_AggregatesCorrectly verifies that AggregateRailStats correctly
// sums dispatches, costs, and identifies escalation counts per rail (ADR §5.2).
func TestRailStats_AggregatesCorrectly(t *testing.T) {
	tests := []struct {
		name              string
		entries           []beads.RailTelemetryEntry
		wantTotal         int
		wantOpenCodeCount int
		wantClaudeCount   int
		wantEscalations   int
		wantOpenCodeCost  float64
		wantClaudeCost    float64
	}{
		{
			name: "all opencode no escalations",
			entries: []beads.RailTelemetryEntry{
				{AgentID: "gt-gastown-polecat-a", Rail: "opencode", TotalCostUSD: 0.10},
				{AgentID: "gt-gastown-polecat-b", Rail: "opencode", TotalCostUSD: 0.08},
				{AgentID: "gt-gastown-polecat-c", Rail: "opencode", TotalCostUSD: 0.12},
			},
			wantTotal:         3,
			wantOpenCodeCount: 3,
			wantClaudeCount:   0,
			wantEscalations:   0,
			wantOpenCodeCost:  0.30,
			wantClaudeCost:    0.00,
		},
		{
			name: "mixed rails with escalation",
			entries: []beads.RailTelemetryEntry{
				{AgentID: "gt-gastown-polecat-a", Rail: "opencode", TotalCostUSD: 0.10},
				{AgentID: "gt-gastown-polecat-b", Rail: "opencode", TotalCostUSD: 0.08},
				{AgentID: "gt-gastown-polecat-c", Rail: "claude", TotalCostUSD: 0.50},
				// This Claude bead was escalated from opencode (DEFERRED → Claude respawn).
				{AgentID: "gt-gastown-polecat-d", Rail: "claude", TotalCostUSD: 0.42, EscalatedFrom: "opencode"},
			},
			wantTotal:         4,
			wantOpenCodeCount: 2,
			wantClaudeCount:   2,
			wantEscalations:   1,
			wantOpenCodeCost:  0.18,
			wantClaudeCost:    0.92,
		},
		{
			name:              "empty entries",
			entries:           []beads.RailTelemetryEntry{},
			wantTotal:         0,
			wantOpenCodeCount: 0,
			wantClaudeCount:   0,
			wantEscalations:   0,
			wantOpenCodeCost:  0.00,
			wantClaudeCost:    0.00,
		},
		{
			name: "pre-M5 entries with empty rail field are skipped",
			entries: []beads.RailTelemetryEntry{
				{AgentID: "gt-gastown-polecat-old", Rail: "", TotalCostUSD: 1.00},
				{AgentID: "gt-gastown-polecat-new", Rail: "opencode", TotalCostUSD: 0.05},
			},
			wantTotal:         1,
			wantOpenCodeCount: 1,
			wantClaudeCount:   0,
			wantEscalations:   0,
			wantOpenCodeCost:  0.05,
			wantClaudeCost:    0.00,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := AggregateRailStats(tc.entries)

			if out.TotalDispatches != tc.wantTotal {
				t.Errorf("TotalDispatches = %d, want %d", out.TotalDispatches, tc.wantTotal)
			}
			if out.EscalationCount != tc.wantEscalations {
				t.Errorf("EscalationCount = %d, want %d", out.EscalationCount, tc.wantEscalations)
			}

			ocCount, ocCost := railCountAndCost(out, "opencode")
			if ocCount != tc.wantOpenCodeCount {
				t.Errorf("opencode dispatches = %d, want %d", ocCount, tc.wantOpenCodeCount)
			}
			if absFloat(ocCost-tc.wantOpenCodeCost) > 1e-9 {
				t.Errorf("opencode total_cost_usd = %.4f, want %.4f", ocCost, tc.wantOpenCodeCost)
			}

			clCount, clCost := railCountAndCost(out, "claude")
			if clCount != tc.wantClaudeCount {
				t.Errorf("claude dispatches = %d, want %d", clCount, tc.wantClaudeCount)
			}
			if absFloat(clCost-tc.wantClaudeCost) > 1e-9 {
				t.Errorf("claude total_cost_usd = %.4f, want %.4f", clCost, tc.wantClaudeCost)
			}
		})
	}
}

// TestRailStats_EscalationThresholdWarning verifies that the 30% threshold
// flip between ✓ and ⚠ status works correctly (ADR §5.2).
func TestRailStats_EscalationThresholdWarning(t *testing.T) {
	tests := []struct {
		name         string
		entries      []beads.RailTelemetryEntry
		wantBreached bool
	}{
		{
			name: "exactly at 30 percent is NOT breached (threshold is strictly greater-than)",
			// 10 opencode + 3 escalations = 3/10 = 30.0% — NOT breached (> not >=)
			entries: func() []beads.RailTelemetryEntry {
				var es []beads.RailTelemetryEntry
				for i := 0; i < 10; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "opencode", TotalCostUSD: 0.05})
				}
				for i := 0; i < 3; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "claude", TotalCostUSD: 0.40, EscalatedFrom: "opencode"})
				}
				return es
			}(),
			wantBreached: false,
		},
		{
			name: "21 percent escalation rate is under threshold",
			// 29 opencode + 6 escalations = 6/29 ≈ 20.7% — not breached
			entries: func() []beads.RailTelemetryEntry {
				var es []beads.RailTelemetryEntry
				for i := 0; i < 29; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "opencode", TotalCostUSD: 0.05})
				}
				// 23 direct Claude dispatches (not escalations)
				for i := 0; i < 23; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "claude", TotalCostUSD: 0.40})
				}
				// 6 escalations from OpenCode
				for i := 0; i < 6; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "claude", TotalCostUSD: 0.40, EscalatedFrom: "opencode"})
				}
				return es
			}(),
			wantBreached: false,
		},
		{
			name: "35 percent escalation rate exceeds threshold",
			// 20 opencode + 7 escalations = 7/20 = 35% — breached
			entries: func() []beads.RailTelemetryEntry {
				var es []beads.RailTelemetryEntry
				for i := 0; i < 20; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "opencode", TotalCostUSD: 0.05})
				}
				for i := 0; i < 7; i++ {
					es = append(es, beads.RailTelemetryEntry{Rail: "claude", TotalCostUSD: 0.40, EscalatedFrom: "opencode"})
				}
				return es
			}(),
			wantBreached: true,
		},
		{
			name: "no opencode dispatches gives zero rate — not breached",
			entries: []beads.RailTelemetryEntry{
				{Rail: "claude", TotalCostUSD: 0.50},
			},
			wantBreached: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := AggregateRailStats(tc.entries)

			if out.ThresholdBreached != tc.wantBreached {
				t.Errorf("ThresholdBreached = %v, want %v (rate=%.1f%%)",
					out.ThresholdBreached, tc.wantBreached, out.EscalationRatePct)
			}

			if tc.wantBreached {
				if !strings.Contains(out.StatusMessage, "EXCEEDS") {
					t.Errorf("status message should contain EXCEEDS when breached, got %q", out.StatusMessage)
				}
			} else {
				if !strings.Contains(out.StatusMessage, "under") {
					t.Errorf("status message should contain 'under' when not breached, got %q", out.StatusMessage)
				}
			}
		})
	}
}

// railCountAndCost returns (dispatches, totalCostUSD) for the named rail in o.
func railCountAndCost(o RailStatsOutput, rail string) (int, float64) {
	for _, r := range o.Rails {
		if r.Rail == rail {
			return r.Dispatches, r.TotalCostUSD
		}
	}
	return 0, 0
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// railStatsThreshold is the escalation-rate percentage at which the classifier
// rules are considered mis-calibrated and need a §5 review (ADR §5.2).
const railStatsThreshold = 30.0

var railStatsJSON bool

var railStatsCmd = &cobra.Command{
	Use:     "rail-stats",
	GroupID: GroupDiag,
	Short:   "Aggregate dual-SDK rail usage, cost, and escalation rate",
	Long: `Show dispatch statistics from the dual-SDK runtime router (ADR §5.2).

Reads rail, rail_reason, total_cost_usd, and escalated_from fields from all
agent beads in the current rig's ledger and produces an aggregated report.

The escalation rate (OpenCode → Claude promotions as % of OpenCode dispatches)
is compared against the 30% recalibration threshold from ADR §5.2.

Examples:
  gt rail-stats           # Text report
  gt rail-stats --json    # JSON output for dashboards`,
	RunE: runRailStats,
}

func init() {
	railStatsCmd.Flags().BoolVar(&railStatsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(railStatsCmd)
}

// RailSummary holds per-rail aggregated metrics.
type RailSummary struct {
	Rail         string  `json:"rail"`
	Dispatches   int     `json:"dispatches"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	AvgCostUSD   float64 `json:"avg_cost_usd"`
	Escalations  int     `json:"escalations,omitempty"` // count where this rail was the escalated_from source
}

// RailStatsOutput is the JSON output structure for gt rail-stats.
type RailStatsOutput struct {
	TotalDispatches   int           `json:"total_dispatches"`
	Rails             []RailSummary `json:"rails"`
	EscalationCount   int           `json:"escalation_count"`
	EscalationRatePct float64       `json:"escalation_rate_pct"`
	ThresholdPct      float64       `json:"threshold_pct"`
	ThresholdBreached bool          `json:"threshold_breached"`
	StatusMessage     string        `json:"status_message"`
}

func runRailStats(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	bd := beads.New(townRoot)
	entries, err := bd.ListRailTelemetry()
	if err != nil {
		return fmt.Errorf("reading rail telemetry: %w", err)
	}

	output := AggregateRailStats(entries)

	if railStatsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	printRailStatsText(output)
	return nil
}

// AggregateRailStats computes RailStatsOutput from a slice of telemetry entries.
// Exported so rail_stats_test.go can call it directly without a live beads database.
func AggregateRailStats(entries []beads.RailTelemetryEntry) RailStatsOutput {
	type accumulator struct {
		dispatches  int
		totalCost   float64
		escalations int // times this rail was the escalated_from source
	}

	acc := map[string]*accumulator{}
	totalDispatches := 0
	escalationCount := 0

	for _, e := range entries {
		rail := e.Rail
		if rail == "" {
			continue
		}
		if acc[rail] == nil {
			acc[rail] = &accumulator{}
		}
		acc[rail].dispatches++
		acc[rail].totalCost += e.TotalCostUSD
		totalDispatches++

		// An escalation is a Claude bead with escalated_from non-empty.
		// It means the OpenCode rail tried first (DEFERRED) and the Witness
		// re-dispatched the same bead to Claude (ADR §5.1 rule #3).
		if e.EscalatedFrom != "" && e.Rail == "claude" {
			escalationCount++
			src := e.EscalatedFrom
			if acc[src] == nil {
				acc[src] = &accumulator{}
			}
			acc[src].escalations++
		}
	}

	// Build output slices in deterministic order: opencode first, claude second,
	// then any unexpected rail names for forward compatibility.
	var rails []RailSummary
	for _, name := range []string{"opencode", "claude"} {
		a := acc[name]
		if a == nil {
			continue
		}
		avg := float64(0)
		if a.dispatches > 0 {
			avg = a.totalCost / float64(a.dispatches)
		}
		rails = append(rails, RailSummary{
			Rail:         name,
			Dispatches:   a.dispatches,
			TotalCostUSD: a.totalCost,
			AvgCostUSD:   avg,
			Escalations:  a.escalations,
		})
	}
	for name, a := range acc {
		if name == "opencode" || name == "claude" {
			continue
		}
		avg := float64(0)
		if a.dispatches > 0 {
			avg = a.totalCost / float64(a.dispatches)
		}
		rails = append(rails, RailSummary{
			Rail:         name,
			Dispatches:   a.dispatches,
			TotalCostUSD: a.totalCost,
			AvgCostUSD:   avg,
			Escalations:  a.escalations,
		})
	}

	// Escalation rate: escalations as a percentage of OpenCode dispatches.
	// ADR §5.2: "count of OpenCode → Claude escalations vs OpenCode completions".
	opencodeDispatches := 0
	if a := acc["opencode"]; a != nil {
		opencodeDispatches = a.dispatches
	}

	ratePct := float64(0)
	if opencodeDispatches > 0 {
		ratePct = float64(escalationCount) / float64(opencodeDispatches) * 100.0
	}

	breached := ratePct > railStatsThreshold

	statusMsg := fmt.Sprintf("under %.0f%% threshold (per ADR §5.2)", railStatsThreshold)
	if breached {
		statusMsg = fmt.Sprintf("EXCEEDS %.0f%% threshold — classifier rules need recalibration (ADR §5.2)", railStatsThreshold)
	}

	return RailStatsOutput{
		TotalDispatches:   totalDispatches,
		Rails:             rails,
		EscalationCount:   escalationCount,
		EscalationRatePct: ratePct,
		ThresholdPct:      railStatsThreshold,
		ThresholdBreached:  breached,
		StatusMessage:     statusMsg,
	}
}

// opencodeDispatchCount returns the dispatches count for the opencode rail in o.
func opencodeDispatchCount(o RailStatsOutput) int {
	for _, r := range o.Rails {
		if r.Rail == "opencode" {
			return r.Dispatches
		}
	}
	return 0
}

func printRailStatsText(o RailStatsOutput) {
	fmt.Println("Rail Statistics (all time)")
	fmt.Println()
	fmt.Printf("  Total dispatches: %d\n", o.TotalDispatches)

	for _, r := range o.Rails {
		pct := float64(0)
		if o.TotalDispatches > 0 {
			pct = float64(r.Dispatches) / float64(o.TotalDispatches) * 100.0
		}
		label := strings.ToUpper(r.Rail[:1]) + r.Rail[1:]
		fmt.Printf("  %-10s %d (%2.0f%%) — avg $%.2f — total $%.2f\n",
			label+":", r.Dispatches, pct, r.AvgCostUSD, r.TotalCostUSD)
	}

	fmt.Println()
	oc := opencodeDispatchCount(o)
	fmt.Printf("  Escalations (OpenCode → Claude): %d", o.EscalationCount)
	if oc > 0 {
		fmt.Printf(" of %d = %.0f%% weekly", oc, o.EscalationRatePct)
	}
	fmt.Println()

	if o.ThresholdBreached {
		fmt.Printf("  Status: %s %s\n", style.Error.Render("⚠"), o.StatusMessage)
	} else {
		fmt.Printf("  Status: %s %s\n", style.Bold.Render("✓"), o.StatusMessage)
	}
}

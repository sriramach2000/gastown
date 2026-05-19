package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/deacon"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mayor"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/witness"
	"github.com/steveyegge/gastown/internal/workspace"
)

// taxonomyJSONFlag controls --json / -j output for gt taxonomy.
var taxonomyJSONFlag bool

// MayorNode is the top-level node in the fleet taxonomy.
type MayorNode struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Rig    string `json:"rig"`
}

// DeaconNode represents a deacon agent in the taxonomy.
type DeaconNode struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Supervises []string `json:"supervises"`
}

// WitnessNode represents a witness agent in the taxonomy.
type WitnessNode struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Alerts     int      `json:"alerts"`
	Supervises []string `json:"supervises"`
}

// PolecatNode represents a polecat agent in the taxonomy.
type PolecatNode struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	HookBead   string `json:"hook_bead"`
	Rig        string `json:"rig"`
	Supervisor string `json:"supervisor"`
}

// Taxonomy is the full fleet hierarchy output.
type Taxonomy struct {
	Mayor     MayorNode     `json:"mayor"`
	Deacons   []DeaconNode  `json:"deacons"`
	Witnesses []WitnessNode `json:"witnesses"`
	Polecats  []PolecatNode `json:"polecats"`
	Ts        time.Time     `json:"ts"`
}

var taxonomyCmd = &cobra.Command{
	Use:   "taxonomy",
	Short: "Print the fleet hierarchy (mayor, deacons, witnesses, polecats)",
	Long: `Print the Gas Town fleet hierarchy as a tree or JSON.

Shows the supervisory structure: Mayor → Deacon(s) → Witness(es) → Polecats.

Examples:
  gt taxonomy           # Human-readable tree
  gt taxonomy --json    # JSON (operator/dashboard use)
  gt taxonomy -j        # JSON (short flag)`,
	RunE: runTaxonomyCmd,
}

func init() {
	taxonomyCmd.Flags().BoolVarP(&taxonomyJSONFlag, "json", "j", false, "Output as JSON")
	rootCmd.AddCommand(taxonomyCmd)
}

// runTaxonomyCmd is the cobra RunE handler for gt taxonomy.
func runTaxonomyCmd(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	tax, err := BuildTaxonomy(ctx, townRoot)
	if err != nil {
		return fmt.Errorf("building taxonomy: %w", err)
	}

	if taxonomyJSONFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tax)
	}

	printTaxonomyTree(tax)
	return nil
}

// BuildTaxonomy queries the bd-backed fleet state and returns the full hierarchy.
//
// It reads:
//   - Mayor: mayor.Manager.IsRunning() for running state
//   - Deacon: deacon.Manager.IsRunning() for running state
//   - Witnesses: one per rig via witness.Manager.IsRunning()
//   - Polecats: polecat.Manager.List() per rig, cross-referenced with agent beads for hook_bead
func BuildTaxonomy(ctx context.Context, root string) (Taxonomy, error) {
	t := tmux.NewTmux()

	// ── Mayor ──────────────────────────────────────────────────────────────────
	mayorMgr := mayor.NewManager(root)
	mayorRunning, _ := mayorMgr.IsRunning()
	mayorStatus := "stopped"
	if mayorRunning {
		mayorStatus = "active"
	}
	// TODO: wire to a real town-name field from workspace.GetTownName or rig config
	//       For now we derive the mayor ID from the session name convention.
	mayorID := mayorMgr.SessionName()
	if mayorID == "" {
		mayorID = "gas-mayor"
	}

	taxMayor := MayorNode{
		ID:     mayorID,
		Status: mayorStatus,
		// TODO: wire to config.TownName once GetTownName returns a stable rig field
		Rig: "lockstep",
	}

	// ── Deacon ────────────────────────────────────────────────────────────────
	deaconMgr := deacon.NewManager(root)
	deaconRunning, _ := deaconMgr.IsRunning()
	deaconStatus := "stopped"
	if deaconRunning {
		deaconStatus = "active"
	}
	deaconID := deaconMgr.SessionName()
	if deaconID == "" {
		deaconID = "gas-deacon-001"
	}

	// Discover rigs to populate what the deacon supervises (witnesses).
	rigs, err := discoverRigsForTownRoot(root)
	if err != nil {
		// Non-fatal: return an empty but structurally valid taxonomy.
		rigs = nil
	}

	var witnessIDs []string
	for _, r := range rigs {
		if r.HasWitness {
			witnessIDs = append(witnessIDs, witnessIDForRig(r.Name))
		}
	}

	taxDeacons := []DeaconNode{
		{
			ID:         deaconID,
			Status:     deaconStatus,
			Supervises: witnessIDs,
		},
	}

	// ── Witnesses & Polecats ──────────────────────────────────────────────────
	var taxWitnesses []WitnessNode
	var taxPolecats []PolecatNode

	for _, r := range rigs {
		wID := witnessIDForRig(r.Name)

		// Witness running state
		witMgr := witness.NewManager(r)
		witRunning, _ := witMgr.IsRunning()
		witStatus := "stopped"
		if witRunning {
			witStatus = "watching"
		}

		// List polecats for this rig
		polecatGit := git.NewGit(r.Path)
		polecatMgr := polecat.NewManager(r, polecatGit, t)
		polecats, listErr := polecatMgr.List()
		if listErr != nil {
			polecats = nil
		}

		var polecatIDs []string
		for _, p := range polecats {
			pid := rigPolecatID(r.Name, p.Name)
			polecatIDs = append(polecatIDs, pid)

			hookBead := p.Issue // Issue carries the current hooked bead ID
			taxPolecats = append(taxPolecats, PolecatNode{
				ID:         pid,
				Status:     string(p.State),
				HookBead:   hookBead,
				Rig:        r.Name,
				Supervisor: wID,
			})
		}

		// Only add a witness node when the rig declares HasWitness.
		// Rigs without a witness still contribute polecats directly supervised
		// by the deacon (reflected in the deacon's Supervises list above).
		if r.HasWitness {
			taxWitnesses = append(taxWitnesses, WitnessNode{
				ID:         wID,
				Status:     witStatus,
				Alerts:     0, // TODO: wire to witness patrol alert counter once exposed
				Supervises: polecatIDs,
			})
		}
	}

	// Ensure slices are never nil (cleaner JSON output).
	if taxDeacons == nil {
		taxDeacons = []DeaconNode{}
	}
	if taxWitnesses == nil {
		taxWitnesses = []WitnessNode{}
	}
	if taxPolecats == nil {
		taxPolecats = []PolecatNode{}
	}

	return Taxonomy{
		Mayor:     taxMayor,
		Deacons:   taxDeacons,
		Witnesses: taxWitnesses,
		Polecats:  taxPolecats,
		Ts:        time.Now().UTC(),
	}, nil
}

// witnessIDForRig returns a stable witness identifier for a rig name.
func witnessIDForRig(rigName string) string {
	return fmt.Sprintf("%s-witness", rigName)
}

// rigPolecatID returns a stable polecat identifier combining rig and polecat name.
func rigPolecatID(rigName, polecatName string) string {
	return fmt.Sprintf("%s-%s", rigName, polecatName)
}

// printTaxonomyTree renders the taxonomy as an operator-friendly tree to stdout.
func printTaxonomyTree(tax Taxonomy) {
	fmt.Print(formatTaxonomyHumanReadable(tax))
}

// formatTaxonomyHumanReadable returns the taxonomy as a human-readable tree string.
// Used by printTaxonomyTree and by tests for output verification.
func formatTaxonomyHumanReadable(tax Taxonomy) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Mayor: %s (%s, rig=%s)\n", tax.Mayor.ID, tax.Mayor.Status, tax.Mayor.Rig))

	for di, d := range tax.Deacons {
		deaconIsLast := di == len(tax.Deacons)-1
		deaconPrefix := "├─"
		if deaconIsLast {
			deaconPrefix = "└─"
		}
		sb.WriteString(fmt.Sprintf("%s Deacon: %s (%s)\n", deaconPrefix, d.ID, d.Status))

		supervisedWitnesses := witnessesSupervised(tax.Witnesses, d.Supervises)
		for wi, w := range supervisedWitnesses {
			witIsLast := wi == len(supervisedWitnesses)-1
			witChildPrefix := "│  "
			if deaconIsLast {
				witChildPrefix = "   "
			}
			witPrefix := witChildPrefix + "├─"
			if witIsLast {
				witPrefix = witChildPrefix + "└─"
			}
			alertStr := ""
			if w.Alerts > 0 {
				alertStr = fmt.Sprintf(", alerts=%d", w.Alerts)
			}
			sb.WriteString(fmt.Sprintf("%s Witness: %s (%s%s)\n", witPrefix, w.ID, w.Status, alertStr))

			supervisedPolecats := polecatsSupervised(tax.Polecats, w.ID)
			for pi, p := range supervisedPolecats {
				polIsLast := pi == len(supervisedPolecats)-1
				polChildPrefix := witChildPrefix + "│  "
				if witIsLast {
					polChildPrefix = witChildPrefix + "   "
				}
				polPrefix := polChildPrefix + "├─"
				if polIsLast {
					polPrefix = polChildPrefix + "└─"
				}
				hookStr := p.HookBead
				if hookStr == "" {
					hookStr = "∅"
				}
				sb.WriteString(fmt.Sprintf("%s Polecat: %s (%s, hook=%s)\n", polPrefix, p.ID, p.Status, hookStr))
			}
		}
	}

	// Polecats not supervised by any witness (no witness on their rig).
	unsupervised := unsupervisedPolecats(tax.Polecats, tax.Witnesses)
	if len(unsupervised) > 0 {
		sb.WriteString("└─ (unsupervised polecats)\n")
		for pi, p := range unsupervised {
			isLast := pi == len(unsupervised)-1
			pfx := "   ├─"
			if isLast {
				pfx = "   └─"
			}
			hookStr := p.HookBead
			if hookStr == "" {
				hookStr = "∅"
			}
			sb.WriteString(fmt.Sprintf("%s Polecat: %s (%s, hook=%s, rig=%s)\n", pfx, p.ID, p.Status, hookStr, p.Rig))
		}
	}

	return sb.String()
}

// witnessesSupervised returns witnesses whose IDs appear in the supervises slice.
func witnessesSupervised(witnesses []WitnessNode, supervises []string) []WitnessNode {
	set := make(map[string]bool, len(supervises))
	for _, id := range supervises {
		set[id] = true
	}
	var result []WitnessNode
	for _, w := range witnesses {
		if set[w.ID] {
			result = append(result, w)
		}
	}
	return result
}

// polecatsSupervised returns polecats whose Supervisor field matches witnessID.
func polecatsSupervised(polecats []PolecatNode, witnessID string) []PolecatNode {
	var result []PolecatNode
	for _, p := range polecats {
		if p.Supervisor == witnessID {
			result = append(result, p)
		}
	}
	return result
}

// unsupervisedPolecats returns polecats whose supervisor does not appear in any WitnessNode.
func unsupervisedPolecats(polecats []PolecatNode, witnesses []WitnessNode) []PolecatNode {
	witnessIDs := make(map[string]bool, len(witnesses))
	for _, w := range witnesses {
		witnessIDs[w.ID] = true
	}
	var result []PolecatNode
	for _, p := range polecats {
		if !witnessIDs[p.Supervisor] {
			result = append(result, p)
		}
	}
	return result
}

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var catJSON bool

var catCmd = &cobra.Command{
	Use:     "cat <bead-id>",
	GroupID: GroupWork,
	Short:   "Display bead content",
	Long: `Display the content of a bead (issue, task, molecule, etc.).

This is a convenience wrapper around 'bd show' that integrates with gt.
Accepts any bead ID with a recognized prefix (gt-*, bd-*, hq-*, mol-*, etc.).

Examples:
  gt cat gt-abc123       # Show a gastown bead
  gt cat bd-abc123       # Show a beads bead
  gt cat hq-xyz789       # Show a town-level bead
  gt cat bd-abc --json   # Output as JSON`,
	Args: cobra.ExactArgs(1),
	RunE: runCat,
}

func init() {
	rootCmd.AddCommand(catCmd)
	catCmd.Flags().BoolVar(&catJSON, "json", false, "Output as JSON")
}

func runCat(cmd *cobra.Command, args []string) error {
	beadID := args[0]

	// Validate it looks like a bead ID
	if !isBeadID(beadID) {
		return fmt.Errorf("invalid bead ID %q (expected format: <prefix>-<id>, e.g. gt-abc123)", beadID)
	}

	// Build bd show command
	bdArgs := []string{"show", beadID}
	if catJSON {
		bdArgs = append(bdArgs, "--json")
	}

	bdCmd := exec.Command("bd", bdArgs...)
	bdCmd.Stdout = os.Stdout
	bdCmd.Stderr = os.Stderr
	// Route to the correct rig database via prefix resolution.
	if dir := resolveBeadDir(beadID); dir != "" && dir != "." {
		bdCmd.Dir = dir
		bdCmd.Env = filterEnvKey(os.Environ(), "BEADS_DIR")
	}

	return bdCmd.Run()
}

// isBeadID checks if a string looks like a bead ID.
// Bead IDs have the format <prefix>-<id> where prefix starts with a lowercase
// letter and may contain lowercase letters and digits afterwards
// (e.g. gt-abc123, bd-xyz, hq-cv-foo, wisp-bar, mol-baz, xl4-t1c, gp-1-foo).
//
// Rig-local prefixes registered in <town>/.beads/routes.jsonl frequently
// include digits (e.g. "xl4-", "j7-"). Rejecting those broke
// 'gt hook attach <rig-bead-id>' even though 'bd show' resolves them fine
// via the rig routing table. See operator report 2026-05-21.
func isBeadID(s string) bool {
	// Must contain a dash and start with a lowercase letter, with at least one
	// character after the dash.
	dashIdx := strings.Index(s, "-")
	if dashIdx <= 0 || dashIdx >= len(s)-1 {
		return false
	}
	// First char of prefix must be a lowercase letter (so "123-abc" still fails).
	if s[0] < 'a' || s[0] > 'z' {
		return false
	}
	// Remaining prefix chars may be lowercase letters or digits.
	for _, c := range s[1:dashIdx] {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

//go:build !wave1_skip_brain_cmd
// +build !wave1_skip_brain_cmd

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/workspace"
)

// brainCmd is the top-level `gt brain` command.
// Subcommands delegate to the internal/brain package (imported after
// swarm/wave1-brain-pkg is merged by the orchestrator).
//
// NOTE: Registration of brainCmd into rootCmd is orchestrator-only.
// The orchestrator adds `rootCmd.AddCommand(brainCmd)` in root.go
// after the octopus-merge of all wave-1 branches (see Gate-2 §Wiring wave W1).
var brainCmd = &cobra.Command{
	Use:     "brain",
	GroupID: GroupServices,
	Short:   "Manage the gbrain knowledge index",
	Long: `Manage the gbrain knowledge index for this town.

gbrain is a local knowledge retrieval sidecar (powered by Bun ≥1.3.10) that
stores polecat session summaries, skill pages, and architectural notes for
fast pre-LLM lookup.

Subcommands:
  gt brain init     # Initialise brain.yml, seed sources table, check Bun
  gt brain doctor   # Health check: index score, staleness, sidecar status
  gt brain stats    # Print brain health JSON
  gt brain search   # Semantic search (short answer surface)
  gt brain query    # Full retrieval query (top-k pages)

The nightly dream cycle (00:30 town-local) runs automatically when the daemon
is running. Trigger manually with: gbrain <town-root> dream-cycle --non-interactive`,
	RunE: requireSubcommand,
}

// brainInitFlags holds flags for `gt brain init`.
var brainInitFlags struct {
	force bool
}

// brainSearchFlags holds flags for `gt brain search`.
var brainSearchFlags struct {
	k    int
	json bool
}

// brainQueryFlags holds flags for `gt brain query`.
var brainQueryFlags struct {
	k    int
	json bool
}

var brainInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialise brain.yml and seed the sources table",
	Long: `Initialise the gbrain knowledge index for this town.

Checks that Bun ≥1.3.10 is installed, writes brain.yml to the town root
(if absent), and seeds the gbrain sources table with default entries.

Fails with an actionable error if Bun is absent or too old — no silent
fallback.`,
	RunE: runBrainInit,
}

var brainDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Health-check the brain index (score, staleness, sidecar)",
	Long: `Run a health check on the gbrain knowledge index.

Reports:
  - Bun version (pass/fail)
  - Index staleness (hours since last dream cycle)
  - Sidecar status (running / not running)
  - Overall doctor score (0–100)

If the sidecar is not running, prints:
  sidecar not running — start with: gbrain serve --http`,
	RunE: runBrainDoctor,
}

var brainStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Print brain health JSON",
	RunE:  runBrainStats,
}

var brainSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Semantic search the brain index (short answer surface)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runBrainSearch,
}

var brainQueryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Full retrieval query returning top-k pages",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runBrainQuery,
}

func init() {
	brainInitCmd.Flags().BoolVar(&brainInitFlags.force, "force", false, "Overwrite existing brain.yml")

	brainSearchCmd.Flags().IntVarP(&brainSearchFlags.k, "k", "k", 5, "Number of results to return")
	brainSearchCmd.Flags().BoolVar(&brainSearchFlags.json, "json", false, "Output as JSON")

	brainQueryCmd.Flags().IntVarP(&brainQueryFlags.k, "k", "k", 5, "Number of pages to return")
	brainQueryCmd.Flags().BoolVar(&brainQueryFlags.json, "json", false, "Output as JSON")

	brainCmd.AddCommand(brainInitCmd)
	brainCmd.AddCommand(brainDoctorCmd)
	brainCmd.AddCommand(brainStatsCmd)
	brainCmd.AddCommand(brainSearchCmd)
	brainCmd.AddCommand(brainQueryCmd)

	// Wiring wave (orchestrator): register brainCmd on rootCmd.
	// Added here in brain.go's init() rather than root.go's init() because
	// gastown's root.go does not aggregate AddCommand calls — every subcommand
	// self-registers via its own init() (see trail.go, crew.go, etc.).
	rootCmd.AddCommand(brainCmd)
}

// runBrainInit implements `gt brain init`.
// Delegates to internal/brain.Proc.Init() after the wave-1 merge.
// Pre-merge: validates workspace, then shells out to `gbrain init` directly.
func runBrainInit(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	// Post-merge this will call: brain.NewProc(townRoot).Init()
	// Pre-merge stub: delegate to shell-out helper.
	return brainShellOut(townRoot, "init")
}

// runBrainDoctor implements `gt brain doctor`.
func runBrainDoctor(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	return brainShellOut(townRoot, "doctor")
}

// runBrainStats implements `gt brain stats`.
func runBrainStats(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	return brainShellOut(townRoot, "stats")
}

// runBrainSearch implements `gt brain search <query>`.
func runBrainSearch(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	query := joinArgs(args)
	extraArgs := []string{
		fmt.Sprintf("--k=%d", brainSearchFlags.k),
	}
	if brainSearchFlags.json {
		extraArgs = append(extraArgs, "--json")
	}
	return brainShellOutArgs(townRoot, "search", query, extraArgs...)
}

// runBrainQuery implements `gt brain query <query>`.
func runBrainQuery(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	query := joinArgs(args)
	extraArgs := []string{
		fmt.Sprintf("--k=%d", brainQueryFlags.k),
	}
	if brainQueryFlags.json {
		extraArgs = append(extraArgs, "--json")
	}
	return brainShellOutArgs(townRoot, "query", query, extraArgs...)
}

// ---------- shell-out helpers -----------------------------------------------
// These helpers delegate to the `gbrain` CLI binary until internal/brain is
// merged. After the wave-1 octopus-merge, these will be replaced with direct
// calls to brain.NewProc(townRoot) and brain.NewClient(cfg).

// brainShellOut runs `gbrain <townRoot> <subcommand>` and streams output.
func brainShellOut(townRoot, subcommand string) error {
	return brainShellOutArgs(townRoot, subcommand, "")
}

// brainShellOutArgs runs `gbrain <townRoot> <subcommand> [query] [extraArgs...]`.
func brainShellOutArgs(townRoot, subcommand, query string, extraArgs ...string) error {
	argv := []string{townRoot, subcommand}
	if query != "" {
		argv = append(argv, query)
	}
	argv = append(argv, extraArgs...)

	c := exec.Command("gbrain", argv...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return fmt.Errorf("gbrain binary not found — install with: bun install -g gbrain")
		}
		return fmt.Errorf("gbrain %s: %w", subcommand, err)
	}
	return nil
}

// joinArgs joins CLI positional args into a single query string.
func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

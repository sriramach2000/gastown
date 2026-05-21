package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/cli_hub"
)

// cliHubCmd is the top-level `gt cli-hub` command.
// Registration into rootCmd is performed by the orchestrator's wiring wave
// (Step W-1: rootCmd.AddCommand(cliHubCmd) in root.go) — NOT here.
// This file only declares cliHubCmd and wires its subcommands in init().
var cliHubCmd = &cobra.Command{
	Use:     "cli-hub",
	Aliases: []string{"clihub", "ch"},
	GroupID: GroupWork,
	Short:   "Manage CLI tools via the cli-anything-hub registry",
	RunE:    requireSubcommand,
	Long: `Manage CLI tools installed into per-polecat Python virtual environments.

cli-hub integrates the HKUDS/CLI-Anything registry (64+ harness CLIs across
29 categories) into gastown's per-polecat isolation model. Each CLI is
installed as a separate cli-anything-<name> pip package inside the polecat's
.venv/ directory.

Every install and invocation is recorded to the cookie-trail (mechanical
write — not honor-system) and forwarded to the brain-deacon for brain-wiki
indexing.

Trust model: trust_registry mode (default) — anything in the registry is
approved unless explicitly on the denylist. The denylist is in
~/gt/.policy/cli_hub_allowlist.yaml.

## cli-hub × gbrain routing (MANDATORY)

If the output goes to the brain, gbrain owns the call.
If the output goes to a file, screen, or external API, cli-hub owns the call.
Chained pipelines always sequence cli-hub → gbrain, never the reverse.

Five failure modes if this rule is violated:
  see docs/design/cli_hub_integration_2026-05-21.md §Layer 4.

Examples:
  gt cli-hub list
  gt cli-hub search creative
  gt cli-hub info blender
  gt cli-hub install blender
  gt cli-hub run blender -- render scene.blend --output out.png
  gt cli-hub uninstall blender
  gt cli-hub catalog-refresh
  gt cli-hub policy show
  gt cli-hub policy deny bad-cli`,
}

// cliHubListJSON controls JSON output for the list command.
var cliHubListJSON bool

var cliHubListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all CLIs in the registry with install status",
	Long: `Show every CLI in the cli-anything-hub registry, annotated with
whether it is installed in the current polecat's .venv/.

Uses the cached catalog (refreshed automatically every hour).`,
	RunE: runCLIHubList,
}

var cliHubSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search the registry by keyword",
	Long: `Filter the catalog by keyword (case-insensitive match against
name, category, and description).

Examples:
  gt cli-hub search creative
  gt cli-hub search "web search"`,
	Args: cobra.ExactArgs(1),
	RunE: runCLIHubSearch,
}

var cliHubInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show details for a CLI",
	Long: `Show the catalog entry, package name, and install status for
a specific CLI.

Example:
  gt cli-hub info blender`,
	Args: cobra.ExactArgs(1),
	RunE: runCLIHubInfo,
}

var cliHubInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install a CLI into the current polecat venv",
	Long: `Install cli-anything-<name> into the current polecat's .venv/.

Steps:
  1. Policy check (trust_registry / denylist)
  2. Ensure .venv/ exists (creates if needed; sets CLI_HUB_NO_TELEMETRY=1)
  3. pip install cli-anything-<name>
  4. Write cookie-trail audit record
  5. Forward to brain-deacon stub

Refuses if name is on the denylist.

Example:
  gt cli-hub install blender`,
	Args: cobra.ExactArgs(1),
	RunE: runCLIHubInstall,
}

var cliHubRunCmd = &cobra.Command{
	Use:   "run <name> -- <args>",
	Short: "Invoke an installed CLI with audit",
	Long: `Invoke cli-anything-<name> with the given arguments.

stdout and stderr are forwarded directly to the terminal (Wave 1).
The --brain-page flag is a Wave 2 feature.

Every invocation writes a cookie-trail audit record (kind=cli_hub_invoke)
with the exit code and duration.

Examples:
  gt cli-hub run blender -- render scene.blend --output out.png
  gt cli-hub run exa -- search "gastown architecture"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCLIHubRun,
}

var cliHubUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a CLI from the current polecat venv",
	Long: `Remove cli-anything-<name> from the current polecat's .venv/.
Writes an audit record (verdict=UNINSTALLED).

Example:
  gt cli-hub uninstall blender`,
	Args: cobra.ExactArgs(1),
	RunE: runCLIHubUninstall,
}

var cliHubCatalogRefreshCmd = &cobra.Command{
	Use:   "catalog-refresh",
	Short: "Force-refresh the CLI registry catalog",
	Long: `Bypass the 1-hour TTL and fetch a fresh copy of the catalog
from the upstream CDN.

Normally the catalog is refreshed automatically every hour; use this
command if you need to see a newly-added CLI immediately.`,
	RunE: runCLIHubCatalogRefresh,
}

// cliHubPolicyCmd is the policy subgroup.
var cliHubPolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage the cli-hub trust registry policy",
	RunE:  requireSubcommand,
}

var cliHubPolicyShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the active trust registry YAML",
	RunE:  runCLIHubPolicyShow,
}

var cliHubPolicyDenyCmd = &cobra.Command{
	Use:   "deny <name>",
	Short: "Add a CLI to the denylist (operator-only)",
	Long: `Append <name> to the denylist in ~/gt/.policy/cli_hub_allowlist.yaml.
The CLI will be refused at install time and, if already installed, should
be uninstalled manually (or use 'gt cli-hub uninstall <name>').`,
	Args: cobra.ExactArgs(1),
	RunE: runCLIHubPolicyDeny,
}

// newEngine returns an Engine configured with the venv path for the current
// working directory. The venv path defaults to ./.venv which matches the
// per-polecat venv model (§D1 / §A1).
func newEngine() *cli_hub.Engine {
	cwd, _ := os.Getwd()
	venvPath := filepath.Join(cwd, ".venv")

	// Wire the cookie-trail function (mechanical write — closes §gap 1).
	// CLIHubAuditEvent is the type defined in internal/cmd/trail.go (HUB-COOKIE).
	// We convert from cli_hub.AuditEvent → CLIHubAuditEvent here to avoid
	// a circular import (cli_hub → cmd would be circular; cmd → cli_hub is fine).
	eng := cli_hub.NewEngine(venvPath)
	eng.Audit.CookieTrailFn = func(e cli_hub.AuditEvent) error {
		return AppendCLIHubEvent(CLIHubAuditEvent{
			Ts:             e.Ts,
			Kind:           e.Kind,
			Name:           e.Name,
			Verdict:        e.Verdict,
			Polecat:        e.Polecat,
			BeadID:         e.BeadID,
			ExitCode:       e.ExitCode,
			DurationMs:     e.DurationMs,
			EscalationLink: e.EscalationLink,
		})
	}
	return eng
}

func runCLIHubList(cmd *cobra.Command, args []string) error {
	eng := newEngine()
	results, err := eng.List()
	if err != nil {
		return err
	}

	if cliHubListJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}

	if len(results) == 0 {
		fmt.Println("No CLIs found in catalog. Run 'gt cli-hub catalog-refresh' to update.")
		return nil
	}

	fmt.Printf("%-25s %-12s %-30s %s\n", "NAME", "INSTALLED", "CATEGORY", "DESCRIPTION")
	fmt.Printf("%-25s %-12s %-30s %s\n",
		"-------------------------", "------------", "------------------------------", "-----------")
	for _, r := range results {
		installed := "no"
		if r.Installed {
			installed = "yes"
		}
		desc := r.Entry.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Printf("%-25s %-12s %-30s %s\n", r.Entry.Name, installed, r.Entry.Category, desc)
	}
	return nil
}

func runCLIHubSearch(cmd *cobra.Command, args []string) error {
	eng := newEngine()
	results, err := eng.Search(args[0])
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Printf("No CLIs matching %q found in catalog.\n", args[0])
		return nil
	}
	for _, r := range results {
		installed := ""
		if r.Installed {
			installed = " [installed]"
		}
		fmt.Printf("  %-20s %s%s\n", r.Entry.Name, r.Entry.Description, installed)
	}
	return nil
}

func runCLIHubInfo(cmd *cobra.Command, args []string) error {
	eng := newEngine()
	info, err := eng.Info(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Name:      %s\n", info.Entry.Name)
	fmt.Printf("Package:   %s\n", info.Entry.Package)
	fmt.Printf("Category:  %s\n", info.Entry.Category)
	fmt.Printf("Source:    %s\n", info.Entry.Source)
	fmt.Printf("Desc:      %s\n", info.Entry.Description)
	fmt.Printf("Installed: %v\n", info.Installed)
	if info.Installed {
		fmt.Printf("VenvPath:  %s\n", info.VenvPath)
	}
	return nil
}

func runCLIHubInstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	eng := newEngine()

	fmt.Printf("Installing %s...\n", name)
	result, err := eng.Install(name)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("Installed %s (%s) in %dms\n", result.Name, result.Package, result.DurationMs)
	fmt.Printf("Audit record written to cookie-trail.\n")
	return nil
}

func runCLIHubRun(cmd *cobra.Command, args []string) error {
	name := args[0]
	var cliArgs []string
	if len(args) > 1 {
		cliArgs = args[1:]
	}

	eng := newEngine()
	result, err := eng.Run(name, cliArgs)
	if err != nil {
		// Exit code from the CLI is already printed by the process; just return
		// the error so the cobra command exits non-zero.
		return err
	}
	_ = result
	return nil
}

func runCLIHubUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	eng := newEngine()

	fmt.Printf("Uninstalling %s...\n", name)
	if err := eng.Uninstall(name); err != nil {
		return fmt.Errorf("uninstall failed: %w", err)
	}
	fmt.Printf("Uninstalled %s.\n", name)
	return nil
}

func runCLIHubCatalogRefresh(cmd *cobra.Command, args []string) error {
	eng := newEngine()
	cat, err := eng.CatalogRefresh()
	if err != nil {
		return err
	}
	fmt.Printf("Catalog refreshed: %d entries, fetched at %s, SHA256=%s\n",
		len(cat.Entries), cat.FetchedAt.Format(time.RFC3339), cat.SHA256[:16]+"...")
	return nil
}

func runCLIHubPolicyShow(cmd *cobra.Command, args []string) error {
	eng := newEngine()
	content, err := eng.PolicyShow()
	if err != nil {
		return err
	}
	fmt.Print(content)
	return nil
}

func runCLIHubPolicyDeny(cmd *cobra.Command, args []string) error {
	name := args[0]
	eng := newEngine()
	if err := eng.PolicyDeny(name); err != nil {
		return err
	}
	fmt.Printf("Added %q to denylist. Future installs of this CLI will be refused.\n", name)
	fmt.Printf("To remove an existing install: gt cli-hub uninstall %s\n", name)
	return nil
}

func init() {
	// List flags
	cliHubListCmd.Flags().BoolVar(&cliHubListJSON, "json", false, "Output as JSON")

	// Wire policy sub-subcommands
	cliHubPolicyCmd.AddCommand(cliHubPolicyShowCmd)
	cliHubPolicyCmd.AddCommand(cliHubPolicyDenyCmd)

	// Wire top-level subcommands
	cliHubCmd.AddCommand(cliHubListCmd)
	cliHubCmd.AddCommand(cliHubSearchCmd)
	cliHubCmd.AddCommand(cliHubInfoCmd)
	cliHubCmd.AddCommand(cliHubInstallCmd)
	cliHubCmd.AddCommand(cliHubRunCmd)
	cliHubCmd.AddCommand(cliHubUninstallCmd)
	cliHubCmd.AddCommand(cliHubCatalogRefreshCmd)
	cliHubCmd.AddCommand(cliHubPolicyCmd)

	// NOTE: rootCmd.AddCommand(cliHubCmd) is intentionally NOT called here.
	// The orchestrator adds it in the wiring wave (Step W-1, root.go)
	// to avoid merge conflicts with other subcommands registered in init().
	// See Gate-2 plan §7 Step W-1.
}

// CLIHubAuditEvent is the shared type for cookie-trail events.
// This is the canonical definition per HUB-COOKIE's contract
// (internal/cmd/trail.go will declare the identical struct).
// Defined here so cli_hub.go can construct it without importing trail.go
// during the parallel-agent period. At integration, the orchestrator
// removes this redeclaration and uses the type from trail.go.
//
// NOTE: When HUB-COOKIE lands and trail.go declares this type,
// the orchestrator's wiring wave must remove this block to avoid
// duplicate type declaration. The field contract is guaranteed identical
// (see Gate-2 §HUB-PKG Field Contract).
type CLIHubAuditEvent struct {
	Ts             time.Time `json:"ts"`
	Kind           string    `json:"kind"`              // "cli_hub_install" | "cli_hub_invoke"
	Name           string    `json:"name"`
	Verdict        string    `json:"verdict,omitempty"`
	Polecat        string    `json:"polecat"`
	BeadID         string    `json:"bead_id,omitempty"`
	ExitCode       int       `json:"exit_code,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
	EscalationLink string    `json:"escalation_link,omitempty"`
}

// AppendCLIHubEvent is the stub for HUB-COOKIE's trail.go function.
// When HUB-COOKIE lands, this stub is removed and the real implementation
// from trail.go is used (same signature, same effect).
// The stub writes to the cookie-trail JSONL so Wave 1 is functional
// even before HUB-COOKIE merges.
func AppendCLIHubEvent(event CLIHubAuditEvent) error {
	home := os.Getenv("HOME")
	trailPath := filepath.Join(home, ".cookie-trail.jsonl")

	f, err := os.OpenFile(trailPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("cli_hub: opening cookie trail %s: %w", trailPath, err)
	}
	defer f.Close()

	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("cli_hub: marshalling cookie trail event: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

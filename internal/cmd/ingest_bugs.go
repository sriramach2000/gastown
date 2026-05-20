package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// ingestBugsRig is the rig to ingest bugs for (e.g. "gastown", "lockstep/refinery").
var ingestBugsRig string

// ingestBugsRigRoot overrides the default GT_ROOT/<rig> inference.
var ingestBugsRigRoot string

// ingestBugsDryRun prints planned actions without creating beads or slinging.
var ingestBugsDryRun bool

// bugIngestionResult captures the outcome of a single bug file ingestion.
type bugIngestionResult struct {
	BugID      string
	FilePath   string
	BeadID     string
	Skipped    bool // already exists
	Failed     bool
	FailReason string
}

// bugFrontmatter holds the parsed YAML frontmatter of a BUG-*.md file.
// Only the fields consumed by ingest-bugs are extracted; the rest are passed
// through to the bead description verbatim.
type bugFrontmatter struct {
	ID       string
	Title    string
	Severity string
	Labels   []string
}

// reBugID matches the BUG-NNNN token in a filename or title.
var reBugID = regexp.MustCompile(`BUG-\d+`)

// severityToPriority maps the 14-field frontmatter severity to a bd priority level.
// CRITICAL→0, HIGH→1, MED→2, LOW/other→3.
func severityToPriority(severity string) int {
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case "CRITICAL":
		return 0
	case "HIGH":
		return 1
	case "MED":
		return 2
	default:
		return 3
	}
}

// parseFrontmatter extracts the YAML frontmatter block from a BUG-*.md file.
// The frontmatter is delimited by `---` on its own line at the start of the file.
// Fields extracted: id, title, severity, labels (list items under `labels:`).
// Any parse error returns a partial result and a non-nil error; callers should
// use the partial result when available and include the error in their report.
func parseFrontmatter(content string) (bugFrontmatter, error) {
	var fm bugFrontmatter
	lines := strings.Split(content, "\n")

	inFrontmatter := false
	fmLines := []string{}
	fmCount := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			fmCount++
			if fmCount == 1 {
				inFrontmatter = true
				continue
			}
			if fmCount == 2 {
				break
			}
		}
		if inFrontmatter {
			fmLines = append(fmLines, line)
		}
	}

	if fmCount < 2 {
		return fm, fmt.Errorf("malformed frontmatter: missing closing ---")
	}

	inLabels := false
	for _, line := range fmLines {
		if strings.HasPrefix(line, "id:") {
			fm.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			inLabels = false
			continue
		}
		if strings.HasPrefix(line, "title:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "title:"))
			fm.Title = strings.Trim(raw, `"'`)
			inLabels = false
			continue
		}
		if strings.HasPrefix(line, "severity:") {
			fm.Severity = strings.TrimSpace(strings.TrimPrefix(line, "severity:"))
			inLabels = false
			continue
		}
		if strings.TrimSpace(line) == "labels:" {
			inLabels = true
			continue
		}
		if inLabels {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				label := strings.TrimPrefix(trimmed, "- ")
				fm.Labels = append(fm.Labels, label)
			} else if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				// Non-empty, non-comment, non-list-item: end of labels block
				inLabels = false
			}
		}
	}

	if fm.Title == "" {
		return fm, fmt.Errorf("frontmatter missing required field: title")
	}
	return fm, nil
}

// findPendingBugFiles returns the list of BUG-*.md files in the pending directory.
func findPendingBugFiles(bugsDir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(bugsDir, "BUG-*.md"))
	if err != nil {
		return nil, fmt.Errorf("globbing bug files in %s: %w", bugsDir, err)
	}
	return entries, nil
}

// existingBeadTitles fetches all bead titles from the rig's database.
// Returns a set of lowercase title strings for O(1) membership test.
func existingBeadTitles(workDir string) (map[string]struct{}, error) {
	out, err := BdCmd("list", "--json", "--limit=0").Dir(workDir).Output()
	if err != nil {
		return nil, fmt.Errorf("listing beads: %w", err)
	}

	var beads []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &beads); err != nil {
		return nil, fmt.Errorf("parsing bead list: %w", err)
	}

	titles := make(map[string]struct{}, len(beads))
	for _, b := range beads {
		titles[strings.ToLower(b.Title)] = struct{}{}
	}
	return titles, nil
}

// beadAlreadyExists returns true if any existing bead title contains bugID.
func beadAlreadyExists(titles map[string]struct{}, bugID string) bool {
	lower := strings.ToLower(bugID)
	for title := range titles {
		if strings.Contains(title, lower) {
			return true
		}
	}
	return false
}

// createBeadDirect calls `bd create` and returns the new bead ID.
func createBeadDirect(workDir, title string, priority int, labels []string, description string) (string, error) {
	args := []string{
		"create",
		"--title", title,
		"--priority", fmt.Sprintf("%d", priority),
		"--description", description,
		"--json",
	}
	for _, l := range labels {
		args = append(args, "--label", l)
	}

	out, err := BdCmd(args...).Dir(workDir).Output()
	if err != nil {
		return "", fmt.Errorf("bd create: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.ID == "" {
		return "", fmt.Errorf("parsing bd create output: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return result.ID, nil
}

// slingBeadDirect calls `gt sling <beadID> <rig>`.
func slingBeadDirect(townRoot, beadID, rig string) error {
	cmd := exec.Command("gt", "sling", beadID, rig)
	cmd.Dir = townRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runIngestBugs is the cobra RunE handler for `gt ingest-bugs`.
func runIngestBugs(cmd *cobra.Command, args []string) error {
	rig := ingestBugsRig
	if len(args) > 0 && rig == "" {
		rig = args[0]
	}
	if rig == "" {
		return fmt.Errorf("rig is required (use --rig or pass as positional arg)")
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding town root: %w", err)
	}

	// Resolve rig root for bug file discovery.
	rigRoot := ingestBugsRigRoot
	if rigRoot == "" {
		rigRoot = filepath.Join(townRoot, rig)
	}

	// Resolve rig work dir (for bd commands).
	rigWorkDir := filepath.Join(townRoot, rig)

	bugsDir := filepath.Join(rigRoot, "refinery", "rig", "docs", "bugs", "pending")

	fmt.Printf("%s Ingesting bugs from %s\n", style.Bold.Render("→"), bugsDir)

	// Step 1: Discover
	bugFiles, err := findPendingBugFiles(bugsDir)
	if err != nil {
		return fmt.Errorf("discovering bug files: %w", err)
	}
	if len(bugFiles) == 0 {
		fmt.Printf("%s No pending bugs found in %s\n", style.Dim.Render("○"), bugsDir)
		return nil
	}
	fmt.Printf("  Found %d pending BUG-*.md files\n", len(bugFiles))

	// Step 2: Fetch existing bead titles for dedup.
	var existingTitles map[string]struct{}
	if !ingestBugsDryRun {
		existingTitles, err = existingBeadTitles(rigWorkDir)
		if err != nil {
			fmt.Printf("%s Could not fetch existing beads for dedup: %v (proceeding without dedup)\n",
				style.Warning.Render("⚠"), err)
			existingTitles = make(map[string]struct{})
		}
	} else {
		existingTitles = make(map[string]struct{})
	}

	// Step 3: Process each bug file.
	results := make([]bugIngestionResult, 0, len(bugFiles))

	for _, filePath := range bugFiles {
		// Extract BUG-NNNN from filename.
		base := filepath.Base(filePath)
		bugID := ""
		if m := reBugID.FindString(base); m != "" {
			bugID = m
		}
		if bugID == "" {
			results = append(results, bugIngestionResult{
				FilePath:   filePath,
				Failed:     true,
				FailReason: "could not extract BUG-ID from filename",
			})
			continue
		}

		// Dedup check.
		if beadAlreadyExists(existingTitles, bugID) {
			results = append(results, bugIngestionResult{
				BugID:    bugID,
				FilePath: filePath,
				Skipped:  true,
			})
			fmt.Printf("  %s %s — already filed, skipping\n", style.Dim.Render("○"), bugID)
			continue
		}

		// Read file content.
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			results = append(results, bugIngestionResult{
				BugID:      bugID,
				FilePath:   filePath,
				Failed:     true,
				FailReason: fmt.Sprintf("read file: %v", err),
			})
			continue
		}
		content := string(contentBytes)

		// Parse frontmatter.
		fm, parseErr := parseFrontmatter(content)
		if parseErr != nil {
			fmt.Printf("  %s %s — frontmatter parse warning: %v\n",
				style.Warning.Render("⚠"), bugID, parseErr)
		}
		if fm.Title == "" {
			fm.Title = strings.TrimSuffix(base, ".md")
		}

		priority := severityToPriority(fm.Severity)

		// Build label list: frontmatter labels + ingestion markers.
		labels := make([]string, 0, len(fm.Labels)+2)
		labels = append(labels, fm.Labels...)
		labels = append(labels, "from-bug-ingestion", "bug-id:"+bugID)

		// Embed BUG-NNNN in title for future dedup.
		beadTitle := fmt.Sprintf("%s (%s)", fm.Title, bugID)

		if ingestBugsDryRun {
			fmt.Printf("  %s [DRY RUN] would create: %q  priority=%d  labels=%s\n",
				style.Bold.Render("→"), beadTitle, priority, strings.Join(labels, ","))
			fmt.Printf("             would sling → %s\n", rig)
			results = append(results, bugIngestionResult{
				BugID:    bugID,
				FilePath: filePath,
				BeadID:   "(dry-run)",
			})
			continue
		}

		// Create bead.
		beadID, createErr := createBeadDirect(rigWorkDir, beadTitle, priority, labels, content)
		if createErr != nil {
			fmt.Printf("  %s %s — create failed: %v\n", style.Warning.Render("✗"), bugID, createErr)
			results = append(results, bugIngestionResult{
				BugID:      bugID,
				FilePath:   filePath,
				Failed:     true,
				FailReason: fmt.Sprintf("bd create: %v", createErr),
			})
			continue
		}
		fmt.Printf("  %s %s → bead %s\n", style.Bold.Render("✓"), bugID, beadID)

		// Sling.
		if slingErr := slingBeadDirect(townRoot, beadID, rig); slingErr != nil {
			fmt.Printf("  %s %s — sling failed (bead %s created but not queued): %v\n",
				style.Warning.Render("⚠"), bugID, beadID, slingErr)
			results = append(results, bugIngestionResult{
				BugID:      bugID,
				FilePath:   filePath,
				BeadID:     beadID,
				Failed:     true,
				FailReason: fmt.Sprintf("gt sling: %v", slingErr),
			})
		} else {
			fmt.Printf("  %s slung %s → %s\n", style.Bold.Render("▶"), beadID, rig)
			results = append(results, bugIngestionResult{
				BugID:    bugID,
				FilePath: filePath,
				BeadID:   beadID,
			})
		}
	}

	// Step 4: Report.
	ingested, skipped, failed := 0, 0, 0
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Failed:
			failed++
		default:
			ingested++
		}
	}

	fmt.Printf("\nBug ingestion complete for rig: %s\n", rig)
	fmt.Printf("  Discovered: %d  |  Ingested: %d  |  Skipped: %d  |  Errors: %d\n",
		len(bugFiles), ingested, skipped, failed)

	if failed > 0 {
		fmt.Printf("%s %d failure(s) — send mail to deacon/ for review\n",
			style.Warning.Render("⚠"), failed)
		for _, r := range results {
			if r.Failed {
				fmt.Printf("    %s: %s\n", r.BugID, r.FailReason)
			}
		}
	}

	return nil
}

var ingestBugsCmd = &cobra.Command{
	Use:     "ingest-bugs [rig]",
	GroupID: GroupWork,
	Short:   "Ingest pending BUG-*.md tickets as bd beads and sling them onto the rig",
	Long: `Autonomous bug-ticket ingestion pipeline.

Reads BUG-*.md files from <rig-root>/refinery/rig/docs/bugs/pending/,
creates a bd bead for each new ticket (idempotent — skips already-filed bugs),
and slings each new bead onto the target rig.

This closes the manual dispatch loop: once wired into the Deacon's patrol
via mol-bug-ingestion, new bug tickets are automatically ingested and queued
for polecat assignment without operator intervention.

Severity to priority mapping:
  CRITICAL → P0   HIGH → P1   MED → P2   LOW → P3

Examples:
  gt ingest-bugs gastown
  gt ingest-bugs gastown --dry-run
  gt ingest-bugs --rig lockstep/refinery --rig-root /path/to/lockstep`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIngestBugs,
}

func init() {
	rootCmd.AddCommand(ingestBugsCmd)

	ingestBugsCmd.Flags().StringVar(&ingestBugsRig, "rig", "",
		"Rig identifier to ingest bugs for (e.g. gastown, lockstep/refinery)")
	ingestBugsCmd.Flags().StringVar(&ingestBugsRigRoot, "rig-root", "",
		"Absolute path to rig root (default: $GT_ROOT/<rig>)")
	ingestBugsCmd.Flags().BoolVar(&ingestBugsDryRun, "dry-run", false,
		"Print planned actions without creating beads or slinging")
}

// IngestBugsFromDir is the testable core of the ingest-bugs command.
// It accepts explicit paths and injectable function dependencies so tests can
// inject fakes without spawning real bd/gt processes.
//
// Parameters:
//   - bugsDir:       full path to the bugs/pending/ directory to scan
//   - rigWorkDir:    working directory for bd commands (rig root with .beads/)
//   - rig:           rig identifier string for gt sling target
//   - dryRun:        if true, count planned actions without executing
//   - listBeads:     injected implementation of existingBeadTitles (nil = real)
//   - createBeadFn:  injected bd create implementation (nil = real)
//   - slingBeadFn:   injected gt sling implementation (nil = real)
//   - townRoot:      GT_ROOT equivalent for gt sling CWD
//   - _:             reserved writer for future structured output (may be nil)
func IngestBugsFromDir(
	bugsDir string,
	rigWorkDir string,
	rig string,
	dryRun bool,
	listBeads func(workDir string) (map[string]struct{}, error),
	createBeadFn func(workDir, title string, priority int, labels []string, description string) (string, error),
	slingBeadFn func(townRoot, beadID, rig string) error,
	townRoot string,
	_ *bufio.Writer, // reserved for future structured output
) (ingested, skipped, failed int, err error) {
	if listBeads == nil {
		listBeads = existingBeadTitles
	}
	if createBeadFn == nil {
		createBeadFn = createBeadDirect
	}
	if slingBeadFn == nil {
		slingBeadFn = slingBeadDirect
	}

	bugFiles, err := findPendingBugFiles(bugsDir)
	if err != nil {
		return 0, 0, 0, err
	}

	var existingTitles map[string]struct{}
	if !dryRun {
		existingTitles, err = listBeads(rigWorkDir)
		if err != nil {
			existingTitles = make(map[string]struct{})
		}
	} else {
		existingTitles = make(map[string]struct{})
	}

	for _, filePath := range bugFiles {
		base := filepath.Base(filePath)
		bugID := ""
		if m := reBugID.FindString(base); m != "" {
			bugID = m
		}
		if bugID == "" {
			failed++
			continue
		}

		if beadAlreadyExists(existingTitles, bugID) {
			skipped++
			continue
		}

		contentBytes, readErr := os.ReadFile(filePath)
		if readErr != nil {
			failed++
			continue
		}
		content := string(contentBytes)

		fm, _ := parseFrontmatter(content)
		if fm.Title == "" {
			fm.Title = strings.TrimSuffix(base, ".md")
		}

		priority := severityToPriority(fm.Severity)
		labels := make([]string, 0, len(fm.Labels)+2)
		labels = append(labels, fm.Labels...)
		labels = append(labels, "from-bug-ingestion", "bug-id:"+bugID)
		beadTitle := fmt.Sprintf("%s (%s)", fm.Title, bugID)

		if dryRun {
			ingested++
			continue
		}

		beadID, createErr := createBeadFn(rigWorkDir, beadTitle, priority, labels, content)
		if createErr != nil {
			failed++
			continue
		}

		if slingErr := slingBeadFn(townRoot, beadID, rig); slingErr != nil {
			failed++
			continue
		}
		ingested++
	}

	return ingested, skipped, failed, nil
}

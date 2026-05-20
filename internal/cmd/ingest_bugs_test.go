package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// writeBugFile writes a BUG-*.md file with the given content into dir.
func writeBugFile(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeBugFile: %v", err)
	}
	return path
}

// fakeListBeads returns a fixed set of titles (lowercase) for dedup testing.
func fakeListBeads(titles ...string) func(string) (map[string]struct{}, error) {
	return func(_ string) (map[string]struct{}, error) {
		m := make(map[string]struct{}, len(titles))
		for _, t := range titles {
			m[strings.ToLower(t)] = struct{}{}
		}
		return m, nil
	}
}

// fakeCreateTracker records every call and returns a deterministic bead ID.
type fakeCreateTracker struct {
	created []string // bead titles in creation order
}

func (f *fakeCreateTracker) create(_, title string, _ int, _ []string, _ string) (string, error) {
	f.created = append(f.created, title)
	return fmt.Sprintf("bead-%d", len(f.created)), nil
}

// fakeSlingTracker records every sling call.
type fakeSlingTracker struct {
	slung []string // bead IDs slung
}

func (f *fakeSlingTracker) sling(_, beadID, _ string) error {
	f.slung = append(f.slung, beadID)
	return nil
}

// ── TestIngestBugs_FindsAllPendingMarkdown ────────────────────────────────────

// TestIngestBugs_FindsAllPendingMarkdown verifies that only BUG-*.md files are
// discovered and that non-matching files (README.md, CHANGELOG-*.md) are ignored.
func TestIngestBugs_FindsAllPendingMarkdown(t *testing.T) {
	bugsDir := t.TempDir()

	// Three valid BUG-*.md files.
	for i, name := range []string{
		"BUG-0001-crash-on-empty-input.md",
		"BUG-0002-nil-pointer-deref.md",
		"BUG-0003-parse-failure.md",
	} {
		writeBugFile(t, bugsDir, name, fmt.Sprintf(`---
id: BUG-%04d
title: "Test bug %d"
severity: HIGH
status: pending
labels:
  - component:core
---

Body text.
`, i+1, i+1))
	}
	// These must NOT be picked up.
	writeBugFile(t, bugsDir, "README.md", "not a bug file")
	writeBugFile(t, bugsDir, "CHANGELOG-0004.md", "also not a bug")

	createT := &fakeCreateTracker{}
	slingT := &fakeSlingTracker{}

	ingested, skipped, failed, err := IngestBugsFromDir(
		bugsDir,
		"/fake/rig",
		"gastown",
		false,
		fakeListBeads(), // no existing beads
		createT.create,
		slingT.sling,
		"/fake/town",
		nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ingested != 3 {
		t.Errorf("ingested = %d, want 3", ingested)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if len(createT.created) != 3 {
		t.Errorf("create calls = %d, want 3", len(createT.created))
	}
	if len(slingT.slung) != 3 {
		t.Errorf("sling calls = %d, want 3", len(slingT.slung))
	}
}

// ── TestIngestBugs_DedupesByExternalRef ──────────────────────────────────────

// TestIngestBugs_DedupesByExternalRef verifies that a bug whose BUG-NNNN token
// already appears in an existing bead title is skipped (idempotent).
func TestIngestBugs_DedupesByExternalRef(t *testing.T) {
	bugsDir := t.TempDir()

	writeBugFile(t, bugsDir, "BUG-0010-already-filed.md", `---
id: BUG-0010
title: "Already filed bug"
severity: MED
status: pending
labels:
  - component:db
---

Body.
`)
	writeBugFile(t, bugsDir, "BUG-0011-new-bug.md", `---
id: BUG-0011
title: "New bug"
severity: LOW
status: pending
---

New body.
`)

	createT := &fakeCreateTracker{}
	slingT := &fakeSlingTracker{}

	// BUG-0010 is already present as a bead title.
	ingested, skipped, failed, err := IngestBugsFromDir(
		bugsDir,
		"/fake/rig",
		"gastown",
		false,
		fakeListBeads("Already filed bug (BUG-0010)"),
		createT.create,
		slingT.sling,
		"/fake/town",
		nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ingested != 1 {
		t.Errorf("ingested = %d, want 1 (only BUG-0011)", ingested)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (BUG-0010 already exists)", skipped)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if len(createT.created) != 1 {
		t.Errorf("create calls = %d, want 1", len(createT.created))
	}
	if len(createT.created) > 0 && !strings.Contains(createT.created[0], "BUG-0011") {
		t.Errorf("expected BUG-0011 in created title, got %q", createT.created[0])
	}
}

// ── TestIngestBugs_MapsSeverityToPriority ────────────────────────────────────

// TestIngestBugs_MapsSeverityToPriority validates the severity→priority mapping.
func TestIngestBugs_MapsSeverityToPriority(t *testing.T) {
	cases := []struct {
		severity string
		want     int
	}{
		{"CRITICAL", 0},
		{"HIGH", 1},
		{"MED", 2},
		{"LOW", 3},
		{"", 3},         // missing → LOW equivalent
		{"UNKNOWN", 3},  // unrecognised → LOW equivalent
		{"critical", 0}, // case-insensitive
		{"high", 1},
		{"med", 2},
	}

	for _, tc := range cases {
		got := severityToPriority(tc.severity)
		if got != tc.want {
			t.Errorf("severityToPriority(%q) = %d, want %d", tc.severity, got, tc.want)
		}
	}
}

// TestIngestBugs_MapsSeverityToPriority_E2E verifies end-to-end that the correct
// priority value is forwarded to createBeadFn for each severity level.
func TestIngestBugs_MapsSeverityToPriority_E2E(t *testing.T) {
	bugsDir := t.TempDir()

	severities := []struct {
		filename string
		severity string
		wantPrio int
	}{
		{"BUG-0020-critical.md", "CRITICAL", 0},
		{"BUG-0021-high.md", "HIGH", 1},
		{"BUG-0022-med.md", "MED", 2},
		{"BUG-0023-low.md", "LOW", 3},
	}

	for _, tc := range severities {
		bugID := reBugID.FindString(tc.filename)
		writeBugFile(t, bugsDir, tc.filename, fmt.Sprintf(`---
id: %s
title: "Severity test"
severity: %s
status: pending
---

Body.
`, bugID, tc.severity))
	}

	type createCall struct {
		title    string
		priority int
	}
	var calls []createCall

	captureCreate := func(_, title string, priority int, _ []string, _ string) (string, error) {
		calls = append(calls, createCall{title, priority})
		return fmt.Sprintf("bead-%d", len(calls)), nil
	}
	slingT := &fakeSlingTracker{}

	_, _, failed, err := IngestBugsFromDir(
		bugsDir, "/fake/rig", "gastown", false,
		fakeListBeads(),
		captureCreate,
		slingT.sling,
		"/fake/town", nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if len(calls) != len(severities) {
		t.Fatalf("create calls = %d, want %d", len(calls), len(severities))
	}

	for i, tc := range severities {
		if calls[i].priority != tc.wantPrio {
			t.Errorf("bug %s: priority = %d, want %d", tc.filename, calls[i].priority, tc.wantPrio)
		}
	}
}

// ── TestIngestBugs_AutoSlingsAfterCreate ─────────────────────────────────────

// TestIngestBugs_AutoSlingsAfterCreate verifies that every created bead is
// slung onto the correct rig immediately after creation.
func TestIngestBugs_AutoSlingsAfterCreate(t *testing.T) {
	bugsDir := t.TempDir()

	writeBugFile(t, bugsDir, "BUG-0030-needs-sling.md", `---
id: BUG-0030
title: "Needs sling"
severity: HIGH
status: pending
labels:
  - component:api
---

Body.
`)

	createT := &fakeCreateTracker{}

	var slingCalls []struct{ beadID, rig string }
	slingFn := func(_, beadID, rig string) error {
		slingCalls = append(slingCalls, struct{ beadID, rig string }{beadID, rig})
		return nil
	}

	ingested, _, failed, err := IngestBugsFromDir(
		bugsDir, "/fake/rig", "gastown", false,
		fakeListBeads(),
		createT.create,
		slingFn,
		"/fake/town", nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if ingested != 1 {
		t.Errorf("ingested = %d, want 1", ingested)
	}
	if len(createT.created) != 1 {
		t.Errorf("create calls = %d, want 1", len(createT.created))
	}
	if len(slingCalls) != 1 {
		t.Fatalf("sling calls = %d, want 1", len(slingCalls))
	}
	if slingCalls[0].rig != "gastown" {
		t.Errorf("sling rig = %q, want %q", slingCalls[0].rig, "gastown")
	}
	// Verify sling receives the bead ID produced by create.
	if slingCalls[0].beadID != "bead-1" {
		t.Errorf("sling beadID = %q, want bead-1", slingCalls[0].beadID)
	}
}

// ── parseFrontmatter unit tests ───────────────────────────────────────────────

func TestParseFrontmatter_AllFields(t *testing.T) {
	content := `---
id: BUG-0099
title: "All fields present"
severity: CRITICAL
status: pending
labels:
  - component:parser
  - from-audit
  - priority:high
---

Body text here.
`
	fm, err := parseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm.ID != "BUG-0099" {
		t.Errorf("ID = %q, want BUG-0099", fm.ID)
	}
	if fm.Title != "All fields present" {
		t.Errorf("Title = %q, want 'All fields present'", fm.Title)
	}
	if fm.Severity != "CRITICAL" {
		t.Errorf("Severity = %q, want CRITICAL", fm.Severity)
	}
	if len(fm.Labels) != 3 {
		t.Errorf("Labels = %v (len %d), want 3 elements", fm.Labels, len(fm.Labels))
	}
}

func TestParseFrontmatter_MissingClosingDelimiter(t *testing.T) {
	content := `---
id: BUG-0100
title: "No closing delimiter"
severity: HIGH
`
	_, err := parseFrontmatter(content)
	if err == nil {
		t.Error("expected error for missing closing ---, got nil")
	}
}

func TestParseFrontmatter_EmptyLabels(t *testing.T) {
	content := `---
id: BUG-0101
title: "No labels"
severity: LOW
status: pending
---

Body.
`
	fm, err := parseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Labels) != 0 {
		t.Errorf("Labels = %v, want empty", fm.Labels)
	}
}

// ── dry-run test ──────────────────────────────────────────────────────────────

func TestIngestBugsFromDir_DryRun(t *testing.T) {
	bugsDir := t.TempDir()
	writeBugFile(t, bugsDir, "BUG-0040-dry.md", `---
id: BUG-0040
title: "Dry run test"
severity: MED
status: pending
---
Body.
`)

	createT := &fakeCreateTracker{}
	slingT := &fakeSlingTracker{}

	ingested, skipped, failed, err := IngestBugsFromDir(
		bugsDir, "/fake/rig", "gastown", true, // dryRun = true
		fakeListBeads(),
		createT.create,
		slingT.sling,
		"/fake/town",
		bufio.NewWriter(os.Stdout),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ingested != 1 {
		t.Errorf("ingested = %d, want 1 (dry-run counts as planned)", ingested)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	// In dry-run mode no real calls must be made.
	if len(createT.created) != 0 {
		t.Errorf("create was called in dry-run mode: %v", createT.created)
	}
	if len(slingT.slung) != 0 {
		t.Errorf("sling was called in dry-run mode: %v", slingT.slung)
	}
}

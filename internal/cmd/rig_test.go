package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/formula"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

func TestIsAgentSessionHealthy_DeadPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	tm := tmux.NewTmux()
	sessionName := "zzrig-dead-pane-test"
	_ = tm.KillSession(sessionName)
	t.Cleanup(func() { _ = tm.KillSession(sessionName) })

	for _, args := range [][]string{
		{"new-session", "-d", "-s", sessionName},
		{"set-option", "-t", sessionName, "remain-on-exit", "on"},
		{"respawn-pane", "-k", "-t", sessionName, "false"},
	} {
		if out, err := tmux.BuildCommand(args...).CombinedOutput(); err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, strings.TrimSpace(string(out)))
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	observedDead := false
	for time.Now().Before(deadline) {
		out, err := tmux.BuildCommand("display-message", "-p", "-t", sessionName, "#{pane_dead}").Output()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			observedDead = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !observedDead {
		t.Fatal("expected retained pane to report pane_dead=1")
	}

	hasSession, err := tm.HasSession(sessionName)
	if err != nil {
		t.Fatalf("HasSession: %v", err)
	}
	if !hasSession {
		t.Fatal("expected retained tmux session to exist")
	}
	if isAgentSessionHealthy(tm, sessionName) {
		t.Fatal("dead retained pane must not be reported healthy")
	}
}

func TestIsGitRemoteURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Remote URLs — should return true
		{"https://github.com/org/repo.git", true},
		{"http://github.com/org/repo.git", true},
		{"git@github.com:org/repo.git", true},
		{"ssh://git@github.com/org/repo.git", true},
		{"git://github.com/org/repo.git", true},
		{"deploy@private-host.internal:repos/app.git", true},

		// Custom git remote helper schemes — should return true
		{"s3://my-bucket/rigs/my-project", true},
		{"codecommit://my-repo", true},
		{"gs://my-bucket/repos/foo", true},

		// Local paths — should return false
		{"/Users/scott/projects/foo", false},
		{"/tmp/repo", false},
		{"./foo", false},
		{"../foo", false},
		{"~/projects/foo", false},
		{"C:\\Users\\scott\\projects\\foo", false},
		{"C:/Users/scott/projects/foo", false},

		// Bare directory name — should return false
		{"foo", false},

		// file:// URIs — explicit local git remotes are allowed
		{"file:///tmp/local-repo.git", true},
		{"file:///Users/scott/projects/foo", true},
		{"file://user@localhost:/tmp/local-repo.git", true},

		// Argument injection — should return false
		{"-oProxyCommand=evil", false},
		{"--upload-pack=touch /tmp/pwned", false},
		{"-c", false},

		// Malformed SCP-style — should return false
		{"@host:path", false},     // empty user
		{"user@:/path", false},    // empty host
		{"localhost:path", false}, // no user (not SCP-style)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isGitRemoteURL(tt.input)
			if got != tt.want {
				t.Errorf("isGitRemoteURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func setupRigTestRegistry(t *testing.T) {
	t.Helper()
	reg := session.NewPrefixRegistry()
	// Use zz-prefixed names to avoid collisions with real rig sessions
	// (e.g. "tr" collides with production rigs that use that prefix).
	reg.Register("zztr", "testrig1223")
	reg.Register("zzor", "otherrig")
	old := session.DefaultRegistry()
	session.SetDefaultRegistry(reg)
	t.Cleanup(func() { session.SetDefaultRegistry(old) })
}

func TestFindRigSessions(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	setupRigTestRegistry(t)

	tm := tmux.NewTmux()

	// Create sessions that match our test rig prefix (zztr- for testrig1223)
	matching := []string{
		"zztr-witness",
		"zztr-refinery",
		"zztr-alpha",
	}
	// Create a non-matching session (zzor- for otherrig)
	nonMatching := "zzor-witness"

	for _, name := range append(matching, nonMatching) {
		_ = tm.KillSession(name) // clean up any leftovers
		if err := tm.NewSessionWithCommand(name, "", "sleep 300"); err != nil {
			t.Fatalf("creating session %s: %v", name, err)
		}
	}
	defer func() {
		for _, name := range append(matching, nonMatching) {
			_ = tm.KillSession(name)
		}
	}()

	got, err := findRigSessions(tm, "testrig1223")
	if err != nil {
		t.Fatalf("findRigSessions: %v", err)
	}

	// Verify all matching sessions are returned
	gotSet := make(map[string]bool, len(got))
	for _, s := range got {
		gotSet[s] = true
	}

	for _, want := range matching {
		if !gotSet[want] {
			t.Errorf("expected session %q in results, got %v", want, got)
		}
	}

	// Verify non-matching session is excluded
	if gotSet[nonMatching] {
		t.Errorf("did not expect session %q in results, got %v", nonMatching, got)
	}

	// Verify count
	if len(got) != len(matching) {
		t.Errorf("expected %d sessions, got %d: %v", len(matching), len(got), got)
	}
}

func TestFindRigSessions_NoSessions(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	// Register a unique prefix for a rig that has no sessions
	reg := session.NewPrefixRegistry()
	reg.Register("zz", "nonexistentrig999")
	old := session.DefaultRegistry()
	session.SetDefaultRegistry(reg)
	defer session.SetDefaultRegistry(old)

	tm := tmux.NewTmux()
	got, err := findRigSessions(tm, "nonexistentrig999")
	if err != nil {
		t.Fatalf("findRigSessions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 sessions, got %d: %v", len(got), got)
	}
}

// TestGtRigAdd_CreatesFreshFormulasWhenGastownAbsent verifies that when a
// town has no gastown rig cloned locally, formula.ProvisionFormulas correctly
// creates .beads/formulas/ with the embedded formula files so that gt sling
// and bd formula list work without a gastown clone (gastown-dogfood-wy8).
func TestGtRigAdd_CreatesFreshFormulasWhenGastownAbsent(t *testing.T) {
	// Simulate a town root that has no .beads/formulas/ yet (no gastown rig).
	townRoot := t.TempDir()

	// Confirm .beads/formulas does NOT exist initially.
	formulasDir := filepath.Join(townRoot, ".beads", "formulas")
	if _, err := os.Stat(formulasDir); !os.IsNotExist(err) {
		t.Fatalf("precondition: formulas dir should not exist yet, got err=%v", err)
	}

	// Call the same function that runRigAdd uses to provision formulas.
	count, err := formula.ProvisionFormulas(townRoot)
	if err != nil {
		t.Fatalf("ProvisionFormulas() error: %v", err)
	}
	if count == 0 {
		t.Fatal("ProvisionFormulas() provisioned 0 formulas; expected ≥1 from embedded binary")
	}

	// .beads/formulas/ must now exist as a real directory (not a dangling symlink).
	info, err := os.Stat(formulasDir)
	if err != nil {
		t.Fatalf(".beads/formulas does not exist after provisioning: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf(".beads/formulas is not a directory (got mode %v)", info.Mode())
	}

	// At least one .formula.toml file must be present and readable.
	entries, err := os.ReadDir(formulasDir)
	if err != nil {
		t.Fatalf("reading .beads/formulas: %v", err)
	}
	var tomlCount int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".formula.toml") {
			tomlCount++
			// Verify the file is readable and non-empty.
			data, err := os.ReadFile(filepath.Join(formulasDir, e.Name()))
			if err != nil {
				t.Errorf("reading %s: %v", e.Name(), err)
			}
			if len(data) == 0 {
				t.Errorf("%s is empty", e.Name())
			}
		}
	}
	if tomlCount == 0 {
		t.Error(".beads/formulas contains no .formula.toml files after provisioning")
	}

	// Verify the embedded formula used by gt sling is present.
	molPolelcatWork := filepath.Join(formulasDir, "mol-polecat-work.formula.toml")
	if _, err := os.Stat(molPolelcatWork); os.IsNotExist(err) {
		t.Error("mol-polecat-work.formula.toml not found; gt sling would fail without gastown rig")
	}
}

// TestGtRigAdd_PreservesExistingFormulasSymlink verifies that when .beads/formulas/
// is already present (e.g. from a prior gt install or gt rig add call),
// formula.ProvisionFormulas does not overwrite user-customised formula files
// (gastown-dogfood-wy8 — belt-and-braces regression).
func TestGtRigAdd_PreservesExistingFormulasSymlink(t *testing.T) {
	townRoot := t.TempDir()

	// Pre-provision with the embedded formulas (simulates existing install or
	// first gt rig add run).
	if _, err := formula.ProvisionFormulas(townRoot); err != nil {
		t.Fatalf("first ProvisionFormulas() error: %v", err)
	}

	// Simulate a user-customised formula by writing custom content.
	formulasDir := filepath.Join(townRoot, ".beads", "formulas")
	customPath := filepath.Join(formulasDir, "mol-polecat-work.formula.toml")
	customContent := []byte("# user customisation — must NOT be overwritten\nversion = 9999\n")
	if err := os.WriteFile(customPath, customContent, 0644); err != nil {
		t.Fatalf("writing custom formula: %v", err)
	}

	// Run ProvisionFormulas a second time (simulates a second gt rig add in the
	// same town, e.g. adding a second non-gastown rig).
	_, err := formula.ProvisionFormulas(townRoot)
	if err != nil {
		t.Fatalf("second ProvisionFormulas() error: %v", err)
	}

	// The customised file must not be overwritten.
	got, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatalf("reading formula after second provisioning: %v", err)
	}
	if string(got) != string(customContent) {
		t.Errorf("user-customised formula was overwritten; got %q, want %q", string(got), string(customContent))
	}
}

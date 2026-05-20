package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rigpkg "github.com/steveyegge/gastown/internal/rig"
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

// TestRigAdd_UsesPrefixInIdentityBead is the gastown-dogfood-k45 regression test.
//
// applyRigBeadTTLOverrides (compact.go) previously hardcoded "gt" when constructing
// the rig identity bead ID, causing a spurious "no route found for prefix gt-" warning
// on every gt command for rigs whose --prefix was not "gt" (e.g. --prefix pp creates
// pp-rig-planner, but the old code looked up gt-rig-planner which had no route).
//
// The fix reads the prefix from rig/config.json (or rigs.json fallback).
// This test verifies:
//  1. rig.LoadRigConfig correctly persists the --prefix value in config.json.
//  2. applyRigBeadTTLOverrides with a rig configured as "pp" does NOT emit the
//     "no route found for prefix gt-" warning that the bug caused.
func TestRigAdd_UsesPrefixInIdentityBead(t *testing.T) {
	// Build a minimal townRoot with a rig config.json that has prefix "pp".
	townRoot := t.TempDir()
	rigName := "planner"
	rigPath := filepath.Join(townRoot, rigName)
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir rig: %v", err)
	}

	// Write a rig config.json with prefix "pp" — mirrors what gt rig add --prefix pp creates.
	cfg := rigpkg.RigConfig{
		Type:    "rig",
		Version: 1,
		Name:    rigName,
		GitURL:  "git@github.com:test/planner.git",
		Beads:   &rigpkg.BeadsConfig{Prefix: "pp"},
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal rig config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), cfgBytes, 0644); err != nil {
		t.Fatalf("write rig config: %v", err)
	}

	// Verify LoadRigConfig round-trips the "pp" prefix.
	loaded, err := rigpkg.LoadRigConfig(rigPath)
	if err != nil {
		t.Fatalf("LoadRigConfig: %v", err)
	}
	if loaded.Beads == nil || loaded.Beads.Prefix != "pp" {
		var gotPrefix string
		if loaded.Beads != nil {
			gotPrefix = loaded.Beads.Prefix
		}
		t.Fatalf("LoadRigConfig returned prefix %q, want %q", gotPrefix, "pp")
	}

	// Capture stderr to verify applyRigBeadTTLOverrides does NOT produce the
	// "no route found for prefix gt-" warning that the k45 bug caused.
	oldStderr := os.Stderr
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stderr = w

	ttls := make(map[string]time.Duration)
	// applyRigBeadTTLOverrides calls bd.Show(pp-rig-planner). bd is not available in
	// unit tests, so Show returns an error and the function returns early without
	// modifying ttls. The important assertion: no "gt-" routing warning is emitted.
	applyRigBeadTTLOverrides(ttls, townRoot, rigName)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	captured := buf.String()

	// Pre-fix: Warning: no route found for prefix "gt-" (bead gt-rig-planner)
	// Post-fix: looks up pp-rig-planner instead; absent beads DB is silent.
	if strings.Contains(captured, `prefix "gt-"`) {
		t.Errorf("k45 regression: applyRigBeadTTLOverrides emitted gt- routing warning for prefix-pp rig:\n%s", captured)
	}
	if strings.Contains(captured, "gt-rig-planner") {
		t.Errorf("k45 regression: applyRigBeadTTLOverrides looked up gt-rig-planner; should look up pp-rig-planner:\n%s", captured)
	}
}

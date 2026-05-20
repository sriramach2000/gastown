package agent

// opencode_session_test.go — unit tests for the OpenCode rail wrapper (M2).
//
// These tests verify the Session interface contract and the session lifecycle
// without requiring a real tmux server or opencode binary. The subprocess-
// dependent behaviour (Start/Stop) is tested with a stub tmux binary injected
// via PATH manipulation.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── TestOpenCodeSession_Rail ────────────────────────────────────────────────
//
// Verifies Rail() returns RailOpenCode ("opencode").
// The M3 router classifier switches on this value, so it must never silently change.

func TestOpenCodeSession_Rail(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "test-polecat"}, "/usr/bin/true")

	got := s.Rail()
	if got != RailOpenCode {
		t.Errorf("Rail() = %q, want RailOpenCode (%q)", got, RailOpenCode)
	}
}

// ─── TestOpenCodeSession_RailIsStable ───────────────────────────────────────
//
// Verifies Rail() returns the same value on every call (immutability invariant).

func TestOpenCodeSession_RailIsStable(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "stable"}, "/usr/bin/true")
	for i := range 5 {
		got := s.Rail()
		if got != RailOpenCode {
			t.Errorf("Rail() call %d = %q, want %q", i, got, RailOpenCode)
		}
	}
}

// ─── TestOpenCodeSession_CompileTimeInterface ────────────────────────────────
//
// The package-level `var _ Session = (*openCodeSession)(nil)` enforces interface
// satisfaction at compile time. This test provides a runtime double-check.

func TestOpenCodeSession_CompileTimeInterface(t *testing.T) {
	t.Parallel()

	var iface Session = newOpenCodeSession(StartOptions{}, "/usr/bin/true")
	if iface == nil {
		t.Fatal("expected non-nil Session from newOpenCodeSession")
	}
	if iface.Rail() != RailOpenCode {
		t.Errorf("Rail() via Session interface = %q, want %q", iface.Rail(), RailOpenCode)
	}
}

// ─── TestOpenCodeSession_StartStopLifecycle ──────────────────────────────────
//
// Mocks the tmux subprocess layer by injecting a stub "tmux" binary via PATH.
// The stub exits 1 for "has-session" (so Stop's poll loop terminates immediately)
// and exits 0 for all other commands.
//
// Verifies:
//   - started=0 before Start()
//   - Start() returns a non-empty SessionID
//   - started=1 after Start()
//   - Stop() returns nil
//   - started=0 (stopped=1) after Stop()

func TestOpenCodeSession_StartStopLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lifecycle test in short mode (requires go build)")
	}

	tmuxStub := buildTmuxStub(t)
	stubDir := filepath.Dir(tmuxStub)

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", stubDir+":"+oldPath)

	workDir := t.TempDir()
	opts := StartOptions{
		PolecatName: "lifecycle-test",
		WorkDir:     workDir,
	}
	s := newOpenCodeSession(opts, "/usr/bin/true")

	if s.started.Load() != 0 {
		t.Fatal("expected started=0 before Start()")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sid, err := s.Start(ctx, StartOptions{})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if sid == "" {
		t.Fatal("Start() returned empty SessionID")
	}
	if s.started.Load() != 1 {
		t.Fatal("expected started=1 after Start()")
	}

	if err := s.Stop(ctx, false); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if s.stopped.Load() != 1 {
		t.Fatal("expected stopped=1 after Stop()")
	}
}

// ─── TestOpenCodeSession_SendRejectsEmptyMsg ────────────────────────────────
//
// Verifies Send() returns an error rather than passing an empty command to OpenCode.

func TestOpenCodeSession_SendRejectsEmptyMsg(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "test"}, "/usr/bin/true")
	s.started.Store(1) // bypass Start for this unit test

	err := s.Send(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty msg, got nil")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected 'must not be empty' in error, got: %v", err)
	}
}

// ─── TestOpenCodeSession_SendRejectsNotStarted ───────────────────────────────
//
// Verifies Send() returns ErrSessionNotStarted when called before Start().

func TestOpenCodeSession_SendRejectsNotStarted(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "test"}, "/usr/bin/true")

	err := s.Send(context.Background(), "fix the bug")
	if err == nil {
		t.Fatal("expected ErrSessionNotStarted, got nil")
	}
	if err != ErrSessionNotStarted {
		t.Errorf("expected ErrSessionNotStarted, got: %v", err)
	}
}

// ─── TestOpenCodeSession_StreamRejectsNotStarted ────────────────────────────
//
// Verifies Stream() returns ErrSessionNotStarted before Start().

func TestOpenCodeSession_StreamRejectsNotStarted(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "test"}, "/usr/bin/true")

	_, err := s.Stream(context.Background())
	if err == nil {
		t.Fatal("expected ErrSessionNotStarted from Stream, got nil")
	}
	if err != ErrSessionNotStarted {
		t.Errorf("expected ErrSessionNotStarted, got: %v", err)
	}
}

// ─── TestOpenCodeSession_StreamEmitsEventDone ────────────────────────────────
//
// Verifies Stream() emits EventDone and closes the channel (M2 stub behaviour).

func TestOpenCodeSession_StreamEmitsEventDone(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "stream-test"}, "/usr/bin/true")
	s.started.Store(1) // bypass Start

	ch, err := s.Stream(context.Background())
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	// Drain the channel.
	var events []Event
	for e := range ch {
		events = append(events, e)
	}

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event from Stream stub, got %d", len(events))
	}
	if events[0].Kind != EventDone {
		t.Errorf("expected EventDone, got Kind=%q", events[0].Kind)
	}
}

// ─── TestOpenCodeSession_StopIsIdempotent ────────────────────────────────────
//
// Verifies Stop() is a no-op on a session that was never started.

func TestOpenCodeSession_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "idempotent"}, "/usr/bin/true")

	err := s.Stop(context.Background(), false)
	if err != nil {
		t.Fatalf("Stop on un-started session should be a no-op, got: %v", err)
	}
	// Call again — must still be no-op.
	err = s.Stop(context.Background(), true)
	if err != nil {
		t.Fatalf("second Stop should be a no-op, got: %v", err)
	}
}

// ─── TestOpenCodeSession_StartDoubleCallReturnsError ────────────────────────
//
// Verifies Start() returns ErrSessionAlreadyStarted when called twice.

func TestOpenCodeSession_StartDoubleCallReturnsError(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "double-start"}, "/usr/bin/true")
	s.started.Store(1) // simulate already-started

	_, err := s.Start(context.Background(), StartOptions{WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on double Start(), got nil")
	}
	if err != ErrSessionAlreadyStarted {
		t.Errorf("expected ErrSessionAlreadyStarted, got: %v", err)
	}
}

// ─── TestOpenCodeSession_ResultSummaryNilBeforeStop ──────────────────────────
//
// Verifies ResultSummary() returns nil before Stop() is called.

func TestOpenCodeSession_ResultSummaryNilBeforeStop(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{PolecatName: "summary-test"}, "/usr/bin/true")

	if got := s.ResultSummary(); got != nil {
		t.Errorf("ResultSummary() before Stop = %+v, want nil", got)
	}
}

// ─── TestOpenCodeSession_DeriveSessionName ───────────────────────────────────
//
// Verifies the tmux session name derivation:
//   - "oc-<polecatName>" when polecatName is set,
//   - "oc-default" when polecatName is empty.

func TestOpenCodeSession_DeriveSessionName(t *testing.T) {
	t.Parallel()

	s := newOpenCodeSession(StartOptions{}, "/usr/bin/true")

	cases := []struct {
		name string
		want string
	}{
		{"alpha", "oc-alpha"},
		{"", "oc-default"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := s.deriveSessionName(StartOptions{PolecatName: tc.name})
			if got != tc.want {
				t.Errorf("deriveSessionName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// ─── TestOpenCodeSession_NoMCPRegistration ───────────────────────────────────
//
// Token-bloat firewall (ADR §2.4): verifies the production source file contains
// no MCP tool registration calls. This is a static source-text check that makes
// the firewall property machine-verifiable.

func TestOpenCodeSession_NoMCPRegistration(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("opencode_session.go")
	if err != nil {
		t.Fatalf("reading opencode_session.go: %v", err)
	}
	text := string(src)

	forbidden := []string{
		"RegisterTool",
		"create_sdk_mcp_server",
		"mcpServer",
		"mcp.NewServer",
	}
	for _, pattern := range forbidden {
		if strings.Contains(text, pattern) {
			t.Errorf("ADR §2.4 token-bloat firewall: opencode_session.go must not contain %q", pattern)
		}
	}
}

// ─── TestNewFactory_WiresOpenCodeRail ────────────────────────────────────────
//
// Verifies New(RailOpenCode, ...) now returns a live session (not ErrRailNotImplemented).
// This proves M2's wiring into the New() factory is complete.

func TestNewFactory_WiresOpenCodeRail(t *testing.T) {
	t.Parallel()

	sess, err := New(RailOpenCode, StartOptions{PolecatName: "factory-test"})
	if err != nil {
		t.Fatalf("New(RailOpenCode, ...) error: %v (expected nil after M2 lands)", err)
	}
	if sess == nil {
		t.Fatal("New(RailOpenCode, ...) returned nil session")
	}
	if sess.Rail() != RailOpenCode {
		t.Errorf("New(RailOpenCode) Rail() = %q, want %q", sess.Rail(), RailOpenCode)
	}
}

// ─── stub binary helpers ─────────────────────────────────────────────────────

// tmuxStubSource is a minimal Go program that acts as a fake tmux binary:
//   - `tmux has-session ...` exits 1 (signals "session gone" so Stop's poll terminates immediately)
//   - all other sub-commands exit 0
const tmuxStubSource = `package main

import (
	"os"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "has-session" {
			os.Exit(1)
		}
	}
	os.Exit(0)
}
`

// buildTmuxStub compiles tmuxStubSource into a temp binary named "tmux".
func buildTmuxStub(t *testing.T) string {
	t.Helper()
	return compileStubBinary(t, "tmux", tmuxStubSource)
}

// compileStubBinary writes source to a temp dir and compiles it via `go build`.
func compileStubBinary(t *testing.T, name, source string) string {
	t.Helper()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not found; skipping binary-stub tests")
	}

	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(srcFile, []byte(source), 0644); err != nil {
		t.Fatalf("write stub source for %s: %v", name, err)
	}

	binPath := filepath.Join(srcDir, name)
	cmd := exec.Command("go", "build", "-o", binPath, srcFile) //nolint:gosec
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile stub %s: %v\n%s", name, err, out)
	}

	t.Cleanup(func() { _ = os.Remove(binPath) })
	return binPath
}

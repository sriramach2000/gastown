package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubState tracks calls to the package-level tmux subprocess shims.
type stubState struct {
	spawnCalled   int
	sendCalled    int
	sendRawCalled int
	captureCalled int
	killCalled    int
	captureOutput string
	spawnErr      error
	sendErr       error
	killErr       error
}

// stubTmuxFuncs replaces the package-level subprocess shims with no-op stubs
// and returns a restore function. All stubs succeed by default; individual
// tests may set fields on stubState to inject failures.
func stubTmuxFuncs(t *testing.T) (*stubState, func()) {
	t.Helper()
	st := &stubState{}

	origSpawn := spawnTmuxSession
	origSend := tmuxSendKeys
	origSendRaw := tmuxSendKeysRaw
	origCapture := tmuxCapturePane
	origKill := tmuxKillSession

	spawnTmuxSession = func(_ context.Context, _ string, _ StartOptions) error {
		st.spawnCalled++
		return st.spawnErr
	}
	tmuxSendKeys = func(_, _ string) error {
		st.sendCalled++
		return st.sendErr
	}
	tmuxSendKeysRaw = func(_, _ string) error {
		st.sendRawCalled++
		return nil
	}
	tmuxCapturePane = func(_ string, _ int) (string, error) {
		st.captureCalled++
		return st.captureOutput, nil
	}
	tmuxKillSession = func(_ string) error {
		st.killCalled++
		return st.killErr
	}

	restore := func() {
		spawnTmuxSession = origSpawn
		tmuxSendKeys = origSend
		tmuxSendKeysRaw = origSendRaw
		tmuxCapturePane = origCapture
		tmuxKillSession = origKill
	}
	return st, restore
}

// ---------------------------------------------------------------------------
// Lifecycle tests — Start / Send / Stop
// ---------------------------------------------------------------------------

// TestClaudeSessionStart_OK verifies the happy-path Start call spawns exactly
// one tmux session and returns a non-empty SessionID.
func TestClaudeSessionStart_OK(t *testing.T) {
	t.Parallel()
	st, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast", WorkDir: "/tmp/repo"})
	id, err := sess.Start(context.Background(), StartOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Error("Start: returned empty SessionID")
	}
	if st.spawnCalled != 1 {
		t.Errorf("spawnTmuxSession called %d times, want 1", st.spawnCalled)
	}
}

// TestClaudeSessionStart_RequiresWorkDir verifies that Start fails when WorkDir
// is empty in both construction opts and call opts.
func TestClaudeSessionStart_RequiresWorkDir(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast"}) // no WorkDir
	_, err := sess.Start(context.Background(), StartOptions{})
	if err == nil {
		t.Fatal("Start with empty WorkDir: expected error, got nil")
	}
}

// TestClaudeSessionStart_Idempotent verifies that calling Start twice returns
// ErrSessionAlreadyStarted on the second call.
func TestClaudeSessionStart_Idempotent(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast", WorkDir: "/tmp/repo"})
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	_, err := sess.Start(context.Background(), StartOptions{})
	if !errors.Is(err, ErrSessionAlreadyStarted) {
		t.Errorf("second Start: got %v, want ErrSessionAlreadyStarted", err)
	}
}

// TestClaudeSessionStart_SpawnError verifies that a spawn failure resets the
// started flag so callers can retry.
func TestClaudeSessionStart_SpawnError(t *testing.T) {
	t.Parallel()
	st, restore := stubTmuxFuncs(t)
	defer restore()

	st.spawnErr = errors.New("tmux unavailable")

	sess := newClaudeSession(StartOptions{PolecatName: "Toast", WorkDir: "/tmp/repo"})
	_, err := sess.Start(context.Background(), StartOptions{})
	if err == nil {
		t.Fatal("Start with spawn error: expected error, got nil")
	}

	// After failure, started flag must be reset — allow retry.
	st.spawnErr = nil
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Errorf("retry after spawn failure: %v", err)
	}
}

// TestClaudeSessionSend_OK verifies Send delivers a message after Start.
func TestClaudeSessionSend_OK(t *testing.T) {
	t.Parallel()
	st, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast", WorkDir: "/tmp/repo"})
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := sess.Send(context.Background(), "check your hook"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if st.sendCalled != 1 {
		t.Errorf("tmuxSendKeys called %d times, want 1", st.sendCalled)
	}
}

// TestClaudeSessionSend_NotStarted verifies Send returns ErrSessionNotStarted
// when Start has not been called.
func TestClaudeSessionSend_NotStarted(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{})
	err := sess.Send(context.Background(), "hello")
	if !errors.Is(err, ErrSessionNotStarted) {
		t.Errorf("Send before Start: got %v, want ErrSessionNotStarted", err)
	}
}

// TestClaudeSessionStream_NotStarted verifies Stream returns ErrSessionNotStarted
// before Start.
func TestClaudeSessionStream_NotStarted(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{})
	_, err := sess.Stream(context.Background())
	if !errors.Is(err, ErrSessionNotStarted) {
		t.Errorf("Stream before Start: got %v, want ErrSessionNotStarted", err)
	}
}

// TestClaudeSessionStop_Idempotent verifies that calling Stop twice is a no-op
// on the second call and tmuxKillSession is called exactly once.
func TestClaudeSessionStop_Idempotent(t *testing.T) {
	t.Parallel()
	st, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast", WorkDir: "/tmp/repo"})
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx := context.Background()
	if err := sess.Stop(ctx, true); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := sess.Stop(ctx, true); err != nil {
		t.Fatalf("second Stop (idempotent): %v", err)
	}
	if st.killCalled != 1 {
		t.Errorf("tmuxKillSession called %d times, want 1", st.killCalled)
	}
}

// TestClaudeSessionStop_SetsResultSummary verifies that Stop populates
// ResultSummary with Rail=RailClaude and Status="stopped".
func TestClaudeSessionStop_SetsResultSummary(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast", WorkDir: "/tmp/repo"})
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := sess.Stop(context.Background(), true); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	summary := sess.ResultSummary()
	if summary == nil {
		t.Fatal("ResultSummary() after Stop = nil, want non-nil")
	}
	if summary.Rail != RailClaude {
		t.Errorf("Summary.Rail = %q, want %q", summary.Rail, RailClaude)
	}
	if summary.Status != "stopped" {
		t.Errorf("Summary.Status = %q, want %q", summary.Status, "stopped")
	}
}

// TestClaudeSessionStop_CallsOnStopHook verifies that the OnStop hook is
// invoked with the correct Summary when Stop is called.
func TestClaudeSessionStop_CallsOnStopHook(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	var hookSummary *Summary
	opts := StartOptions{
		PolecatName: "Toast",
		WorkDir:     "/tmp/repo",
		Hooks: Hooks{
			OnStop: func(s *Summary) { hookSummary = s },
		},
	}
	sess := newClaudeSession(opts)
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := sess.Stop(context.Background(), true); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if hookSummary == nil {
		t.Fatal("OnStop hook was not called")
	}
	if hookSummary.Rail != RailClaude {
		t.Errorf("OnStop summary.Rail = %q, want %q", hookSummary.Rail, RailClaude)
	}
}

// TestClaudeSessionStop_BeforeStart verifies Stop is safe when Start was never
// called (e.g. caller aborts before starting).
func TestClaudeSessionStop_BeforeStart(t *testing.T) {
	t.Parallel()
	st, restore := stubTmuxFuncs(t)
	defer restore()

	sess := newClaudeSession(StartOptions{PolecatName: "Toast"})
	if err := sess.Stop(context.Background(), true); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	// tmuxKillSession should not be called — sessionName is empty.
	if st.killCalled != 0 {
		t.Errorf("tmuxKillSession called %d times, want 0", st.killCalled)
	}
}

// ---------------------------------------------------------------------------
// Stream test
// ---------------------------------------------------------------------------

// TestClaudeSessionStream_EmitsEventsAndDone verifies the Stream goroutine
// emits EventText deltas and closes with EventDone when the idle prompt appears.
func TestClaudeSessionStream_EmitsEventsAndDone(t *testing.T) {
	t.Parallel()

	// Use a sequential capture stub (can't use stubTmuxFuncs for this since we
	// need per-call state).
	origSpawn := spawnTmuxSession
	origSend := tmuxSendKeys
	origSendRaw := tmuxSendKeysRaw
	origKill := tmuxKillSession
	origCapture := tmuxCapturePane
	t.Cleanup(func() {
		spawnTmuxSession = origSpawn
		tmuxSendKeys = origSend
		tmuxSendKeysRaw = origSendRaw
		tmuxKillSession = origKill
		tmuxCapturePane = origCapture
	})

	spawnTmuxSession = func(_ context.Context, _ string, _ StartOptions) error { return nil }
	tmuxSendKeys = func(_, _ string) error { return nil }
	tmuxSendKeysRaw = func(_, _ string) error { return nil }
	tmuxKillSession = func(_ string) error { return nil }

	// Progressive output ending with the idle prompt indicator.
	captureOutputs := []string{
		"loading context...\n",
		"loading context...\nrunning tools\n",
		"loading context...\nrunning tools\n❯",
	}
	callCount := 0
	tmuxCapturePane = func(_ string, _ int) (string, error) {
		if callCount < len(captureOutputs) {
			out := captureOutputs[callCount]
			callCount++
			return out, nil
		}
		return captureOutputs[len(captureOutputs)-1], nil
	}

	sess := newClaudeSession(StartOptions{PolecatName: "stream-test", WorkDir: "/tmp/repo"})
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := sess.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotText []string
	var gotDone bool
	for ev := range ch {
		switch ev.Kind {
		case EventText:
			gotText = append(gotText, ev.Text)
		case EventDone:
			gotDone = true
		case EventError:
			t.Errorf("unexpected EventError: %v", ev.Err)
		}
	}

	if !gotDone {
		t.Error("Stream closed without EventDone")
	}
	if len(gotText) == 0 {
		t.Error("Stream emitted no EventText events")
	}
}

// ---------------------------------------------------------------------------
// mergeOpts tests
// ---------------------------------------------------------------------------

// TestMergeOpts_CallerOverridesBase verifies that caller-provided fields win
// over construction opts during Start.
func TestMergeOpts_CallerOverridesBase(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	base := StartOptions{PolecatName: "base-name", WorkDir: "/base"}
	caller := StartOptions{PolecatName: "caller-name", WorkDir: "/caller"}

	sess := newClaudeSession(base)
	if _, err := sess.Start(context.Background(), caller); err != nil {
		t.Fatalf("Start: %v", err)
	}

	sess.mu.Lock()
	merged := sess.opts
	sess.mu.Unlock()

	if merged.PolecatName != "caller-name" {
		t.Errorf("PolecatName = %q, want caller-name", merged.PolecatName)
	}
	if merged.WorkDir != "/caller" {
		t.Errorf("WorkDir = %q, want /caller", merged.WorkDir)
	}
}

// TestMergeOpts_BaseKeptWhenCallerEmpty verifies that base fields are preserved
// when caller opts are zero-valued.
func TestMergeOpts_BaseKeptWhenCallerEmpty(t *testing.T) {
	t.Parallel()
	_, restore := stubTmuxFuncs(t)
	defer restore()

	base := StartOptions{PolecatName: "base-name", WorkDir: "/base", BeadSeverity: "P2"}
	sess := newClaudeSession(base)
	if _, err := sess.Start(context.Background(), StartOptions{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	sess.mu.Lock()
	merged := sess.opts
	sess.mu.Unlock()

	if merged.BeadSeverity != "P2" {
		t.Errorf("BeadSeverity = %q, want P2", merged.BeadSeverity)
	}
}

// ---------------------------------------------------------------------------
// buildSessionName tests
// ---------------------------------------------------------------------------

// TestBuildSessionName verifies the tmux session name format matches the
// session_manager.go convention (gt-<name>).
func TestBuildSessionName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts StartOptions
		want string
	}{
		{
			name: "normal polecat name",
			opts: StartOptions{PolecatName: "Toast"},
			want: "gt-Toast",
		},
		{
			name: "empty polecat name defaults to 'polecat'",
			opts: StartOptions{},
			want: "gt-polecat",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildSessionName(tc.opts); got != tc.want {
				t.Errorf("buildSessionName = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// diffOutput tests
// ---------------------------------------------------------------------------

func TestDiffOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		prev string
		next string
		want string
	}{
		{
			name: "pure append",
			prev: "abc",
			next: "abcdef",
			want: "def",
		},
		{
			name: "no change",
			prev: "abc",
			next: "abc",
			want: "",
		},
		{
			name: "full refresh (pane scrolled)",
			prev: "abcdef",
			next: "xyz",
			want: "xyz",
		},
		{
			name: "empty prev",
			prev: "",
			next: "hello",
			want: "hello",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := diffOutput(tc.prev, tc.next); got != tc.want {
				t.Errorf("diffOutput(%q, %q) = %q, want %q", tc.prev, tc.next, got, tc.want)
			}
		})
	}
}

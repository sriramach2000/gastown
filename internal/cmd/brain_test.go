package cmd

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestBrainCmdRegistered verifies that brainCmd has the expected subcommands.
func TestBrainCmdRegistered(t *testing.T) {
	t.Parallel()

	subNames := map[string]bool{}
	for _, sub := range brainCmd.Commands() {
		subNames[sub.Name()] = true
	}

	required := []string{"init", "doctor", "stats", "search", "query"}
	for _, name := range required {
		if !subNames[name] {
			t.Errorf("brainCmd missing subcommand %q", name)
		}
	}
}

// TestBrainCmdUse verifies brainCmd.Use is "brain".
func TestBrainCmdUse(t *testing.T) {
	t.Parallel()
	if brainCmd.Use != "brain" {
		t.Errorf("brainCmd.Use = %q, want %q", brainCmd.Use, "brain")
	}
}

// TestBrainSearchArgsRequired verifies `gt brain search` requires at least 1 arg.
func TestBrainSearchArgsRequired(t *testing.T) {
	t.Parallel()
	err := brainSearchCmd.Args(brainSearchCmd, []string{})
	if err == nil {
		t.Error("brainSearchCmd.Args should reject empty args")
	}
}

// TestBrainQueryArgsRequired verifies `gt brain query` requires at least 1 arg.
func TestBrainQueryArgsRequired(t *testing.T) {
	t.Parallel()
	err := brainQueryCmd.Args(brainQueryCmd, []string{})
	if err == nil {
		t.Error("brainQueryCmd.Args should reject empty args")
	}
}

// TestJoinArgs verifies the query-joining helper.
func TestJoinArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"polecat", "lifecycle"}, "polecat lifecycle"},
		{[]string{"single"}, "single"},
		{[]string{}, ""},
	}
	for _, tc := range tests {
		got := joinArgs(tc.in)
		if got != tc.want {
			t.Errorf("joinArgs(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------- scheduler tests --------------------------------------------------

// TestNextDreamTimeAfterMidnight verifies nextDreamTime returns today's 00:30
// when called before 00:30.
func TestNextDreamTimeAfterMidnight(t *testing.T) {
	t.Parallel()

	loc := time.Local
	// 23:00 — 00:30 is still in the future (next calendar day 00:30).
	now := time.Date(2026, 5, 21, 23, 0, 0, 0, loc)
	got := nextDreamTime(now)
	want := time.Date(2026, 5, 22, 0, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("nextDreamTime(%v) = %v, want %v", now, got, want)
	}
}

// TestNextDreamTimeBeforeDream verifies that calling at 00:00 returns today's 00:30.
func TestNextDreamTimeBeforeDream(t *testing.T) {
	t.Parallel()

	loc := time.Local
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, loc)
	got := nextDreamTime(now)
	want := time.Date(2026, 5, 21, 0, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("nextDreamTime(%v) = %v, want %v", now, got, want)
	}
}

// TestNextDreamTimeAfterDream verifies that calling after 00:30 returns the
// NEXT day's 00:30.
func TestNextDreamTimeAfterDream(t *testing.T) {
	t.Parallel()

	loc := time.Local
	// 01:00 — already past 00:30, so next is tomorrow.
	now := time.Date(2026, 5, 21, 1, 0, 0, 0, loc)
	got := nextDreamTime(now)
	want := time.Date(2026, 5, 22, 0, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("nextDreamTime(%v) = %v, want %v", now, got, want)
	}
}

// TestNextDreamTimeExactlyAt verifies that nextDreamTime(00:30:00) advances
// to the next day (not returning the same instant).
func TestNextDreamTimeExactlyAt(t *testing.T) {
	t.Parallel()

	loc := time.Local
	now := time.Date(2026, 5, 21, 0, 30, 0, 0, loc)
	got := nextDreamTime(now)
	if !got.After(now) {
		t.Errorf("nextDreamTime(exactly 00:30) should return a future time, got %v", got)
	}
}

// TestNextDreamTimeIsAlways0030 verifies the returned time is always HH:MM == 00:30.
func TestNextDreamTimeIsAlways0030(t *testing.T) {
	t.Parallel()

	loc := time.Local
	// Walk through several times of day and confirm the result is always 00:30.
	offsets := []int{0, 1, 2, 6, 12, 22, 23}
	for _, h := range offsets {
		now := time.Date(2026, 5, 21, h, 45, 0, 0, loc)
		got := nextDreamTime(now)
		if got.Hour() != brainDreamHour || got.Minute() != brainDreamMinute {
			t.Errorf("nextDreamTime at hour %d: got %02d:%02d, want %02d:%02d",
				h, got.Hour(), got.Minute(), brainDreamHour, brainDreamMinute)
		}
	}
}

// TestStartBrainDreamTickerCancelImmediately verifies that the ticker goroutine
// exits cleanly when context is cancelled before the first fire.
func TestStartBrainDreamTickerCancelImmediately(t *testing.T) {
	t.Parallel()

	// Point BRAIN_TEST_TOWNROOT to a temp dir to prevent real gbrain calls.
	townRoot := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		startBrainDreamTicker(ctx, townRoot)
	}()

	// Cancel immediately — goroutine should exit before the 00:30 sleep fires.
	cancel()

	select {
	case <-done:
		// pass
	case <-time.After(5 * time.Second):
		t.Error("startBrainDreamTicker did not exit after context cancel within 5s")
	}
}

// TestFindGbrainBinaryFallsBackToPath verifies findGbrainBinary returns an
// error when gbrain is not present in node_modules or PATH.
// (Uses a temp dir with no node_modules and a PATH that doesn't contain gbrain.)
func TestFindGbrainBinaryFallsBackToPath(t *testing.T) {
	t.Parallel()

	townRoot := t.TempDir()

	// Temporarily override PATH to ensure gbrain is not found.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir()) // empty dir with no binaries
	defer os.Setenv("PATH", origPath)

	_, err := findGbrainBinary(townRoot)
	if err == nil {
		t.Error("findGbrainBinary should return error when gbrain is not found")
	}
}

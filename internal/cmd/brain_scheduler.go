package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// brainDreamHour is the hour (town-local) at which the nightly dream cycle fires.
	brainDreamHour = 0
	// brainDreamMinute is the minute at which the nightly dream cycle fires.
	brainDreamMinute = 30
	// brainDreamTimeout is the maximum wall-clock time allowed for a dream cycle run.
	brainDreamTimeout = 30 * time.Minute
)

// startBrainDreamTicker starts the nightly gbrain dream-cycle scheduler.
// It fires at 00:30 town-local time and calls:
//
//	gbrain <town-root> dream-cycle --non-interactive
//
// The ticker is self-rearming: after each run it computes the next 00:30
// occurrence and sleeps until then. It exits when ctx is cancelled (daemon
// shutdown). Errors are logged but do not kill the daemon.
//
// This function is called as a goroutine from runDaemonRun:
//
//	go startBrainDreamTicker(ctx, townRoot)
func startBrainDreamTicker(ctx context.Context, townRoot string) {
	logger := log.New(os.Stderr, "[brain-scheduler] ", log.LstdFlags)

	for {
		next := nextDreamTime(time.Now())
		logger.Printf("Dream cycle scheduled for %s (in %v)", next.Format("2006-01-02 15:04:05 MST"), time.Until(next).Round(time.Second))

		select {
		case <-ctx.Done():
			logger.Printf("Brain dream scheduler stopped (context cancelled)")
			return
		case <-time.After(time.Until(next)):
			// Firewall: if context was cancelled while sleeping, stop.
			if ctx.Err() != nil {
				return
			}
			runBrainDreamCycle(ctx, townRoot, logger)
		}
	}
}

// runBrainDreamCycle executes `gbrain <townRoot> dream-cycle --non-interactive`
// with a timeout. Errors are logged but do not propagate — the scheduler loop
// continues regardless.
func runBrainDreamCycle(ctx context.Context, townRoot string, logger *log.Logger) {
	brainBin, err := findGbrainBinary(townRoot)
	if err != nil {
		logger.Printf("Dream cycle skipped: %v", err)
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, brainDreamTimeout)
	defer cancel()

	c := exec.CommandContext(runCtx, brainBin, townRoot, "dream-cycle", "--non-interactive")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Dir = townRoot

	logger.Printf("Running dream cycle: %s %s dream-cycle --non-interactive", brainBin, townRoot)
	if err := c.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			logger.Printf("Dream cycle timed out after %v", brainDreamTimeout)
		} else {
			logger.Printf("Dream cycle error: %v", err)
		}
		return
	}
	logger.Printf("Dream cycle completed successfully")
}

// nextDreamTime returns the next 00:30 local time after now.
// If 00:30 is still in the future today it returns today's 00:30;
// otherwise it returns tomorrow's 00:30.
func nextDreamTime(now time.Time) time.Time {
	loc := now.Location()
	candidate := time.Date(now.Year(), now.Month(), now.Day(), brainDreamHour, brainDreamMinute, 0, 0, loc)
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}

// findGbrainBinary returns the path to the gbrain binary.
// It checks the town-local node_modules/.bin first (Bun workspace install),
// then falls back to PATH.
func findGbrainBinary(townRoot string) (string, error) {
	// Check town-local Bun install first.
	localBin := filepath.Join(townRoot, "node_modules", ".bin", "gbrain")
	if _, err := os.Stat(localBin); err == nil {
		return localBin, nil
	}

	// Fall back to PATH.
	path, err := exec.LookPath("gbrain")
	if err != nil {
		return "", fmt.Errorf("gbrain binary not found in town node_modules or PATH — install with: bun install -g gbrain")
	}
	return path, nil
}

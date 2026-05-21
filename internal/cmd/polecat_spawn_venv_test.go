package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsurePolecatVenvCreatesVenv verifies that ensurePolecatVenv creates
// .venv/bin/python when no venv exists yet.
func TestEnsurePolecatVenvCreatesVenv(t *testing.T) {
	clonePath := t.TempDir()

	if err := ensurePolecatVenv(clonePath); err != nil {
		t.Fatalf("ensurePolecatVenv() error = %v, want nil", err)
	}

	venvPython := filepath.Join(clonePath, ".venv", "bin", "python")
	if _, err := os.Stat(venvPython); err != nil {
		t.Fatalf("ensurePolecatVenv() did not create %s: %v", venvPython, err)
	}
}

// TestEnsurePolecatVenvIdempotent verifies that calling ensurePolecatVenv a second
// time on an already-initialised venv does not fail (fast-path hit).
func TestEnsurePolecatVenvIdempotent(t *testing.T) {
	clonePath := t.TempDir()

	if err := ensurePolecatVenv(clonePath); err != nil {
		t.Fatalf("first ensurePolecatVenv() error = %v, want nil", err)
	}
	if err := ensurePolecatVenv(clonePath); err != nil {
		t.Fatalf("second ensurePolecatVenv() error = %v, want nil (idempotent)", err)
	}
}

// TestEnsurePolecatVenvWritesTelemetryFlag verifies that CLI_HUB_NO_TELEMETRY=1
// is written into .venv/bin/activate after venv creation (§D11).
func TestEnsurePolecatVenvWritesTelemetryFlag(t *testing.T) {
	clonePath := t.TempDir()

	if err := ensurePolecatVenv(clonePath); err != nil {
		t.Fatalf("ensurePolecatVenv() error = %v, want nil", err)
	}

	activatePath := filepath.Join(clonePath, ".venv", "bin", "activate")
	content, err := os.ReadFile(activatePath)
	if err != nil {
		t.Fatalf("reading activate script: %v", err)
	}
	if !strings.Contains(string(content), "CLI_HUB_NO_TELEMETRY") {
		t.Errorf("activate script missing CLI_HUB_NO_TELEMETRY=1; activate content (first 200 chars): %.200s", string(content))
	}
}

// TestEnsurePolecatVenvTelemetryFlagNotDuplicated verifies that calling
// ensurePolecatVenv twice does not append CLI_HUB_NO_TELEMETRY twice.
func TestEnsurePolecatVenvTelemetryFlagNotDuplicated(t *testing.T) {
	clonePath := t.TempDir()

	if err := ensurePolecatVenv(clonePath); err != nil {
		t.Fatalf("first ensurePolecatVenv() error = %v", err)
	}
	if err := ensurePolecatVenv(clonePath); err != nil {
		t.Fatalf("second ensurePolecatVenv() error = %v", err)
	}

	activatePath := filepath.Join(clonePath, ".venv", "bin", "activate")
	content, err := os.ReadFile(activatePath)
	if err != nil {
		t.Fatalf("reading activate script: %v", err)
	}
	occurrences := strings.Count(string(content), "CLI_HUB_NO_TELEMETRY")
	if occurrences != 1 {
		t.Errorf("CLI_HUB_NO_TELEMETRY appears %d times in activate, want exactly 1", occurrences)
	}
}

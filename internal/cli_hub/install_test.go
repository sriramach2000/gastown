package cli_hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToPackageName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"blender", "cli-anything-blender"},
		{"BLENDER", "cli-anything-blender"},
		{"cli-anything-blender", "cli-anything-blender"},
		{"cli-anything-BLENDER", "cli-anything-blender"},
		{"Exa", "cli-anything-exa"},
	}
	for _, tc := range cases {
		got := toPackageName(tc.input)
		if got != tc.want {
			t.Errorf("toPackageName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestInstaller_EnsureVenv_CreatesDirectory(t *testing.T) {
	// Skip if python3 is unavailable — this is an integration-style test
	if _, err := findPython3(); err != nil {
		t.Skip("python3 not available:", err)
	}

	dir := t.TempDir()
	venvPath := filepath.Join(dir, ".venv")
	installer := NewInstaller(venvPath)

	if err := installer.EnsureVenv(); err != nil {
		t.Fatalf("EnsureVenv: %v", err)
	}

	// Verify effect: pyvenv.cfg must exist (Block 14 compliance)
	cfgPath := filepath.Join(venvPath, "pyvenv.cfg")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("pyvenv.cfg not found after EnsureVenv: %v", err)
	}

	// Verify pip is accessible
	pipPath, err := installer.pipPath()
	if err != nil {
		t.Errorf("pip not found after EnsureVenv: %v", err)
	}
	if _, err := os.Stat(pipPath); err != nil {
		t.Errorf("pip binary missing at %s: %v", pipPath, err)
	}
}

func TestInstaller_EnsureVenv_Idempotent(t *testing.T) {
	if _, err := findPython3(); err != nil {
		t.Skip("python3 not available:", err)
	}

	dir := t.TempDir()
	venvPath := filepath.Join(dir, ".venv")
	installer := NewInstaller(venvPath)

	// Create twice — must not error
	if err := installer.EnsureVenv(); err != nil {
		t.Fatalf("first EnsureVenv: %v", err)
	}
	if err := installer.EnsureVenv(); err != nil {
		t.Fatalf("second EnsureVenv (idempotent): %v", err)
	}
}

func TestInstaller_EnsureVenv_NoTelemetrySet(t *testing.T) {
	if _, err := findPython3(); err != nil {
		t.Skip("python3 not available:", err)
	}

	dir := t.TempDir()
	venvPath := filepath.Join(dir, ".venv")
	installer := NewInstaller(venvPath)

	if err := installer.EnsureVenv(); err != nil {
		t.Fatalf("EnsureVenv: %v", err)
	}

	// Check that CLI_HUB_NO_TELEMETRY=1 was written to the activate script (§D11)
	activatePath := filepath.Join(venvPath, "bin", "activate")
	data, err := os.ReadFile(activatePath)
	if err != nil {
		t.Fatalf("reading activate script: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("activate script is empty")
	}
	found := false
	for _, line := range splitLines(string(data)) {
		if line == "export CLI_HUB_NO_TELEMETRY=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("CLI_HUB_NO_TELEMETRY=1 not found in activate script")
	}
}

func TestInstaller_IsInstalled_FalseForEmptyVenv(t *testing.T) {
	if _, err := findPython3(); err != nil {
		t.Skip("python3 not available:", err)
	}

	dir := t.TempDir()
	venvPath := filepath.Join(dir, ".venv")
	installer := NewInstaller(venvPath)

	if err := installer.EnsureVenv(); err != nil {
		t.Fatalf("EnsureVenv: %v", err)
	}

	if installer.IsInstalled("blender") {
		t.Error("expected IsInstalled to return false for freshly-created venv")
	}
}

func TestInstaller_PipPath_ErrorOnMissingVenv(t *testing.T) {
	installer := &Installer{VenvPath: "/nonexistent/.venv"}
	_, err := installer.pipPath()
	if err == nil {
		t.Error("expected error from pipPath with nonexistent venv")
	}
}

func TestNewInstaller_DefaultsToCurrentDirVenv(t *testing.T) {
	installer := NewInstaller("")
	if installer.VenvPath == "" {
		t.Error("expected non-empty VenvPath when no path given")
	}
}

func TestFindPython3_LocatesInterpreter(t *testing.T) {
	p, err := findPython3()
	if err != nil {
		t.Skipf("python3 not available on this machine: %v", err)
	}
	if p == "" {
		t.Error("findPython3 returned empty path")
	}
	// Verify it's executable
	if _, err := os.Stat(p); err != nil {
		t.Errorf("python3 path %s is not accessible: %v", p, err)
	}
}

// splitLines splits a string into lines, trimming trailing whitespace.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			line := s[start:i]
			// trim CR
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

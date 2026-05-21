package brain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// No brain.yml present → all defaults applied, no error.
	tmpDir := t.TempDir()

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig with no brain.yml: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("expected default port %d, got %d", DefaultPort, cfg.Port)
	}
	if cfg.SearchMode != DefaultSearchMode {
		t.Errorf("expected default search mode %q, got %q", DefaultSearchMode, cfg.SearchMode)
	}
	if cfg.GbrainVersion != DefaultGbrainVersion {
		t.Errorf("expected default gbrain version %q, got %q", DefaultGbrainVersion, cfg.GbrainVersion)
	}
	if cfg.Doctor.DailyUSDCap != DefaultDoctorDailyUSDCap {
		t.Errorf("expected default daily USD cap %.2f, got %.2f", DefaultDoctorDailyUSDCap, cfg.Doctor.DailyUSDCap)
	}
}

func TestLoadConfig_FullYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yaml := `
port: 8000
search_mode: precision
gbrain_version: v0.38.0.0
source: gastown-town-xl4
doctor:
  daily_usd_cap: 2.5
`
	if err := os.WriteFile(filepath.Join(tmpDir, ConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 8000 {
		t.Errorf("expected port 8000, got %d", cfg.Port)
	}
	if cfg.SearchMode != "precision" {
		t.Errorf("expected search_mode precision, got %q", cfg.SearchMode)
	}
	if cfg.GbrainVersion != "v0.38.0.0" {
		t.Errorf("expected gbrain_version v0.38.0.0, got %q", cfg.GbrainVersion)
	}
	if cfg.Source != "gastown-town-xl4" {
		t.Errorf("expected source gastown-town-xl4, got %q", cfg.Source)
	}
	if cfg.Doctor.DailyUSDCap != 2.5 {
		t.Errorf("expected daily_usd_cap 2.5, got %.2f", cfg.Doctor.DailyUSDCap)
	}
}

func TestLoadConfig_PartialYAML_DefaultsApplied(t *testing.T) {
	// Only port is set; other fields should fall back to defaults.
	tmpDir := t.TempDir()
	yaml := "port: 9999\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("expected port 9999, got %d", cfg.Port)
	}
	if cfg.SearchMode != DefaultSearchMode {
		t.Errorf("expected default search mode after partial YAML, got %q", cfg.SearchMode)
	}
	if cfg.Doctor.DailyUSDCap != DefaultDoctorDailyUSDCap {
		t.Errorf("expected default daily_usd_cap after partial YAML, got %.2f", cfg.Doctor.DailyUSDCap)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ConfigFileName), []byte("port: [not a number"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadConfig_UnreadableFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ConfigFileName)
	if err := os.WriteFile(path, []byte("port: 1234"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make it unreadable.
	if err := os.Chmod(path, 0000); err != nil {
		t.Skip("chmod not effective on this platform")
	}
	defer os.Chmod(path, 0644) //nolint:errcheck

	// Skip if running as root (root ignores permissions).
	if os.Getuid() == 0 {
		t.Skip("running as root: permission check not enforced")
	}

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for unreadable brain.yml, got nil")
	}
}

func TestApplyDefaults_ZeroValues(t *testing.T) {
	cfg := &BrainConfig{}
	applyDefaults(cfg)

	if cfg.Port != DefaultPort {
		t.Errorf("applyDefaults: port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.SearchMode != DefaultSearchMode {
		t.Errorf("applyDefaults: search_mode = %q, want %q", cfg.SearchMode, DefaultSearchMode)
	}
	if cfg.GbrainVersion != DefaultGbrainVersion {
		t.Errorf("applyDefaults: gbrain_version = %q, want %q", cfg.GbrainVersion, DefaultGbrainVersion)
	}
	if cfg.Doctor.DailyUSDCap != DefaultDoctorDailyUSDCap {
		t.Errorf("applyDefaults: doctor.daily_usd_cap = %.2f, want %.2f", cfg.Doctor.DailyUSDCap, DefaultDoctorDailyUSDCap)
	}
}

func TestApplyDefaults_PreservesExplicitZeroPort(t *testing.T) {
	// Port 0 is explicitly invalid (not user-settable), so applyDefaults
	// replaces it with the default. This test documents that behaviour.
	cfg := &BrainConfig{Port: 0}
	applyDefaults(cfg)
	if cfg.Port != DefaultPort {
		t.Errorf("expected default port for zero value, got %d", cfg.Port)
	}
}

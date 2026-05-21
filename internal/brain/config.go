package brain

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigFileName is the name of the brain configuration file at the town root.
	ConfigFileName = "brain.yml"

	// DefaultPort is the default HTTP port for the gbrain sidecar.
	DefaultPort = 7891

	// DefaultSearchMode is the default search strategy; tune after 30-day traffic
	// window via `gbrain search stats` + `gbrain search tune`.
	DefaultSearchMode = "balanced"

	// DefaultDoctorDailyUSDCap is the conservative daily cost cap for
	// `gbrain doctor --remediate`. Raisable via brain.yml.
	DefaultDoctorDailyUSDCap = 1.0

	// DefaultGbrainVersion is the pinned gbrain release used when brain.yml
	// does not specify one. Matches the version audited in Gate-1 design.
	DefaultGbrainVersion = "v0.37.7.0"
)

// BrainConfig holds the configuration for the gbrain integration.
// Loaded from brain.yml in the town root. All fields have sensible defaults
// so a minimal brain.yml (or even an absent one) still produces a usable config.
//
// Wave-2 note: this struct is the single source of truth for BrainConfig.
// brain-deacon (Wave 2) must import internal/brain and use this type rather
// than re-defining its own config struct — see Gate-1 §R5 risk mitigation.
type BrainConfig struct {
	// Port is the localhost port for `gbrain serve --http`. Default: 7891.
	Port int `yaml:"port"`

	// SearchMode controls the retrieval strategy. Values: "balanced", "precision",
	// "recall". Default: "balanced". Tune after 30-day traffic window.
	SearchMode string `yaml:"search_mode"`

	// GbrainVersion pins the gbrain release for reproducible installs.
	// Set in brain.yml as: gbrain_version: "v0.37.7.0"
	GbrainVersion string `yaml:"gbrain_version"`

	// Doctor holds cost-cap and remediation settings for `gbrain doctor`.
	Doctor DoctorConfig `yaml:"doctor"`

	// Source is the gbrain source ID registered with `gbrain sources add`.
	// Used by brain-deacon ingest to identify the origin of ingested pages.
	Source string `yaml:"source"`
}

// DoctorConfig holds settings for `gbrain doctor --remediate`.
type DoctorConfig struct {
	// DailyUSDCap is the maximum cost in USD allowed per day for automated
	// remediation. Default: 1.0. Enforced by brain-dog before invoking
	// `gbrain doctor --remediate --max-usd <cap>`.
	DailyUSDCap float64 `yaml:"daily_usd_cap"`
}

// LoadConfig reads brain.yml from the given town root directory and returns
// a BrainConfig with defaults applied for any missing fields.
// If brain.yml does not exist, returns a default config (not an error) — this
// lets `gt brain init` run before brain.yml is written.
func LoadConfig(townRoot string) (*BrainConfig, error) {
	cfg := defaultConfig()

	path := filepath.Join(townRoot, ConfigFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Apply defaults for zero-valued fields so partial YAML is still safe.
	applyDefaults(cfg)

	return cfg, nil
}

// defaultConfig returns a BrainConfig populated with all defaults.
func defaultConfig() *BrainConfig {
	return &BrainConfig{
		Port:          DefaultPort,
		SearchMode:    DefaultSearchMode,
		GbrainVersion: DefaultGbrainVersion,
		Doctor: DoctorConfig{
			DailyUSDCap: DefaultDoctorDailyUSDCap,
		},
	}
}

// applyDefaults fills zero-valued fields with their defaults.
// This ensures that a partial brain.yml (e.g., only specifying port) still
// produces a fully-usable config.
func applyDefaults(cfg *BrainConfig) {
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.SearchMode == "" {
		cfg.SearchMode = DefaultSearchMode
	}
	if cfg.GbrainVersion == "" {
		cfg.GbrainVersion = DefaultGbrainVersion
	}
	if cfg.Doctor.DailyUSDCap == 0 {
		cfg.Doctor.DailyUSDCap = DefaultDoctorDailyUSDCap
	}
}

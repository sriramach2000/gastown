package capacity

import (
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestIsPolecatReuseAllowed(t *testing.T) {
	tests := []struct {
		name string
		cfg  *SchedulerConfig
		want bool
	}{
		{
			name: "nil config returns false (default: per-job-worktree)",
			cfg:  nil,
			want: false,
		},
		{
			name: "nil AllowPolecatReuse field returns false",
			cfg:  &SchedulerConfig{},
			want: false,
		},
		{
			name: "AllowPolecatReuse=false returns false",
			cfg:  &SchedulerConfig{AllowPolecatReuse: boolPtr(false)},
			want: false,
		},
		{
			name: "AllowPolecatReuse=true returns true (legacy reuse mode)",
			cfg:  &SchedulerConfig{AllowPolecatReuse: boolPtr(true)},
			want: true,
		},
		{
			name: "DefaultSchedulerConfig returns false (AllowPolecatReuse unset)",
			cfg:  DefaultSchedulerConfig(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsPolecatReuseAllowed()
			if got != tt.want {
				t.Errorf("IsPolecatReuseAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPolecatSpawn_AlwaysFreshWhenReuseDisabled verifies that the default config
// (allow_polecat_reuse absent/false) never allows reuse.
// The spawn path in polecat_spawn.go gates FindIdlePolecat behind this check;
// this test confirms the gate value is correct for all default-off scenarios.
func TestPolecatSpawn_AlwaysFreshWhenReuseDisabled(t *testing.T) {
	// Scenario: two beads slung sequentially with allow_polecat_reuse=false (default).
	// Each sling must see IsPolecatReuseAllowed()==false, preventing FindIdlePolecat.

	defaultCfg := DefaultSchedulerConfig() // AllowPolecatReuse is nil
	if defaultCfg.IsPolecatReuseAllowed() {
		t.Error("DefaultSchedulerConfig: IsPolecatReuseAllowed() must be false — "+
			"per-job-worktree model requires reuse off by default")
	}

	absentCfg := &SchedulerConfig{} // explicit empty, no AllowPolecatReuse field
	if absentCfg.IsPolecatReuseAllowed() {
		t.Error("SchedulerConfig{}: IsPolecatReuseAllowed() must be false when field absent")
	}

	explicitFalseCfg := &SchedulerConfig{AllowPolecatReuse: boolPtr(false)}
	if explicitFalseCfg.IsPolecatReuseAllowed() {
		t.Error("allow_polecat_reuse=false: IsPolecatReuseAllowed() must be false")
	}

	// Only when explicitly enabled should reuse be allowed (operator opt-in).
	enabledCfg := &SchedulerConfig{AllowPolecatReuse: boolPtr(true)}
	if !enabledCfg.IsPolecatReuseAllowed() {
		t.Error("allow_polecat_reuse=true: IsPolecatReuseAllowed() must be true (legacy mode)")
	}
}

// TestPolecatCleanup_RemovesWorktreeOnDone is a unit-level gate check:
// verifies the config accessor that gates the per-job-worktree destroy path
// in done.go. The filesystem-level removal is tested by the existing
// TestRemoveWithOptions tests in the polecat package.
func TestPolecatCleanup_RemovesWorktreeOnDone(t *testing.T) {
	// When allow_polecat_reuse is false (default), done.go takes the destroy path.
	// Confirm the config gate that controls this decision returns the right value.
	cfg := DefaultSchedulerConfig()
	if cfg.IsPolecatReuseAllowed() {
		t.Fatal("default config must have reuse disabled — done.go destroy path will not fire")
	}

	// When explicitly enabled, done.go should take the idle-sync path instead.
	cfgEnabled := &SchedulerConfig{AllowPolecatReuse: boolPtr(true)}
	if !cfgEnabled.IsPolecatReuseAllowed() {
		t.Fatal("allow_polecat_reuse=true must enable legacy idle path")
	}
}

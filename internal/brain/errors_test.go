package brain

import (
	"errors"
	"testing"
)

func TestBrainNotRunningError_Error(t *testing.T) {
	err := &BrainNotRunningError{Port: 7891, Err: ErrBrainNotRunning}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	// Must mention the port so operators know which port to target.
	if !contains(msg, "7891") {
		t.Errorf("expected port 7891 in error message, got: %s", msg)
	}
	// Must give an actionable run command.
	if !contains(msg, "gbrain serve --http") {
		t.Errorf("expected remediation command in error message, got: %s", msg)
	}
}

func TestBrainNotRunningError_Unwrap(t *testing.T) {
	err := &BrainNotRunningError{Port: 7891, Err: ErrBrainNotRunning}
	if !errors.Is(err, ErrBrainNotRunning) {
		t.Error("expected errors.Is(err, ErrBrainNotRunning) to be true")
	}
}

func TestBunTooOldError_Error(t *testing.T) {
	err := &BunTooOldError{Detected: "1.2.0", Minimum: "1.3.10"}
	msg := err.Error()
	if !contains(msg, "1.2.0") {
		t.Errorf("expected detected version in error, got: %s", msg)
	}
	if !contains(msg, "1.3.10") {
		t.Errorf("expected minimum version in error, got: %s", msg)
	}
	// Must include install hint.
	if !contains(msg, "bun") {
		t.Errorf("expected install hint in error, got: %s", msg)
	}
}

func TestBunTooOldError_Unwrap(t *testing.T) {
	err := &BunTooOldError{Detected: "1.2.0", Minimum: "1.3.10"}
	if !errors.Is(err, ErrBunTooOld) {
		t.Error("expected errors.Is(err, ErrBunTooOld) to be true")
	}
}

func TestBunMissingError_Error(t *testing.T) {
	err := &BunMissingError{}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	if !contains(msg, "bun") {
		t.Errorf("expected 'bun' in error message, got: %s", msg)
	}
	// Must include install hint.
	if !contains(msg, "brew install bun") && !contains(msg, "bun.sh/install") {
		t.Errorf("expected install instructions in error message, got: %s", msg)
	}
}

func TestBunMissingError_Unwrap(t *testing.T) {
	err := &BunMissingError{}
	if !errors.Is(err, ErrBunMissing) {
		t.Error("expected errors.Is(err, ErrBunMissing) to be true")
	}
}

// contains is a helper used across test files in this package.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

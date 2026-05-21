package brain

import (
	"context"
	"errors"
	"testing"
)

// fakeRunner returns a fixed output and error, simulating `bun --version`.
func fakeRunner(output string, err error) CommandRunner {
	return func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(output), err
	}
}

// fakeLookFound simulates exec.LookPath finding the binary.
func fakeLookFound(_ string) (string, error) { return "/usr/local/bin/bun", nil }

// fakeLookMissing simulates exec.LookPath not finding the binary.
func fakeLookMissing(_ string) (string, error) { return "", errors.New("not found") }

func TestCheckBunVersionWith_OK(t *testing.T) {
	// bun 1.3.10 exactly meets the minimum.
	err := checkBunVersionFull(fakeRunner("1.3.10\n", nil), fakeLookFound)
	if err != nil {
		t.Errorf("expected no error for bun 1.3.10, got: %v", err)
	}
}

func TestCheckBunVersionWith_NewerVersion(t *testing.T) {
	err := checkBunVersionFull(fakeRunner("1.5.0\n", nil), fakeLookFound)
	if err != nil {
		t.Errorf("expected no error for bun 1.5.0, got: %v", err)
	}
}

func TestCheckBunVersionWith_TooOld(t *testing.T) {
	err := checkBunVersionFull(fakeRunner("1.3.9\n", nil), fakeLookFound)
	if err == nil {
		t.Fatal("expected error for bun 1.3.9, got nil")
	}
	if !errors.Is(err, ErrBunTooOld) {
		t.Errorf("expected ErrBunTooOld, got: %v", err)
	}
	var tooOld *BunTooOldError
	if !errors.As(err, &tooOld) {
		t.Errorf("expected *BunTooOldError, got: %T", err)
	}
	if tooOld.Detected != "1.3.9" {
		t.Errorf("expected detected version 1.3.9, got %q", tooOld.Detected)
	}
	if tooOld.Minimum != MinBunVersion {
		t.Errorf("expected minimum version %q, got %q", MinBunVersion, tooOld.Minimum)
	}
}

func TestCheckBunVersionWith_MajorTooOld(t *testing.T) {
	err := checkBunVersionFull(fakeRunner("0.9.0\n", nil), fakeLookFound)
	if !errors.Is(err, ErrBunTooOld) {
		t.Errorf("expected ErrBunTooOld for 0.9.0, got: %v", err)
	}
}

func TestCheckBunVersionWith_RunnerError(t *testing.T) {
	// Runner fails after LookPath succeeds → BunMissingError.
	err := checkBunVersionFull(fakeRunner("", errors.New("exec error")), fakeLookFound)
	if !errors.Is(err, ErrBunMissing) {
		t.Errorf("expected ErrBunMissing when runner fails, got: %v", err)
	}
}

func TestCheckBunVersionWith_BunNotInPath(t *testing.T) {
	err := checkBunVersionFull(fakeRunner("1.5.0\n", nil), fakeLookMissing)
	if !errors.Is(err, ErrBunMissing) {
		t.Errorf("expected ErrBunMissing when not in PATH, got: %v", err)
	}
}

func TestParseBunVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1.3.10\n", "1.3.10"},
		{"1.3.10", "1.3.10"},
		{"2.0.0-canary.1", "2.0.0"},
		{"1.5.0\r\n", "1.5.0"},
		{"  1.4.2  ", "1.4.2"},
		{"bun 1.3.10", ""},    // unexpected prefix → no match
		{"", ""},              // empty
		{"not-a-version", ""}, // garbage
	}

	for _, tt := range tests {
		got := parseBunVersion(tt.input)
		if got != tt.expected {
			t.Errorf("parseBunVersion(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCheckBunVersionWith_ExactMinimum(t *testing.T) {
	// 1.3.10 == MinBunVersion must pass (boundary condition).
	err := checkBunVersionFull(fakeRunner("1.3.10", nil), fakeLookFound)
	if err != nil {
		t.Errorf("exact minimum version 1.3.10 should pass, got: %v", err)
	}
}

func TestCheckBunVersionWith_PatchAboveMinimum(t *testing.T) {
	err := checkBunVersionFull(fakeRunner("1.3.11", nil), fakeLookFound)
	if err != nil {
		t.Errorf("1.3.11 > 1.3.10 should pass, got: %v", err)
	}
}

func TestCheckBunVersionWith_PatchBelowMinimum(t *testing.T) {
	err := checkBunVersionFull(fakeRunner("1.3.9", nil), fakeLookFound)
	if !errors.Is(err, ErrBunTooOld) {
		t.Errorf("1.3.9 < 1.3.10 should fail with ErrBunTooOld, got: %v", err)
	}
}

func TestBunInstallHint_NotEmpty(t *testing.T) {
	if BunInstallHint == "" {
		t.Error("BunInstallHint must not be empty")
	}
}

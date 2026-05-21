// Package brain provides the Go wrapper for the gbrain knowledge layer.
// It exposes two surfaces: an HTTP MCP client for read/query operations
// against a running `gbrain serve --http` sidecar, and CLI shell-out helpers
// for lifecycle operations (init, doctor, migrate, rebuild).
package brain

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the brain package.
var (
	// ErrBrainNotRunning is returned when the gbrain HTTP sidecar is not
	// reachable on the configured port. The caller should advise the operator
	// to run: gbrain serve --http
	ErrBrainNotRunning = errors.New("brain sidecar not running")

	// ErrBunMissing is returned when the bun binary cannot be found in PATH.
	// GT brain init requires Bun ≥1.3.10; no Node.js fallback.
	ErrBunMissing = errors.New("bun binary not found in PATH")

	// ErrBunTooOld is returned when bun is present but below the minimum
	// required version (1.3.10).
	ErrBunTooOld = errors.New("bun version too old")
)

// BrainNotRunningError wraps ErrBrainNotRunning with the configured port
// so callers can print a specific remediation command.
type BrainNotRunningError struct {
	Port int
	Err  error
}

func (e *BrainNotRunningError) Error() string {
	return fmt.Sprintf("%s (port %d) — run: gbrain serve --http --port %d", e.Err, e.Port, e.Port)
}

func (e *BrainNotRunningError) Unwrap() error { return e.Err }

// BunTooOldError wraps ErrBunTooOld with detected and minimum versions.
type BunTooOldError struct {
	Detected string
	Minimum  string
}

func (e *BunTooOldError) Error() string {
	return fmt.Sprintf("%s: detected %s, need ≥%s — %s", ErrBunTooOld, e.Detected, e.Minimum, BunInstallHint)
}

func (e *BunTooOldError) Unwrap() error { return ErrBunTooOld }

// BunMissingError wraps ErrBunMissing with install instructions.
type BunMissingError struct{}

func (e *BunMissingError) Error() string {
	return fmt.Sprintf("%s — %s", ErrBunMissing, BunInstallHint)
}

func (e *BunMissingError) Unwrap() error { return ErrBunMissing }

// BunInstallHint is the actionable install instruction printed when Bun is
// absent or too old.
const BunInstallHint = "install via: brew install bun  OR  curl -fsSL https://bun.sh/install | bash"

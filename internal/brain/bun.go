package brain

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/deps"
)

const (
	// MinBunVersion is the minimum Bun version required by gbrain.
	// Bun ≥1.3.10 is a hard runtime requirement per Gate-1 §Key facts.
	// No Node.js fallback.
	MinBunVersion = "1.3.10"

	// bunVersionTimeout is the maximum time to wait for `bun --version`.
	bunVersionTimeout = 10 * time.Second
)

// bunVersionRE matches the numeric version output from `bun --version`.
// Bun outputs just "1.3.10\n" or "1.3.10-canary.N\n".
var bunVersionRE = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// CommandRunner is a function that executes a named binary with arguments and
// returns combined stdout+stderr. Exposed as a field on Bun* funcs so tests
// can inject a fake runner without spawning real processes.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner executes the binary using os/exec.
func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// CheckBunVersion verifies that Bun ≥1.3.10 is installed.
// Returns nil on success.
// Returns *BunMissingError when the binary is absent from PATH.
// Returns *BunTooOldError when the binary is present but below MinBunVersion.
//
// This function uses the real OS exec. For unit tests that need a fake runner,
// use checkBunVersionWith instead.
func CheckBunVersion() error {
	return checkBunVersionWith(defaultRunner)
}

// checkBunVersionWith is the testable core of CheckBunVersion.
// runner is called with ("bun", "--version"); its output is parsed.
// lookPath mirrors exec.LookPath; pass nil to use the real implementation.
func checkBunVersionWith(runner CommandRunner) error {
	return checkBunVersionFull(runner, exec.LookPath)
}

// checkBunVersionFull is the fully-injectable version used in tests that need
// to fake both the runner AND the PATH lookup.
func checkBunVersionFull(runner CommandRunner, lookPath func(string) (string, error)) error {
	_, err := lookPath("bun")
	if err != nil {
		return &BunMissingError{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), bunVersionTimeout)
	defer cancel()

	output, err := runner(ctx, "bun", "--version")
	if err != nil {
		// bun was found in PATH but failed to run; treat as missing.
		return &BunMissingError{}
	}

	version := parseBunVersion(string(output))
	if version == "" {
		// Unparseable output; treat as missing rather than silently continuing.
		return fmt.Errorf("bun --version output not parseable: %q — %s", strings.TrimSpace(string(output)), BunInstallHint)
	}

	if deps.CompareVersions(version, MinBunVersion) < 0 {
		return &BunTooOldError{
			Detected: version,
			Minimum:  MinBunVersion,
		}
	}

	return nil
}

// parseBunVersion extracts the "X.Y.Z" prefix from `bun --version` output.
// Returns "" if the output does not match the expected pattern.
func parseBunVersion(output string) string {
	output = strings.TrimSpace(output)
	matches := bunVersionRE.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// BunVersionString returns the installed Bun version string, or an error if
// Bun is absent or undetectable. Useful for display in `gt brain doctor`.
func BunVersionString() (string, error) {
	_, err := exec.LookPath("bun")
	if err != nil {
		return "", &BunMissingError{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), bunVersionTimeout)
	defer cancel()

	output, err := defaultRunner(ctx, "bun", "--version")
	if err != nil {
		return "", &BunMissingError{}
	}

	v := parseBunVersion(string(output))
	if v == "" {
		return "", fmt.Errorf("could not parse bun version from: %q", strings.TrimSpace(string(output)))
	}
	return v, nil
}

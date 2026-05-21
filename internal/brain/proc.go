package brain

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// procTimeout is the default timeout for gbrain CLI subprocesses.
	// Lifecycle commands (init, migrate, rebuild) can be slow on first run.
	procTimeout = 5 * time.Minute
)

// ProcResult holds the captured output of a gbrain CLI invocation.
type ProcResult struct {
	// Stdout is the standard output of the command.
	Stdout string
	// Stderr is the standard error of the command.
	Stderr string
	// ExitCode is the process exit code (0 = success).
	ExitCode int
}

// Combined returns stdout and stderr concatenated, which is useful for
// display in `gt brain doctor` output.
func (r *ProcResult) Combined() string {
	parts := make([]string, 0, 2)
	if r.Stdout != "" {
		parts = append(parts, r.Stdout)
	}
	if r.Stderr != "" {
		parts = append(parts, r.Stderr)
	}
	return strings.Join(parts, "\n")
}

// OK returns true when the process exited with code 0.
func (r *ProcResult) OK() bool {
	return r.ExitCode == 0
}

// Proc provides CLI shell-out helpers for gbrain lifecycle operations.
// Each method shells out to the `gbrain` binary (which must be in PATH or
// accessible via the path in BrainConfig) and captures stdout+stderr.
//
// The gbrain binary is invoked with a context that carries procTimeout.
// Callers may pass a shorter context to override the timeout.
type Proc struct {
	// GbrainPath is the path to the gbrain binary. When empty, exec.LookPath
	// is used to find "gbrain" on PATH.
	GbrainPath string

	// runner is the function used to run commands. Defaults to osRunner.
	// Injected in tests to avoid spawning real processes.
	runner procRunner
}

// procRunner abstracts command execution so tests can inject a fake.
type procRunner func(ctx context.Context, name string, args []string, env []string) (*ProcResult, error)

// NewProc creates a Proc that finds `gbrain` via PATH.
func NewProc() *Proc {
	return &Proc{runner: osRunner}
}

// NewProcWithPath creates a Proc using the given absolute path to gbrain.
func NewProcWithPath(gbrainPath string) *Proc {
	return &Proc{GbrainPath: gbrainPath, runner: osRunner}
}

// Init runs `gbrain <townBrainPath> init` to set up the brain directory,
// seed the sources table, and write brain.yml defaults.
// townBrainPath is the path to the town's brain directory (e.g. ~/gt/<town>/brain).
func (p *Proc) Init(ctx context.Context, townBrainPath string) (*ProcResult, error) {
	return p.run(ctx, []string{townBrainPath, "init"}, nil)
}

// Doctor runs `gbrain <townBrainPath> doctor` and returns the health report.
func (p *Proc) Doctor(ctx context.Context, townBrainPath string) (*ProcResult, error) {
	return p.run(ctx, []string{townBrainPath, "doctor"}, nil)
}

// DoctorRemediate runs `gbrain <townBrainPath> doctor --remediate --max-usd <cap>`.
// cap is the daily USD cost ceiling enforced before the call by brain-dog.
func (p *Proc) DoctorRemediate(ctx context.Context, townBrainPath string, cap float64) (*ProcResult, error) {
	return p.run(ctx, []string{
		townBrainPath,
		"doctor",
		"--remediate",
		fmt.Sprintf("--max-usd=%.2f", cap),
	}, nil)
}

// Migrate runs `gbrain <townBrainPath> migrate` to apply pending DB migrations.
func (p *Proc) Migrate(ctx context.Context, townBrainPath string) (*ProcResult, error) {
	return p.run(ctx, []string{townBrainPath, "migrate"}, nil)
}

// Rebuild runs `gbrain <townBrainPath> rebuild --confirm-destructive` to
// drop and recreate the derived DB from the markdown source-of-truth repo.
// This is safe per Gate-1 §system-of-record: the DB is rebuildable.
func (p *Proc) Rebuild(ctx context.Context, townBrainPath string) (*ProcResult, error) {
	return p.run(ctx, []string{townBrainPath, "rebuild", "--confirm-destructive"}, nil)
}

// DreamCycle runs `gbrain <townBrainPath> dream-cycle --non-interactive`.
// Called by the nightly scheduler entry in internal/cmd/brain_scheduler.go.
func (p *Proc) DreamCycle(ctx context.Context, townBrainPath string) (*ProcResult, error) {
	return p.run(ctx, []string{townBrainPath, "dream-cycle", "--non-interactive"}, nil)
}

// run resolves the gbrain binary, applies procTimeout, and executes the command.
func (p *Proc) run(ctx context.Context, args []string, env []string) (*ProcResult, error) {
	binPath, err := p.resolveBin()
	if err != nil {
		return nil, err
	}

	// Wrap context with procTimeout unless caller already set a shorter deadline.
	tCtx, cancel := context.WithTimeout(ctx, procTimeout)
	defer cancel()

	return p.runner(tCtx, binPath, args, env)
}

// resolveBin returns the path to the gbrain binary.
func (p *Proc) resolveBin() (string, error) {
	if p.GbrainPath != "" {
		return p.GbrainPath, nil
	}
	path, err := exec.LookPath("gbrain")
	if err != nil {
		return "", fmt.Errorf("gbrain binary not found in PATH: %w (install via: bun install -g github:sriramach2000/gbrain)", err)
	}
	return path, nil
}

// osRunner is the production implementation of procRunner. It captures
// stdout and stderr separately and returns the exit code.
func osRunner(ctx context.Context, name string, args []string, env []string) (*ProcResult, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: name is resolved via LookPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(env) > 0 {
		cmd.Env = env
	}

	runErr := cmd.Run()

	result := &ProcResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			// Non-zero exit is not a Go error; callers inspect result.OK().
			return result, nil
		}
		// Context timeout or process spawn failure — propagate as Go error.
		return result, fmt.Errorf("running gbrain: %w", runErr)
	}

	return result, nil
}

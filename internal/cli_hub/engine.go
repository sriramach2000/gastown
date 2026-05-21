package cli_hub

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Engine orchestrates catalog lookup, policy checking, installation,
// invocation, and audit for the cli-hub subsystem.
// It is the single entry point called by the cobra subcommand.
type Engine struct {
	Catalog   *CatalogFetcher
	Policy    *PolicyChecker
	Installer *Installer
	Audit     *AuditWriter

	// PolecatName identifies the polecat context for audit records.
	// If empty, uses the hostname as a fallback.
	PolecatName string
}

// NewEngine creates an Engine with default configuration.
// venvPath should be the polecat's .venv directory (e.g. ClonePath + "/.venv").
func NewEngine(venvPath string) *Engine {
	name, _ := os.Hostname()
	return &Engine{
		Catalog:     NewCatalogFetcher(),
		Policy:      NewPolicyChecker(),
		Installer:   NewInstaller(venvPath),
		Audit:       NewAuditWriter(),
		PolecatName: name,
	}
}

// RunResult holds the outcome of a CLI invocation.
type RunResult struct {
	Name       string
	Args       []string
	ExitCode   int
	DurationMs int64
	Stdout     []byte
	Stderr     []byte
}

// ListResult holds catalog+install status for a CLI entry.
type ListResult struct {
	Entry       CLIEntry
	Installed   bool
	VenvPath    string
}

// List returns all catalog entries annotated with install status.
func (e *Engine) List() ([]ListResult, error) {
	cat, err := e.Catalog.Get()
	if err != nil {
		return nil, fmt.Errorf("cli_hub list: %w", err)
	}
	results := make([]ListResult, 0, len(cat.Entries))
	for _, entry := range cat.Entries {
		results = append(results, ListResult{
			Entry:     entry,
			Installed: e.Installer.IsInstalled(entry.Name),
			VenvPath:  e.Installer.VenvPath,
		})
	}
	return results, nil
}

// Search returns catalog entries matching keyword, annotated with install status.
func (e *Engine) Search(keyword string) ([]ListResult, error) {
	cat, err := e.Catalog.Get()
	if err != nil {
		return nil, fmt.Errorf("cli_hub search: %w", err)
	}
	entries := e.Catalog.Search(cat, keyword)
	results := make([]ListResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, ListResult{
			Entry:     entry,
			Installed: e.Installer.IsInstalled(entry.Name),
			VenvPath:  e.Installer.VenvPath,
		})
	}
	return results, nil
}

// Info returns the catalog entry and install status for a named CLI.
func (e *Engine) Info(name string) (*ListResult, error) {
	cat, err := e.Catalog.Get()
	if err != nil {
		return nil, fmt.Errorf("cli_hub info: %w", err)
	}
	entry := e.Catalog.Lookup(cat, name)
	if entry == nil {
		return nil, fmt.Errorf("cli_hub: %q not found in catalog", name)
	}
	return &ListResult{
		Entry:     *entry,
		Installed: e.Installer.IsInstalled(name),
		VenvPath:  e.Installer.VenvPath,
	}, nil
}

// Install runs the full trust-check → venv-ensure → pip-install → audit pipeline.
// Returns an error if the policy denies the request or if install fails.
func (e *Engine) Install(name string) (InstallResult, error) {
	// 1. Policy check
	verdict := e.Policy.Check(name)
	if verdict.Verdict == PolicyDenied {
		event := AuditEvent{
			Ts:      time.Now().UTC(),
			Kind:    AuditKindInstall,
			Name:    name,
			Verdict: string(PolicyDenied) + " " + verdict.Reason,
			Polecat: e.PolecatName,
		}
		_ = e.Audit.Write(event)
		return InstallResult{
			Name:    name,
			Verdict: PolicyDenied,
			Reason:  verdict.Reason,
		}, fmt.Errorf("cli_hub: install denied: %s", verdict.Reason)
	}

	// 2. Catalog presence check (warn but don't block — trust_registry mode)
	cat, err := e.Catalog.Get()
	if err == nil {
		if entry := e.Catalog.Lookup(cat, name); entry == nil {
			fmt.Fprintf(os.Stderr,
				"cli_hub: warning: %q not found in catalog; installing anyway (trust_registry mode)\n", name)
		}
	}

	// 3. pip install
	result, err := e.Installer.Install(name)
	result.Verdict = PolicyApproved
	if err != nil {
		result.Verdict = PolicyDenied
		result.Reason = err.Error()
	}

	// 4. Audit (always, regardless of install outcome)
	verdictStr := string(result.Verdict)
	if result.Reason != "" {
		verdictStr = string(result.Verdict) + " " + result.Reason
	}
	event := AuditEvent{
		Ts:         time.Now().UTC(),
		Kind:       AuditKindInstall,
		Name:       name,
		Verdict:    verdictStr,
		Polecat:    e.PolecatName,
		DurationMs: result.DurationMs,
	}
	_ = e.Audit.Write(event)

	return result, err
}

// Uninstall removes the CLI from the polecat venv and emits an audit record.
func (e *Engine) Uninstall(name string) error {
	start := time.Now()
	err := e.Installer.Uninstall(name)

	event := AuditEvent{
		Ts:         time.Now().UTC(),
		Kind:       AuditKindInstall, // reuse install kind; verdict distinguishes
		Name:       name,
		Verdict:    "UNINSTALLED",
		Polecat:    e.PolecatName,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		event.Verdict = "UNINSTALL_FAILED " + err.Error()
	}
	_ = e.Audit.Write(event)
	return err
}

// Run invokes the installed CLI with the given arguments and returns the result.
// stdout/stderr are forwarded to the process's stdio (Wave 1 — no brain-page redirect yet).
func (e *Engine) Run(name string, args []string) (RunResult, error) {
	start := time.Now()
	result := RunResult{Name: name, Args: args}

	// Locate the CLI binary inside the venv
	binaryPath, err := e.cliPath(name)
	if err != nil {
		result.ExitCode = 1
		result.DurationMs = time.Since(start).Milliseconds()
		return result, fmt.Errorf("cli_hub: %q not installed in venv %s; run `gt cli-hub install %s` first",
			name, e.Installer.VenvPath, name)
	}

	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	result.DurationMs = time.Since(start).Milliseconds()

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	} else if runErr != nil {
		result.ExitCode = 1
	}

	// Audit every invocation
	event := AuditEvent{
		Ts:         time.Now().UTC(),
		Kind:       AuditKindInvoke,
		Name:       name,
		Polecat:    e.PolecatName,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
	}
	_ = e.Audit.Write(event)

	if runErr != nil {
		return result, fmt.Errorf("cli_hub: run %s exit %d: %w", name, result.ExitCode, runErr)
	}
	return result, nil
}

// CatalogRefresh forces a re-fetch of the registry catalog.
func (e *Engine) CatalogRefresh() (*Catalog, error) {
	return e.Catalog.ForceRefresh()
}

// PolicyShow returns the raw content of the trust registry YAML.
func (e *Engine) PolicyShow() (string, error) {
	return e.Policy.ReadPolicy()
}

// PolicyDeny appends name to the denylist.
func (e *Engine) PolicyDeny(name string) error {
	return e.Policy.DenyName(name)
}

// cliPath returns the path to the cli-anything-<name> entry point inside the venv.
func (e *Engine) cliPath(name string) (string, error) {
	// cli-anything packages install as "cli-anything-<name>" entry point
	binName := "cli-anything-" + strings.ToLower(name)
	binPath := filepath.Join(e.Installer.VenvPath, "bin", binName)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}
	// Windows path
	winPath := filepath.Join(e.Installer.VenvPath, "Scripts", binName+".exe")
	if _, err := os.Stat(winPath); err == nil {
		return winPath, nil
	}
	return "", fmt.Errorf("binary %s not found in %s", binName, e.Installer.VenvPath)
}

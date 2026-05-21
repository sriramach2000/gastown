package cli_hub

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InstallResult holds the outcome of a cli-hub install operation.
type InstallResult struct {
	Name       string
	Package    string
	VenvPath   string
	DurationMs int64
	Verdict    PolicyVerdict
	Reason     string
}

// Installer manages pip-based installation into a per-polecat venv.
type Installer struct {
	// VenvPath is the path to the polecat's .venv directory.
	// If empty, uses the current working directory's .venv.
	VenvPath string
}

// NewInstaller creates an Installer targeting the given venv path.
// If venvPath is empty, it falls back to ./.venv in the current directory.
func NewInstaller(venvPath string) *Installer {
	if venvPath == "" {
		cwd, _ := os.Getwd()
		venvPath = filepath.Join(cwd, ".venv")
	}
	return &Installer{VenvPath: venvPath}
}

// EnsureVenv creates the Python virtual environment at VenvPath if it does not exist.
// Sets CLI_HUB_NO_TELEMETRY=1 in the venv activation profile (§D11).
func (i *Installer) EnsureVenv() error {
	if _, err := os.Stat(i.VenvPath); err == nil {
		// Already exists — verify it's a real venv by checking pyvenv.cfg
		cfgPath := filepath.Join(i.VenvPath, "pyvenv.cfg")
		if _, cfgErr := os.Stat(cfgPath); cfgErr == nil {
			return nil // venv looks healthy
		}
	}

	// Create the venv
	python, err := findPython3()
	if err != nil {
		return fmt.Errorf("cli_hub: cannot find python3 to create venv: %w", err)
	}

	cmd := exec.Command(python, "-m", "venv", i.VenvPath)
	cmd.Stdout = os.Stderr // progress to stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cli_hub: venv creation failed at %s: %w", i.VenvPath, err)
	}

	// Verify effect (Block 14 compliance — do not trust exit code alone)
	cfgPath := filepath.Join(i.VenvPath, "pyvenv.cfg")
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("cli_hub: venv creation produced no effect (pyvenv.cfg missing at %s)", i.VenvPath)
	}

	// Set CLI_HUB_NO_TELEMETRY=1 in the venv activate script (§D11)
	if err := i.setNoTelemetry(); err != nil {
		// Non-fatal: log and continue
		fmt.Fprintf(os.Stderr, "cli_hub: warning: could not set CLI_HUB_NO_TELEMETRY: %v\n", err)
	}

	return nil
}

// Install installs the cli-anything-<name> package into the polecat venv.
// It first ensures the venv exists, then runs pip install.
func (i *Installer) Install(name string) (InstallResult, error) {
	start := time.Now()
	pkg := toPackageName(name)

	if err := i.EnsureVenv(); err != nil {
		return InstallResult{Name: name, Package: pkg, VenvPath: i.VenvPath}, err
	}

	pip, err := i.pipPath()
	if err != nil {
		return InstallResult{Name: name, Package: pkg, VenvPath: i.VenvPath}, err
	}

	cmd := exec.Command(pip, "install", pkg)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return InstallResult{
			Name:       name,
			Package:    pkg,
			VenvPath:   i.VenvPath,
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("cli_hub: pip install %s failed: %w", pkg, err)
	}

	// Verify effect: confirm the package appears in site-packages (Block 14)
	if err := i.verifyInstalled(name); err != nil {
		return InstallResult{
			Name:       name,
			Package:    pkg,
			VenvPath:   i.VenvPath,
			DurationMs: time.Since(start).Milliseconds(),
		}, fmt.Errorf("cli_hub: pip install produced no effect for %s: %w", pkg, err)
	}

	return InstallResult{
		Name:       name,
		Package:    pkg,
		VenvPath:   i.VenvPath,
		DurationMs: time.Since(start).Milliseconds(),
		Verdict:    PolicyApproved,
	}, nil
}

// Uninstall removes the cli-anything-<name> package from the polecat venv.
func (i *Installer) Uninstall(name string) error {
	pkg := toPackageName(name)
	pip, err := i.pipPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(pip, "uninstall", "-y", pkg)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IsInstalled returns true if cli-anything-<name> is present in the venv.
func (i *Installer) IsInstalled(name string) bool {
	return i.verifyInstalled(name) == nil
}

// verifyInstalled checks that the package's site-packages dir or dist-info exists.
func (i *Installer) verifyInstalled(name string) error {
	// Use pip show as the canonical check
	pip, err := i.pipPath()
	if err != nil {
		return err
	}
	pkg := toPackageName(name)
	cmd := exec.Command(pip, "show", pkg)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("package %s not found in venv %s", pkg, i.VenvPath)
	}
	return nil
}

// setNoTelemetry appends CLI_HUB_NO_TELEMETRY=1 to the venv activate script.
func (i *Installer) setNoTelemetry() error {
	// Activate scripts differ by platform; try the POSIX path first
	activatePaths := []string{
		filepath.Join(i.VenvPath, "bin", "activate"),
		filepath.Join(i.VenvPath, "Scripts", "activate"),
	}
	for _, ap := range activatePaths {
		if _, err := os.Stat(ap); err == nil {
			f, err := os.OpenFile(ap, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = fmt.Fprintln(f, "\nexport CLI_HUB_NO_TELEMETRY=1")
			return err
		}
	}
	return fmt.Errorf("no activate script found in %s", i.VenvPath)
}

// pipPath returns the path to the pip binary inside the venv.
func (i *Installer) pipPath() (string, error) {
	candidates := []string{
		filepath.Join(i.VenvPath, "bin", "pip"),
		filepath.Join(i.VenvPath, "bin", "pip3"),
		filepath.Join(i.VenvPath, "Scripts", "pip.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("cli_hub: pip not found in venv %s", i.VenvPath)
}

// toPackageName converts a CLI name to its PyPI package name.
func toPackageName(name string) string {
	name = strings.ToLower(name)
	if strings.HasPrefix(name, "cli-anything-") {
		return name
	}
	return "cli-anything-" + name
}

// findPython3 locates a suitable python3 binary on PATH.
func findPython3() (string, error) {
	for _, candidate := range []string{"python3", "python3.12", "python3.11", "python3.10", "python"} {
		if p, err := exec.LookPath(candidate); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("python3 (>=3.10) not found on PATH")
}

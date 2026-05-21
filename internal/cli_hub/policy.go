package cli_hub

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyMode controls how the trust registry evaluates an install request.
type PolicyMode string

const (
	PolicyModeTrustRegistry  PolicyMode = "trust_registry"
	PolicyModeExplicitAllowlist PolicyMode = "explicit_allowlist"
)

// PolicyVerdict is the result of a policy check.
type PolicyVerdict string

const (
	PolicyApproved PolicyVerdict = "APPROVED"
	PolicyDenied   PolicyVerdict = "DENIED"
	PolicyUnknown  PolicyVerdict = "UNKNOWN"
)

// PolicyResult holds the verdict + reason string.
type PolicyResult struct {
	Verdict PolicyVerdict
	Reason  string // populated when Verdict != APPROVED
}

func (r PolicyResult) String() string {
	if r.Verdict == PolicyApproved {
		return string(PolicyApproved)
	}
	return fmt.Sprintf("%s %s", r.Verdict, r.Reason)
}

// allowlistYAML mirrors the schema of cli_hub_allowlist.yaml.
type allowlistYAML struct {
	Policy struct {
		Mode        PolicyMode `yaml:"mode"`
		RegistryURL string     `yaml:"registry_url"`
		Rationale   string     `yaml:"rationale"`
	} `yaml:"policy"`
	Allowlisted []string `yaml:"allowlisted"`
	Denylist    []string `yaml:"denylist"`
}

// PolicyChecker evaluates install requests against the trust registry.
type PolicyChecker struct {
	// AllowlistPath is the path to cli_hub_allowlist.yaml.
	// Defaults to ~/gt/.policy/cli_hub_allowlist.yaml.
	AllowlistPath string

	// CheckScriptPath is the path to the Python check.sh.
	// Defaults to ~/gt/.policy/check.sh.
	CheckScriptPath string
}

// NewPolicyChecker creates a PolicyChecker with default paths.
func NewPolicyChecker() *PolicyChecker {
	home := os.Getenv("HOME")
	policyDir := filepath.Join(home, "gt", ".policy")
	return &PolicyChecker{
		AllowlistPath:   filepath.Join(policyDir, "cli_hub_allowlist.yaml"),
		CheckScriptPath: filepath.Join(policyDir, "check.sh"),
	}
}

// Check evaluates whether name is allowed to be installed.
//
// The check order:
//  1. Try the Python check.sh script.
//  2. If check.sh exits 2 (PyYAML missing — §gap 2), fall back to Go-native YAML parse.
//  3. If check.sh is absent entirely, fall back to Go-native YAML parse.
func (p *PolicyChecker) Check(name string) PolicyResult {
	// Try the Python script first
	result, scriptErr, exitCode := p.runCheckScript(name)
	if scriptErr == nil {
		return result
	}
	// Exit code 2 = PyYAML import error (§A7, §gap 2 fallback)
	if exitCode == 2 || isExitCode(scriptErr, 2) {
		return p.checkGoNative(name)
	}
	// Script absent or not executable — fall back to Go-native
	if os.IsNotExist(scriptErr) || isPermError(scriptErr) {
		return p.checkGoNative(name)
	}
	// Other script errors: propagate as UNKNOWN
	return PolicyResult{
		Verdict: PolicyUnknown,
		Reason:  fmt.Sprintf("policy check script error: %v", scriptErr),
	}
}

// ReadPolicy loads and returns the raw allowlist YAML for display.
func (p *PolicyChecker) ReadPolicy() (string, error) {
	data, err := os.ReadFile(p.AllowlistPath)
	if err != nil {
		return "", fmt.Errorf("cli_hub: reading allowlist %s: %w", p.AllowlistPath, err)
	}
	return string(data), nil
}

// DenyName appends name to the denylist in the allowlist YAML file.
func (p *PolicyChecker) DenyName(name string) error {
	al, err := p.loadAllowlistYAML()
	if err != nil {
		return err
	}
	for _, d := range al.Denylist {
		if strings.EqualFold(d, name) {
			return nil // already denied
		}
	}
	al.Denylist = append(al.Denylist, name)
	return p.saveAllowlistYAML(al)
}

// runCheckScript runs the Python check.sh and returns (result, error, exitCode).
func (p *PolicyChecker) runCheckScript(name string) (PolicyResult, error, int) {
	if _, err := os.Stat(p.CheckScriptPath); os.IsNotExist(err) {
		return PolicyResult{}, err, -1
	}
	cmd := exec.Command("bash", p.CheckScriptPath, name)
	out, err := cmd.Output()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			code := exitErr.ExitCode()
			if code == 1 {
				// Explicit deny from the script
				return PolicyResult{
					Verdict: PolicyDenied,
					Reason:  fmt.Sprintf("POLICY HIGH: cli_hub_install of %q is on the denylist", name),
				}, nil, 1
			}
			return PolicyResult{}, err, code
		}
		return PolicyResult{}, err, -1
	}
	output := strings.TrimSpace(string(out))
	_ = output
	return PolicyResult{Verdict: PolicyApproved}, nil, 0
}

// checkGoNative applies the policy rules using Go's YAML parser.
// This closes §gap 2: silent-skip when PyYAML is missing.
func (p *PolicyChecker) checkGoNative(name string) PolicyResult {
	al, err := p.loadAllowlistYAML()
	if err != nil {
		// If we can't read the policy file at all, fail open with UNKNOWN
		return PolicyResult{
			Verdict: PolicyUnknown,
			Reason:  fmt.Sprintf("cannot read allowlist: %v", err),
		}
	}

	nameLower := strings.ToLower(name)

	// Denylist always overrides
	for _, d := range al.Denylist {
		if strings.ToLower(d) == nameLower {
			return PolicyResult{
				Verdict: PolicyDenied,
				Reason:  fmt.Sprintf("POLICY HIGH: cli_hub_install of %q is on the denylist", name),
			}
		}
	}

	switch al.Policy.Mode {
	case PolicyModeTrustRegistry, "":
		// In trust_registry mode, anything not on the denylist is APPROVED
		return PolicyResult{Verdict: PolicyApproved}

	case PolicyModeExplicitAllowlist:
		// Must appear in allowlisted list
		for _, a := range al.Allowlisted {
			if strings.ToLower(a) == nameLower {
				return PolicyResult{Verdict: PolicyApproved}
			}
		}
		return PolicyResult{
			Verdict: PolicyDenied,
			Reason:  fmt.Sprintf("%q not in explicit allowlist; file an escalation to add it", name),
		}

	default:
		return PolicyResult{
			Verdict: PolicyUnknown,
			Reason:  fmt.Sprintf("unknown policy mode %q", al.Policy.Mode),
		}
	}
}

// loadAllowlistYAML reads and parses the allowlist YAML file.
func (p *PolicyChecker) loadAllowlistYAML() (*allowlistYAML, error) {
	data, err := os.ReadFile(p.AllowlistPath)
	if err != nil {
		return nil, err
	}
	var al allowlistYAML
	if err := yaml.Unmarshal(data, &al); err != nil {
		return nil, fmt.Errorf("parsing allowlist YAML: %w", err)
	}
	return &al, nil
}

// saveAllowlistYAML writes the allowlist YAML back to disk.
func (p *PolicyChecker) saveAllowlistYAML(al *allowlistYAML) error {
	data, err := yaml.Marshal(al)
	if err != nil {
		return err
	}
	return os.WriteFile(p.AllowlistPath, data, 0o600)
}

// isExitCode returns true if err is an *exec.ExitError with the given code.
func isExitCode(err error, code int) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode() == code
	}
	return false
}

// isPermError returns true if err is a permission-denied error.
func isPermError(err error) bool {
	return strings.Contains(err.Error(), "permission denied")
}

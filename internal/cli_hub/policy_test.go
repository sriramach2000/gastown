package cli_hub

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicyYAML(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "cli_hub_allowlist.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing policy YAML: %v", err)
	}
	return path
}

func newTestChecker(t *testing.T, yamlContent string) *PolicyChecker {
	t.Helper()
	dir := t.TempDir()
	path := writePolicyYAML(t, dir, yamlContent)
	return &PolicyChecker{
		AllowlistPath:   path,
		CheckScriptPath: filepath.Join(dir, "check.sh"), // does not exist → Go fallback
	}
}

func TestPolicyChecker_TrustRegistryMode_ApprovedByDefault(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: trust_registry
allowlisted: []
denylist: []
`)
	result := checker.checkGoNative("blender")
	if result.Verdict != PolicyApproved {
		t.Errorf("expected APPROVED in trust_registry mode, got %v: %s", result.Verdict, result.Reason)
	}
}

func TestPolicyChecker_TrustRegistryMode_DeniedWhenOnDenylist(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: trust_registry
allowlisted: []
denylist:
  - malicious-cli
  - banned-tool
`)
	result := checker.checkGoNative("malicious-cli")
	if result.Verdict != PolicyDenied {
		t.Errorf("expected DENIED for denylist entry, got %v", result.Verdict)
	}
	if result.Reason == "" {
		t.Error("expected non-empty reason for denial")
	}
}

func TestPolicyChecker_TrustRegistryMode_DenylistCaseInsensitive(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: trust_registry
allowlisted: []
denylist:
  - BadTool
`)
	result := checker.checkGoNative("badtool")
	if result.Verdict != PolicyDenied {
		t.Errorf("expected DENIED for case-insensitive denylist match, got %v", result.Verdict)
	}
}

func TestPolicyChecker_ExplicitAllowlistMode_ApprovedWhenListed(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: explicit_allowlist
allowlisted:
  - blender
  - gimp
denylist: []
`)
	result := checker.checkGoNative("blender")
	if result.Verdict != PolicyApproved {
		t.Errorf("expected APPROVED for allowlisted entry, got %v", result.Verdict)
	}
}

func TestPolicyChecker_ExplicitAllowlistMode_DeniedWhenNotListed(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: explicit_allowlist
allowlisted:
  - blender
denylist: []
`)
	result := checker.checkGoNative("exa")
	if result.Verdict != PolicyDenied {
		t.Errorf("expected DENIED for non-allowlisted entry, got %v", result.Verdict)
	}
}

func TestPolicyChecker_ExplicitAllowlistMode_DenylistOverridesAllowlist(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: explicit_allowlist
allowlisted:
  - evil-cli
denylist:
  - evil-cli
`)
	// Denylist takes precedence over allowlist
	result := checker.checkGoNative("evil-cli")
	if result.Verdict != PolicyDenied {
		t.Errorf("expected DENIED (denylist overrides allowlist), got %v", result.Verdict)
	}
}

func TestPolicyChecker_UnknownMode(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: weird_mode
allowlisted: []
denylist: []
`)
	result := checker.checkGoNative("anytool")
	if result.Verdict != PolicyUnknown {
		t.Errorf("expected UNKNOWN for unknown mode, got %v", result.Verdict)
	}
}

func TestPolicyChecker_MissingAllowlistFile(t *testing.T) {
	checker := &PolicyChecker{
		AllowlistPath:   "/nonexistent/cli_hub_allowlist.yaml",
		CheckScriptPath: "/nonexistent/check.sh",
	}
	result := checker.checkGoNative("blender")
	if result.Verdict != PolicyUnknown {
		t.Errorf("expected UNKNOWN when allowlist missing, got %v", result.Verdict)
	}
}

func TestPolicyChecker_DenyName_AppendsToDenylist(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: trust_registry
allowlisted: []
denylist: []
`)
	if err := checker.DenyName("new-bad-cli"); err != nil {
		t.Fatalf("DenyName: %v", err)
	}

	// Re-read and verify
	result := checker.checkGoNative("new-bad-cli")
	if result.Verdict != PolicyDenied {
		t.Errorf("expected DENIED after DenyName, got %v", result.Verdict)
	}
}

func TestPolicyChecker_DenyName_Idempotent(t *testing.T) {
	checker := newTestChecker(t, `
policy:
  mode: trust_registry
allowlisted: []
denylist:
  - already-denied
`)
	// Denying something already denied should not error
	if err := checker.DenyName("already-denied"); err != nil {
		t.Fatalf("DenyName idempotent: %v", err)
	}
}

func TestPolicyChecker_ReadPolicy_ReturnsYAML(t *testing.T) {
	content := `
policy:
  mode: trust_registry
allowlisted: []
denylist: []
`
	checker := newTestChecker(t, content)
	got, err := checker.ReadPolicy()
	if err != nil {
		t.Fatalf("ReadPolicy: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty policy YAML")
	}
}

func TestPolicyChecker_CheckGoNative_FallbackForPyYAMLExit2(t *testing.T) {
	// Simulate the §gap 2 scenario: check.sh present but exits 2 (PyYAML missing)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "check.sh")
	// Script that always exits 2
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("writing check.sh: %v", err)
	}
	yamlPath := writePolicyYAML(t, dir, `
policy:
  mode: trust_registry
allowlisted: []
denylist: []
`)
	checker := &PolicyChecker{
		AllowlistPath:   yamlPath,
		CheckScriptPath: scriptPath,
	}
	// Check() should fall back to Go-native and return APPROVED
	result := checker.Check("blender")
	if result.Verdict != PolicyApproved {
		t.Errorf("expected APPROVED after exit-2 fallback, got %v: %s", result.Verdict, result.Reason)
	}
}

func TestPolicyResult_String(t *testing.T) {
	approved := PolicyResult{Verdict: PolicyApproved}
	if approved.String() != "APPROVED" {
		t.Errorf("unexpected String() for APPROVED: %s", approved.String())
	}

	denied := PolicyResult{Verdict: PolicyDenied, Reason: "on denylist"}
	if denied.String() != "DENIED on denylist" {
		t.Errorf("unexpected String() for DENIED: %s", denied.String())
	}
}

package cmd

import "testing"

// TestRoleRequiresWorktreeIntegrity locks down which roles demand a real git
// worktree via worktreeintegrity.Validate. Witness is excluded — per
// internal/rig/manager.go:817 the witness directory is created with no clone,
// and per internal/config/roles/witness.toml the witness work_dir has no /rig
// subpath. A prior version of this switch incorrectly listed RoleWitness, which
// caused every `gt prime --hook` on a witness role to fail with a misleading
// "missing .git metadata" error. Regression for BUG-0004 in the xl4 bug catalog.
func TestRoleRequiresWorktreeIntegrity(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		// Roles that DO have a git worktree (require .git metadata).
		{RolePolecat, true},
		{RoleCrew, true},
		{RoleRefinery, true},
		{RoleDog, true},
		{RoleBoot, true},

		// Roles that do NOT have a git worktree.
		{RoleWitness, false}, // BUG-0004 regression: must be false, not true.
		{RoleMayor, false},   // Mayor has its own clone path, not checked via this gate.
		{RoleDeacon, false},
		{RoleUnknown, false},

		// Sanity: an unrecognized role string must be safe (default-deny).
		{Role("not-a-real-role"), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.role), func(t *testing.T) {
			got := roleRequiresWorktreeIntegrity(tc.role)
			if got != tc.want {
				t.Fatalf("roleRequiresWorktreeIntegrity(%q) = %v, want %v",
					tc.role, got, tc.want)
			}
		})
	}
}

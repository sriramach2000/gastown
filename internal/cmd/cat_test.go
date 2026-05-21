package cmd

import "testing"

func TestIsBeadID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid bead IDs - all known prefixes
		{"gt-abc123", true},
		{"bd-xyz", true},
		{"hq-foo", true},
		{"mol-bar", true},
		{"wisp-baz", true},
		{"dolt-qux", true},
		{"sky-abc", true},
		{"wy-def", true},
		// Multi-segment prefixes
		{"hq-cv-foo", true},
		// Rig-local prefixes with digits (registered in <town>/.beads/routes.jsonl).
		// Regression for 2026-05-21 'gt hook attach xl4-t1c' rejection.
		{"xl4-t1c", true},
		{"j7-abc", true},
		{"gp-1-foo", true},
		{"wyv-9", true},
		// Invalid inputs
		{"", false},
		{"-abc", false},
		{"abc", false},
		{"ABC-123", false},
		{"123-abc", false},
		{"gt-", false},
		{"-", false},
		{"4xl-t1c", false}, // prefix must START with a letter
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isBeadID(tt.input)
			if got != tt.want {
				t.Errorf("isBeadID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

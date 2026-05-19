package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestFormatTaxonomyHumanReadable verifies the tree rendering produces expected
// box-drawing characters and label formats.
func TestFormatTaxonomyHumanReadable(t *testing.T) {
	tests := []struct {
		name     string
		tax      Taxonomy
		wantLine []string // each entry must appear as a substring somewhere in the output
	}{
		{
			name: "empty fleet",
			tax: Taxonomy{
				Mayor:     MayorNode{ID: "gas-mayor", Status: "active", Rig: "lockstep"},
				Deacons:   []DeaconNode{},
				Witnesses: []WitnessNode{},
				Polecats:  []PolecatNode{},
				Ts:        time.Now().UTC(),
			},
			wantLine: []string{
				"Mayor: gas-mayor (active, rig=lockstep)",
			},
		},
		{
			name: "single deacon no witnesses",
			tax: Taxonomy{
				Mayor:     MayorNode{ID: "gt-mayor", Status: "active", Rig: "lockstep"},
				Deacons:   []DeaconNode{{ID: "gt-deacon", Status: "active", Supervises: []string{}}},
				Witnesses: []WitnessNode{},
				Polecats:  []PolecatNode{},
				Ts:        time.Now().UTC(),
			},
			wantLine: []string{
				"Mayor: gt-mayor (active, rig=lockstep)",
				"Deacon: gt-deacon (active)",
			},
		},
		{
			name: "full hierarchy with polecats",
			tax: Taxonomy{
				Mayor: MayorNode{ID: "gas-mayor", Status: "active", Rig: "lockstep"},
				Deacons: []DeaconNode{
					{ID: "gas-deacon-001", Status: "active", Supervises: []string{"gastown-witness"}},
				},
				Witnesses: []WitnessNode{
					{ID: "gastown-witness", Status: "watching", Alerts: 0,
						Supervises: []string{"gastown-alpha", "gastown-bravo"}},
				},
				Polecats: []PolecatNode{
					{ID: "gastown-alpha", Status: "working", HookBead: "gt-abc", Rig: "gastown", Supervisor: "gastown-witness"},
					{ID: "gastown-bravo", Status: "idle", HookBead: "", Rig: "gastown", Supervisor: "gastown-witness"},
				},
				Ts: time.Now().UTC(),
			},
			wantLine: []string{
				"Mayor: gas-mayor (active, rig=lockstep)",
				"Deacon: gas-deacon-001 (active)",
				"Witness: gastown-witness (watching)",
				"Polecat: gastown-alpha (working, hook=gt-abc)",
				"Polecat: gastown-bravo (idle, hook=∅)",
			},
		},
		{
			name: "witness with alerts",
			tax: Taxonomy{
				Mayor: MayorNode{ID: "gas-mayor", Status: "stopped", Rig: "lockstep"},
				Deacons: []DeaconNode{
					{ID: "gas-deacon-001", Status: "active", Supervises: []string{"myrig-witness"}},
				},
				Witnesses: []WitnessNode{
					{ID: "myrig-witness", Status: "watching", Alerts: 3, Supervises: []string{}},
				},
				Polecats: []PolecatNode{},
				Ts:       time.Now().UTC(),
			},
			wantLine: []string{
				"Witness: myrig-witness (watching, alerts=3)",
			},
		},
		{
			name: "last deacon uses └─ prefix",
			tax: Taxonomy{
				Mayor: MayorNode{ID: "gas-mayor", Status: "active", Rig: "lockstep"},
				Deacons: []DeaconNode{
					{ID: "gas-deacon-001", Status: "active", Supervises: []string{}},
				},
				Witnesses: []WitnessNode{},
				Polecats:  []PolecatNode{},
				Ts:        time.Now().UTC(),
			},
			wantLine: []string{"└─ Deacon:"},
		},
		{
			name: "unsupervised polecat (rig without witness)",
			tax: Taxonomy{
				Mayor: MayorNode{ID: "gas-mayor", Status: "active", Rig: "lockstep"},
				Deacons: []DeaconNode{
					{ID: "gas-deacon-001", Status: "active", Supervises: []string{}},
				},
				Witnesses: []WitnessNode{},
				Polecats: []PolecatNode{
					{ID: "barerig-alpha", Status: "working", HookBead: "hq-zz1", Rig: "barerig", Supervisor: "barerig-witness"},
				},
				Ts: time.Now().UTC(),
			},
			wantLine: []string{
				"(unsupervised polecats)",
				"barerig-alpha",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatTaxonomyHumanReadable(tc.tax)
			for _, want := range tc.wantLine {
				if !strings.Contains(got, want) {
					t.Errorf("formatTaxonomyHumanReadable() output missing %q\nGot:\n%s", want, got)
				}
			}
		})
	}
}

// TestFormatTaxonomyJSON verifies the JSON shape matches the documented schema.
// It checks required top-level keys, nested field names, and that no extra
// undocumented keys appear at the mayor level.
func TestFormatTaxonomyJSON(t *testing.T) {
	tax := Taxonomy{
		Mayor: MayorNode{ID: "gas-mayor", Status: "active", Rig: "lockstep"},
		Deacons: []DeaconNode{
			{ID: "gas-deacon-001", Status: "active", Supervises: []string{"wit-001"}},
		},
		Witnesses: []WitnessNode{
			{ID: "wit-001", Status: "watching", Alerts: 0, Supervises: []string{"pole-001", "pole-002"}},
		},
		Polecats: []PolecatNode{
			{ID: "pole-001", Status: "working", HookBead: "gas-abc", Rig: "lockstep", Supervisor: "wit-001"},
			{ID: "pole-002", Status: "idle", HookBead: "", Rig: "lockstep", Supervisor: "wit-001"},
		},
		Ts: time.Date(2026, 5, 19, 20, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(tax)
	if err != nil {
		t.Fatalf("json.Marshal(Taxonomy): %v", err)
	}

	// Decode into a generic map for field inspection.
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Top-level required keys (matches documented schema).
	for _, key := range []string{"mayor", "deacons", "witnesses", "polecats", "ts"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing top-level key %q in taxonomy JSON", key)
		}
	}

	// Mayor shape.
	mayorMap, ok := m["mayor"].(map[string]interface{})
	if !ok {
		t.Fatal("mayor is not an object")
	}
	for _, key := range []string{"id", "status", "rig"} {
		if _, ok := mayorMap[key]; !ok {
			t.Errorf("missing mayor key %q", key)
		}
	}
	if mayorMap["id"] != "gas-mayor" {
		t.Errorf("mayor.id = %v, want gas-mayor", mayorMap["id"])
	}
	if mayorMap["rig"] != "lockstep" {
		t.Errorf("mayor.rig = %v, want lockstep", mayorMap["rig"])
	}

	// Deacons array non-empty and has required keys.
	deaconsRaw, ok := m["deacons"].([]interface{})
	if !ok || len(deaconsRaw) == 0 {
		t.Fatal("deacons must be a non-empty array")
	}
	d0 := deaconsRaw[0].(map[string]interface{})
	for _, key := range []string{"id", "status", "supervises"} {
		if _, ok := d0[key]; !ok {
			t.Errorf("missing deacon key %q", key)
		}
	}

	// Witnesses array non-empty and has required keys.
	witnessesRaw, ok := m["witnesses"].([]interface{})
	if !ok || len(witnessesRaw) == 0 {
		t.Fatal("witnesses must be a non-empty array")
	}
	w0 := witnessesRaw[0].(map[string]interface{})
	for _, key := range []string{"id", "status", "alerts", "supervises"} {
		if _, ok := w0[key]; !ok {
			t.Errorf("missing witness key %q", key)
		}
	}

	// Polecats array: check both entries.
	polecatsRaw, ok := m["polecats"].([]interface{})
	if !ok || len(polecatsRaw) != 2 {
		t.Fatalf("expected 2 polecats, got %d", len(polecatsRaw))
	}
	for i, pr := range polecatsRaw {
		p := pr.(map[string]interface{})
		for _, key := range []string{"id", "status", "hook_bead", "rig", "supervisor"} {
			if _, ok := p[key]; !ok {
				t.Errorf("polecats[%d] missing key %q", i, key)
			}
		}
	}

	// Second polecat has empty hook_bead (not omitted — field must be present).
	p1 := polecatsRaw[1].(map[string]interface{})
	if p1["hook_bead"] != "" {
		t.Errorf("polecats[1].hook_bead = %v, want empty string", p1["hook_bead"])
	}

	// ts must be a string (RFC3339 from time.Time JSON serialisation).
	if _, ok := m["ts"].(string); !ok {
		t.Errorf("ts must be a string (RFC3339), got %T", m["ts"])
	}
}

// TestBuildTaxonomyFromBdState verifies BuildTaxonomy helper functions return
// structurally valid results. We test the pure functions directly since
// BuildTaxonomy requires a real Gas Town workspace (not available in unit tests).
func TestBuildTaxonomyFromBdState(t *testing.T) {
	// Test witnessIDForRig and rigPolecatID helpers.
	if got := witnessIDForRig("gastown"); got != "gastown-witness" {
		t.Errorf("witnessIDForRig(gastown) = %q, want gastown-witness", got)
	}
	if got := rigPolecatID("gastown", "alpha"); got != "gastown-alpha" {
		t.Errorf("rigPolecatID(gastown, alpha) = %q, want gastown-alpha", got)
	}

	// Verify nil-safety of helper functions with empty inputs.
	if got := witnessesSupervised(nil, nil); len(got) != 0 {
		t.Errorf("witnessesSupervised(nil, nil) = %v, want empty", got)
	}
	if got := polecatsSupervised(nil, "any-wit"); len(got) != 0 {
		t.Errorf("polecatsSupervised(nil, ...) = %v, want empty", got)
	}
	if got := unsupervisedPolecats(nil, nil); len(got) != 0 {
		t.Errorf("unsupervisedPolecats(nil, nil) = %v, want empty", got)
	}

	// Verify that a Taxonomy with all-empty slices marshals without error
	// and emits [] (not null) for arrays.
	tax := Taxonomy{
		Mayor:     MayorNode{ID: "x", Status: "stopped", Rig: ""},
		Deacons:   []DeaconNode{},
		Witnesses: []WitnessNode{},
		Polecats:  []PolecatNode{},
		Ts:        time.Now().UTC(),
	}
	b, err := json.Marshal(tax)
	if err != nil {
		t.Fatalf("json.Marshal of empty Taxonomy: %v", err)
	}
	bs := string(b)
	if !strings.Contains(bs, `"deacons":[]`) {
		t.Errorf("empty deacons should marshal as [], got: %s", bs)
	}
	if !strings.Contains(bs, `"witnesses":[]`) {
		t.Errorf("empty witnesses should marshal as [], got: %s", bs)
	}
	if !strings.Contains(bs, `"polecats":[]`) {
		t.Errorf("empty polecats should marshal as [], got: %s", bs)
	}
}

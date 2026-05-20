package witness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/events"
)

// TestEventEmitter_WritesJSONL verifies that WriteEventJSONL appends a valid
// JSONL line that parseEventLine can round-trip back to a PolecatEvent.
func TestEventEmitter_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".events.jsonl")

	ev := events.Event{
		Timestamp:  "2026-05-19T18:00:00Z",
		Source:     "gt",
		Type:       events.TypeSpawn,
		Actor:      "gt",
		Payload:    events.SpawnPayload("planner", "obsidian"),
		Visibility: events.VisibilityFeed,
	}

	if err := WriteEventJSONL(path, ev); err != nil {
		t.Fatalf("WriteEventJSONL: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	line := string(data)
	// Should be a single JSONL line
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}

	var roundTrip events.Event
	if err := json.Unmarshal([]byte(line), &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal round-trip: %v", err)
	}
	if roundTrip.Type != events.TypeSpawn {
		t.Errorf("Type = %q, want %q", roundTrip.Type, events.TypeSpawn)
	}
	if got := roundTrip.Payload["polecat"]; got != "obsidian" {
		t.Errorf("payload.polecat = %v, want %q", got, "obsidian")
	}
}

// TestEventEmitter_EmitsSpawnEvent verifies that the emitter picks up a spawn
// event written to the JSONL file and delivers it on the output channel.
func TestEventEmitter_EmitsSpawnEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".events.jsonl")

	// Pre-create the file so the fsnotify watcher can watch the parent dir.
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	_ = f.Close()

	out := make(chan PolecatEvent, 8)
	emitter := NewEventEmitter(path, out)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go emitter.Run(ctx)

	// Give the watcher goroutine a moment to start watching.
	time.Sleep(50 * time.Millisecond)

	ev := events.Event{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "gt",
		Type:       events.TypeSpawn,
		Actor:      "gt",
		Payload:    events.SpawnPayload("planner", "obsidian"),
		Visibility: events.VisibilityFeed,
	}
	if err := WriteEventJSONL(path, ev); err != nil {
		t.Fatalf("WriteEventJSONL: %v", err)
	}

	select {
	case pe := <-out:
		if pe.EventType != "polecat-spawned" {
			t.Errorf("EventType = %q, want %q", pe.EventType, "polecat-spawned")
		}
		if pe.State != "spawned" {
			t.Errorf("State = %q, want %q", pe.State, "spawned")
		}
		if pe.PolecatID != "obsidian" {
			t.Errorf("PolecatID = %q, want %q", pe.PolecatID, "obsidian")
		}
		if pe.Rig != "planner" {
			t.Errorf("Rig = %q, want %q", pe.Rig, "planner")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for polecat-spawned event from emitter")
	}
}

// TestParseEventLine_SpawnFromPayload verifies that spawn events with payload
// fields produce the correct PolecatEvent shape.
func TestParseEventLine_SpawnFromPayload(t *testing.T) {
	ev := events.Event{
		Timestamp:  "2026-05-19T18:00:00Z",
		Source:     "gt",
		Type:       events.TypeSpawn,
		Actor:      "gt",
		Payload:    events.SpawnPayload("myrig", "toast"),
		Visibility: events.VisibilityFeed,
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	pe, ok := parseEventLine(string(data))
	if !ok {
		t.Fatal("parseEventLine returned ok=false for spawn event")
	}
	if pe.EventType != "polecat-spawned" {
		t.Errorf("EventType = %q, want polecat-spawned", pe.EventType)
	}
	if pe.PolecatID != "toast" {
		t.Errorf("PolecatID = %q, want toast", pe.PolecatID)
	}
	if pe.Rig != "myrig" {
		t.Errorf("Rig = %q, want myrig", pe.Rig)
	}
	if pe.TS != "2026-05-19T18:00:00Z" {
		t.Errorf("TS = %q, want 2026-05-19T18:00:00Z", pe.TS)
	}
}

// TestParseEventLine_HookFromActor verifies that hook events fall back to
// actor-based identity extraction when polecat is not in the payload.
func TestParseEventLine_HookFromActor(t *testing.T) {
	ev := events.Event{
		Timestamp:  "2026-05-19T18:01:00Z",
		Source:     "gt",
		Type:       events.TypeHook,
		Actor:      "planner/polecats/obsidian",
		Payload:    events.HookPayload("pp-bzs"),
		Visibility: events.VisibilityFeed,
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	pe, ok := parseEventLine(string(data))
	if !ok {
		t.Fatal("parseEventLine returned ok=false for hook event")
	}
	if pe.EventType != "polecat-hooked" {
		t.Errorf("EventType = %q, want polecat-hooked", pe.EventType)
	}
	if pe.PolecatID != "obsidian" {
		t.Errorf("PolecatID = %q, want obsidian", pe.PolecatID)
	}
	if pe.Rig != "planner" {
		t.Errorf("Rig = %q, want planner", pe.Rig)
	}
	if pe.HookBead != "pp-bzs" {
		t.Errorf("HookBead = %q, want pp-bzs", pe.HookBead)
	}
}

// TestParseEventLine_IgnoresIrrelevantTypes verifies that non-polecat event
// types are silently skipped.
func TestParseEventLine_IgnoresIrrelevantTypes(t *testing.T) {
	irrelevant := []string{
		events.TypePatrolStarted,
		events.TypeMergeStarted,
		events.TypeBoot,
		events.TypeMail,
	}
	for _, evType := range irrelevant {
		ev := events.Event{
			Timestamp: "2026-05-19T18:00:00Z",
			Source:    "gt",
			Type:      evType,
			Actor:     "gt",
		}
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("json.Marshal %q: %v", evType, err)
		}
		if _, ok := parseEventLine(string(data)); ok {
			t.Errorf("parseEventLine returned ok=true for event type %q (should be filtered)", evType)
		}
	}
}

// TestParseActor_VariousFormats verifies actor string parsing edge cases.
func TestParseActor_VariousFormats(t *testing.T) {
	cases := []struct {
		actor       string
		wantRig     string
		wantPolecat string
	}{
		{"planner/polecats/obsidian", "planner", "obsidian"},
		{"greenplace/polecats/Toast", "greenplace", "Toast"},
		{"gt", "", ""},
		{"", "", ""},
		{"badformat", "", ""},
		{"a/b/c", "", ""},
	}
	for _, tc := range cases {
		rig, polecat := parseActor(tc.actor)
		if rig != tc.wantRig || polecat != tc.wantPolecat {
			t.Errorf("parseActor(%q) = (%q, %q), want (%q, %q)",
				tc.actor, rig, polecat, tc.wantRig, tc.wantPolecat)
		}
	}
}

// TestMapEventType_Coverage verifies all mapped and unmapped cases.
func TestMapEventType_Coverage(t *testing.T) {
	mapped := map[string]struct {
		wantSSE   string
		wantState string
	}{
		events.TypeSpawn:          {"polecat-spawned", "spawned"},
		events.TypeSling:          {"polecat-spawned", "spawned"},
		events.TypeHook:           {"polecat-hooked", "hooked"},
		events.TypeDone:           {"polecat-done", "done"},
		events.TypeKill:           {"polecat-done", "done"},
		events.TypePolecatNudged:  {"polecat-working", "working"},
		events.TypePolecatChecked: {"polecat-working", "working"},
	}
	for evType, want := range mapped {
		sseType, state, ok := mapEventType(evType)
		if !ok {
			t.Errorf("mapEventType(%q): ok=false, want true", evType)
		}
		if sseType != want.wantSSE {
			t.Errorf("mapEventType(%q) sseType = %q, want %q", evType, sseType, want.wantSSE)
		}
		if state != want.wantState {
			t.Errorf("mapEventType(%q) state = %q, want %q", evType, state, want.wantState)
		}
	}

	// Should not be mapped
	unmapped := []string{events.TypePatrolStarted, events.TypeMail, events.TypeBoot, "unknown"}
	for _, evType := range unmapped {
		if _, _, ok := mapEventType(evType); ok {
			t.Errorf("mapEventType(%q): ok=true, want false", evType)
		}
	}
}

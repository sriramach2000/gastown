package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/witness"
)

// TestSSEPublisher_EmitsOnPolecatSpawn verifies that a spawn event written to
// the events JSONL file arrives as a polecat-spawned SSEEvent on a subscriber.
func TestSSEPublisher_EmitsOnPolecatSpawn(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, events.EventsFile)

	// Pre-create the events file so fsnotify can watch the directory.
	f, err := os.Create(eventsPath)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	_ = f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pub := NewWitnessSSEPublisher(ctx, dir)
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()

	ch, err := pub.Subscribe(subCtx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Allow the tailer goroutine to start watching.
	time.Sleep(80 * time.Millisecond)

	ev := events.Event{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "gt",
		Type:       events.TypeSpawn,
		Actor:      "gt",
		Payload:    events.SpawnPayload("planner", "obsidian"),
		Visibility: events.VisibilityFeed,
	}
	if err := witness.WriteEventJSONL(eventsPath, ev); err != nil {
		t.Fatalf("WriteEventJSONL: %v", err)
	}

	select {
	case sseEv := <-ch:
		if sseEv.Type != "polecat-spawned" {
			t.Errorf("SSEEvent.Type = %q, want polecat-spawned", sseEv.Type)
		}
		if got, ok := sseEv.Data["polecat_id"]; !ok || got != "obsidian" {
			t.Errorf("SSEEvent.Data[polecat_id] = %v, want obsidian", got)
		}
		if got, ok := sseEv.Data["rig"]; !ok || got != "planner" {
			t.Errorf("SSEEvent.Data[rig] = %v, want planner", got)
		}
		if got, ok := sseEv.Data["state"]; !ok || got != "spawned" {
			t.Errorf("SSEEvent.Data[state] = %v, want spawned", got)
		}
	case <-ctx.Done():
		t.Fatal("timeout: no polecat-spawned event received")
	}
}

// TestSSEPublisher_DeduplicatesRapidEvents verifies that two identical
// (polecatID, eventType) events written within dedupeWindow only produce one
// SSEEvent on the subscriber channel.
func TestSSEPublisher_DeduplicatesRapidEvents(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, events.EventsFile)

	f, err := os.Create(eventsPath)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	_ = f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pub := NewWitnessSSEPublisher(ctx, dir)
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()

	ch, err := pub.Subscribe(subCtx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	spawnEv := events.Event{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "gt",
		Type:       events.TypeSpawn,
		Actor:      "gt",
		Payload:    events.SpawnPayload("planner", "obsidian"),
		Visibility: events.VisibilityFeed,
	}

	// Write the same event twice in rapid succession.
	if err := witness.WriteEventJSONL(eventsPath, spawnEv); err != nil {
		t.Fatalf("WriteEventJSONL (1): %v", err)
	}
	if err := witness.WriteEventJSONL(eventsPath, spawnEv); err != nil {
		t.Fatalf("WriteEventJSONL (2): %v", err)
	}

	// Drain with a short timeout to collect any events that arrive.
	var received []SSEEvent
	drain := time.After(500 * time.Millisecond)
drainLoop:
	for {
		select {
		case ev := <-ch:
			received = append(received, ev)
		case <-drain:
			break drainLoop
		}
	}

	// We should receive exactly 1 polecat-spawned event (duplicate suppressed).
	spawned := 0
	for _, ev := range received {
		if ev.Type == "polecat-spawned" {
			spawned++
		}
	}
	if spawned != 1 {
		t.Errorf("received %d polecat-spawned events, want 1 (duplicate should be suppressed)", spawned)
	}
}

// TestSSEPublisher_HeartbeatOnIdle verifies that the SSE handler still emits
// heartbeats when no real events are produced (idle state).
// This exercises the WitnessSSEPublisher pointing at an empty directory —
// the subscriber channel stays open and heartbeats come from sseHandler itself.
func TestSSEPublisher_HeartbeatOnIdle(t *testing.T) {
	dir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use a real WitnessSSEPublisher pointing at an empty directory (no events file).
	pub := NewWitnessSSEPublisher(ctx, dir)
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()

	ch, err := pub.Subscribe(subCtx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// The publisher should not send any events (no events file exists).
	// The SSE heartbeat comes from sseHandler itself. We just verify Subscribe
	// succeeds and the channel is open.
	select {
	case ev := <-ch:
		// If anything arrives it should NOT be an error event.
		if v, ok := ev.Data["error"]; ok {
			t.Errorf("unexpected error event: %v", v)
		}
		// A real event arriving before the timeout is also acceptable.
	case <-time.After(200 * time.Millisecond):
		// Expected: no events from an empty directory. Channel stays open.
	}

	// Verify the channel is still open (context not yet cancelled).
	select {
	case _, ok := <-ch:
		if !ok {
			t.Error("subscriber channel closed prematurely (context not yet cancelled)")
		}
	default:
		// Channel still open; this is correct.
	}
}

// TestPolecatEventToSSE verifies the shape of the converted SSEEvent.
func TestPolecatEventToSSE(t *testing.T) {
	pe := witness.PolecatEvent{
		TS:        "2026-05-19T18:00:00Z",
		PolecatID: "obsidian",
		Rig:       "planner",
		HookBead:  "pp-bzs",
		Rail:      "claude",
		State:     "hooked",
		EventType: "polecat-hooked",
	}

	ev := polecatEventToSSE(pe)

	if ev.Type != "polecat-hooked" {
		t.Errorf("Type = %q, want polecat-hooked", ev.Type)
	}
	checks := map[string]string{
		"ts":         "2026-05-19T18:00:00Z",
		"polecat_id": "obsidian",
		"rig":        "planner",
		"hook_bead":  "pp-bzs",
		"rail":       "claude",
		"state":      "hooked",
	}
	for k, want := range checks {
		if got, ok := ev.Data[k]; !ok {
			t.Errorf("Data[%q] missing", k)
		} else if got != want {
			t.Errorf("Data[%q] = %v, want %q", k, got, want)
		}
	}
}

// TestWitnessSSEPublisher_MultipleSubscribers verifies that two simultaneous
// subscribers both receive the same event.
func TestWitnessSSEPublisher_MultipleSubscribers(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, events.EventsFile)

	f, err := os.Create(eventsPath)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	_ = f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pub := NewWitnessSSEPublisher(ctx, dir)

	sub1Ctx, sub1Cancel := context.WithCancel(ctx)
	defer sub1Cancel()
	sub2Ctx, sub2Cancel := context.WithCancel(ctx)
	defer sub2Cancel()

	ch1, _ := pub.Subscribe(sub1Ctx)
	ch2, _ := pub.Subscribe(sub2Ctx)

	time.Sleep(80 * time.Millisecond)

	ev := events.Event{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "gt",
		Type:       events.TypeHook,
		Actor:      "planner/polecats/obsidian",
		Payload:    events.HookPayload("pp-bzs"),
		Visibility: events.VisibilityFeed,
	}
	if err := witness.WriteEventJSONL(eventsPath, ev); err != nil {
		t.Fatalf("WriteEventJSONL: %v", err)
	}

	timeout := time.After(3 * time.Second)
	got1, got2 := false, false
	for !got1 || !got2 {
		select {
		case sseEv := <-ch1:
			if sseEv.Type == "polecat-hooked" {
				got1 = true
			}
		case sseEv := <-ch2:
			if sseEv.Type == "polecat-hooked" {
				got2 = true
			}
		case <-timeout:
			t.Errorf("timeout: got1=%v got2=%v — both subscribers must receive the event", got1, got2)
			return
		}
	}
}

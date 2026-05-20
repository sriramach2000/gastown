// Package witness provides witness lifecycle and monitoring operations.
package witness

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/steveyegge/gastown/internal/events"
)

// PolecatEvent is a structured polecat lifecycle event emitted by EventEmitter.
// The shape matches the SSE payload contract: all fields must be present and
// typed exactly as documented (the browser depends on this shape).
type PolecatEvent struct {
	// TS is the RFC3339 timestamp of the event.
	TS string `json:"ts"`
	// PolecatID is the polecat name (e.g., "obsidian").
	PolecatID string `json:"polecat_id"`
	// Rig is the rig the polecat belongs to (e.g., "planner").
	Rig string `json:"rig"`
	// HookBead is the bead currently hooked by this polecat, or "" if none.
	HookBead string `json:"hook_bead"`
	// Rail is "claude" or "opencode"; empty means unknown.
	Rail string `json:"rail"`
	// State is the canonical lifecycle state for this event type.
	State string `json:"state"`
	// EventType is the SSE event name (e.g., "polecat-spawned").
	// Tagged with json:"-" because it is carried on the SSE envelope, not the payload.
	EventType string `json:"-"`
}

// EventEmitter tails <town>/.events.jsonl and emits PolecatEvents for polecat
// lifecycle transitions. It bridges the events package JSONL log to the
// SSE publisher so live polecat state reaches the dashboard without polling.
//
// It implements Option (a) from the task specification: fsnotify-based file
// tail decoupled from in-process polecat state machines.
type EventEmitter struct {
	eventsPath string
	out        chan<- PolecatEvent
}

// NewEventEmitter creates an EventEmitter that reads from eventsPath and
// writes parsed PolecatEvents to out.
//
// Call Run(ctx) in a goroutine to start tailing. The emitter closes gracefully
// when ctx is cancelled — it does NOT close out (the caller owns that channel).
func NewEventEmitter(eventsPath string, out chan<- PolecatEvent) *EventEmitter {
	return &EventEmitter{
		eventsPath: eventsPath,
		out:        out,
	}
}

// NewEventEmitterForTown creates an EventEmitter for the standard events file
// location inside townRoot.
func NewEventEmitterForTown(townRoot string, out chan<- PolecatEvent) *EventEmitter {
	path := filepath.Join(townRoot, events.EventsFile)
	return NewEventEmitter(path, out)
}

// Run tails eventsPath until ctx is cancelled.
// It skips historical entries and only forwards new lines appended after start.
// Errors opening or watching the file are logged but never fatal — the caller
// (SSEPublisher) falls back to heartbeat-only mode if the emitter cannot run.
func (e *EventEmitter) Run(ctx context.Context) {
	// Seek to the end of the file so we only forward new events, not history.
	offset, err := e.currentFileSize()
	if err != nil {
		// File may not exist yet. Start from 0 so we catch the first write.
		offset = 0
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("event_emitter: fsnotify init failed for %s: %v", e.eventsPath, err)
		return
	}
	defer func() { _ = watcher.Close() }()

	// Watch the parent directory: watching the file directly can miss inode
	// replace-on-write patterns (some editors, atomic-rename log rotation).
	dir := filepath.Dir(e.eventsPath)
	if err := watcher.Add(dir); err != nil {
		log.Printf("event_emitter: watching dir %s failed: %v", dir, err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only care about writes to .events.jsonl itself (not other files in
			// the directory). fsnotify provides the full path.
			if ev.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			if filepath.Base(ev.Name) != filepath.Base(e.eventsPath) {
				continue
			}
			newOffset, parseErr := e.readNewLines(ctx, offset)
			if parseErr != nil {
				log.Printf("event_emitter: reading new lines: %v", parseErr)
			}
			offset = newOffset

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("event_emitter: watcher error: %v", watchErr)
		}
	}
}

// currentFileSize returns the current byte size of eventsPath so Run can seek
// past historical content on startup.
func (e *EventEmitter) currentFileSize() (int64, error) {
	info, err := os.Stat(e.eventsPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// readNewLines reads lines appended since offset, parses each line, and emits
// matching PolecatEvents. Returns the new file offset.
func (e *EventEmitter) readNewLines(ctx context.Context, offset int64) (int64, error) {
	f, err := os.Open(e.eventsPath) //nolint:gosec // path from trusted configuration
	if err != nil {
		return offset, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, fmt.Errorf("seek: %w", err)
	}

	scanner := bufio.NewScanner(f)
	var newOffset int64 = offset
	for scanner.Scan() {
		line := scanner.Text()
		newOffset += int64(len(line)) + 1 // +1 for newline
		if line == "" {
			continue
		}
		pe, ok := parseEventLine(line)
		if !ok {
			continue
		}
		select {
		case <-ctx.Done():
			return newOffset, nil
		case e.out <- pe:
		default:
			// Drop if the consumer is slow; dashboard events are best-effort.
		}
	}
	return newOffset, scanner.Err()
}

// parseEventLine parses a single JSONL line into a PolecatEvent.
// Returns (event, true) if the line is a polecat lifecycle event we care about,
// (zero, false) otherwise.
func parseEventLine(line string) (PolecatEvent, bool) {
	var raw events.Event
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return PolecatEvent{}, false
	}

	// Map events.Type → SSE event type + polecat state.
	sseType, state, ok := mapEventType(raw.Type)
	if !ok {
		return PolecatEvent{}, false
	}

	pe := PolecatEvent{
		TS:        raw.Timestamp,
		EventType: sseType,
		State:     state,
	}

	// Extract polecat identity from the payload fields.
	if raw.Payload != nil {
		pe.PolecatID = stringField(raw.Payload, "polecat")
		pe.Rig = stringField(raw.Payload, "rig")
		pe.HookBead = stringField(raw.Payload, "bead")
		pe.Rail = stringField(raw.Payload, "rail")
	}

	// Fall back to actor parsing when payload doesn't have polecat/rig fields.
	// Actor format: "<rig>/polecats/<Name>".
	if pe.PolecatID == "" || pe.Rig == "" {
		rigFromActor, pcFromActor := parseActor(raw.Actor)
		if pe.PolecatID == "" {
			pe.PolecatID = pcFromActor
		}
		if pe.Rig == "" {
			pe.Rig = rigFromActor
		}
	}

	if pe.TS == "" {
		pe.TS = time.Now().UTC().Format(time.RFC3339)
	}

	return pe, true
}

// mapEventType maps an events.Type string to (sseEventType, polecatState, ok).
// Only polecat-relevant transitions are mapped; everything else returns ok=false.
func mapEventType(t string) (sseType, state string, ok bool) {
	switch t {
	case events.TypeSpawn:
		return "polecat-spawned", "spawned", true
	case events.TypeSling:
		return "polecat-spawned", "spawned", true
	case events.TypeHook:
		return "polecat-hooked", "hooked", true
	case events.TypeDone:
		return "polecat-done", "done", true
	case events.TypeKill:
		return "polecat-done", "done", true
	case events.TypePolecatNudged:
		return "polecat-working", "working", true
	case events.TypePolecatChecked:
		return "polecat-working", "working", true
	default:
		return "", "", false
	}
}

// parseActor extracts (rig, polecatName) from an actor string like
// "greenplace/polecats/Toast". Returns empty strings if the format doesn't match.
func parseActor(actor string) (rig, polecat string) {
	// Format: "<rig>/polecats/<Name>"
	parts := strings.SplitN(actor, "/", 3)
	if len(parts) == 3 && parts[1] == "polecats" {
		return parts[0], parts[2]
	}
	return "", ""
}

// stringField safely extracts a string from a map[string]interface{} payload.
func stringField(payload map[string]interface{}, key string) string {
	if v, ok := payload[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// WriteEventJSONL appends a raw events.Event as a JSONL line to path.
// This is a test helper that lets event_emitter_test.go simulate event ingestion
// without spawning real gt processes.
func WriteEventJSONL(path string, ev events.Event) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // path from trusted test config
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

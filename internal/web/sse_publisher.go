package web

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/witness"
	"github.com/steveyegge/gastown/internal/workspace"
)

// dedupeWindow is the minimum interval between identical (polecatID+eventType)
// events forwarded to subscribers. Rapid-fire duplicate events within this
// window are coalesced so the dashboard doesn't flicker.
const dedupeWindow = 2 * time.Second

// dedupeKey is the composite key used for deduplication.
type dedupeKey struct {
	polecatID string
	eventType string
}

// WitnessSSEPublisher is an SSEPublisher backed by the witness events JSONL log.
// It tails <townRoot>/.events.jsonl via fsnotify and converts polecat lifecycle
// events into SSEEvents for all active subscribers.
//
// Concurrency: Subscribe is safe to call from multiple goroutines. The internal
// broadcast loop is a single goroutine; it never blocks on slow subscribers
// (events are dropped rather than backing up the channel).
type WitnessSSEPublisher struct {
	townRoot string

	mu          sync.Mutex
	subscribers map[chan SSEEvent]struct{}

	dedupeMu sync.Mutex
	lastSeen map[dedupeKey]time.Time
}

// NewWitnessSSEPublisher creates a publisher that tails the events JSONL file
// at townRoot and broadcasts polecat lifecycle events to all subscribers.
//
// ctx controls the lifetime of the internal tailer goroutine. When ctx is
// cancelled the tailer exits; existing subscriber channels are not closed
// (the SSE handler closes them via ctx cancellation).
func NewWitnessSSEPublisher(ctx context.Context, townRoot string) *WitnessSSEPublisher {
	p := &WitnessSSEPublisher{
		townRoot:    townRoot,
		subscribers: make(map[chan SSEEvent]struct{}),
		lastSeen:    make(map[dedupeKey]time.Time),
	}
	go p.run(ctx)
	return p
}

// NewWitnessSSEPublisherAutoDiscover is like NewWitnessSSEPublisher but
// discovers townRoot automatically from the current working directory.
// If the workspace cannot be found it falls back to NoopPublisher behaviour
// (heartbeats only, no real events).
func NewWitnessSSEPublisherAutoDiscover(ctx context.Context) SSEPublisher {
	townRoot, err := workspace.FindFromCwd()
	if err != nil || townRoot == "" {
		log.Printf("sse_publisher: cannot find town root (heartbeat-only mode): %v", err)
		return NewNoopPublisher()
	}
	return NewWitnessSSEPublisher(ctx, townRoot)
}

// Subscribe returns a channel that receives SSEEvents until ctx is cancelled.
// Each call creates a new buffered subscriber channel. The caller must drain it
// promptly to avoid dropping events (buffer size: 32).
func (p *WitnessSSEPublisher) Subscribe(ctx context.Context) (<-chan SSEEvent, error) {
	ch := make(chan SSEEvent, 32)

	p.mu.Lock()
	p.subscribers[ch] = struct{}{}
	p.mu.Unlock()

	// Remove subscriber when ctx is cancelled.
	go func() {
		<-ctx.Done()
		p.mu.Lock()
		delete(p.subscribers, ch)
		p.mu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// run starts the EventEmitter tailer and broadcasts parsed PolecatEvents.
func (p *WitnessSSEPublisher) run(ctx context.Context) {
	rawCh := make(chan witness.PolecatEvent, 64)
	emitter := witness.NewEventEmitterForTown(p.townRoot, rawCh)

	go emitter.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case pe, ok := <-rawCh:
			if !ok {
				return
			}
			if p.isDuplicate(pe) {
				continue
			}
			p.broadcast(polecatEventToSSE(pe))
		}
	}
}

// isDuplicate returns true and updates lastSeen when the same (polecatID,
// eventType) pair was seen within dedupeWindow.
func (p *WitnessSSEPublisher) isDuplicate(pe witness.PolecatEvent) bool {
	key := dedupeKey{polecatID: pe.PolecatID, eventType: pe.EventType}
	now := time.Now()

	p.dedupeMu.Lock()
	defer p.dedupeMu.Unlock()

	if last, ok := p.lastSeen[key]; ok && now.Sub(last) < dedupeWindow {
		return true
	}
	p.lastSeen[key] = now
	return false
}

// broadcast sends ev to all current subscribers without blocking.
// Slow subscribers that have a full channel get the event dropped.
func (p *WitnessSSEPublisher) broadcast(ev SSEEvent) {
	p.mu.Lock()
	subs := make([]chan SSEEvent, 0, len(p.subscribers))
	for ch := range p.subscribers {
		subs = append(subs, ch)
	}
	p.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Subscriber is slow or disconnected; drop rather than block.
		}
	}
}

// polecatEventToSSE converts a witness.PolecatEvent to the SSEEvent shape
// that the SSE handler serialises and sends to the browser.
func polecatEventToSSE(pe witness.PolecatEvent) SSEEvent {
	// Marshal pe then unmarshal into map[string]any so the payload is a plain
	// map (required by SSEEvent.Data) without duplicating field logic.
	data, err := json.Marshal(pe)
	if err != nil {
		log.Printf("sse_publisher: marshal polecat event: %v", err)
		return SSEEvent{Type: pe.EventType, Data: map[string]any{"error": "marshal failure"}}
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("sse_publisher: unmarshal polecat event payload: %v", err)
		return SSEEvent{Type: pe.EventType, Data: map[string]any{"error": "unmarshal failure"}}
	}
	return SSEEvent{Type: pe.EventType, Data: payload}
}

// eventsFilePath returns the path to the town's raw events log.
// Used in tests and integration callers that need the path directly.
func eventsFilePath(townRoot string) string {
	return filepath.Join(townRoot, events.EventsFile)
}

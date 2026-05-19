package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// SSEEvent is a single server-sent event payload.
type SSEEvent struct {
	Type string         // event type: "polecat-status" | "bead-update" | "heartbeat"
	Data map[string]any // JSON-serializable payload
}

// SSEPublisher is the source of events. Production wires this to convoy/witness;
// tests pass an in-memory implementation.
type SSEPublisher interface {
	Subscribe(ctx context.Context) (<-chan SSEEvent, error)
}

// defaultHeartbeatInterval is the cadence used when NewSSEHandler receives zero emitInterval.
const defaultHeartbeatInterval = 30 * time.Second

// sseHandler streams server-sent events to connected clients.
type sseHandler struct {
	publisher    SSEPublisher
	emitInterval time.Duration
}

// NewSSEHandler returns an http.Handler for /events.
// emitInterval is the heartbeat cadence (default 30s if zero).
func NewSSEHandler(publisher SSEPublisher, emitInterval time.Duration) http.Handler {
	if emitInterval <= 0 {
		emitInterval = defaultHeartbeatInterval
	}
	return &sseHandler{
		publisher:    publisher,
		emitInterval: emitInterval,
	}
}

// ServeHTTP streams SSE frames until the client disconnects or the publisher closes.
func (h *sseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()

	events, err := h.publisher.Subscribe(ctx)
	if err != nil {
		log.Printf("sse: subscribe failed: %v", err)
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}

	heartbeat := time.NewTicker(h.emitInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-events:
			if !ok {
				// Publisher closed the channel; end the stream cleanly.
				return
			}
			if err := writeSSEEvent(w, ev.Type, ev.Data); err != nil {
				log.Printf("sse: write event failed: %v", err)
				return
			}
			flusher.Flush()

		case t := <-heartbeat.C:
			payload := map[string]any{"ts": t.Unix()}
			if err := writeSSEEvent(w, "heartbeat", payload); err != nil {
				log.Printf("sse: write heartbeat failed: %v", err)
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent serializes payload as JSON and writes an SSE frame to w.
// The frame format is:
//
//	event: <eventType>\n
//	data: <json>\n
//	\n
func writeSSEEvent(w http.ResponseWriter, eventType string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
	return err
}

// NoopPublisher is a production SSEPublisher that emits no events.
// The handler still emits heartbeats on its own schedule. This is the
// production wiring until a real event source (witness/polecat lifecycle)
// is connected.
type NoopPublisher struct{}

// NewNoopPublisher returns a publisher that never emits events.
// The returned channel stays open until the context is cancelled.
func NewNoopPublisher() *NoopPublisher { return &NoopPublisher{} }

// Subscribe returns a never-closing channel. The SSE handler treats this as
// "no events arrive; emit heartbeats only".
func (n *NoopPublisher) Subscribe(ctx context.Context) (<-chan SSEEvent, error) {
	ch := make(chan SSEEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

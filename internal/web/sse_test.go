package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubPublisher is a test-only SSEPublisher that emits pre-loaded events.
// The channel is left open after draining so the handler blocks until ctx cancel.
type stubPublisher struct {
	events []SSEEvent // events to emit on Subscribe; may be empty
	err    error      // if non-nil, Subscribe returns this error
}

func (s *stubPublisher) Subscribe(_ context.Context) (<-chan SSEEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan SSEEvent, len(s.events))
	for _, ev := range s.events {
		ch <- ev
	}
	// Channel stays open; handler blocks until ctx cancel or close.
	return ch, nil
}

// closingPublisher emits events and closes the channel, causing the handler to exit.
type closingPublisher struct {
	events []SSEEvent
}

func (c *closingPublisher) Subscribe(_ context.Context) (<-chan SSEEvent, error) {
	ch := make(chan SSEEvent, len(c.events))
	for _, ev := range c.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// nonFlusherWriter is a minimal http.ResponseWriter that does NOT implement http.Flusher.
// Used to exercise the "streaming not supported" 500 path in sseHandler.ServeHTTP.
type nonFlusherWriter struct {
	header http.Header
	code   int
	buf    strings.Builder
}

func newNonFlusherWriter() *nonFlusherWriter {
	return &nonFlusherWriter{header: make(http.Header), code: http.StatusOK}
}

func (w *nonFlusherWriter) Header() http.Header       { return w.header }
func (w *nonFlusherWriter) WriteHeader(code int)      { w.code = code }
func (w *nonFlusherWriter) Write(b []byte) (int, error) { return w.buf.Write(b) }

// TestSSEHandlerEmitsHeartbeat verifies a heartbeat event lands within 200ms
// when emitInterval is 50ms.
func TestSSEHandlerEmitsHeartbeat(t *testing.T) {
	pub := &stubPublisher{} // no events — only heartbeats fire
	handler := NewSSEHandler(pub, 50*time.Millisecond)

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil && ctx.Err() == nil {
		t.Fatalf("GET /events: %v", err)
	}
	if resp == nil {
		t.Fatal("no response received")
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.After(200 * time.Millisecond)
	lineCh := make(chan string)
	go func() {
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()

	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				t.Error("stream closed before heartbeat received")
				return
			}
			if strings.HasPrefix(line, "event: heartbeat") {
				return // success
			}
		case <-deadline:
			t.Error("no heartbeat event received within 200ms")
			return
		}
	}
}

// TestSSEHandlerForwardsPublishedEvent verifies that a polecat-status event
// emitted by the publisher arrives in the SSE stream.
func TestSSEHandlerForwardsPublishedEvent(t *testing.T) {
	event := SSEEvent{
		Type: "polecat-status",
		Data: map[string]any{"name": "dag", "status": "active"},
	}
	pub := &closingPublisher{events: []SSEEvent{event}}
	handler := NewSSEHandler(pub, 30*time.Second)

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil && ctx.Err() == nil {
		t.Fatalf("GET /events: %v", err)
	}
	if resp == nil {
		t.Fatal("no response received")
	}
	defer resp.Body.Close()

	foundEventLine := false
	foundDataLine := false

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "event: polecat-status" {
			foundEventLine = true
		}
		if strings.HasPrefix(line, "data:") && strings.Contains(line, "dag") {
			foundDataLine = true
		}
		if foundEventLine && foundDataLine {
			return // success
		}
	}

	if !foundEventLine {
		t.Error("event: polecat-status line not found in stream")
	}
	if !foundDataLine {
		t.Error("data line containing 'dag' not found in stream")
	}
}

// TestSSEHandlerExitsOnContextCancel verifies the handler returns within 200ms
// of the client context being cancelled (no goroutine leak).
func TestSSEHandlerExitsOnContextCancel(t *testing.T) {
	pub := &stubPublisher{} // no events; handler blocks waiting for heartbeat or events
	handler := NewSSEHandler(pub, 30*time.Second)

	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, _ := http.DefaultClient.Do(req) //nolint:errcheck
		if resp != nil {
			resp.Body.Close()
		}
	}()

	// Let the connection establish, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Handler exited cleanly after context cancel.
	case <-time.After(200 * time.Millisecond):
		t.Error("handler did not exit within 200ms after context cancel")
	}
}

// TestSSEHandlerRejectsNonFlusherWriter verifies that when the ResponseWriter
// does not implement http.Flusher, the handler returns HTTP 500.
func TestSSEHandlerRejectsNonFlusherWriter(t *testing.T) {
	pub := &stubPublisher{}
	handler := NewSSEHandler(pub, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	w := newNonFlusherWriter()

	handler.ServeHTTP(w, req)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500 for non-flusher ResponseWriter", w.code)
	}
	if !strings.Contains(w.buf.String(), "streaming not supported") {
		t.Errorf("body = %q, want 'streaming not supported'", w.buf.String())
	}
}

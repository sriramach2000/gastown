package brain

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient creates a Client wired to the given httptest.Server.
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient(DefaultPort)
	c.baseURL = srv.URL
	c.httpClient = srv.Client()
	return c
}

// jsonRPCOK builds a valid JSON-RPC 2.0 success response body.
func jsonRPCOK(t *testing.T, result any) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshaling result: %v", err)
	}
	resp := jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      1,
		Result:  json.RawMessage(raw),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshaling JSON-RPC response: %v", err)
	}
	return b
}

// jsonRPCErr builds a JSON-RPC 2.0 error response body.
func jsonRPCErr(t *testing.T, code int, msg string) []byte {
	t.Helper()
	resp := jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      1,
		Error:   &jsonRPCError{Code: code, Message: msg},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshaling JSON-RPC error: %v", err)
	}
	return b
}

func TestClient_Search_OK(t *testing.T) {
	results := []SearchResult{
		{Slug: "wiki/days/2026-05-21", Title: "2026-05-21", Score: 0.95, Excerpt: "polecat lifecycle"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonRPCOK(t, results))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.Search(context.Background(), "polecat lifecycle", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Slug != "wiki/days/2026-05-21" {
		t.Errorf("expected slug wiki/days/2026-05-21, got %q", got[0].Slug)
	}
}

func TestClient_Search_SidecarNotRunning(t *testing.T) {
	// Point client at a port with nothing listening.
	c := NewClient(19999)
	c.httpClient = &http.Client{Timeout: defaultHTTPTimeout}

	_, err := c.Search(context.Background(), "query", 3)
	if err == nil {
		t.Fatal("expected error when sidecar not running")
	}
	if !errors.Is(err, ErrBrainNotRunning) {
		t.Errorf("expected ErrBrainNotRunning, got: %v", err)
	}
	if !IsNotRunning(err) {
		t.Error("expected IsNotRunning(err) == true")
	}
}

func TestClient_Search_503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Search(context.Background(), "q", 3)
	if !errors.Is(err, ErrBrainNotRunning) {
		t.Errorf("expected ErrBrainNotRunning for 503, got: %v", err)
	}
}

func TestClient_Search_RPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonRPCErr(t, -32600, "invalid request"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Search(context.Background(), "q", 3)
	if err == nil {
		t.Fatal("expected error for JSON-RPC error response")
	}
	if !contains(err.Error(), "brain RPC error") {
		t.Errorf("expected 'brain RPC error' in message, got: %v", err)
	}
}

func TestClient_Query_OK(t *testing.T) {
	results := []SearchResult{
		{Slug: "wiki/beads/gt-abc", Title: "gt-abc", Score: 0.8, Excerpt: "mayor decision"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the method name in the request body.
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Method != "query" {
			t.Errorf("expected method 'query', got %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonRPCOK(t, results))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.Query(context.Background(), "what did the mayor decide on convoy 42?", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
}

func TestClient_Stats_OK(t *testing.T) {
	stats := StatsResult{
		PageCount:   100,
		ChunkCount:  500,
		DoctorScore: 95.5,
		Healthy:     true,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Method != "get_stats" {
			t.Errorf("expected method 'get_stats', got %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonRPCOK(t, stats))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	got, err := c.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil StatsResult")
	}
	if got.PageCount != 100 {
		t.Errorf("expected PageCount 100, got %d", got.PageCount)
	}
	if got.RawJSON == nil {
		t.Error("expected RawJSON to be preserved")
	}
}

func TestClient_Call_EscapeHatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Method != "traverse_graph" {
			t.Errorf("expected traverse_graph method, got %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jsonRPCOK(t, map[string]string{"nodes": "2"}))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	raw, err := c.Call(context.Background(), "traverse_graph", map[string]any{"slug": "people/alice"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(raw) == 0 {
		t.Error("expected non-empty raw result")
	}
}

func TestNewClientFromConfig(t *testing.T) {
	cfg := &BrainConfig{Port: 8888}
	c := NewClientFromConfig(cfg)
	if c.port != 8888 {
		t.Errorf("expected port 8888, got %d", c.port)
	}
}

func TestIsNotRunning(t *testing.T) {
	if IsNotRunning(nil) {
		t.Error("IsNotRunning(nil) should be false")
	}
	if IsNotRunning(errors.New("other error")) {
		t.Error("IsNotRunning should be false for unrelated errors")
	}
	err := &BrainNotRunningError{Port: 7891, Err: ErrBrainNotRunning}
	if !IsNotRunning(err) {
		t.Error("IsNotRunning should be true for BrainNotRunningError")
	}
}

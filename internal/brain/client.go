package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// defaultHTTPTimeout is the per-request timeout for the brain HTTP client.
	defaultHTTPTimeout = 30 * time.Second

	// jsonRPCVersion is the JSON-RPC protocol version used by gbrain serve --http.
	jsonRPCVersion = "2.0"
)

// Client is an HTTP MCP client that speaks JSON-RPC to a running
// `gbrain serve --http` sidecar on localhost:<port>.
//
// Use NewClient to create a Client. The zero value is not usable.
type Client struct {
	baseURL    string
	httpClient *http.Client
	port       int
}

// NewClient creates a Client configured to connect to the gbrain sidecar on
// the given port. The underlying http.Client is created with a sensible
// timeout; callers can replace it via WithHTTPClient for testing.
func NewClient(port int) *Client {
	return &Client{
		baseURL:    fmt.Sprintf("http://localhost:%d", port),
		port:       port,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// NewClientFromConfig creates a Client using port and search mode from cfg.
func NewClientFromConfig(cfg *BrainConfig) *Client {
	return NewClient(cfg.Port)
}

// WithHTTPClient replaces the underlying http.Client. Primarily used in tests
// to inject an httptest.Server-backed transport.
func (c *Client) WithHTTPClient(hc *http.Client) *Client {
	c.httpClient = hc
	return c
}

// jsonRPCRequest is the wire format for a JSON-RPC 2.0 call.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is the wire format for a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the error object embedded in a JSON-RPC 2.0 error response.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SearchResult is a single result from a brain search or query operation.
type SearchResult struct {
	// Slug is the page identifier (e.g., "wiki/days/2026-05-21").
	Slug string `json:"slug"`
	// Title is the human-readable page title.
	Title string `json:"title"`
	// Score is the relevance score (higher is more relevant).
	Score float64 `json:"score"`
	// Excerpt is a short text snippet from the matching page chunk.
	Excerpt string `json:"excerpt"`
	// Source is the gbrain source ID for the page.
	Source string `json:"source"`
}

// StatsResult holds the output of the get_stats + get_health operations.
type StatsResult struct {
	// PageCount is the total number of pages in the brain.
	PageCount int `json:"page_count"`
	// ChunkCount is the total number of indexed chunks.
	ChunkCount int `json:"chunk_count"`
	// DoctorScore is the brain integrity score (0–100).
	DoctorScore float64 `json:"doctor_score"`
	// Healthy indicates whether the sidecar considers itself healthy.
	Healthy bool `json:"healthy"`
	// RawJSON is the unparsed response body, preserved for display by
	// `gt brain stats` even if future fields are added upstream.
	RawJSON json.RawMessage `json:"-"`
}

// Search performs a hybrid (vector + BM25 + rerank) search against the brain.
// Returns up to k results. The brain sidecar must be running.
func (c *Client) Search(ctx context.Context, query string, k int) ([]SearchResult, error) {
	params := map[string]any{
		"query": query,
		"k":     k,
	}
	raw, err := c.call(ctx, "search", params)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("decoding search results: %w", err)
	}
	return results, nil
}

// Query performs an intent-aware query that composes a markdown answer from
// retrieved chunks. Returns up to k results. The brain sidecar must be running.
func (c *Client) Query(ctx context.Context, question string, k int) ([]SearchResult, error) {
	params := map[string]any{
		"query": question,
		"k":     k,
	}
	raw, err := c.call(ctx, "query", params)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, fmt.Errorf("decoding query results: %w", err)
	}
	return results, nil
}

// Stats fetches brain health and statistics from the running sidecar.
func (c *Client) Stats(ctx context.Context) (*StatsResult, error) {
	raw, err := c.call(ctx, "get_stats", nil)
	if err != nil {
		return nil, err
	}
	var stats StatsResult
	// Best-effort parse; preserve raw for callers that want full JSON.
	_ = json.Unmarshal(raw, &stats)
	stats.RawJSON = raw
	return &stats, nil
}

// Call is the escape hatch for any of the ~47 gbrain MCP operations not
// covered by the typed helpers. Returns the raw JSON result bytes.
func (c *Client) Call(ctx context.Context, op string, params any) (json.RawMessage, error) {
	return c.call(ctx, op, params)
}

// call sends a JSON-RPC request to the sidecar and returns the raw result.
// Wraps HTTP and JSON-RPC protocol errors into typed errors where possible.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	reqBody, err := json.Marshal(jsonRPCRequest{
		JSONRPC: jsonRPCVersion,
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding request for %q: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rpc", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("building request for %q: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Connection refused or network error → sidecar not running.
		return nil, &BrainNotRunningError{Port: c.port, Err: ErrBrainNotRunning}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response for %q: %w", method, err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway {
		return nil, &BrainNotRunningError{Port: c.port, Err: ErrBrainNotRunning}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP %d for %q: %s", resp.StatusCode, method, string(body))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("decoding JSON-RPC response for %q: %w", method, err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("brain RPC error %d for %q: %s", rpcResp.Error.Code, method, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// IsNotRunning returns true if err (or any unwrapped error) is ErrBrainNotRunning.
func IsNotRunning(err error) bool {
	return errors.Is(err, ErrBrainNotRunning)
}

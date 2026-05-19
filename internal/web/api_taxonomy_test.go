package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubTaxonomyResponse returns a fixed TaxonomyResponse for use in handler tests.
func stubTaxonomyResponse() TaxonomyResponse {
	return TaxonomyResponse{
		Mayor: TaxonomyMayorNode{ID: "gas-mayor", Status: "active", Rig: "lockstep"},
		Deacons: []TaxonomyDeaconNode{
			{ID: "gas-deacon-001", Status: "active", Supervises: []string{"gastown-witness"}},
		},
		Witnesses: []TaxonomyWitnessNode{
			{ID: "gastown-witness", Status: "watching", Alerts: 0, Supervises: []string{"gastown-alpha"}},
		},
		Polecats: []TaxonomyPolecatNode{
			{ID: "gastown-alpha", Status: "working", HookBead: "gt-abc", Rig: "lockstep", Supervisor: "gastown-witness"},
		},
		Ts: time.Date(2026, 5, 19, 20, 0, 0, 0, time.UTC),
	}
}

// makeStubBuilder returns a taxonomyBuilder that always returns the given response.
func makeStubBuilder(tax TaxonomyResponse) taxonomyBuilder {
	return func(_ context.Context, _ string) (TaxonomyResponse, error) {
		return tax, nil
	}
}

// makeErrBuilder returns a taxonomyBuilder that always returns the given error.
func makeErrBuilder(msg string) taxonomyBuilder {
	return func(_ context.Context, _ string) (TaxonomyResponse, error) {
		return TaxonomyResponse{}, &taxonomyTestError{msg}
	}
}

type taxonomyTestError struct{ msg string }

func (e *taxonomyTestError) Error() string { return e.msg }

// TestTaxonomyHandlerReturnsJSON verifies the happy-path: GET returns 200,
// Content-Type application/json, and a decodable TaxonomyResponse.
func TestTaxonomyHandlerReturnsJSON(t *testing.T) {
	want := stubTaxonomyResponse()
	h := newTaxonomyHandlerFull(makeStubBuilder(want), "/fake/root")

	req := httptest.NewRequest(http.MethodGet, "/api/taxonomy", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// Decode response and verify shape.
	var got TaxonomyResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode: %v", err)
	}

	if got.Mayor.ID != want.Mayor.ID {
		t.Errorf("mayor.id = %q, want %q", got.Mayor.ID, want.Mayor.ID)
	}
	if got.Mayor.Status != want.Mayor.Status {
		t.Errorf("mayor.status = %q, want %q", got.Mayor.Status, want.Mayor.Status)
	}
	if got.Mayor.Rig != want.Mayor.Rig {
		t.Errorf("mayor.rig = %q, want %q", got.Mayor.Rig, want.Mayor.Rig)
	}

	if len(got.Deacons) != 1 {
		t.Fatalf("deacons len = %d, want 1", len(got.Deacons))
	}
	if got.Deacons[0].ID != "gas-deacon-001" {
		t.Errorf("deacons[0].id = %q, want gas-deacon-001", got.Deacons[0].ID)
	}

	if len(got.Witnesses) != 1 {
		t.Fatalf("witnesses len = %d, want 1", len(got.Witnesses))
	}
	if got.Witnesses[0].ID != "gastown-witness" {
		t.Errorf("witnesses[0].id = %q, want gastown-witness", got.Witnesses[0].ID)
	}

	if len(got.Polecats) != 1 {
		t.Fatalf("polecats len = %d, want 1", len(got.Polecats))
	}
	if got.Polecats[0].HookBead != "gt-abc" {
		t.Errorf("polecats[0].hook_bead = %q, want gt-abc", got.Polecats[0].HookBead)
	}
}

// TestTaxonomyHandlerRejectsPOST verifies that POST returns 405.
func TestTaxonomyHandlerRejectsPOST(t *testing.T) {
	h := newTaxonomyHandlerFull(makeStubBuilder(stubTaxonomyResponse()), "/fake/root")

	req := httptest.NewRequest(http.MethodPost, "/api/taxonomy", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

// TestTaxonomyHandlerRejectsPUT verifies that PUT also returns 405.
func TestTaxonomyHandlerRejectsPUT(t *testing.T) {
	h := newTaxonomyHandlerFull(makeStubBuilder(stubTaxonomyResponse()), "/fake/root")

	req := httptest.NewRequest(http.MethodPut, "/api/taxonomy", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

// TestTaxonomyHandlerSets2sCache verifies the Cache-Control header is set to
// max-age=2, matching the dashboard SWR cadence.
func TestTaxonomyHandlerSets2sCache(t *testing.T) {
	h := newTaxonomyHandlerFull(makeStubBuilder(stubTaxonomyResponse()), "/fake/root")

	req := httptest.NewRequest(http.MethodGet, "/api/taxonomy", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	cc := rr.Header().Get("Cache-Control")
	if cc != "max-age=2" {
		t.Errorf("Cache-Control = %q, want max-age=2", cc)
	}
}

// TestTaxonomyHandlerBuildError verifies that a build error produces a
// 500 response with a JSON {"error": "..."} body.
func TestTaxonomyHandlerBuildError(t *testing.T) {
	h := newTaxonomyHandlerFull(makeErrBuilder("bd is unavailable"), "/fake/root")

	req := httptest.NewRequest(http.MethodGet, "/api/taxonomy", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected non-empty error field in 500 response body")
	}
	if !strings.Contains(body["error"], "bd is unavailable") {
		t.Errorf("error body = %q, want to contain 'bd is unavailable'", body["error"])
	}
}

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/steveyegge/gastown/internal/workspace"
)

// TaxonomyMayorNode is the mayor entry in the taxonomy response.
type TaxonomyMayorNode struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Rig    string `json:"rig"`
}

// TaxonomyDeaconNode is a deacon entry in the taxonomy response.
type TaxonomyDeaconNode struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Supervises []string `json:"supervises"`
}

// TaxonomyWitnessNode is a witness entry in the taxonomy response.
type TaxonomyWitnessNode struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Alerts     int      `json:"alerts"`
	Supervises []string `json:"supervises"`
}

// TaxonomyPolecatNode is a polecat entry in the taxonomy response.
type TaxonomyPolecatNode struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	HookBead   string `json:"hook_bead"`
	Rig        string `json:"rig"`
	Supervisor string `json:"supervisor"`
}

// TaxonomyResponse is the JSON payload returned by GET /api/taxonomy.
type TaxonomyResponse struct {
	Mayor     TaxonomyMayorNode    `json:"mayor"`
	Deacons   []TaxonomyDeaconNode `json:"deacons"`
	Witnesses []TaxonomyWitnessNode `json:"witnesses"`
	Polecats  []TaxonomyPolecatNode `json:"polecats"`
	Ts        time.Time            `json:"ts"`
}

// taxonomyBuilder is a function that builds a TaxonomyResponse given a root path.
// Defined as a type to allow injection in tests without subprocess spawning.
type taxonomyBuilder func(ctx context.Context, root string) (TaxonomyResponse, error)

// NewTaxonomyHandler returns an http.Handler for GET /api/taxonomy.
//
// It uses the registered taxonomy builder (which calls cmd.BuildTaxonomy via
// a bridge injected at startup) to fetch the fleet hierarchy and emits the
// result as application/json with a short Cache-Control header that matches
// the dashboard SWR (stale-while-revalidate) cadence of 2 seconds.
//
// Response codes:
//   - 200: fleet hierarchy as JSON
//   - 405: method other than GET
//   - 500: workspace not found or build error
//
// NOTE: Route registration (case path == "/taxonomy") in handler.go is
// wired by the orchestrator after merge.
// NOTE: The bridge between cmd.BuildTaxonomy and web.TaxonomyResponse is
// provided via SetTaxonomyBuilder() which must be called before first use.
// Orchestrator wires this in handler.go / setup.go after merge.
func NewTaxonomyHandler() http.Handler {
	return newTaxonomyHandlerWithBuilder(defaultTaxonomyBuilder)
}

// defaultTaxonomyBuilder is the real builder, set at startup via SetTaxonomyBuilder.
// It defaults to an error stub so the handler fails loudly if never wired.
var defaultTaxonomyBuilder taxonomyBuilder = func(_ context.Context, _ string) (TaxonomyResponse, error) {
	return TaxonomyResponse{}, &taxonomyNotWiredError{}
}

// taxonomyNotWiredError is returned when the handler is called before the builder is set.
type taxonomyNotWiredError struct{}

func (e *taxonomyNotWiredError) Error() string {
	return "taxonomy builder not wired — call SetTaxonomyBuilder() at startup"
}

// SetTaxonomyBuilder registers the function that builds TaxonomyResponse.
// Called once at startup (by handler.go / setup.go wiring — orchestrator responsibility).
func SetTaxonomyBuilder(b taxonomyBuilder) {
	defaultTaxonomyBuilder = b
}

// newTaxonomyHandlerWithBuilder constructs the handler with a specific builder.
// Used by NewTaxonomyHandler (real path) and tests (stub path).
func newTaxonomyHandlerWithBuilder(build taxonomyBuilder) http.Handler {
	return newTaxonomyHandlerFull(build, "")
}

// newTaxonomyHandlerFull is the innermost constructor.
// When townRootOverride is non-empty it bypasses workspace.FindFromCwdOrError
// (used in tests with no real workspace on disk).
func newTaxonomyHandlerFull(build taxonomyBuilder, townRootOverride string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		root := townRootOverride
		if root == "" {
			var err error
			root, err = workspace.FindFromCwdOrError()
			if err != nil {
				sendTaxonomyError(w, err.Error())
				return
			}
		}

		ctx := r.Context()
		tax, err := build(ctx, root)
		if err != nil {
			sendTaxonomyError(w, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=2")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(tax)
	})
}

// sendTaxonomyError writes a 500 JSON error response.
func sendTaxonomyError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

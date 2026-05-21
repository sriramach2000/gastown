package cli_hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSKILLMd_Basic(t *testing.T) {
	skillMd := `
| CLI | Description | Category |
|-----|-------------|----------|
| blender | 3D modeling and rendering | creative |
| gimp | Image editing | creative |
| clibrowser | Web browsing | productivity |
`
	entries := parseSKILLMd([]byte(skillMd))
	if len(entries) == 0 {
		t.Fatal("expected entries from SKILL.md parse, got none")
	}

	found := false
	for _, e := range entries {
		if e.Name == "blender" {
			found = true
			if e.Package != "cli-anything-blender" {
				t.Errorf("expected package cli-anything-blender, got %s", e.Package)
			}
		}
	}
	if !found {
		t.Error("expected blender entry in parsed catalog")
	}
}

func TestParseSKILLMd_Empty(t *testing.T) {
	entries := parseSKILLMd([]byte("# No table here\nJust prose."))
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from prose-only SKILL.md, got %d", len(entries))
	}
}

func TestCatalogFetcher_DiskCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := &CatalogFetcher{
		URL:      "http://unused",
		CacheDir: dir,
		TTL:      CatalogTTL,
		client:   &http.Client{},
	}

	cat := &Catalog{
		Entries: []CLIEntry{
			{Name: "blender", Package: "cli-anything-blender", Category: "creative", Source: "harness"},
		},
		FetchedAt: time.Now(),
		SHA256:    "deadbeef",
		URL:       "http://unused",
	}

	if err := f.saveDiskCache(cat); err != nil {
		t.Fatalf("saveDiskCache: %v", err)
	}

	loaded, err := f.loadDiskCache()
	if err != nil {
		t.Fatalf("loadDiskCache: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Name != "blender" {
		t.Errorf("unexpected entry name: %s", loaded.Entries[0].Name)
	}
}

func TestCatalogFetcher_Get_UsesHTTPServer(t *testing.T) {
	skillMdBody := `
| CLI | Description | Category |
|-----|-------------|----------|
| exa | Web search | productivity |
| ollama | Local LLM | ai |
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(skillMdBody))
	}))
	defer srv.Close()

	dir := t.TempDir()
	f := &CatalogFetcher{
		URL:      srv.URL,
		CacheDir: dir,
		TTL:      CatalogTTL,
		client:   srv.Client(),
	}

	cat, err := f.Get()
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if len(cat.Entries) == 0 {
		t.Error("expected catalog entries, got none")
	}
	if cat.SHA256 == "" {
		t.Error("expected SHA256 digest to be populated")
	}

	// Verify disk cache was written
	cachePath := filepath.Join(dir, "catalog_cache.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file at %s, got %v", cachePath, err)
	}
}

func TestCatalogFetcher_Get_UsesDiskCacheOnHTTPFailure(t *testing.T) {
	// Clear in-memory cache so we don't get a hit from a previous test
	catalogCacheMu.Lock()
	catalogCache = nil
	catalogCacheAt = time.Time{}
	catalogCacheMu.Unlock()

	dir := t.TempDir()
	f := &CatalogFetcher{
		URL:      "http://127.0.0.1:0", // unreachable
		CacheDir: dir,
		TTL:      time.Millisecond, // expire immediately to force re-fetch
		client:   &http.Client{Timeout: time.Second},
	}

	// Pre-populate stale disk cache
	stale := &Catalog{
		Entries:   []CLIEntry{{Name: "stale-cli", Package: "cli-anything-stale-cli"}},
		FetchedAt: time.Now().Add(-2 * time.Hour), // 2h old — stale
		SHA256:    "abc",
		URL:       "http://127.0.0.1:0",
	}
	if err := f.saveDiskCache(stale); err != nil {
		t.Fatalf("saveDiskCache: %v", err)
	}

	cat, err := f.Get()
	if err != nil {
		t.Fatalf("expected fallback to disk cache, got error: %v", err)
	}
	if len(cat.Entries) == 0 || cat.Entries[0].Name != "stale-cli" {
		t.Error("expected stale-cli from disk cache fallback")
	}

	// Clean up global cache
	catalogCacheMu.Lock()
	catalogCache = nil
	catalogCacheAt = time.Time{}
	catalogCacheMu.Unlock()
}

func TestCatalogFetcher_Search(t *testing.T) {
	cat := &Catalog{
		Entries: []CLIEntry{
			{Name: "blender", Description: "3D modeling", Category: "creative"},
			{Name: "gimp", Description: "Image editing", Category: "creative"},
			{Name: "exa", Description: "Web search", Category: "productivity"},
		},
	}
	f := NewCatalogFetcher()
	results := f.Search(cat, "creative")
	if len(results) != 2 {
		t.Errorf("expected 2 creative results, got %d", len(results))
	}
	results = f.Search(cat, "search")
	if len(results) != 1 || results[0].Name != "exa" {
		t.Errorf("expected 1 result for 'search', got %v", results)
	}
}

func TestCatalogFetcher_Lookup(t *testing.T) {
	cat := &Catalog{
		Entries: []CLIEntry{
			{Name: "blender", Package: "cli-anything-blender"},
			{Name: "Exa", Package: "cli-anything-exa"},
		},
	}
	f := NewCatalogFetcher()

	// Case-insensitive lookup
	entry := f.Lookup(cat, "EXA")
	if entry == nil {
		t.Fatal("expected to find Exa (case-insensitive)")
	}
	if entry.Name != "Exa" {
		t.Errorf("unexpected name: %s", entry.Name)
	}

	missing := f.Lookup(cat, "nonexistent")
	if missing != nil {
		t.Error("expected nil for nonexistent CLI")
	}
}

func TestCatalogFetcher_ForceRefresh(t *testing.T) {
	skillMdBody := `
| CLI | Description |
|-----|-------------|
| fresh | Fresh CLI |
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(skillMdBody))
	}))
	defer srv.Close()

	// Seed in-memory cache with stale data
	catalogCacheMu.Lock()
	catalogCache = &Catalog{Entries: []CLIEntry{{Name: "stale"}}}
	catalogCacheAt = time.Now().Add(-2 * time.Hour)
	catalogCacheMu.Unlock()

	dir := t.TempDir()
	f := &CatalogFetcher{
		URL:      srv.URL,
		CacheDir: dir,
		TTL:      CatalogTTL,
		client:   srv.Client(),
	}

	cat, err := f.ForceRefresh()
	if err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	// Should have fresh data
	if len(cat.Entries) == 0 {
		t.Error("expected fresh entries after ForceRefresh")
	}

	// Clean up global cache
	catalogCacheMu.Lock()
	catalogCache = nil
	catalogCacheMu.Unlock()
}

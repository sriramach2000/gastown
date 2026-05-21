package cli_hub

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultCatalogURL is the upstream SKILL.md catalog endpoint.
// v1 trusts this CDN; Wave 3 will mirror to gastown-owned S3 (§D9).
const DefaultCatalogURL = "https://reeceyang.sgp1.cdn.digitaloceanspaces.com/SKILL.md"

// CatalogTTL is the default cache lifetime per §D2 (matches cli-anything-hub wrapper default).
const CatalogTTL = time.Hour

// ForcedRefreshInterval is the maximum age before a forced re-fetch regardless of TTL.
const ForcedRefreshInterval = 24 * time.Hour

// CLIEntry represents a single CLI tool in the registry catalog.
type CLIEntry struct {
	Name        string `json:"name"`
	Package     string `json:"package"`   // pip package name: cli-anything-<name>
	Category    string `json:"category"`
	Description string `json:"description"`
	Source      string `json:"source"` // "harness" | "public"
}

// Catalog holds the full set of CLIs fetched from the registry.
type Catalog struct {
	Entries   []CLIEntry `json:"entries"`
	FetchedAt time.Time  `json:"fetched_at"`
	SHA256    string     `json:"sha256"` // hex digest of raw response body
	URL       string     `json:"url"`
}

// catalogCache is a process-level in-memory cache to avoid redundant HTTP calls
// within the same process invocation.
var (
	catalogCacheMu sync.Mutex
	catalogCache   *Catalog
	catalogCacheAt time.Time
)

// CatalogFetcher fetches and caches the CLI registry catalog.
type CatalogFetcher struct {
	URL      string
	CacheDir string // directory for on-disk cache file
	TTL      time.Duration
	client   *http.Client
}

// NewCatalogFetcher creates a CatalogFetcher with default settings.
func NewCatalogFetcher() *CatalogFetcher {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "cli-hub")
	return &CatalogFetcher{
		URL:      DefaultCatalogURL,
		CacheDir: cacheDir,
		TTL:      CatalogTTL,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// cacheFilePath returns the on-disk JSON cache file path.
func (f *CatalogFetcher) cacheFilePath() string {
	return filepath.Join(f.CacheDir, "catalog_cache.json")
}

// Get returns the catalog, using the in-memory cache, on-disk cache, or
// a fresh HTTP fetch in that order of preference.
func (f *CatalogFetcher) Get() (*Catalog, error) {
	catalogCacheMu.Lock()
	defer catalogCacheMu.Unlock()

	now := time.Now()

	// 1. In-memory cache hit
	if catalogCache != nil && now.Sub(catalogCacheAt) < f.TTL {
		return catalogCache, nil
	}

	// 2. On-disk cache hit
	if c, err := f.loadDiskCache(); err == nil {
		age := now.Sub(c.FetchedAt)
		if age < f.TTL {
			catalogCache = c
			catalogCacheAt = now
			return c, nil
		}
		// Stale but use as fallback if fetch fails below
		_ = c
	}

	// 3. Fresh HTTP fetch
	c, err := f.fetchRemote()
	if err != nil {
		// Fall back to stale disk cache rather than failing completely
		if stale, diskErr := f.loadDiskCache(); diskErr == nil {
			catalogCache = stale
			catalogCacheAt = now
			return stale, nil
		}
		return nil, fmt.Errorf("cli_hub: catalog fetch failed: %w", err)
	}

	if err := f.saveDiskCache(c); err != nil {
		// Non-fatal: continue with in-memory result
		_ = err
	}

	catalogCache = c
	catalogCacheAt = now
	return c, nil
}

// ForceRefresh bypasses TTL and fetches a fresh catalog immediately.
func (f *CatalogFetcher) ForceRefresh() (*Catalog, error) {
	catalogCacheMu.Lock()
	defer catalogCacheMu.Unlock()

	c, err := f.fetchRemote()
	if err != nil {
		return nil, fmt.Errorf("cli_hub: forced catalog refresh failed: %w", err)
	}
	if err := f.saveDiskCache(c); err != nil {
		_ = err
	}
	catalogCache = c
	catalogCacheAt = time.Now()
	return c, nil
}

// Search returns entries whose name, category, or description contains keyword (case-insensitive).
func (f *CatalogFetcher) Search(catalog *Catalog, keyword string) []CLIEntry {
	kw := strings.ToLower(keyword)
	var results []CLIEntry
	for _, e := range catalog.Entries {
		if strings.Contains(strings.ToLower(e.Name), kw) ||
			strings.Contains(strings.ToLower(e.Category), kw) ||
			strings.Contains(strings.ToLower(e.Description), kw) {
			results = append(results, e)
		}
	}
	return results
}

// Lookup returns the entry for name, or nil if not found.
func (f *CatalogFetcher) Lookup(catalog *Catalog, name string) *CLIEntry {
	for i, e := range catalog.Entries {
		if strings.EqualFold(e.Name, name) {
			return &catalog.Entries[i]
		}
	}
	return nil
}

// fetchRemote performs the actual HTTP GET and parses the SKILL.md catalog.
func (f *CatalogFetcher) fetchRemote() (*Catalog, error) {
	resp, err := f.client.Get(f.URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d from catalog URL", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading catalog body: %w", err)
	}

	sum := sha256.Sum256(body)
	digest := fmt.Sprintf("%x", sum)

	entries := parseSKILLMd(body)

	return &Catalog{
		Entries:   entries,
		FetchedAt: time.Now(),
		SHA256:    digest,
		URL:       f.URL,
	}, nil
}

// parseSKILLMd parses the SKILL.md text into CLIEntry records.
// The upstream SKILL.md uses a Markdown table format; we do a best-effort parse.
func parseSKILLMd(data []byte) []CLIEntry {
	var entries []CLIEntry
	lines := strings.Split(string(data), "\n")
	inTable := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Detect table rows: lines starting with "|" containing at least 2 "|"
		if !strings.HasPrefix(line, "|") {
			if inTable {
				inTable = false
			}
			continue
		}
		if strings.Contains(line, "---") {
			inTable = true
			continue
		}
		inTable = true
		// Split columns
		cols := strings.Split(line, "|")
		if len(cols) < 3 {
			continue
		}
		// Trim whitespace from columns
		cleaned := make([]string, 0, len(cols))
		for _, c := range cols {
			cleaned = append(cleaned, strings.TrimSpace(c))
		}
		// Skip header row
		if len(cleaned) > 1 && (strings.EqualFold(cleaned[1], "name") || strings.EqualFold(cleaned[1], "cli")) {
			continue
		}
		if len(cleaned) > 2 && cleaned[1] != "" {
			name := cleaned[1]
			desc := ""
			category := ""
			source := "harness"
			if len(cleaned) > 2 {
				desc = cleaned[2]
			}
			if len(cleaned) > 3 {
				category = cleaned[3]
			}
			pkg := "cli-anything-" + strings.ToLower(name)
			entries = append(entries, CLIEntry{
				Name:        name,
				Package:     pkg,
				Category:    category,
				Description: desc,
				Source:      source,
			})
		}
	}
	return entries
}

// loadDiskCache reads the cached JSON from disk.
func (f *CatalogFetcher) loadDiskCache() (*Catalog, error) {
	data, err := os.ReadFile(f.cacheFilePath())
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// saveDiskCache writes the catalog to the on-disk JSON cache.
func (f *CatalogFetcher) saveDiskCache(c *Catalog) error {
	if err := os.MkdirAll(f.CacheDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(f.cacheFilePath(), data, 0o600)
}

package cli_hub

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// AuditEvent is the NDJSON record written per install/invoke operation.
// Field names match the CLIHubAuditEvent contract from internal/cmd/trail.go (HUB-COOKIE).
type AuditEvent struct {
	Ts             time.Time `json:"ts"`
	Kind           string    `json:"kind"`             // "cli_hub_install" | "cli_hub_invoke"
	Name           string    `json:"name"`
	Verdict        string    `json:"verdict,omitempty"`
	Polecat        string    `json:"polecat"`
	BeadID         string    `json:"bead_id,omitempty"`
	ExitCode       int       `json:"exit_code,omitempty"`
	DurationMs     int64     `json:"duration_ms,omitempty"`
	EscalationLink string    `json:"escalation_link,omitempty"`
}

// AuditKindInstall and AuditKindInvoke are the canonical kind values.
const (
	AuditKindInstall = "cli_hub_install"
	AuditKindInvoke  = "cli_hub_invoke"
)

// AuditWriter writes NDJSON audit records to the local audit log and
// forwards them to the cookie-trail (via the cmd package contract) and
// optionally to the brain-deacon stub.
type AuditWriter struct {
	// AuditDir is the directory for per-day audit JSONL files.
	// Defaults to ~/.local/share/cli-hub/audit/
	AuditDir string

	// CookieTrailFn is called with each event to append to ~/.cookie-trail.jsonl.
	// This is injected to avoid a circular import with internal/cmd.
	// In production, internal/cmd/cli_hub.go sets this to cmd.AppendCLIHubEvent.
	// Tests may set it to a no-op or capture function.
	CookieTrailFn func(event AuditEvent) error

	// DeaconFn is the fire-and-forget brain-deacon ingest stub (A3).
	// In Wave 1, if gbrain hasn't merged, it logs and drops.
	DeaconFn func(event AuditEvent)
}

// NewAuditWriter creates an AuditWriter with default paths and a no-op cookie trail.
func NewAuditWriter() *AuditWriter {
	home := os.Getenv("HOME")
	return &AuditWriter{
		AuditDir:      filepath.Join(home, ".local", "share", "cli-hub", "audit"),
		CookieTrailFn: nil,   // caller must wire in cmd.AppendCLIHubEvent
		DeaconFn:      deaconStub,
	}
}

// Write records an AuditEvent to the local JSONL file, forwards to the
// cookie-trail function, and fires the deacon stub asynchronously.
func (w *AuditWriter) Write(event AuditEvent) error {
	if event.Ts.IsZero() {
		event.Ts = time.Now().UTC()
	}

	// 1. Write to local audit JSONL
	if err := w.writeLocal(event); err != nil {
		// Non-fatal: log and continue so as not to block the user operation
		fmt.Fprintf(os.Stderr, "cli_hub: audit write error: %v\n", err)
	}

	// 2. Forward to cookie-trail (closes §gap 1 — mechanical write)
	if w.CookieTrailFn != nil {
		if err := w.CookieTrailFn(event); err != nil {
			fmt.Fprintf(os.Stderr, "cli_hub: cookie-trail write error: %v\n", err)
		}
	}

	// 3. Brain-deacon ingest — fire-and-forget goroutine (§A3)
	if w.DeaconFn != nil {
		go w.DeaconFn(event)
	}

	return nil
}

// writeLocal appends the NDJSON record to ~/.local/share/cli-hub/audit/<YYYY-MM-DD>.jsonl.
func (w *AuditWriter) writeLocal(event AuditEvent) error {
	if err := os.MkdirAll(w.AuditDir, 0o700); err != nil {
		return fmt.Errorf("creating audit dir %s: %w", w.AuditDir, err)
	}

	day := event.Ts.Format("2006-01-02")
	logFile := filepath.Join(w.AuditDir, day+".jsonl")

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening audit file %s: %w", logFile, err)
	}
	defer f.Close()

	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshalling audit event: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// deaconStub is the Wave 1 no-op brain-deacon ingest stub (§A3).
// It logs the event and drops it. When gbrain Wave 1 merges, this will be
// replaced by a real ingest call.
func deaconStub(event AuditEvent) {
	log.Printf("cli_hub: gbrain not ready, event queued (kind=%s name=%s ts=%s)",
		event.Kind, event.Name, event.Ts.Format(time.RFC3339))
}

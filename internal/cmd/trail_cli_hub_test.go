package cmd

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// overrideCookieTrailPath temporarily redirects cookie-trail writes to a
// temp path and restores the original on cleanup.
func overrideCookieTrailPath(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	trailFile := filepath.Join(tmp, ".cookie-trail.jsonl")
	orig := cookieTrailPath
	cookieTrailPath = func() (string, error) { return trailFile, nil }
	t.Cleanup(func() { cookieTrailPath = orig })
	return trailFile
}

// readAllCLIHubEvents reads every NDJSON line from path and unmarshals it.
func readAllCLIHubEvents(t *testing.T, path string) []CLIHubAuditEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening cookie-trail: %v", err)
	}
	defer f.Close()

	var events []CLIHubAuditEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var e CLIHubAuditEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshaling line %q: %v", line, err)
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning cookie-trail: %v", err)
	}
	return events
}

// TestTrailCLIHubAppendInstallEvent verifies that AppendCLIHubEvent writes a
// parseable NDJSON record with the expected fields for a cli_hub_install event.
func TestTrailCLIHubAppendInstallEvent(t *testing.T) {
	trailFile := overrideCookieTrailPath(t)

	now := time.Now().UTC().Truncate(time.Second)
	evt := CLIHubAuditEvent{
		Ts:      now,
		Kind:    "cli_hub_install",
		Name:    "clibrowser",
		Verdict: "APPROVED",
		Polecat: "mytown/rigs/main/polecats/alpha",
	}

	if err := AppendCLIHubEvent(evt); err != nil {
		t.Fatalf("AppendCLIHubEvent() error = %v", err)
	}

	// Effect verification: the file must exist and be non-empty.
	info, err := os.Stat(trailFile)
	if err != nil {
		t.Fatalf("cookie-trail file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("cookie-trail file is empty after AppendCLIHubEvent")
	}

	got := readAllCLIHubEvents(t, trailFile)
	if len(got) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(got))
	}

	g := got[0]
	if !g.Ts.Equal(now) {
		t.Errorf("Ts = %v, want %v", g.Ts, now)
	}
	if g.Kind != "cli_hub_install" {
		t.Errorf("Kind = %q, want %q", g.Kind, "cli_hub_install")
	}
	if g.Name != "clibrowser" {
		t.Errorf("Name = %q, want %q", g.Name, "clibrowser")
	}
	if g.Verdict != "APPROVED" {
		t.Errorf("Verdict = %q, want %q", g.Verdict, "APPROVED")
	}
	if g.Polecat != "mytown/rigs/main/polecats/alpha" {
		t.Errorf("Polecat = %q, want %q", g.Polecat, "mytown/rigs/main/polecats/alpha")
	}
}

// TestTrailCLIHubAppendInvokeEvent verifies a cli_hub_invoke event with exit
// code, duration, and bead_id fields.
func TestTrailCLIHubAppendInvokeEvent(t *testing.T) {
	trailFile := overrideCookieTrailPath(t)

	evt := CLIHubAuditEvent{
		Ts:         time.Now().UTC(),
		Kind:       "cli_hub_invoke",
		Name:       "clibrowser",
		Polecat:    "mytown/rigs/main/polecats/beta",
		BeadID:     "gt-4242",
		ExitCode:   0,
		DurationMs: 837,
	}

	if err := AppendCLIHubEvent(evt); err != nil {
		t.Fatalf("AppendCLIHubEvent() error = %v", err)
	}

	got := readAllCLIHubEvents(t, trailFile)
	if len(got) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(got))
	}

	g := got[0]
	if g.Kind != "cli_hub_invoke" {
		t.Errorf("Kind = %q, want %q", g.Kind, "cli_hub_invoke")
	}
	if g.BeadID != "gt-4242" {
		t.Errorf("BeadID = %q, want %q", g.BeadID, "gt-4242")
	}
	if g.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", g.ExitCode)
	}
	if g.DurationMs != 837 {
		t.Errorf("DurationMs = %d, want 837", g.DurationMs)
	}
}

// TestTrailCLIHubAppendMultipleEvents verifies that successive calls append
// new records and that each can be read back independently.
func TestTrailCLIHubAppendMultipleEvents(t *testing.T) {
	trailFile := overrideCookieTrailPath(t)

	events := []CLIHubAuditEvent{
		{
			Ts:      time.Now().UTC(),
			Kind:    "cli_hub_install",
			Name:    "tool-a",
			Polecat: "town/rigs/r1/polecats/p1",
			Verdict: "APPROVED",
		},
		{
			Ts:         time.Now().UTC(),
			Kind:       "cli_hub_invoke",
			Name:       "tool-a",
			Polecat:    "town/rigs/r1/polecats/p1",
			ExitCode:   1,
			DurationMs: 200,
		},
		{
			Ts:      time.Now().UTC(),
			Kind:    "cli_hub_install",
			Name:    "tool-b",
			Polecat: "town/rigs/r1/polecats/p2",
			Verdict: "DENIED policy:denylist",
		},
	}

	for i, evt := range events {
		if err := AppendCLIHubEvent(evt); err != nil {
			t.Fatalf("AppendCLIHubEvent(%d) error = %v", i, err)
		}
	}

	got := readAllCLIHubEvents(t, trailFile)
	if len(got) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(got))
	}

	if got[0].Name != "tool-a" || got[0].Kind != "cli_hub_install" {
		t.Errorf("record 0 = %+v, want install tool-a", got[0])
	}
	if got[1].Name != "tool-a" || got[1].Kind != "cli_hub_invoke" || got[1].ExitCode != 1 {
		t.Errorf("record 1 = %+v, want invoke tool-a exit=1", got[1])
	}
	if got[2].Name != "tool-b" || got[2].Verdict != "DENIED policy:denylist" {
		t.Errorf("record 2 = %+v, want install tool-b denied", got[2])
	}
}

// TestTrailCLIHubZeroTsFilledAutomatically verifies that a zero Ts is
// replaced by a non-zero timestamp before the record is written.
func TestTrailCLIHubZeroTsFilledAutomatically(t *testing.T) {
	trailFile := overrideCookieTrailPath(t)

	evt := CLIHubAuditEvent{
		// Ts intentionally zero
		Kind:    "cli_hub_install",
		Name:    "clibrowser",
		Polecat: "town/rigs/r1/polecats/p1",
		Verdict: "APPROVED",
	}

	before := time.Now().UTC()
	if err := AppendCLIHubEvent(evt); err != nil {
		t.Fatalf("AppendCLIHubEvent() error = %v", err)
	}
	after := time.Now().UTC()

	got := readAllCLIHubEvents(t, trailFile)
	if len(got) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(got))
	}
	if got[0].Ts.IsZero() {
		t.Error("Ts is still zero after AppendCLIHubEvent with zero Ts input")
	}
	if got[0].Ts.Before(before) || got[0].Ts.After(after) {
		t.Errorf("Ts = %v, want in [%v, %v]", got[0].Ts, before, after)
	}
}

// TestTrailCLIHubOmitEmptyFields verifies that omitempty fields are absent
// from the JSON when not set.
func TestTrailCLIHubOmitEmptyFields(t *testing.T) {
	trailFile := overrideCookieTrailPath(t)

	evt := CLIHubAuditEvent{
		Ts:      time.Now().UTC(),
		Kind:    "cli_hub_install",
		Name:    "clibrowser",
		Polecat: "town/rigs/r1/polecats/p1",
		// Verdict, BeadID, ExitCode, DurationMs, EscalationLink all zero/empty
	}

	if err := AppendCLIHubEvent(evt); err != nil {
		t.Fatalf("AppendCLIHubEvent() error = %v", err)
	}

	// Read raw bytes to verify omitempty fields are absent.
	raw, err := os.ReadFile(trailFile)
	if err != nil {
		t.Fatalf("reading cookie-trail: %v", err)
	}

	for _, absent := range []string{"verdict", "bead_id", "exit_code", "duration_ms", "escalation_link"} {
		if trailContainsSubstr(string(raw), `"`+absent+`"`) {
			t.Errorf("field %q present in JSON but should be omitted when zero/empty:\n%s", absent, raw)
		}
	}
}

// TestTrailCLIHubEscalationLink verifies the escalation_link field is written
// and read back correctly.
func TestTrailCLIHubEscalationLink(t *testing.T) {
	overrideCookieTrailPath(t)

	evt := CLIHubAuditEvent{
		Ts:             time.Now().UTC(),
		Kind:           "cli_hub_install",
		Name:           "clibrowser",
		Polecat:        "town/rigs/r1/polecats/p1",
		Verdict:        "DENIED policy:denylist",
		EscalationLink: "https://internal.example.com/policy/request?tool=clibrowser",
	}

	if err := AppendCLIHubEvent(evt); err != nil {
		t.Fatalf("AppendCLIHubEvent() error = %v", err)
	}

	trailFile, _ := cookieTrailPath()
	got := readAllCLIHubEvents(t, trailFile)
	if len(got) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(got))
	}
	if got[0].EscalationLink != evt.EscalationLink {
		t.Errorf("EscalationLink = %q, want %q", got[0].EscalationLink, evt.EscalationLink)
	}
}

// trailContainsSubstr reports whether s contains substr.
// Named distinctly to avoid conflict with contains() in mq_test.go.
func trailContainsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

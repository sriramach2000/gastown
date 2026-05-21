package cli_hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditWriter_WriteLocal_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	w := &AuditWriter{
		AuditDir:      dir,
		CookieTrailFn: nil,
		DeaconFn:      nil,
	}

	event := AuditEvent{
		Ts:      time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
		Kind:    AuditKindInstall,
		Name:    "blender",
		Verdict: "APPROVED",
		Polecat: "polecat-a",
	}

	if err := w.writeLocal(event); err != nil {
		t.Fatalf("writeLocal: %v", err)
	}

	// Verify the file was created at the right path
	expectedFile := filepath.Join(dir, "2026-05-21.jsonl")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("expected audit file at %s: %v", expectedFile, err)
	}

	// Verify the JSON is valid and contains expected fields
	var record AuditEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &record); err != nil {
		t.Fatalf("parsing audit JSON: %v", err)
	}
	if record.Kind != AuditKindInstall {
		t.Errorf("expected kind %s, got %s", AuditKindInstall, record.Kind)
	}
	if record.Name != "blender" {
		t.Errorf("expected name blender, got %s", record.Name)
	}
	if record.Verdict != "APPROVED" {
		t.Errorf("expected verdict APPROVED, got %s", record.Verdict)
	}
}

func TestAuditWriter_WriteLocal_AppendsMultipleRecords(t *testing.T) {
	dir := t.TempDir()
	w := &AuditWriter{AuditDir: dir}

	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{Ts: ts, Kind: AuditKindInstall, Name: "blender", Polecat: "p1"},
		{Ts: ts, Kind: AuditKindInvoke, Name: "blender", Polecat: "p1", ExitCode: 0},
		{Ts: ts, Kind: AuditKindInstall, Name: "gimp", Polecat: "p1"},
	}

	for _, e := range events {
		if err := w.writeLocal(e); err != nil {
			t.Fatalf("writeLocal: %v", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-05-21.jsonl"))
	if err != nil {
		t.Fatalf("reading audit file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 NDJSON lines, got %d", len(lines))
	}
}

func TestAuditWriter_Write_PopulatesTimestamp(t *testing.T) {
	dir := t.TempDir()
	w := &AuditWriter{AuditDir: dir}

	// Pass an event with zero Ts — Write should populate it
	event := AuditEvent{
		Kind:    AuditKindInstall,
		Name:    "exa",
		Polecat: "p1",
	}

	before := time.Now()
	if err := w.Write(event); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after := time.Now()

	// Read back and check timestamp
	files, _ := os.ReadDir(dir)
	if len(files) == 0 {
		t.Fatal("no audit file created")
	}
	data, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	var record AuditEvent
	_ = json.Unmarshal([]byte(strings.TrimSpace(string(data))), &record)

	if record.Ts.IsZero() {
		t.Error("expected non-zero timestamp after Write")
	}
	if record.Ts.Before(before) || record.Ts.After(after.Add(time.Second)) {
		t.Errorf("timestamp out of expected range: %v", record.Ts)
	}
}

func TestAuditWriter_Write_CallsCookieTrailFn(t *testing.T) {
	dir := t.TempDir()
	var captured []AuditEvent
	w := &AuditWriter{
		AuditDir: dir,
		CookieTrailFn: func(e AuditEvent) error {
			captured = append(captured, e)
			return nil
		},
	}

	event := AuditEvent{
		Ts:      time.Now().UTC(),
		Kind:    AuditKindInstall,
		Name:    "blender",
		Polecat: "p1",
		Verdict: "APPROVED",
	}
	if err := w.Write(event); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if len(captured) != 1 {
		t.Errorf("expected CookieTrailFn to be called once, got %d calls", len(captured))
	}
	if captured[0].Name != "blender" {
		t.Errorf("unexpected event name in cookie trail: %s", captured[0].Name)
	}
}

func TestAuditWriter_Write_DeaconFnCalledAsync(t *testing.T) {
	dir := t.TempDir()
	called := make(chan AuditEvent, 1)
	w := &AuditWriter{
		AuditDir: dir,
		DeaconFn: func(e AuditEvent) {
			called <- e
		},
	}

	event := AuditEvent{
		Ts:      time.Now().UTC(),
		Kind:    AuditKindInvoke,
		Name:    "ollama",
		Polecat: "p2",
	}
	if err := w.Write(event); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Wait briefly for the goroutine
	select {
	case e := <-called:
		if e.Name != "ollama" {
			t.Errorf("unexpected DeaconFn event name: %s", e.Name)
		}
	case <-time.After(time.Second):
		t.Error("DeaconFn was not called within 1s")
	}
}

func TestAuditWriter_WriteLocal_CreatesAuditDirIfMissing(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "deep", "nested", "dir")
	// dir does NOT exist yet
	w := &AuditWriter{AuditDir: dir}

	event := AuditEvent{
		Ts:      time.Now().UTC(),
		Kind:    AuditKindInstall,
		Name:    "gimp",
		Polecat: "p1",
	}
	if err := w.writeLocal(event); err != nil {
		t.Fatalf("writeLocal with missing dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("audit dir was not created: %v", err)
	}
}

func TestAuditEvent_KindConstants(t *testing.T) {
	if AuditKindInstall != "cli_hub_install" {
		t.Errorf("unexpected AuditKindInstall value: %s", AuditKindInstall)
	}
	if AuditKindInvoke != "cli_hub_invoke" {
		t.Errorf("unexpected AuditKindInvoke value: %s", AuditKindInvoke)
	}
}

func TestDeaconStub_DoesNotPanic(t *testing.T) {
	// deaconStub should never panic — it just logs
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("deaconStub panicked: %v", r)
		}
	}()
	deaconStub(AuditEvent{
		Ts:      time.Now().UTC(),
		Kind:    AuditKindInstall,
		Name:    "blender",
		Polecat: "p1",
	})
}

func TestNewAuditWriter_DefaultsPopulated(t *testing.T) {
	w := NewAuditWriter()
	if w.AuditDir == "" {
		t.Error("expected non-empty AuditDir")
	}
	if w.DeaconFn == nil {
		t.Error("expected non-nil DeaconFn")
	}
}

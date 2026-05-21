package brain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// newFakeProc returns a Proc whose runner returns the given result/error.
func newFakeProc(result *ProcResult, err error) *Proc {
	return &Proc{
		GbrainPath: "/fake/gbrain",
		runner: func(_ context.Context, _ string, _ []string, _ []string) (*ProcResult, error) {
			return result, err
		},
	}
}

// newCapturingProc returns a Proc whose runner captures the args it receives.
func newCapturingProc(captured *[]string) *Proc {
	return &Proc{
		GbrainPath: "/fake/gbrain",
		runner: func(_ context.Context, _ string, args []string, _ []string) (*ProcResult, error) {
			*captured = append(*captured, args...)
			return &ProcResult{Stdout: "ok", ExitCode: 0}, nil
		},
	}
}

func TestProcResult_OK(t *testing.T) {
	r := &ProcResult{ExitCode: 0}
	if !r.OK() {
		t.Error("ExitCode 0 should be OK")
	}
	r2 := &ProcResult{ExitCode: 1}
	if r2.OK() {
		t.Error("ExitCode 1 should not be OK")
	}
}

func TestProcResult_Combined(t *testing.T) {
	r := &ProcResult{Stdout: "hello", Stderr: "world"}
	combined := r.Combined()
	if !strings.Contains(combined, "hello") || !strings.Contains(combined, "world") {
		t.Errorf("Combined() missing content: %q", combined)
	}
}

func TestProcResult_Combined_OnlyStdout(t *testing.T) {
	r := &ProcResult{Stdout: "only stdout"}
	if r.Combined() != "only stdout" {
		t.Errorf("Combined() with empty stderr should be just stdout, got: %q", r.Combined())
	}
}

func TestProcResult_Combined_Empty(t *testing.T) {
	r := &ProcResult{}
	if r.Combined() != "" {
		t.Errorf("Combined() with empty stdout+stderr should be empty, got: %q", r.Combined())
	}
}

func TestProc_Init_PassesCorrectArgs(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	result, err := p.Init(context.Background(), "/town/brain")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !result.OK() {
		t.Errorf("expected OK result, exit %d", result.ExitCode)
	}
	if len(captured) < 2 || captured[0] != "/town/brain" || captured[1] != "init" {
		t.Errorf("expected args [/town/brain init], got %v", captured)
	}
}

func TestProc_Doctor_PassesCorrectArgs(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	_, err := p.Doctor(context.Background(), "/town/brain")
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(captured) < 2 || captured[1] != "doctor" {
		t.Errorf("expected 'doctor' arg, got %v", captured)
	}
}

func TestProc_DoctorRemediate_PassesCorrectArgs(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	_, err := p.DoctorRemediate(context.Background(), "/town/brain", 1.5)
	if err != nil {
		t.Fatalf("DoctorRemediate: %v", err)
	}
	// Must include --remediate and a --max-usd flag.
	hasRemediate := false
	hasMaxUSD := false
	for _, a := range captured {
		if a == "--remediate" {
			hasRemediate = true
		}
		if strings.HasPrefix(a, "--max-usd=") {
			hasMaxUSD = true
		}
	}
	if !hasRemediate {
		t.Errorf("expected --remediate in args, got %v", captured)
	}
	if !hasMaxUSD {
		t.Errorf("expected --max-usd= in args, got %v", captured)
	}
}

func TestProc_DoctorRemediate_FormatsCap(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	_, _ = p.DoctorRemediate(context.Background(), "/town/brain", 2.0)
	var capArg string
	for _, a := range captured {
		if strings.HasPrefix(a, "--max-usd=") {
			capArg = a
		}
	}
	if capArg != "--max-usd=2.00" {
		t.Errorf("expected --max-usd=2.00, got %q", capArg)
	}
}

func TestProc_Migrate_PassesCorrectArgs(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	_, err := p.Migrate(context.Background(), "/town/brain")
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	found := false
	for _, a := range captured {
		if a == "migrate" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'migrate' in args, got %v", captured)
	}
}

func TestProc_Rebuild_PassesConfirmDestructive(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	_, err := p.Rebuild(context.Background(), "/town/brain")
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	hasFlag := false
	for _, a := range captured {
		if a == "--confirm-destructive" {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Errorf("expected --confirm-destructive in args, got %v", captured)
	}
}

func TestProc_DreamCycle_PassesNonInteractive(t *testing.T) {
	var captured []string
	p := newCapturingProc(&captured)
	_, err := p.DreamCycle(context.Background(), "/town/brain")
	if err != nil {
		t.Fatalf("DreamCycle: %v", err)
	}
	hasDream := false
	hasNI := false
	for _, a := range captured {
		if a == "dream-cycle" {
			hasDream = true
		}
		if a == "--non-interactive" {
			hasNI = true
		}
	}
	if !hasDream {
		t.Errorf("expected 'dream-cycle' in args, got %v", captured)
	}
	if !hasNI {
		t.Errorf("expected '--non-interactive' in args, got %v", captured)
	}
}

func TestProc_RunnerError_Propagated(t *testing.T) {
	runErr := errors.New("context deadline exceeded")
	p := &Proc{
		GbrainPath: "/fake/gbrain",
		runner: func(_ context.Context, _ string, _ []string, _ []string) (*ProcResult, error) {
			return &ProcResult{}, runErr
		},
	}
	_, err := p.Init(context.Background(), "/town/brain")
	if err == nil {
		t.Fatal("expected runner error to be propagated")
	}
}

func TestProc_NonZeroExitNotGoError(t *testing.T) {
	// A non-zero exit code is returned in ProcResult.ExitCode, not as a Go error.
	p := newFakeProc(&ProcResult{ExitCode: 1, Stderr: "something went wrong"}, nil)
	result, err := p.Doctor(context.Background(), "/town/brain")
	if err != nil {
		t.Fatalf("expected nil Go error for non-zero exit, got: %v", err)
	}
	if result.OK() {
		t.Error("expected !OK() for ExitCode 1")
	}
}

func TestNewProc(t *testing.T) {
	p := NewProc()
	if p == nil {
		t.Fatal("NewProc() returned nil")
	}
	if p.runner == nil {
		t.Error("NewProc() should set runner")
	}
}

func TestNewProcWithPath(t *testing.T) {
	p := NewProcWithPath("/usr/local/bin/gbrain")
	if p.GbrainPath != "/usr/local/bin/gbrain" {
		t.Errorf("expected GbrainPath /usr/local/bin/gbrain, got %q", p.GbrainPath)
	}
}

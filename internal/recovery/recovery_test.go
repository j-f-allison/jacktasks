package recovery_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/j-f-allison/jacktasks/internal/recovery"
)

func testSentinel() recovery.Sentinel {
	return recovery.Sentinel{
		Version:            1,
		SessionID:          "test-session-id",
		ProjectID:          "test-project-id",
		ProjectName:        "Test Project",
		CategoryID:         "test-category-id",
		CategoryName:       "Test Category",
		PlannedDurationMin: 25,
		StartedAt:          1000000,
		TargetEndAt:        1001500,
		Pauses: []recovery.PauseRecord{
			{Start: 1000300, End: 1000360},
		},
		Captures: []recovery.CaptureRecord{
			{ID: "cap-1", Text: "do this", CapturedAt: 1000200},
		},
		State:     "active",
		WrittenAt: 1000450,
	}
}

func TestRoundTrip(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	s := testSentinel()

	if err := recovery.Write(dir, s); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := recovery.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned nil, want sentinel")
	}
	if got.SessionID != s.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, s.SessionID)
	}
	if got.State != s.State {
		t.Errorf("State = %q, want %q", got.State, s.State)
	}
	if len(got.Captures) != 1 {
		t.Fatalf("len(Captures) = %d, want 1", len(got.Captures))
	}
	if got.Captures[0].Text != "do this" {
		t.Errorf("Captures[0].Text = %q, want %q", got.Captures[0].Text, "do this")
	}
	if len(got.Pauses) != 1 {
		t.Fatalf("len(Pauses) = %d, want 1", len(got.Pauses))
	}
}

func TestReadAbsent(t *testing.T) {
	dir := t.TempDir()
	got, err := recovery.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != nil {
		t.Errorf("Read absent: got %+v, want nil", got)
	}
}

func TestClearWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := recovery.Clear(dir); err != nil {
		t.Fatalf("Clear when absent: %v", err)
	}
}

func TestClearRemovesFile(t *testing.T) {
	dir := t.TempDir()
	if err := recovery.Write(dir, testSentinel()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := recovery.Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, err := recovery.Read(dir)
	if err != nil {
		t.Fatalf("Read after Clear: %v", err)
	}
	if got != nil {
		t.Error("Read after Clear returned non-nil")
	}
}

func TestMalformedRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "active.json")
	if err := os.WriteFile(p, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := recovery.Read(dir)
	if err == nil {
		t.Error("Read malformed: want error, got nil")
	}
}

func TestAtomicWriteNoStrayTmp(t *testing.T) {
	dir := t.TempDir()
	if err := recovery.Write(dir, testSentinel()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "active.json.tmp" {
			t.Error("stray .tmp file left after successful Write")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "active.json")); err != nil {
		t.Errorf("active.json should exist after Write: %v", err)
	}
}

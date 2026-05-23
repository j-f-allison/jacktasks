package session

import (
	"errors"
	"testing"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
)

var (
	testCatID  = "cat-abc"
	testProjID = "proj-xyz"
)

// epoch is an arbitrary fixed reference time for all tests.
var epoch = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

func setupToActive(t *testing.T, minutes int) (*Machine, time.Time) {
	t.Helper()
	m := &Machine{}
	now := epoch

	if err := m.BeginSetup(); err != nil {
		t.Fatalf("BeginSetup: %v", err)
	}
	if err := m.SetCategory(testCatID, now); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}
	if err := m.SetProject(testProjID, now); err != nil {
		t.Fatalf("SetProject: %v", err)
	}
	if err := m.SetDuration(minutes, now); err != nil {
		t.Fatalf("SetDuration: %v", err)
	}
	if m.State() != StateActive {
		t.Fatalf("want Active, got %s", m.State())
	}
	return m, now
}

// --- Setup transitions ---

func TestSetupFlow(t *testing.T) {
	m := &Machine{}
	now := epoch

	if m.State() != StateIdle {
		t.Fatalf("want Idle, got %s", m.State())
	}

	if err := m.BeginSetup(); err != nil {
		t.Fatalf("BeginSetup: %v", err)
	}
	if m.State() != StateSetupCategory {
		t.Fatalf("want SetupCategory, got %s", m.State())
	}

	if err := m.SetCategory(testCatID, now); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}
	if m.State() != StateSetupProject {
		t.Fatalf("want SetupProject, got %s", m.State())
	}

	if err := m.SetProject(testProjID, now); err != nil {
		t.Fatalf("SetProject: %v", err)
	}
	if m.State() != StateSetupDuration {
		t.Fatalf("want SetupDuration, got %s", m.State())
	}

	if err := m.SetDuration(25, now); err != nil {
		t.Fatalf("SetDuration: %v", err)
	}
	if m.State() != StateActive {
		t.Fatalf("want Active, got %s", m.State())
	}
	if m.SessionID() == "" {
		t.Fatal("expected non-empty session ID after SetDuration")
	}
}

func TestSetCategoryWrongState(t *testing.T) {
	m, now := setupToActive(t, 25)
	err := m.SetCategory(testCatID, now)
	if !errors.Is(err, ErrWrongState) {
		t.Errorf("want ErrWrongState, got %v", err)
	}
}

func TestSetCategoryEmpty(t *testing.T) {
	m := &Machine{}
	_ = m.BeginSetup()
	if err := m.SetCategory("", epoch); err == nil {
		t.Fatal("expected error for empty categoryID")
	}
}

func TestBeginSetupWrongState(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = now
	if err := m.BeginSetup(); !errors.Is(err, ErrWrongState) {
		t.Errorf("want ErrWrongState, got %v", err)
	}
}

func TestSetDurationZero(t *testing.T) {
	m := &Machine{}
	_ = m.SetCategory(testCatID, epoch)
	_ = m.SetProject(testProjID, epoch)
	if err := m.SetDuration(0, epoch); err == nil {
		t.Fatal("expected error for zero duration")
	}
}

// --- Pause / Resume ---

func TestPauseResume(t *testing.T) {
	m, now := setupToActive(t, 25)

	pauseAt := now.Add(10 * time.Minute)
	if err := m.Pause(pauseAt); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if m.State() != StatePaused {
		t.Fatalf("want Paused, got %s", m.State())
	}

	// 5-minute pause
	resumeAt := pauseAt.Add(5 * time.Minute)
	if err := m.Resume(resumeAt); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if m.State() != StateActive {
		t.Fatalf("want Active, got %s", m.State())
	}

	// target end should have shifted forward by 5 minutes
	wantTarget := now.Add(25*time.Minute + 5*time.Minute)
	if !m.targetEnd.Equal(wantTarget) {
		t.Errorf("targetEnd = %v, want %v", m.targetEnd, wantTarget)
	}
}

func TestPauseWrongState(t *testing.T) {
	m := &Machine{}
	if err := m.Pause(epoch); !errors.Is(err, ErrWrongState) {
		t.Errorf("want ErrWrongState, got %v", err)
	}
}

func TestResumeWrongState(t *testing.T) {
	m, now := setupToActive(t, 25)
	if err := m.Resume(now); !errors.Is(err, ErrWrongState) {
		t.Errorf("want ErrWrongState, got %v", err)
	}
}

// --- Captures ---

func TestAddCapture(t *testing.T) {
	m, now := setupToActive(t, 25)

	captureAt := now.Add(5 * time.Minute)
	if err := m.AddCapture("buy milk", captureAt); err != nil {
		t.Fatalf("AddCapture: %v", err)
	}
	if err := m.AddCapture("check email", captureAt.Add(time.Minute)); err != nil {
		t.Fatalf("AddCapture 2: %v", err)
	}

	caps := m.Captures()
	if len(caps) != 2 {
		t.Fatalf("got %d captures, want 2", len(caps))
	}
	if caps[0].Text != "buy milk" {
		t.Errorf("caps[0].Text = %q", caps[0].Text)
	}
	if caps[0].ID == "" {
		t.Error("capture ID should be non-empty")
	}
}

func TestAddCaptureWhilePaused(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.Pause(now.Add(5 * time.Minute))

	if err := m.AddCapture("a thought", now.Add(6*time.Minute)); err != nil {
		t.Fatalf("AddCapture while paused: %v", err)
	}
	if len(m.Captures()) != 1 {
		t.Errorf("want 1 capture, got %d", len(m.Captures()))
	}
}

func TestAddCaptureEmptyText(t *testing.T) {
	m, now := setupToActive(t, 25)
	if err := m.AddCapture("", now); err == nil {
		t.Fatal("expected error for empty text")
	}
}

// --- Extend ---

func TestExtend(t *testing.T) {
	m, now := setupToActive(t, 25)

	before := m.targetEnd
	if err := m.Extend(10, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("Extend: %v", err)
	}
	want := before.Add(10 * time.Minute)
	if !m.targetEnd.Equal(want) {
		t.Errorf("targetEnd = %v, want %v", m.targetEnd, want)
	}
}

func TestExtendZero(t *testing.T) {
	m, now := setupToActive(t, 25)
	if err := m.Extend(0, now); err == nil {
		t.Fatal("expected error for zero extension")
	}
}

// --- End + duration accounting ---

func TestEndCompletedStatus(t *testing.T) {
	m, now := setupToActive(t, 25)

	// end exactly at planned duration
	endAt := now.Add(25 * time.Minute)
	if err := m.End(endAt); err != nil {
		t.Fatalf("End: %v", err)
	}
	if m.State() != StateEndingNotes {
		t.Fatalf("want EndingNotes, got %s", m.State())
	}
	if m.Status() != store.SessionCompleted {
		t.Errorf("Status = %q, want completed", m.Status())
	}
}

func TestEndEndedEarlyStatus(t *testing.T) {
	m, now := setupToActive(t, 25)

	// end after 10 minutes — short of planned 25
	if err := m.End(now.Add(10 * time.Minute)); err != nil {
		t.Fatalf("End: %v", err)
	}
	if m.Status() != store.SessionEndedEarly {
		t.Errorf("Status = %q, want ended_early", m.Status())
	}
}

func TestActualDurationNoPauses(t *testing.T) {
	m, now := setupToActive(t, 25)

	endAt := now.Add(20 * time.Minute)
	_ = m.End(endAt)

	if m.actualDurationSec() != 20*60 {
		t.Errorf("actualDurationSec = %d, want %d", m.actualDurationSec(), 20*60)
	}
}

func TestActualDurationWithPauses(t *testing.T) {
	m, now := setupToActive(t, 30)

	// pause for 3 minutes at T+10
	_ = m.Pause(now.Add(10 * time.Minute))
	_ = m.Resume(now.Add(13 * time.Minute))

	// pause for 2 minutes at T+20
	_ = m.Pause(now.Add(20 * time.Minute))
	_ = m.Resume(now.Add(22 * time.Minute))

	// end at T+35 (35 min elapsed, 5 min paused → 30 min actual)
	_ = m.End(now.Add(35 * time.Minute))

	want := 30 * 60
	if m.actualDurationSec() != want {
		t.Errorf("actualDurationSec = %d, want %d", m.actualDurationSec(), want)
	}
}

func TestEndWhilePausedClosesInterval(t *testing.T) {
	m, now := setupToActive(t, 30)

	// pause at T+10, never resume — end at T+15
	_ = m.Pause(now.Add(10 * time.Minute))
	_ = m.End(now.Add(15 * time.Minute))

	// 15 min elapsed, 5 min paused → 10 min actual
	want := 10 * 60
	if m.actualDurationSec() != want {
		t.Errorf("actualDurationSec = %d, want %d", m.actualDurationSec(), want)
	}
}

// --- TimeRemaining ---

func TestTimeRemainingActive(t *testing.T) {
	m, now := setupToActive(t, 25)

	rem := m.TimeRemaining(now.Add(10 * time.Minute))
	want := 15 * time.Minute
	if rem != want {
		t.Errorf("TimeRemaining = %v, want %v", rem, want)
	}
}

func TestTimeRemainingPaused(t *testing.T) {
	m, now := setupToActive(t, 25)

	// pause at T+10
	_ = m.Pause(now.Add(10 * time.Minute))

	// check remaining at T+15 while still paused: 10 min worked, 15 remain
	rem := m.TimeRemaining(now.Add(15 * time.Minute))
	want := 15 * time.Minute
	if rem != want {
		t.Errorf("TimeRemaining while paused = %v, want %v", rem, want)
	}
}

// --- EndingNotes → WhatNext ---

func TestSetEndNotes(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.End(now.Add(25 * time.Minute))

	if err := m.SetEndNotes("good session", now.Add(26*time.Minute)); err != nil {
		t.Fatalf("SetEndNotes: %v", err)
	}
	if m.State() != StateWhatNext {
		t.Fatalf("want WhatNext, got %s", m.State())
	}
}

// --- WhatNext actions ---

func TestContinueSession(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.End(now.Add(25 * time.Minute))
	_ = m.SetEndNotes("", now.Add(26*time.Minute))

	firstID := m.SessionID()
	// continue with 10 more minutes
	contAt := now.Add(27 * time.Minute)
	if err := m.ContinueSession(10, contAt); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	if m.State() != StateActive {
		t.Fatalf("want Active, got %s", m.State())
	}
	if m.SessionID() == firstID {
		t.Error("ContinueSession should assign a new session ID")
	}
	if m.CategoryID() != testCatID || m.ProjectID() != testProjID {
		t.Error("ContinueSession should preserve category and project")
	}
	if len(m.Captures()) != 0 {
		t.Error("captures should be cleared for new session")
	}
}

func TestNewSession(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.End(now.Add(25 * time.Minute))
	_ = m.SetEndNotes("", now.Add(26*time.Minute))

	if err := m.NewSession(now.Add(27 * time.Minute)); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if m.State() != StateSetupCategory {
		t.Fatalf("want SetupCategory, got %s", m.State())
	}
	if m.CategoryID() != "" || m.ProjectID() != "" {
		t.Error("NewSession should clear category and project")
	}
}

func TestBreakCycle(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.End(now.Add(25 * time.Minute))
	_ = m.SetEndNotes("", now.Add(26*time.Minute))

	if err := m.StartBreak(now.Add(27 * time.Minute)); err != nil {
		t.Fatalf("StartBreak: %v", err)
	}
	if m.State() != StateBreak {
		t.Fatalf("want Break, got %s", m.State())
	}

	if err := m.EndBreak(now.Add(32 * time.Minute)); err != nil {
		t.Fatalf("EndBreak: %v", err)
	}
	if m.State() != StateWhatNext {
		t.Fatalf("want WhatNext, got %s", m.State())
	}
}

func TestFinish(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.End(now.Add(25 * time.Minute))
	_ = m.SetEndNotes("", now.Add(26*time.Minute))

	if err := m.Finish(now.Add(27 * time.Minute)); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if m.State() != StateIdle {
		t.Fatalf("want Idle, got %s", m.State())
	}
}

// --- ToStoreSessionInput ---

func TestToStoreSessionInput(t *testing.T) {
	m, now := setupToActive(t, 25)
	_ = m.End(now.Add(20 * time.Minute))
	_ = m.SetEndNotes("wrap-up note", now.Add(21*time.Minute))

	in, err := m.ToStoreSessionInput("test-device")
	if err != nil {
		t.Fatalf("ToStoreSessionInput: %v", err)
	}
	if in.CategoryID != testCatID {
		t.Errorf("CategoryID = %q, want %q", in.CategoryID, testCatID)
	}
	if in.ProjectID != testProjID {
		t.Errorf("ProjectID = %q, want %q", in.ProjectID, testProjID)
	}
	if in.PlannedDurationMin != 25 {
		t.Errorf("PlannedDurationMin = %d, want 25", in.PlannedDurationMin)
	}
	if in.ActualDurationSec != 20*60 {
		t.Errorf("ActualDurationSec = %d, want %d", in.ActualDurationSec, 20*60)
	}
	if in.Status != store.SessionEndedEarly {
		t.Errorf("Status = %q, want ended_early", in.Status)
	}
	if in.EndNotes != "wrap-up note" {
		t.Errorf("EndNotes = %q", in.EndNotes)
	}
	if in.DeviceID != "test-device" {
		t.Errorf("DeviceID = %q", in.DeviceID)
	}
}

func TestToStoreSessionInputBeforeEnd(t *testing.T) {
	m, now := setupToActive(t, 25)
	_, err := m.ToStoreSessionInput("dev")
	_ = now
	if err == nil {
		t.Fatal("expected error before End is called")
	}
}

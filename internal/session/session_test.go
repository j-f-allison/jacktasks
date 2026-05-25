package session

import (
	"errors"
	"testing"
	"time"

	"github.com/j-f-allison/jacktasks/internal/recovery"
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
	if err := m.SetProject(testProjID, now); err != nil {
		t.Fatalf("SetProject: %v", err)
	}
	if err := m.SetCategory(testCatID, now); err != nil {
		t.Fatalf("SetCategory: %v", err)
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
	if m.State() != StateSetupProject {
		t.Fatalf("want SetupProject, got %s", m.State())
	}

	if err := m.SetProject(testProjID, now); err != nil {
		t.Fatalf("SetProject: %v", err)
	}
	if m.State() != StateSetupCategory {
		t.Fatalf("want SetupCategory, got %s", m.State())
	}

	if err := m.SetCategory(testCatID, now); err != nil {
		t.Fatalf("SetCategory: %v", err)
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
	_ = m.SetProject(testProjID, epoch)
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
	_ = m.SetProject(testProjID, epoch)
	_ = m.SetCategory(testCatID, epoch)
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

func TestResumeFromEndingNotesAfterTimerExpiry(t *testing.T) {
	m, now := setupToActive(t, 25)

	// timer expires naturally
	endAt := now.Add(25 * time.Minute)
	if err := m.End(endAt); err != nil {
		t.Fatalf("End: %v", err)
	}
	if m.State() != StateEndingNotes {
		t.Fatalf("setup: want EndingNotes, got %s", m.State())
	}

	if err := m.ResumeFromEndingNotes(endAt); err != nil {
		t.Fatalf("ResumeFromEndingNotes: %v", err)
	}
	if m.State() != StateActive {
		t.Fatalf("state = %s, want Active", m.State())
	}
	if !m.endedAt.IsZero() {
		t.Errorf("endedAt should be cleared")
	}
	if m.status != "" {
		t.Errorf("status = %q, want cleared", m.status)
	}
	// Timer had expired (targetEnd == endAt), so it should be reset to now
	// so a follow-up Extend gives meaningful remaining time.
	if !m.targetEnd.Equal(endAt) {
		t.Errorf("targetEnd = %v, want reset to now (%v)", m.targetEnd, endAt)
	}

	// Extend by 5 should give exactly 5 min remaining.
	if err := m.Extend(5, endAt); err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if rem := m.TimeRemaining(endAt); rem != 5*time.Minute {
		t.Errorf("TimeRemaining = %v, want 5m", rem)
	}
}

func TestResumeFromEndingNotesEarlyEnd(t *testing.T) {
	// User ended at 10 of a 25-min session, then Tabs to extend. The
	// original targetEnd is still 15 min in the future, so it should be
	// preserved (not reset to now).
	m, now := setupToActive(t, 25)
	endAt := now.Add(10 * time.Minute)
	if err := m.End(endAt); err != nil {
		t.Fatalf("End: %v", err)
	}
	originalTarget := m.targetEnd

	if err := m.ResumeFromEndingNotes(endAt); err != nil {
		t.Fatalf("ResumeFromEndingNotes: %v", err)
	}
	if !m.targetEnd.Equal(originalTarget) {
		t.Errorf("targetEnd = %v, want preserved %v", m.targetEnd, originalTarget)
	}
}

func TestResumeFromEndingNotesWrongState(t *testing.T) {
	m, now := setupToActive(t, 25)
	if err := m.ResumeFromEndingNotes(now); err == nil {
		t.Fatalf("want ErrWrongState from Active, got nil")
	}
}

func TestEndEndedEarlyStatus(t *testing.T) {
	m, now := setupToActive(t, 25)

	// end after 10 minutes — 15 remaining, well above the 5-min threshold
	if err := m.End(now.Add(10 * time.Minute)); err != nil {
		t.Fatalf("End: %v", err)
	}
	if m.Status() != store.SessionEndedEarly {
		t.Errorf("Status = %q, want ended_early", m.Status())
	}
}

func TestEndNearCompleteIsCompleted(t *testing.T) {
	// Ending with exactly 5 min remaining should be completed, not ended_early.
	m, now := setupToActive(t, 25)
	if err := m.End(now.Add(20 * time.Minute)); err != nil {
		t.Fatalf("End: %v", err)
	}
	if m.Status() != store.SessionCompleted {
		t.Errorf("5 min remaining: Status = %q, want completed", m.Status())
	}

	// 6 min remaining — just over the threshold — is still ended_early.
	m2, now2 := setupToActive(t, 25)
	if err := m2.End(now2.Add(19 * time.Minute)); err != nil {
		t.Fatalf("End: %v", err)
	}
	if m2.Status() != store.SessionEndedEarly {
		t.Errorf("6 min remaining: Status = %q, want ended_early", m2.Status())
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
	if m.State() != StateSetupProject {
		t.Fatalf("want SetupProject, got %s", m.State())
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
	// 20 of 25 min = 5 min remaining, which is at the near-complete threshold → completed.
	if in.Status != store.SessionCompleted {
		t.Errorf("Status = %q, want completed", in.Status)
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

func TestSetProjectEmpty(t *testing.T) {
	m := &Machine{}
	now := epoch
	_ = m.BeginSetup()

	if err := m.SetProject("", now); err != nil {
		t.Fatalf("SetProject empty: %v", err)
	}
	if m.State() != StateSetupCategory {
		t.Errorf("want SetupCategory, got %s", m.State())
	}
	if m.ProjectID() != "" {
		t.Errorf("ProjectID = %q, want empty", m.ProjectID())
	}
}

// --- Snapshot / Hydrate ---

func TestSnapshotRoundTripActive(t *testing.T) {
	m, now := setupToActive(t, 25)
	capAt := now.Add(5 * time.Minute)
	_ = m.AddCapture("buy milk", capAt)

	snapAt := now.Add(10 * time.Minute)
	snap, err := m.Snapshot(snapAt, "MyProject", "Coding")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snap.State != "active" {
		t.Errorf("snap.State = %q, want active", snap.State)
	}
	if snap.SessionID != m.SessionID() {
		t.Errorf("snap.SessionID = %q, want %q", snap.SessionID, m.SessionID())
	}
	if snap.ProjectName != "MyProject" {
		t.Errorf("snap.ProjectName = %q, want MyProject", snap.ProjectName)
	}
	if len(snap.Captures) != 1 {
		t.Fatalf("len(snap.Captures) = %d, want 1", len(snap.Captures))
	}

	m2, err := Hydrate(snap, snapAt.Add(time.Second))
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if m2.State() != StateActive {
		t.Errorf("hydrated state = %s, want Active", m2.State())
	}
	if m2.CategoryID() != testCatID {
		t.Errorf("CategoryID = %q, want %q", m2.CategoryID(), testCatID)
	}
	if m2.ProjectID() != testProjID {
		t.Errorf("ProjectID = %q, want %q", m2.ProjectID(), testProjID)
	}
	if m2.PlannedMin() != 25 {
		t.Errorf("PlannedMin = %d, want 25", m2.PlannedMin())
	}
	if len(m2.Captures()) != 1 {
		t.Fatalf("len(Captures) = %d, want 1", len(m2.Captures()))
	}
	if m2.Captures()[0].Text != "buy milk" {
		t.Errorf("Captures[0].Text = %q", m2.Captures()[0].Text)
	}
}

func TestSnapshotRoundTripPaused(t *testing.T) {
	m, now := setupToActive(t, 25)

	pauseAt := now.Add(10 * time.Minute)
	_ = m.Pause(pauseAt)

	snapAt := now.Add(12 * time.Minute)
	snap, err := m.Snapshot(snapAt, "Proj", "Cat")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snap.State != "paused" {
		t.Errorf("snap.State = %q, want paused", snap.State)
	}
	if snap.CurrentPauseStart == 0 {
		t.Error("expected CurrentPauseStart set for paused state")
	}
	if len(snap.Pauses) != 0 {
		t.Errorf("expected no completed pauses, got %d", len(snap.Pauses))
	}

	m2, err := Hydrate(snap, snapAt.Add(time.Second))
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if m2.State() != StatePaused {
		t.Errorf("hydrated state = %s, want Paused", m2.State())
	}
}

func TestSnapshotWhileIdle(t *testing.T) {
	m := &Machine{}
	_, err := m.Snapshot(epoch, "", "")
	if err == nil {
		t.Fatal("expected error for Snapshot in Idle state")
	}
}

func TestHydratePausedWithoutCurrentPauseStart(t *testing.T) {
	s := recovery.Sentinel{
		SessionID:          "s",
		CategoryID:         "c",
		PlannedDurationMin: 25,
		StartedAt:          epoch.Add(-10 * time.Minute).Unix(),
		TargetEndAt:        epoch.Add(15 * time.Minute).Unix(),
		Pauses:             []recovery.PauseRecord{},
		State:              "paused",
		// CurrentPauseStart deliberately omitted
	}
	_, err := Hydrate(s, epoch)
	if err == nil {
		t.Fatal("expected error: paused state without current_pause_start")
	}
}

func TestHydrateInvalidState(t *testing.T) {
	s := recovery.Sentinel{
		SessionID:          "s",
		CategoryID:         "c",
		PlannedDurationMin: 25,
		StartedAt:          epoch.Add(-10 * time.Minute).Unix(),
		TargetEndAt:        epoch.Add(15 * time.Minute).Unix(),
		State:              "idle",
	}
	_, err := Hydrate(s, epoch)
	if err == nil {
		t.Fatal("expected error for invalid state")
	}
}

func TestSnapshotWithCompletedPause(t *testing.T) {
	m, now := setupToActive(t, 30)

	// complete one pause interval
	_ = m.Pause(now.Add(5 * time.Minute))
	_ = m.Resume(now.Add(8 * time.Minute))

	snap, err := m.Snapshot(now.Add(10*time.Minute), "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Pauses) != 1 {
		t.Fatalf("expected 1 completed pause, got %d", len(snap.Pauses))
	}
	if snap.CurrentPauseStart != 0 {
		t.Error("expected CurrentPauseStart to be 0 in Active state")
	}

	m2, err := Hydrate(snap, now.Add(10*time.Minute+time.Second))
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if m2.State() != StateActive {
		t.Errorf("want Active, got %s", m2.State())
	}
}

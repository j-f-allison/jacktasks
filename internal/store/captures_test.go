package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// captureFixtures creates the category, project, and session needed for
// capture tests, returning the session ID.
func captureFixtures(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)
	sess, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID:         catID,
		ProjectID:          projID,
		PlannedDurationMin: 25,
		ActualDurationSec:  1500,
		StartedAt:          time.Now().Add(-25 * time.Minute),
		EndedAt:            time.Now(),
		Status:             SessionCompleted,
		DeviceID:           "macbook-test",
	})
	if err != nil {
		t.Fatalf("fixture session: %v", err)
	}
	return sess.ID
}

func TestCreateCapture(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessID := captureFixtures(t, s)

	c, err := s.CreateCapture(ctx, "", sessID, "email the prof about the deadline")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if c.SessionID != sessID {
		t.Errorf("SessionID = %q, want %q", c.SessionID, sessID)
	}
	if c.Text != "email the prof about the deadline" {
		t.Errorf("Text = %q", c.Text)
	}
	if c.Cleared || c.SentToReminders {
		t.Error("flags should default to false")
	}
}

func TestCreateCaptureRequiresText(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessID := captureFixtures(t, s)

	if _, err := s.CreateCapture(ctx, "", sessID, ""); err == nil {
		t.Fatal("expected error for empty text, got nil")
	}
}

func TestCreateCaptureInvalidSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateCapture(ctx, "", "no-such-session", "orphan"); err == nil {
		t.Fatal("expected FK error, got nil")
	}
}

func TestListCapturesBySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessID := captureFixtures(t, s)

	list, err := s.ListCapturesBySession(ctx, sessID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty, got %d", len(list))
	}

	texts := []string{"first thought", "second thought", "third thought"}
	for _, txt := range texts {
		if _, err := s.CreateCapture(ctx, "", sessID, txt); err != nil {
			t.Fatalf("create %q: %v", txt, err)
		}
	}

	list, err = s.ListCapturesBySession(ctx, sessID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d, want 3", len(list))
	}
	// captured_at granularity is seconds — three captures in the same
	// second are not strictly ordered. Verify membership, not order.
	got := make(map[string]bool, 3)
	for _, c := range list {
		got[c.Text] = true
	}
	for _, want := range texts {
		if !got[want] {
			t.Errorf("missing capture %q", want)
		}
	}
}

func TestMarkCaptureCleared(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessID := captureFixtures(t, s)

	c, err := s.CreateCapture(ctx, "", sessID, "to be cleared")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.MarkCaptureCleared(ctx, c.ID); err != nil {
		t.Fatalf("mark cleared: %v", err)
	}

	list, _ := s.ListCapturesBySession(ctx, sessID)
	if len(list) != 1 || !list[0].Cleared {
		t.Errorf("expected cleared=true, got %+v", list)
	}

	if err := s.MarkCaptureCleared(ctx, "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestMarkCaptureSentToReminders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sessID := captureFixtures(t, s)

	c, err := s.CreateCapture(ctx, "", sessID, "to be sent")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.MarkCaptureSentToReminders(ctx, c.ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	list, _ := s.ListCapturesBySession(ctx, sessID)
	if len(list) != 1 || !list[0].SentToReminders {
		t.Errorf("expected sent_to_reminders=true, got %+v", list)
	}
}
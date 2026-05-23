package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// sessionFixtures creates a project + category for session tests.
func sessionFixtures(t *testing.T, s *Store) (catID, projID string) {
	t.Helper()
	ctx := context.Background()
	proj, err := s.CreateProject(ctx, "memo")
	if err != nil {
		t.Fatalf("fixture project: %v", err)
	}
	cat, err := s.CreateCategory(ctx, "Coding", proj.ID)
	if err != nil {
		t.Fatalf("fixture category: %v", err)
	}
	return cat.ID, proj.ID
}

func TestCreateSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	started := time.Now().Add(-25 * time.Minute)
	ended := time.Now()

	sess, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID:         catID,
		ProjectID:          projID,
		PlannedDurationMin: 25,
		ActualDurationSec:  1500,
		StartedAt:          started,
		EndedAt:            ended,
		EndNotes:           "found a good case to cite",
		Status:             SessionCompleted,
		DeviceID:           "macbook-test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if sess.Status != SessionCompleted {
		t.Errorf("Status = %q, want %q", sess.Status, SessionCompleted)
	}
	if sess.EndNotes != "found a good case to cite" {
		t.Errorf("EndNotes = %q", sess.EndNotes)
	}
}

func TestCreateSessionInvalidStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	_, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID: catID,
		ProjectID:  projID,
		Status:     SessionStatus("bogus"),
		DeviceID:   "macbook-test",
	})
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
}

func TestCreateSessionRequiresDeviceID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	_, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID: catID,
		ProjectID:  projID,
		Status:     SessionCompleted,
	})
	if err == nil {
		t.Fatal("expected error when device_id missing, got nil")
	}
}

func TestCreateSessionInvalidProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, _ := sessionFixtures(t, s)

	_, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID: catID,
		ProjectID:  "no-such-project",
		Status:     SessionCompleted,
		DeviceID:   "macbook-test",
	})
	if err == nil {
		t.Fatal("expected FK error for invalid project_id, got nil")
	}
}

func TestCreateSessionNoProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, _ := sessionFixtures(t, s)

	sess, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID:         catID,
		ProjectID:          "",
		PlannedDurationMin: 25,
		ActualDurationSec:  1500,
		StartedAt:          time.Now().Add(-25 * time.Minute),
		EndedAt:            time.Now(),
		Status:             SessionCompleted,
		DeviceID:           "macbook-test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty string", sess.ProjectID)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProjectID != "" {
		t.Errorf("roundtrip ProjectID = %q, want empty string", got.ProjectID)
	}
}

func TestListSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	list, err := s.ListSessions(ctx, 0)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty, got %d", len(list))
	}

	now := time.Now()
	for i := 0; i < 3; i++ {
		_, err := s.CreateSession(ctx, CreateSessionInput{
			CategoryID:         catID,
			ProjectID:          projID,
			PlannedDurationMin: 25,
			ActualDurationSec:  1500,
			StartedAt:          now.Add(time.Duration(i) * time.Minute),
			EndedAt:            now.Add(time.Duration(i)*time.Minute + 25*time.Minute),
			Status:             SessionCompleted,
			DeviceID:           "macbook-test",
		})
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	list, err = s.ListSessions(ctx, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d, want 3", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].StartedAt.Before(list[i].StartedAt) {
			t.Errorf("list[%d] (%v) before list[%d] (%v); expected DESC",
				i-1, list[i-1].StartedAt, i, list[i].StartedAt)
		}
	}
}

func TestGetSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	created, err := s.CreateSession(ctx, CreateSessionInput{
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
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID || got.CategoryID != created.CategoryID {
		t.Errorf("mismatch: got %+v", got)
	}

	if _, err := s.GetSession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got err %v, want ErrNotFound", err)
	}
}

func TestLatestSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.LatestSession(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty store: got err %v, want ErrNotFound", err)
	}

	catID, projID := sessionFixtures(t, s)
	now := time.Now()

	for i := 0; i < 3; i++ {
		_, err := s.CreateSession(ctx, CreateSessionInput{
			CategoryID:         catID,
			ProjectID:          projID,
			PlannedDurationMin: 25,
			ActualDurationSec:  1500,
			StartedAt:          now.Add(time.Duration(i) * time.Minute),
			EndedAt:            now.Add(time.Duration(i)*time.Minute + 25*time.Minute),
			Status:             SessionEndedEarly,
			DeviceID:           "macbook-test",
		})
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	latest, err := s.LatestSession(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Status != SessionEndedEarly {
		t.Errorf("Status = %q, want %q", latest.Status, SessionEndedEarly)
	}
	// latest should be the one with StartedAt = now + 2 minutes
	if !latest.StartedAt.Equal(now.Add(2 * time.Minute).Truncate(time.Second)) {
		t.Errorf("StartedAt = %v, want %v", latest.StartedAt, now.Add(2*time.Minute).Truncate(time.Second))
	}
}

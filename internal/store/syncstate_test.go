package store

import (
	"context"
	"testing"
)

func TestGetSyncStateAbsent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, err := s.GetSyncState(ctx, "projects")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st.LastPullAt != 0 || st.LastPushAt != 0 {
		t.Errorf("expected zero state, got %+v", st)
	}
}

func TestSetLastPushAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetLastPushAt(ctx, "projects", 1000); err != nil {
		t.Fatalf("set push: %v", err)
	}
	st, err := s.GetSyncState(ctx, "projects")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st.LastPushAt != 1000 {
		t.Errorf("LastPushAt = %d, want 1000", st.LastPushAt)
	}
	if st.LastPullAt != 0 {
		t.Errorf("LastPullAt = %d, want 0 (should be untouched)", st.LastPullAt)
	}
}

func TestSetLastPullAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetLastPullAt(ctx, "sessions", 2000); err != nil {
		t.Fatalf("set pull: %v", err)
	}
	st, err := s.GetSyncState(ctx, "sessions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st.LastPullAt != 2000 {
		t.Errorf("LastPullAt = %d, want 2000", st.LastPullAt)
	}
	if st.LastPushAt != 0 {
		t.Errorf("LastPushAt = %d, want 0 (should be untouched)", st.LastPushAt)
	}
}

func TestSyncStateUpsertBothFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Set push, then pull — each should not clobber the other.
	if err := s.SetLastPushAt(ctx, "captures", 100); err != nil {
		t.Fatalf("set push: %v", err)
	}
	if err := s.SetLastPullAt(ctx, "captures", 200); err != nil {
		t.Fatalf("set pull: %v", err)
	}

	st, err := s.GetSyncState(ctx, "captures")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st.LastPushAt != 100 {
		t.Errorf("LastPushAt = %d, want 100", st.LastPushAt)
	}
	if st.LastPullAt != 200 {
		t.Errorf("LastPullAt = %d, want 200", st.LastPullAt)
	}

	// Advance push; pull should be undisturbed.
	if err := s.SetLastPushAt(ctx, "captures", 300); err != nil {
		t.Fatalf("advance push: %v", err)
	}
	st, _ = s.GetSyncState(ctx, "captures")
	if st.LastPushAt != 300 {
		t.Errorf("LastPushAt = %d, want 300", st.LastPushAt)
	}
	if st.LastPullAt != 200 {
		t.Errorf("LastPullAt = %d, want 200 (unchanged)", st.LastPullAt)
	}
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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

// TestMigrateCapturesUpdatedAt verifies that migrateCapturesUpdatedAt adds the
// column to a pre-migration DB, backfills updated_at from created_at, and is
// idempotent when called a second time.
func TestMigrateCapturesUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Build a minimal DB without the updated_at column using the old schema.
	oldSchema := `
		CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			deleted_at INTEGER, archived INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE categories (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			project_id TEXT REFERENCES projects(id),
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			deleted_at INTEGER, archived INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE sessions (id TEXT PRIMARY KEY,
			project_id TEXT REFERENCES projects(id),
			category_id TEXT NOT NULL REFERENCES categories(id),
			planned_duration_min INTEGER NOT NULL,
			actual_duration_sec INTEGER NOT NULL,
			started_at INTEGER NOT NULL, ended_at INTEGER NOT NULL,
			end_notes TEXT, status TEXT NOT NULL,
			created_at INTEGER NOT NULL, device_id TEXT NOT NULL);
		CREATE TABLE captures (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			text TEXT NOT NULL,
			captured_at INTEGER NOT NULL,
			cleared INTEGER NOT NULL DEFAULT 0,
			sent_to_reminders INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		);
		CREATE TABLE sync_state (table_name TEXT PRIMARY KEY,
			last_pull_at INTEGER, last_push_at INTEGER);
		CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT);
	`
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := rawDB.Exec(oldSchema); err != nil {
		rawDB.Close()
		t.Fatalf("create old schema: %v", err)
	}
	// Insert a capture row so we can verify the backfill.
	const createdAt = int64(1716480000)
	if _, err := rawDB.Exec(
		`INSERT INTO projects VALUES ('p1','proj',?,?,NULL,0)`, createdAt, createdAt,
	); err != nil {
		rawDB.Close()
		t.Fatalf("insert project: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO categories VALUES ('c1','cat','p1',?,?,NULL,0)`, createdAt, createdAt,
	); err != nil {
		rawDB.Close()
		t.Fatalf("insert category: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO sessions VALUES ('s1','p1','c1',25,1500,?,?,NULL,'completed',?,'dev')`,
		createdAt, createdAt, createdAt,
	); err != nil {
		rawDB.Close()
		t.Fatalf("insert session: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO captures VALUES ('cap1','s1','thought',?,0,0,?)`,
		createdAt, createdAt,
	); err != nil {
		rawDB.Close()
		t.Fatalf("insert capture: %v", err)
	}
	rawDB.Close()

	// Open via Store.Open — this runs the migration.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// The existing capture should have updated_at == created_at (backfilled).
	list, err := s.ListCapturesBySession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("list captures: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 capture, got %d", len(list))
	}
	if list[0].UpdatedAt.Unix() != createdAt {
		t.Errorf("UpdatedAt = %d, want %d (backfill from created_at)", list[0].UpdatedAt.Unix(), createdAt)
	}

	// Second Open should be a no-op (column already present).
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}
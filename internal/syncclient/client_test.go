package syncclient_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncclient"
	"github.com/j-f-allison/jacktasks/internal/syncserver"
)

const testToken = "test-token"

// newStore opens a fresh isolated store.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// newSyncServer starts an httptest server backed by its own store.
// Returns the server and its underlying store (for seeding / inspection).
func newSyncServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	serverSt := newStore(t)
	srv := httptest.NewServer(syncserver.NewMux(serverSt, testToken))
	t.Cleanup(srv.Close)
	return srv, serverSt
}

// syncCfg builds a Config pointing at srv.
func syncCfg(srv *httptest.Server) syncclient.Config {
	return syncclient.Config{URL: srv.URL, Token: testToken}
}

// seedProject creates a project + category + session + capture in st and returns
// them. All timestamps use the provided base Unix second to keep tests deterministic.
func seedProject(t *testing.T, st *store.Store, name string, base int64) (proj *store.Project, cat *store.Category) {
	t.Helper()
	ctx := context.Background()
	var err error
	proj, err = st.CreateProject(ctx, name)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	cat, err = st.CreateCategory(ctx, "Coding", proj.ID)
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	return proj, cat
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestSyncRoundTrip: Mac A creates data, syncs up; Mac B syncs down and gets it.
func TestSyncRoundTrip(t *testing.T) {
	srv, _ := newSyncServer(t)
	macA := newStore(t)
	macB := newStore(t)
	ctx := context.Background()
	cfg := syncCfg(srv)

	// Mac A: create a project + category + session + capture.
	proj, cat := seedProject(t, macA, "jacktasks", 0)
	sess, err := macA.CreateSession(ctx, store.CreateSessionInput{
		CategoryID:         cat.ID,
		ProjectID:          proj.ID,
		PlannedDurationMin: 25,
		ActualDurationSec:  1500,
		StartedAt:          time.Now().Add(-25 * time.Minute),
		EndedAt:            time.Now(),
		Status:             store.SessionCompleted,
		DeviceID:           "mac-a",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := macA.CreateCapture(ctx, "", sess.ID, "remember to write tests"); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	// Mac A syncs up.
	var out bytes.Buffer
	if err := syncclient.Sync(ctx, macA, cfg, &out); err != nil {
		t.Fatalf("Mac A sync: %v\noutput: %s", err, out.String())
	}
	t.Logf("Mac A output:\n%s", out.String())

	// Mac B syncs down.
	out.Reset()
	if err := syncclient.Sync(ctx, macB, cfg, &out); err != nil {
		t.Fatalf("Mac B sync: %v\noutput: %s", err, out.String())
	}
	t.Logf("Mac B output:\n%s", out.String())

	// Verify Mac B has Mac A's project.
	projects, err := macB.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "jacktasks" {
		t.Errorf("Mac B projects: %+v, want [{jacktasks}]", projects)
	}

	// Verify Mac B has the category.
	cats, err := macB.ListCategoriesByProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 1 || cats[0].Name != "Coding" {
		t.Errorf("Mac B categories: %+v, want [{Coding}]", cats)
	}

	// Verify Mac B has the session.
	sessions, err := macB.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != sess.ID {
		t.Errorf("Mac B sessions: got %d, want 1 with id %s", len(sessions), sess.ID)
	}

	// Verify Mac B has the capture.
	caps, err := macB.ListCapturesBySession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("list captures: %v", err)
	}
	if len(caps) != 1 || caps[0].Text != "remember to write tests" {
		t.Errorf("Mac B captures: %+v", caps)
	}
}

// TestSyncIdempotent: syncing again without new data sends 0 rows on second push.
func TestSyncIdempotent(t *testing.T) {
	srv, _ := newSyncServer(t)
	macA := newStore(t)
	ctx := context.Background()
	cfg := syncCfg(srv)

	seedProject(t, macA, "homelab", 0)

	// First sync.
	var out bytes.Buffer
	if err := syncclient.Sync(ctx, macA, cfg, &out); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	first := out.String()

	// Second sync — no new data.
	out.Reset()
	if err := syncclient.Sync(ctx, macA, cfg, &out); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	second := out.String()

	// First sync should have pushed non-zero rows for at least one table.
	allZero := true
	for _, line := range strings.Split(strings.TrimSpace(first), "\n") {
		if line != "" && !strings.Contains(line, "pushed 0,") {
			allZero = false
			break
		}
	}
	if allZero {
		t.Errorf("first sync: expected some non-zero pushes\n%s", first)
	}

	// Second sync should push 0 for every table (bookmarks advanced).
	for _, line := range strings.Split(strings.TrimSpace(second), "\n") {
		if !strings.Contains(line, "pushed 0") {
			t.Errorf("second sync non-zero push on line: %q", line)
		}
	}
}

// TestSyncLWWConvergence: both Macs edit the same project (different timestamps);
// after both sync, both have the newer version.
func TestSyncLWWConvergence(t *testing.T) {
	srv, _ := newSyncServer(t)
	macA := newStore(t)
	macB := newStore(t)
	ctx := context.Background()
	cfg := syncCfg(srv)

	// Mac A creates a project and syncs.
	proj, _ := seedProject(t, macA, "Original Name", 0)

	var out bytes.Buffer
	if err := syncclient.Sync(ctx, macA, cfg, &out); err != nil {
		t.Fatalf("Mac A first sync: %v", err)
	}

	// Mac B syncs down (gets the project).
	out.Reset()
	if err := syncclient.Sync(ctx, macB, cfg, &out); err != nil {
		t.Fatalf("Mac B first sync: %v", err)
	}

	// Verify Mac B has the project.
	projB, err := macB.GetProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("get project on Mac B: %v", err)
	}
	if projB.Name != "Original Name" {
		t.Errorf("Mac B project name = %q, want %q", projB.Name, "Original Name")
	}

	// Mac A updates its project name and syncs immediately.
	// This locks in Mac A's lastPullAt before Mac B makes its competing update.
	if err := macA.UpdateProject(ctx, proj.ID, "Mac A Name"); err != nil {
		t.Fatalf("update on Mac A: %v", err)
	}
	out.Reset()
	if err := syncclient.Sync(ctx, macA, cfg, &out); err != nil {
		t.Fatalf("Mac A second sync: %v", err)
	}
	// Server now has "Mac A Name". Mac A's lastPullAt ≈ now.

	// Mac B updates with a strictly newer timestamp (sleep ensures it).
	time.Sleep(2 * time.Second)
	if err := macB.UpdateProject(ctx, proj.ID, "Mac B Name"); err != nil {
		t.Fatalf("update on Mac B: %v", err)
	}

	// Mac B syncs: pushes "Mac B Name" (newer) → server accepts it (LWW win).
	// Mac B also pulls its own update back (no-op).
	out.Reset()
	if err := syncclient.Sync(ctx, macB, cfg, &out); err != nil {
		t.Fatalf("Mac B second sync: %v", err)
	}

	// Mac A syncs again: pushes nothing new, pulls "Mac B Name" from server.
	// "Mac B Name".updated_at > Mac A's lastPullAt → LWW applies on Mac A.
	out.Reset()
	if err := syncclient.Sync(ctx, macA, cfg, &out); err != nil {
		t.Fatalf("Mac A third sync: %v", err)
	}

	// Both Macs should now have "Mac B Name".
	projA, err := macA.GetProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("get project on Mac A: %v", err)
	}
	if projA.Name != "Mac B Name" {
		t.Errorf("Mac A converged name = %q, want %q", projA.Name, "Mac B Name")
	}
	projB, err = macB.GetProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("get project on Mac B after final sync: %v", err)
	}
	if projB.Name != "Mac B Name" {
		t.Errorf("Mac B converged name = %q, want %q", projB.Name, "Mac B Name")
	}
}

// TestSyncBadToken verifies that a wrong token causes an early error.
func TestSyncBadToken(t *testing.T) {
	srv, _ := newSyncServer(t)
	macA := newStore(t)
	ctx := context.Background()

	badCfg := syncclient.Config{URL: srv.URL, Token: "wrong-token"}
	seedProject(t, macA, "test", 0)

	var out bytes.Buffer
	err := syncclient.Sync(ctx, macA, badCfg, &out)
	if err == nil {
		t.Fatal("expected error with bad token, got nil")
	}
}

// TestSyncMissingConfig verifies that empty URL or Token is rejected immediately.
func TestSyncMissingConfig(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	tests := []struct {
		name string
		cfg  syncclient.Config
	}{
		{"empty URL", syncclient.Config{URL: "", Token: "tok"}},
		{"empty token", syncclient.Config{URL: "http://localhost", Token: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := syncclient.Sync(ctx, st, tt.cfg, &out); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

package syncserver_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
)

// seedSession inserts a project, a category under it, and one finished session,
// returning the project and category names for assertions.
func seedSession(t *testing.T, st *store.Store, notes string, status store.SessionStatus) (string, string) {
	t.Helper()
	ctx := context.Background()
	proj, err := st.CreateProject(ctx, "Website")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	cat, err := st.CreateCategory(ctx, "Coding", proj.ID)
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	start := time.Now().Add(-30 * time.Minute)
	_, err = st.CreateSession(ctx, store.CreateSessionInput{
		CategoryID:         cat.ID,
		ProjectID:          proj.ID,
		PlannedDurationMin: 25,
		ActualDurationSec:  1500,
		StartedAt:          start,
		EndedAt:            start.Add(25 * time.Minute),
		EndNotes:           notes,
		Status:             status,
		DeviceID:           "test-device",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return proj.Name, cat.Name
}

// getNoAuth fetches a path without any Authorization header.
func getNoAuth(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(body)
}

func TestWebSessionsRendersWithoutAuth(t *testing.T) {
	srv, st := newTestServer(t)
	projName, catName := seedSession(t, st, "wrapped up the parser", store.SessionCompleted)

	resp, body := getNoAuth(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	for _, want := range []string{projName, catName, "wrapped up the parser", "25m", "done"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestWebSessionsEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := getNoAuth(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "No sessions logged yet") {
		t.Errorf("empty page missing placeholder; got:\n%s", body)
	}
}

func TestWebSessionsEarlyBadge(t *testing.T) {
	srv, st := newTestServer(t)
	seedSession(t, st, "", store.SessionEndedEarly)
	resp, body := getNoAuth(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "early") {
		t.Errorf("body missing early badge")
	}
}

// The sync API must still require the token even though "/" is public.
func TestPullStillRequiresToken(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, _ := getNoAuth(t, srv.URL+"/pull?table=sessions")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

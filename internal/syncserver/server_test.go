package syncserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncproto"
	"github.com/j-f-allison/jacktasks/internal/syncserver"
)

const testToken = "test-secret-token"

// newTestServer opens a fresh in-memory store, wires the sync mux, and returns
// an httptest.Server and the store. Caller owns closing both.
func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv := httptest.NewServer(syncserver.NewMux(st, testToken))
	t.Cleanup(func() {
		srv.Close()
		st.Close()
	})
	return srv, st
}

// doRequest sends a request and returns the *http.Response. Always sets auth header.
func doRequest(t *testing.T, srv *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// decodeJSON decodes resp.Body into v.
func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// TestHealthz checks the liveness endpoint (no auth required).
func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var h syncproto.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !h.OK {
		t.Error("expected ok=true")
	}
}

// TestAuthRequired verifies that push/pull reject missing or wrong tokens.
func TestAuthRequired(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		auth   string
	}{
		{"pull no auth", "GET", "/pull?table=projects&since=0", ""},
		{"pull wrong token", "GET", "/pull?table=projects&since=0", "Bearer wrong"},
		{"push no auth", "POST", "/push?table=projects", ""},
		{"push wrong token", "POST", "/push?table=projects", "Bearer wrong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, srv.URL+tt.path, nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

// TestUnknownTable verifies 400 on an unrecognised table name.
func TestUnknownTable(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := doRequest(t, srv, "GET", "/pull?table=widgets&since=0", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("pull unknown table: status = %d, want 400", resp.StatusCode)
	}

	resp = doRequest(t, srv, "POST", "/push?table=widgets", syncproto.PushRequest{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("push unknown table: status = %d, want 400", resp.StatusCode)
	}
}

// projectRow builds a wire-format project row.
func projectRow(id, name string, updatedAt int64) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"created_at": updatedAt,
		"updated_at": updatedAt,
		"deleted_at": nil,
		"archived":   int64(0),
	}
}

// TestPushPullProjects exercises the full push → pull round-trip for projects.
func TestPushPullProjects(t *testing.T) {
	srv, _ := newTestServer(t)

	ts := time.Now().Unix()
	rows := []map[string]any{
		projectRow("proj-1", "Alpha", ts),
		projectRow("proj-2", "Beta", ts),
	}

	// Push both projects.
	resp := doRequest(t, srv, "POST", "/push?table=projects", syncproto.PushRequest{Rows: rows})
	var pushResp syncproto.PushResponse
	decodeJSON(t, resp, &pushResp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("push status = %d", resp.StatusCode)
	}
	if pushResp.Accepted != 2 {
		t.Errorf("accepted = %d, want 2", pushResp.Accepted)
	}
	if len(pushResp.Rejected) != 0 {
		t.Errorf("unexpected rejections: %v", pushResp.Rejected)
	}

	// Pull all (since=0).
	resp = doRequest(t, srv, "GET", "/pull?table=projects&since=0", nil)
	var pullResp syncproto.PullResponse
	decodeJSON(t, resp, &pullResp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pull status = %d", resp.StatusCode)
	}
	if len(pullResp.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(pullResp.Rows))
	}

	// Pull with since=as_of should return 0: using the bookmark from a completed
	// pull as the next since value must not re-return already-seen rows.
	resp = doRequest(t, srv, "GET", "/pull?table=projects&since="+itoa(pullResp.AsOf), nil)
	var pullResp2 syncproto.PullResponse
	decodeJSON(t, resp, &pullResp2)
	if len(pullResp2.Rows) != 0 {
		t.Errorf("pull since as_of: got %d rows, want 0", len(pullResp2.Rows))
	}
}

// TestPushProjectsLWW verifies last-write-wins conflict resolution on projects.
func TestPushProjectsLWW(t *testing.T) {
	srv, _ := newTestServer(t)

	ts := time.Now().Unix()

	// Push initial row.
	resp := doRequest(t, srv, "POST", "/push?table=projects", syncproto.PushRequest{
		Rows: []map[string]any{projectRow("proj-1", "Original", ts)},
	})
	resp.Body.Close()

	// Push an update with newer updated_at — should win.
	resp = doRequest(t, srv, "POST", "/push?table=projects", syncproto.PushRequest{
		Rows: []map[string]any{projectRow("proj-1", "Updated", ts+10)},
	})
	resp.Body.Close()

	// Push a stale update — should lose (older updated_at).
	resp = doRequest(t, srv, "POST", "/push?table=projects", syncproto.PushRequest{
		Rows: []map[string]any{projectRow("proj-1", "Stale", ts-5)},
	})
	resp.Body.Close()

	// Pull and check the name is "Updated" (the winner).
	resp = doRequest(t, srv, "GET", "/pull?table=projects&since=0", nil)
	var pullResp syncproto.PullResponse
	decodeJSON(t, resp, &pullResp)
	if len(pullResp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(pullResp.Rows))
	}
	if name, _ := pullResp.Rows[0]["name"].(string); name != "Updated" {
		t.Errorf("name = %q, want %q", name, "Updated")
	}
}

// TestPushSessionsAppendOnly verifies that sessions are deduplicated on push.
func TestPushSessionsAppendOnly(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Create the project + category the session references.
	proj, _ := st.CreateProject(ctx, "Work")
	cat, _ := st.CreateCategory(ctx, "Coding", proj.ID)

	ts := time.Now().Unix()
	sessRow := map[string]any{
		"id":                   "sess-1",
		"project_id":           proj.ID,
		"category_id":          cat.ID,
		"planned_duration_min": int64(25),
		"actual_duration_sec":  int64(1500),
		"started_at":           ts,
		"ended_at":             ts + 1500,
		"end_notes":            nil,
		"status":               "completed",
		"created_at":           ts,
		"device_id":            "macbook",
	}

	// Push twice — second should be ignored (insert-or-ignore).
	for i := 0; i < 2; i++ {
		resp := doRequest(t, srv, "POST", "/push?table=sessions", syncproto.PushRequest{
			Rows: []map[string]any{sessRow},
		})
		var pr syncproto.PushResponse
		decodeJSON(t, resp, &pr)
		if pr.Accepted != 1 {
			t.Errorf("push %d: accepted = %d, want 1", i+1, pr.Accepted)
		}
	}

	// Pull should return exactly one session.
	resp := doRequest(t, srv, "GET", "/pull?table=sessions&since=0", nil)
	var pullResp syncproto.PullResponse
	decodeJSON(t, resp, &pullResp)
	if len(pullResp.Rows) != 1 {
		t.Fatalf("sessions after two pushes: got %d, want 1", len(pullResp.Rows))
	}
}

// TestPushCapturesLWWFlags verifies that capture flag updates use LWW on updated_at.
func TestPushCapturesLWWFlags(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Seed the DB with a project, category, session, and capture.
	proj, _ := st.CreateProject(ctx, "Work")
	cat, _ := st.CreateCategory(ctx, "Coding", proj.ID)
	sess, _ := st.CreateSession(ctx, store.CreateSessionInput{
		CategoryID:         cat.ID,
		ProjectID:          proj.ID,
		PlannedDurationMin: 25,
		ActualDurationSec:  1500,
		StartedAt:          time.Now().Add(-25 * time.Minute),
		EndedAt:            time.Now(),
		Status:             store.SessionCompleted,
		DeviceID:           "dev",
	})
	cap, _ := st.CreateCapture(ctx, "cap-1", sess.ID, "do the thing")
	ts := cap.UpdatedAt.Unix()

	// Push an update marking the capture cleared with a newer updated_at.
	capRow := map[string]any{
		"id":                cap.ID,
		"session_id":        sess.ID,
		"text":              "do the thing",
		"captured_at":       ts,
		"cleared":           int64(1), // now cleared
		"sent_to_reminders": int64(0),
		"created_at":        ts,
		"updated_at":        ts + 5, // newer
	}
	resp := doRequest(t, srv, "POST", "/push?table=captures", syncproto.PushRequest{
		Rows: []map[string]any{capRow},
	})
	var pr syncproto.PushResponse
	decodeJSON(t, resp, &pr)
	if pr.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", pr.Accepted)
	}

	// Push a stale update trying to unset cleared — should be rejected by LWW.
	staleRow := map[string]any{
		"id":                cap.ID,
		"session_id":        sess.ID,
		"text":              "do the thing",
		"captured_at":       ts,
		"cleared":           int64(0), // trying to revert
		"sent_to_reminders": int64(0),
		"created_at":        ts,
		"updated_at":        ts - 1, // older — should lose
	}
	resp = doRequest(t, srv, "POST", "/push?table=captures", syncproto.PushRequest{
		Rows: []map[string]any{staleRow},
	})
	resp.Body.Close()

	// Pull and verify cleared=1 (the newer update won).
	resp = doRequest(t, srv, "GET", "/pull?table=captures&since=0", nil)
	var pullResp syncproto.PullResponse
	decodeJSON(t, resp, &pullResp)
	if len(pullResp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(pullResp.Rows))
	}
	// Pull response is JSON-decoded into map[string]any, so numbers arrive as
	// float64. Type-assert accordingly.
	cleared, _ := pullResp.Rows[0]["cleared"].(float64)
	if cleared != 1 {
		t.Errorf("cleared = %v, want 1 (stale update should not have won)", cleared)
	}
}

// TestPullEmptyReturnsArray verifies that a pull on an empty table returns []
// (not JSON null), so clients can range over the result safely.
func TestPullEmptyReturnsArray(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := doRequest(t, srv, "GET", "/pull?table=projects&since=0", nil)
	defer resp.Body.Close()

	// Unmarshal into a raw message to check the literal shape.
	var raw struct {
		Rows json.RawMessage `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw.Rows) == "null" {
		t.Error("empty pull returned null rows, want []")
	}
}

// TestPushMissingIDRejected verifies that rows without an id field are rejected.
func TestPushMissingIDRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	badRow := map[string]any{
		"name":       "No ID",
		"created_at": int64(0),
		"updated_at": int64(0),
		"archived":   int64(0),
	}
	resp := doRequest(t, srv, "POST", "/push?table=projects", syncproto.PushRequest{
		Rows: []map[string]any{badRow},
	})
	var pr syncproto.PushResponse
	decodeJSON(t, resp, &pr)
	if pr.Accepted != 0 {
		t.Errorf("accepted = %d, want 0", pr.Accepted)
	}
	if len(pr.Rejected) != 1 {
		t.Errorf("rejected = %v, want 1 entry", pr.Rejected)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

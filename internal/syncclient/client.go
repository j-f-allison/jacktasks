// Package syncclient implements the jacktasks client-side sync logic.
// It runs one push-before-pull cycle per synced table and updates
// sync_state bookmarks on each successful table completion.
package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncproto"
)

// Config holds the connection details for the sync server.
// Both fields are required; Sync returns an error if either is empty.
type Config struct {
	URL   string // e.g. "http://100.64.0.1:8484" — no trailing slash
	Token string // shared bearer token
}

// Sync runs one full push-pull cycle for all synced tables in FK-safe order
// (projects → categories → sessions → captures). Each table's bookmarks are
// updated before moving to the next; a failure stops the cycle at that table.
//
// A one-line summary per table is written to out:
//
//	projects:   pushed 3, pulled 0
//	categories: pushed 1, pulled 2
//
// Returns the first error encountered. Partial sync is intentional — the
// completed tables' bookmarks are already persisted.
func Sync(ctx context.Context, st *store.Store, cfg Config, out io.Writer) error {
	if cfg.URL == "" || cfg.Token == "" {
		return fmt.Errorf("JACKTASKS_SYNC_URL and JACKTASKS_SYNC_TOKEN are required")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	for _, table := range syncproto.SyncedTables {
		pushed, pulled, err := syncTable(ctx, st, cfg, client, table)
		if err != nil {
			fmt.Fprintf(out, "%-12s error: %v\n", table+":", err)
			return fmt.Errorf("sync %s: %w", table, err)
		}
		fmt.Fprintf(out, "%-12s pushed %d, pulled %d\n", table+":", pushed, pulled)
	}
	return nil
}

// syncTable runs one push-pull cycle for a single table.
func syncTable(
	ctx context.Context,
	st *store.Store,
	cfg Config,
	client *http.Client,
	table string,
) (pushed, pulled int, err error) {
	// ── Push ─────────────────────────────────────────────────────────────────

	syncSt, err := st.GetSyncState(ctx, table)
	if err != nil {
		return 0, 0, fmt.Errorf("get sync state: %w", err)
	}

	// Snapshot time before reading local rows so any row created during the
	// HTTP call still has updated_at > pushAt and will be caught next sync.
	pushAt := time.Now().Unix()

	localRows, err := st.PullSince(ctx, table, syncSt.LastPushAt)
	if err != nil {
		return 0, 0, fmt.Errorf("read local rows: %w", err)
	}

	if len(localRows) > 0 {
		if err := doPush(ctx, client, cfg, table, localRows); err != nil {
			return 0, 0, fmt.Errorf("push: %w", err)
		}
	}
	pushed = len(localRows)

	if err := st.SetLastPushAt(ctx, table, pushAt); err != nil {
		return pushed, 0, fmt.Errorf("record push bookmark: %w", err)
	}

	// ── Pull ─────────────────────────────────────────────────────────────────

	// Re-read so we get the current last_pull_at (SetLastPushAt preserves it).
	syncSt, err = st.GetSyncState(ctx, table)
	if err != nil {
		return pushed, 0, fmt.Errorf("get sync state (pull): %w", err)
	}

	pullResp, err := doPull(ctx, client, cfg, table, syncSt.LastPullAt)
	if err != nil {
		return pushed, 0, fmt.Errorf("pull: %w", err)
	}

	if len(pullResp.Rows) > 0 {
		if _, _, err := st.UpsertFromSync(ctx, table, pullResp.Rows, 0); err != nil {
			return pushed, 0, fmt.Errorf("apply pulled rows: %w", err)
		}
	}
	pulled = len(pullResp.Rows)

	if err := st.SetLastPullAt(ctx, table, pullResp.AsOf); err != nil {
		return pushed, pulled, fmt.Errorf("record pull bookmark: %w", err)
	}

	return pushed, pulled, nil
}

// doPush POSTs rows to /push?table=<name>. Returns an error if the server
// returns a non-200 status or rejects any rows.
func doPush(ctx context.Context, client *http.Client, cfg Config, table string, rows []map[string]any) error {
	body, err := json.Marshal(syncproto.PushRequest{Rows: rows})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.URL+"/push?table="+table, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var pr syncproto.PushResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(pr.Rejected) > 0 {
		return fmt.Errorf("%d row(s) rejected by server: %v", len(pr.Rejected), pr.Rejected)
	}
	return nil
}

// doPull GETs /pull?table=<name>&since=<sinceUnix> and returns the response.
func doPull(ctx context.Context, client *http.Client, cfg Config, table string, sinceUnix int64) (syncproto.PullResponse, error) {
	url := cfg.URL + "/pull?table=" + table + "&since=" + strconv.FormatInt(sinceUnix, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return syncproto.PullResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		return syncproto.PullResponse{}, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return syncproto.PullResponse{}, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var pr syncproto.PullResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return syncproto.PullResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return pr, nil
}

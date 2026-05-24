// Package syncserver implements the jacktasks-sync HTTP server handlers.
// It depends on store (for data access) and syncproto (for wire types).
package syncserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/j-f-allison/jacktasks/internal/store"
	"github.com/j-f-allison/jacktasks/internal/syncproto"
)

// NewMux builds and returns the HTTP mux for the sync server.
// token is the required bearer token; requests without it get 401.
func NewMux(st *store.Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /push", handlePush(st))
	mux.HandleFunc("GET /pull", handlePull(st))
	return authMiddleware(token, mux)
}

// authMiddleware rejects requests that do not present the correct bearer token.
// The /healthz endpoint is exempt — it is used for basic liveness checks.
func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			auth := r.Header.Get("Authorization")
			want := "Bearer " + token
			if auth != want {
				writeError(w, http.StatusUnauthorized, "invalid or missing token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleHealthz responds to liveness checks.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, syncproto.HealthResponse{OK: true})
}

// handlePush returns a handler for POST /push?table=<name>.
// Body: {"rows": [<map>, ...]}. Applies the appropriate conflict strategy per
// table. Returns {"accepted": N, "rejected": [<id>, ...]}.
func handlePush(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table := r.URL.Query().Get("table")
		if !validTable(table) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown table %q", table))
			return
		}

		var req syncproto.PushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
			return
		}

		accepted, rejected, err := st.UpsertFromSync(r.Context(), table, req.Rows, time.Now().Unix())
		if err != nil {
			log.Printf("push %s: %v", table, err)
			writeError(w, http.StatusInternalServerError, "upsert failed")
			return
		}

		writeJSON(w, http.StatusOK, syncproto.PushResponse{
			Accepted: accepted,
			Rejected: rejected,
		})
	}
}

// handlePull returns a handler for GET /pull?table=<name>&since=<unix_sec>.
// Returns {"rows": [...], "as_of": <unix_sec>}. since defaults to 0 (all rows).
func handlePull(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table := r.URL.Query().Get("table")
		if !validTable(table) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown table %q", table))
			return
		}

		since := int64(0)
		if s := r.URL.Query().Get("since"); s != "" {
			var err error
			since, err = strconv.ParseInt(s, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid since %q", s))
				return
			}
		}

		asOf := time.Now().Unix()
		rows, err := st.PullSinceArrived(r.Context(), table, since)
		if err != nil {
			log.Printf("pull %s since %d: %v", table, since, err)
			writeError(w, http.StatusInternalServerError, "pull failed")
			return
		}
		if rows == nil {
			rows = []map[string]any{} // never return JSON null for rows
		}

		writeJSON(w, http.StatusOK, syncproto.PullResponse{
			Rows: rows,
			AsOf: asOf,
		})
	}
}

// validTable reports whether table is one of the known syncable table names.
func validTable(table string) bool {
	for _, t := range syncproto.SyncedTables {
		if t == table {
			return true
		}
	}
	return false
}

// writeJSON marshals v and writes it as application/json with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

// writeError writes a plain-text error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	fmt.Fprintln(w, strings.TrimSpace(msg))
}

// Package syncproto defines the JSON wire types shared between the
// jacktasks-sync server (cmd/jacktasks-sync) and the client sync subcommand
// (cmd/jacktasks). Both sides import this package; neither side imports the
// other.
//
// Wire conventions:
//   - Timestamps are Unix epoch seconds (int64), matching DB storage.
//   - NULL DB fields are JSON null (*int64 or *string as appropriate).
//   - Boolean DB fields (cleared, sent_to_reminders, archived) are int (0/1).
package syncproto

// PushRequest is the body of POST /push?table=<name>.
type PushRequest struct {
	Rows []map[string]any `json:"rows"`
}

// PushResponse is returned by POST /push.
type PushResponse struct {
	Accepted int      `json:"accepted"`
	Rejected []string `json:"rejected,omitempty"` // UUIDs that failed
}

// PullResponse is returned by GET /pull?table=<name>&since=<unix_sec>.
type PullResponse struct {
	Rows []map[string]any `json:"rows"`
	AsOf int64            `json:"as_of"` // server time at query execution
}

// HealthResponse is returned by GET /healthz.
type HealthResponse struct {
	OK bool `json:"ok"`
}

// Table names used in query parameters. Centralised here to avoid typos in
// both client and server.
const (
	TableProjects   = "projects"
	TableCategories = "categories"
	TableSessions   = "sessions"
	TableCaptures   = "captures"
)

// SyncedTables is the ordered list of tables the client syncs. Order matters:
// push before pull, and parent tables (projects) before children (categories,
// sessions, captures) so FK constraints are satisfied on inserts.
var SyncedTables = []string{
	TableProjects,
	TableCategories,
	TableSessions,
	TableCaptures,
}

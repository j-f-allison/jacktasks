package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// tableColumns lists the wire columns for each synced table. Order matches the
// SELECT in PullSince and the INSERT in UpsertFromSync. Must stay in sync with
// schema.sql.
var tableColumns = map[string][]string{
	"projects": {
		"id", "name", "created_at", "updated_at", "deleted_at", "archived",
	},
	"categories": {
		"id", "name", "project_id", "created_at", "updated_at", "deleted_at", "archived",
	},
	"sessions": {
		"id", "project_id", "category_id",
		"planned_duration_min", "actual_duration_sec",
		"started_at", "ended_at", "end_notes",
		"status", "created_at", "device_id",
	},
	"captures": {
		"id", "session_id", "text", "captured_at",
		"cleared", "sent_to_reminders", "created_at", "updated_at",
	},
}

// pullColumn is the column used for "newer than" filtering on each table.
var pullColumn = map[string]string{
	"projects":   "updated_at",
	"categories": "updated_at",
	"sessions":   "created_at",
	"captures":   "updated_at",
}

// PullSince returns all rows from the named table whose pull-column value is
// strictly greater than sinceUnix. Rows are returned as raw maps using SQLite's
// native types (int64 for integers, string for text, nil for NULL) — ready for
// JSON marshalling.
//
// Returns an error if the table name is unknown.
func (s *Store) PullSince(ctx context.Context, table string, sinceUnix int64) ([]map[string]any, error) {
	cols, ok := tableColumns[table]
	if !ok {
		return nil, fmt.Errorf("unknown table %q", table)
	}
	col, ok := pullColumn[table]
	if !ok {
		return nil, fmt.Errorf("no pull column for table %q", table)
	}

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s > ? ORDER BY %s",
		strings.Join(cols, ", "), table, col, col,
	)
	rows, err := s.db.QueryContext(ctx, query, sinceUnix)
	if err != nil {
		return nil, fmt.Errorf("pull %s: %w", table, err)
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		row, err := scanRowToMap(rows, cols)
		if err != nil {
			return nil, fmt.Errorf("scan %s row: %w", table, err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpsertFromSync applies incoming rows to the named table using the appropriate
// conflict strategy:
//   - sessions: INSERT OR IGNORE (pure append, immutable after write)
//   - captures: INSERT new rows; for existing rows update flag columns only if
//     incoming updated_at is newer (last-write-wins on flags)
//   - projects, categories: INSERT new rows; for existing rows update mutable
//     columns only if incoming updated_at is newer (last-write-wins)
//
// Returns the count of rows accepted and a slice of IDs that were skipped or
// errored. An error is returned only for non-row-level failures (unknown table,
// DB connection problems).
func (s *Store) UpsertFromSync(ctx context.Context, table string, rows []map[string]any) (int, []string, error) {
	if _, ok := tableColumns[table]; !ok {
		return 0, nil, fmt.Errorf("unknown table %q", table)
	}

	accepted := 0
	var rejected []string

	for _, row := range rows {
		id, _ := row["id"].(string)
		if id == "" {
			rejected = append(rejected, "<missing id>")
			continue
		}
		var err error
		switch table {
		case "projects":
			err = upsertProject(ctx, s.db, row)
		case "categories":
			err = upsertCategory(ctx, s.db, row)
		case "sessions":
			err = upsertSession(ctx, s.db, row)
		case "captures":
			err = upsertCapture(ctx, s.db, row)
		}
		if err != nil {
			rejected = append(rejected, id)
		} else {
			accepted++
		}
	}
	return accepted, rejected, nil
}

// upsertProject: last-write-wins on updated_at.
func upsertProject(ctx context.Context, db *sql.DB, row map[string]any) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, created_at, updated_at, deleted_at, archived)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name       = excluded.name,
		  updated_at = excluded.updated_at,
		  deleted_at = excluded.deleted_at,
		  archived   = excluded.archived
		WHERE excluded.updated_at > projects.updated_at`,
		row["id"], row["name"], row["created_at"], row["updated_at"],
		row["deleted_at"], row["archived"],
	)
	return err
}

// upsertCategory: last-write-wins on updated_at.
func upsertCategory(ctx context.Context, db *sql.DB, row map[string]any) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO categories (id, name, project_id, created_at, updated_at, deleted_at, archived)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name       = excluded.name,
		  project_id = excluded.project_id,
		  updated_at = excluded.updated_at,
		  deleted_at = excluded.deleted_at,
		  archived   = excluded.archived
		WHERE excluded.updated_at > categories.updated_at`,
		row["id"], row["name"], row["project_id"], row["created_at"],
		row["updated_at"], row["deleted_at"], row["archived"],
	)
	return err
}

// upsertSession: pure append, INSERT OR IGNORE.
func upsertSession(ctx context.Context, db *sql.DB, row map[string]any) error {
	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO sessions
		(id, project_id, category_id, planned_duration_min, actual_duration_sec,
		 started_at, ended_at, end_notes, status, created_at, device_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row["id"], row["project_id"], row["category_id"],
		row["planned_duration_min"], row["actual_duration_sec"],
		row["started_at"], row["ended_at"], row["end_notes"],
		row["status"], row["created_at"], row["device_id"],
	)
	return err
}

// upsertCapture: insert new rows; for existing rows update flag columns
// only if incoming updated_at is newer (last-write-wins on flags).
func upsertCapture(ctx context.Context, db *sql.DB, row map[string]any) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO captures
		(id, session_id, text, captured_at, cleared, sent_to_reminders, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  cleared           = excluded.cleared,
		  sent_to_reminders = excluded.sent_to_reminders,
		  updated_at        = excluded.updated_at
		WHERE excluded.updated_at > captures.updated_at`,
		row["id"], row["session_id"], row["text"], row["captured_at"],
		row["cleared"], row["sent_to_reminders"], row["created_at"], row["updated_at"],
	)
	return err
}

// scanRowToMap scans a single row from rows into a map[string]any keyed by
// column name. Values are the raw SQLite types: int64, float64, string, []byte,
// or nil.
func scanRowToMap(rows *sql.Rows, cols []string) (map[string]any, error) {
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	row := make(map[string]any, len(cols))
	for i, col := range cols {
		row[col] = vals[i]
	}
	return row, nil
}

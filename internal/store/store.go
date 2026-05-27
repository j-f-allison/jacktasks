package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

// arrivedAtBackfill maps each sync table to the client-side timestamp column
// used to backfill arrived_at for rows that existed before the migration.
var arrivedAtBackfill = map[string]string{
	"projects":   "updated_at",
	"categories": "updated_at",
	"sessions":   "created_at",
	"captures":   "created_at",
}

//go:embed schema.sql
var schemaSQL string

// Store wraps a SQLite database connection for jacktasks.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at path, applies pragmas, and runs the schema.
// Safe to call on an existing DB.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=DELETE",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply %q: %w", p, err)
		}
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := migrateCapturesUpdatedAt(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate captures.updated_at: %w", err)
	}

	if err := migrateArrivedAt(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate arrived_at: %w", err)
	}

	if err := migrateRemindersListName(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate projects.reminders_list_name: %w", err)
	}

	if err := migrateCategoryTargets(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate categories target columns: %w", err)
	}

	return &Store{db: db}, nil
}

// migrateCategoryTargets adds target_minutes, target_period, schedule_mask, and
// target_sessions columns to categories if absent. All nullable — no backfill
// needed. Safe to call on a DB that already has the columns (no-op).
func migrateCategoryTargets(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(categories)")
	if err != nil {
		return fmt.Errorf("table_info(categories): %w", err)
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info(categories): %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info rows: %w", err)
	}

	adds := []struct {
		col string
		def string
	}{
		{"target_minutes", "ALTER TABLE categories ADD COLUMN target_minutes INTEGER"},
		{"target_period", "ALTER TABLE categories ADD COLUMN target_period TEXT"},
		{"schedule_mask", "ALTER TABLE categories ADD COLUMN schedule_mask INTEGER"},
		{"target_sessions", "ALTER TABLE categories ADD COLUMN target_sessions INTEGER"},
	}
	for _, a := range adds {
		if !existing[a.col] {
			if _, err := db.Exec(a.def); err != nil {
				return fmt.Errorf("alter categories add %s: %w", a.col, err)
			}
		}
	}
	return nil
}

// migrateRemindersListName adds the reminders_list_name column to projects if
// absent. NULL = no associated Reminders list. Safe to call on a DB that
// already has the column (no-op).
func migrateRemindersListName(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(projects)")
	if err != nil {
		return fmt.Errorf("table_info(projects): %w", err)
	}
	defer rows.Close()

	exists := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info(projects): %w", err)
		}
		if name == "reminders_list_name" {
			exists = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info rows: %w", err)
	}

	if !exists {
		if _, err := db.Exec(`ALTER TABLE projects ADD COLUMN reminders_list_name TEXT`); err != nil {
			return fmt.Errorf("alter projects: %w", err)
		}
	}
	return nil
}

// migrateCapturesUpdatedAt adds the updated_at column to captures if it does
// not exist. Existing rows get updated_at = created_at so LWW comparisons
// against epoch-zero never accidentally clobber them.
// Safe to call on a DB that already has the column (no-op).
func migrateCapturesUpdatedAt(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(captures)")
	if err != nil {
		return fmt.Errorf("table_info: %w", err)
	}
	defer rows.Close()

	columnExists := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "updated_at" {
			columnExists = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info rows: %w", err)
	}

	if !columnExists {
		// Column absent — add it and backfill from created_at.
		if _, err := db.Exec(`ALTER TABLE captures ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("alter table: %w", err)
		}
		if _, err := db.Exec(`UPDATE captures SET updated_at = created_at WHERE updated_at = 0`); err != nil {
			return fmt.Errorf("backfill updated_at: %w", err)
		}
	}

	// Always ensure the index exists — schema.sql omits it to avoid ordering
	// issues when the column is added by this migration.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_captures_updated ON captures(updated_at)`); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	return nil
}

// migrateArrivedAt adds an arrived_at column to each sync table if absent, and
// backfills existing rows using the table's best available client-side timestamp
// so that a fresh pull (since=0) can still retrieve them. The column is used by
// the server to track when a row was first received, enabling correct pull
// filtering even when rows arrive out of chronological order.
// Safe to call on a DB that already has the column (no-op per table).
func migrateArrivedAt(db *sql.DB) error {
	for table, backfillCol := range arrivedAtBackfill {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return fmt.Errorf("table_info(%s): %w", table, err)
		}

		exists := false
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
				rows.Close()
				return fmt.Errorf("scan table_info(%s): %w", table, err)
			}
			if name == "arrived_at" {
				exists = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("table_info(%s) rows: %w", table, err)
		}

		if !exists {
			if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN arrived_at INTEGER NOT NULL DEFAULT 0"); err != nil {
				return fmt.Errorf("alter %s: %w", table, err)
			}
			if _, err := db.Exec("UPDATE " + table + " SET arrived_at = " + backfillCol + " WHERE arrived_at = 0"); err != nil {
				return fmt.Errorf("backfill %s.arrived_at: %w", table, err)
			}
		}

		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_" + table + "_arrived ON " + table + "(arrived_at)"); err != nil {
			return fmt.Errorf("index %s: %w", table, err)
		}
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

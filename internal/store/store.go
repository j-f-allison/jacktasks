package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

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

	return &Store{db: db}, nil
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

// Close releases the database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

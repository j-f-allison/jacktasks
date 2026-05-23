package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SyncState holds the last push/pull timestamps for one table.
// Both fields default to 0 (epoch) when no sync has occurred — 0 is safe
// because all real timestamps are well above 0.
type SyncState struct {
	LastPullAt int64 // unix epoch seconds; 0 = never pulled
	LastPushAt int64 // unix epoch seconds; 0 = never pushed
}

// GetSyncState returns the sync bookmarks for the named table.
// Returns a zero-value SyncState (both fields 0) if the table has never synced.
func (s *Store) GetSyncState(ctx context.Context, tableName string) (SyncState, error) {
	var lastPullAt, lastPushAt sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT last_pull_at, last_push_at FROM sync_state WHERE table_name = ?`,
		tableName,
	).Scan(&lastPullAt, &lastPushAt)
	if err == sql.ErrNoRows {
		return SyncState{}, nil
	}
	if err != nil {
		return SyncState{}, fmt.Errorf("get sync state %q: %w", tableName, err)
	}
	var st SyncState
	if lastPullAt.Valid {
		st.LastPullAt = lastPullAt.Int64
	}
	if lastPushAt.Valid {
		st.LastPushAt = lastPushAt.Int64
	}
	return st, nil
}

// SetLastPushAt records the time of the most recent successful push for tableName.
// Upserts the row; does not disturb last_pull_at.
func (s *Store) SetLastPushAt(ctx context.Context, tableName string, unixSec int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (table_name, last_push_at) VALUES (?, ?)
		 ON CONFLICT(table_name) DO UPDATE SET last_push_at = excluded.last_push_at`,
		tableName, unixSec,
	)
	if err != nil {
		return fmt.Errorf("set last_push_at %q: %w", tableName, err)
	}
	return nil
}

// SetLastPullAt records the time of the most recent successful pull for tableName.
// Upserts the row; does not disturb last_push_at.
func (s *Store) SetLastPullAt(ctx context.Context, tableName string, unixSec int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (table_name, last_pull_at) VALUES (?, ?)
		 ON CONFLICT(table_name) DO UPDATE SET last_pull_at = excluded.last_pull_at`,
		tableName, unixSec,
	)
	if err != nil {
		return fmt.Errorf("set last_pull_at %q: %w", tableName, err)
	}
	return nil
}

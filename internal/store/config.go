package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// SetConfig writes or overwrites a config key. UPSERT semantics.
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO config (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set config %q: %w", key, err)
	}
	return nil
}

// GetConfig returns the value for key. Returns ErrNotFound if missing.
func (s *Store) GetConfig(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM config WHERE key = ?`, key,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get config %q: %w", key, err)
	}
	return v, nil
}

// DeviceID returns this machine's stable identifier, generating and
// persisting one on first call.
func (s *Store) DeviceID(ctx context.Context) (string, error) {
	const key = "device_id"
	v, err := s.GetConfig(ctx, key)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	id := uuid.NewString()
	if err := s.SetConfig(ctx, key, id); err != nil {
		return "", err
	}
	return id, nil
}
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SessionStatus is the terminal state of a finished session.
type SessionStatus string

const (
	SessionCompleted SessionStatus = "completed"
	SessionAbandoned SessionStatus = "abandoned"
)

// Valid reports whether s is one of the recognized statuses.
func (s SessionStatus) Valid() bool {
	return s == SessionCompleted || s == SessionAbandoned
}

// Session is an immutable historical record of one work block. Written
// only on session end; never updated.
type Session struct {
	ID                 string
	CategoryID         string
	ProjectID          string
	PlannedDurationMin int
	ActualDurationSec  int
	StartedAt          time.Time
	EndedAt            time.Time
	EndNotes           string
	Status             SessionStatus
	CreatedAt          time.Time
	DeviceID           string
}

// CreateSessionInput collects the fields for CreateSession.
type CreateSessionInput struct {
	CategoryID         string
	ProjectID          string
	PlannedDurationMin int
	ActualDurationSec  int
	StartedAt          time.Time
	EndedAt            time.Time
	EndNotes           string
	Status             SessionStatus
	DeviceID           string
}

// CreateSession writes a finished session. FKs to categories and projects
// are enforced by SQL. Status is validated in Go before the insert.
func (s *Store) CreateSession(ctx context.Context, in CreateSessionInput) (*Session, error) {
	if !in.Status.Valid() {
		return nil, fmt.Errorf("invalid status %q", in.Status)
	}
	if in.DeviceID == "" {
		return nil, errors.New("device_id required")
	}

	sess := &Session{
		ID:                 uuid.NewString(),
		CategoryID:         in.CategoryID,
		ProjectID:          in.ProjectID,
		PlannedDurationMin: in.PlannedDurationMin,
		ActualDurationSec:  in.ActualDurationSec,
		StartedAt:          in.StartedAt,
		EndedAt:            in.EndedAt,
		EndNotes:           in.EndNotes,
		Status:             in.Status,
		CreatedAt:          time.Now(),
		DeviceID:           in.DeviceID,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions
		 (id, category_id, project_id, planned_duration_min, actual_duration_sec,
		  started_at, ended_at, end_notes, status, created_at, device_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.CategoryID, sess.ProjectID,
		sess.PlannedDurationMin, sess.ActualDurationSec,
		sess.StartedAt.Unix(), sess.EndedAt.Unix(),
		sess.EndNotes, string(sess.Status),
		sess.CreatedAt.Unix(), sess.DeviceID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// ListSessions returns sessions newest-first, up to limit. limit<=0 uses 100.
func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, category_id, project_id, planned_duration_min, actual_duration_sec,
		        started_at, ended_at, end_notes, status, created_at, device_id
		 FROM sessions
		 ORDER BY started_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// GetSession returns the session with the given id. Returns ErrNotFound
// if no row matches.
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, category_id, project_id, planned_duration_min, actual_duration_sec,
		        started_at, ended_at, end_notes, status, created_at, device_id
		 FROM sessions WHERE id = ?`,
		id,
	)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func scanSession(r rowScanner) (Session, error) {
	var sess Session
	var startedAt, endedAt, createdAt int64
	var endNotes sql.NullString
	var status string

	if err := r.Scan(
		&sess.ID, &sess.CategoryID, &sess.ProjectID,
		&sess.PlannedDurationMin, &sess.ActualDurationSec,
		&startedAt, &endedAt,
		&endNotes, &status,
		&createdAt, &sess.DeviceID,
	); err != nil {
		return sess, err
	}
	sess.StartedAt = time.Unix(startedAt, 0)
	sess.EndedAt = time.Unix(endedAt, 0)
	sess.CreatedAt = time.Unix(createdAt, 0)
	if endNotes.Valid {
		sess.EndNotes = endNotes.String
	}
	sess.Status = SessionStatus(status)
	return sess, nil
}
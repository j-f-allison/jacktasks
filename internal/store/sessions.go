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
	SessionCompleted  SessionStatus = "completed"
	SessionEndedEarly SessionStatus = "ended_early"
)

// Valid reports whether s is one of the recognized statuses.
func (s SessionStatus) Valid() bool {
	return s == SessionCompleted || s == SessionEndedEarly
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
	var projectID sql.NullString
	if in.ProjectID != "" {
		projectID = sql.NullString{String: in.ProjectID, Valid: true}
	}
	var endNotesPut sql.NullString
	if in.EndNotes != "" {
		endNotesPut = sql.NullString{String: in.EndNotes, Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions
		 (id, category_id, project_id, planned_duration_min, actual_duration_sec,
		  started_at, ended_at, end_notes, status, created_at, device_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.CategoryID, projectID,
		sess.PlannedDurationMin, sess.ActualDurationSec,
		sess.StartedAt.Unix(), sess.EndedAt.Unix(),
		endNotesPut, string(sess.Status),
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

// SessionView is a session enriched with the human-readable names of its
// project and category, for display surfaces that don't want to resolve IDs
// themselves (e.g. the read-only web view on the sync server).
type SessionView struct {
	Session
	ProjectName  string // empty when the session has no project
	CategoryName string // empty only if the category row is missing
}

// ListSessionViews returns sessions newest-first, up to limit, each joined with
// its project and category names. limit<=0 uses 100. A missing project (NULL
// project_id) yields an empty ProjectName.
func (s *Store) ListSessionViews(ctx context.Context, limit int) ([]SessionView, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT se.id, se.category_id, se.project_id, se.planned_duration_min,
		        se.actual_duration_sec, se.started_at, se.ended_at, se.end_notes,
		        se.status, se.created_at, se.device_id,
		        p.name, c.name
		 FROM sessions se
		 LEFT JOIN projects p ON p.id = se.project_id
		 LEFT JOIN categories c ON c.id = se.category_id
		 ORDER BY se.started_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query session views: %w", err)
	}
	defer rows.Close()

	var out []SessionView
	for rows.Next() {
		var v SessionView
		var startedAt, endedAt, createdAt int64
		var projectID, endNotes, projectName, categoryName sql.NullString
		var status string

		if err := rows.Scan(
			&v.ID, &v.CategoryID, &projectID,
			&v.PlannedDurationMin, &v.ActualDurationSec,
			&startedAt, &endedAt, &endNotes, &status,
			&createdAt, &v.DeviceID,
			&projectName, &categoryName,
		); err != nil {
			return nil, err
		}
		v.StartedAt = time.Unix(startedAt, 0)
		v.EndedAt = time.Unix(endedAt, 0)
		v.CreatedAt = time.Unix(createdAt, 0)
		if projectID.Valid {
			v.ProjectID = projectID.String
		}
		if endNotes.Valid {
			v.EndNotes = endNotes.String
		}
		v.Status = SessionStatus(status)
		v.ProjectName = projectName.String
		v.CategoryName = categoryName.String
		out = append(out, v)
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

// LatestSession returns the most recently started session, or ErrNotFound
// if none exist. Used for resume-on-restart detection.
func (s *Store) LatestSession(ctx context.Context) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, category_id, project_id, planned_duration_min, actual_duration_sec,
		        started_at, ended_at, end_notes, status, created_at, device_id
		 FROM sessions
		 ORDER BY started_at DESC
		 LIMIT 1`,
	)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("latest session: %w", err)
	}
	return &sess, nil
}

// CountTodaySessions returns the number of sessions whose started_at falls
// within the calendar day defined by the given time (using its local timezone).
func (s *Store) CountTodaySessions(ctx context.Context, now time.Time) (int, error) {
	// Compute midnight boundaries in the same location as now.
	loc := now.Location()
	y, m, d := now.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, loc).Unix()
	dayEnd := time.Date(y, m, d+1, 0, 0, 0, 0, loc).Unix()

	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE started_at >= ? AND started_at < ?`,
		dayStart, dayEnd,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count today sessions: %w", err)
	}
	return n, nil
}

// SumCategorySecondsBetween returns the total actual_duration_sec for sessions
// of the given category whose started_at falls in [start, end).
func (s *Store) SumCategorySecondsBetween(ctx context.Context, categoryID string, start, end int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(actual_duration_sec), 0)
		 FROM sessions
		 WHERE category_id = ? AND started_at >= ? AND started_at < ?`,
		categoryID, start, end,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("sum category seconds: %w", err)
	}
	return n, nil
}

// CategoryActiveBetween reports whether any session for the given category
// started within [start, end). Used for presence-only target checks.
func (s *Store) CategoryActiveBetween(ctx context.Context, categoryID string, start, end int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions
		 WHERE category_id = ? AND started_at >= ? AND started_at < ?
		 LIMIT 1`,
		categoryID, start, end,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("category active between: %w", err)
	}
	return n > 0, nil
}

func scanSession(r rowScanner) (Session, error) {
	var sess Session
	var startedAt, endedAt, createdAt int64
	var projectID, endNotes sql.NullString
	var status string

	if err := r.Scan(
		&sess.ID, &sess.CategoryID, &projectID,
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
	if projectID.Valid {
		sess.ProjectID = projectID.String
	}
	if endNotes.Valid {
		sess.EndNotes = endNotes.String
	}
	sess.Status = SessionStatus(status)
	return sess, nil
}
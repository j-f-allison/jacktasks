package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Project is a top-level grouping for work.
type Project struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	Archived  bool
}

// CreateProject inserts a new project and returns it fully populated.
func (s *Store) CreateProject(ctx context.Context, name string) (*Project, error) {
	now := time.Now()
	p := &Project{
		ID:        uuid.NewString(),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, created_at, updated_at, archived)
		 VALUES (?, ?, ?, ?, 0)`,
		p.ID, p.Name, p.CreatedAt.Unix(), p.UpdatedAt.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
	return p, nil
}

// ListProjects returns all live (not deleted, not archived) projects,
// sorted by name case-insensitively.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, created_at, updated_at, deleted_at, archived
		 FROM projects
		 WHERE deleted_at IS NULL AND archived = 0
		 ORDER BY name COLLATE NOCASE`,
	)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject returns the project with the given id, including soft-deleted
// and archived rows. Returns ErrNotFound if no row matches.
func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at, updated_at, deleted_at, archived
		 FROM projects WHERE id = ?`,
		id,
	)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanProject(r rowScanner) (Project, error) {
	var p Project
	var createdAt, updatedAt int64
	var deletedAt sql.NullInt64
	var archived int

	if err := r.Scan(&p.ID, &p.Name, &createdAt, &updatedAt, &deletedAt, &archived); err != nil {
		return p, err
	}
	p.CreatedAt = time.Unix(createdAt, 0)
	p.UpdatedAt = time.Unix(updatedAt, 0)
	if deletedAt.Valid {
		t := time.Unix(deletedAt.Int64, 0)
		p.DeletedAt = &t
	}
	p.Archived = archived != 0
	return p, nil
}

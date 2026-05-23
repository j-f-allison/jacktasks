package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Project belongs to exactly one Category.
type Project struct {
	ID         string
	Name       string
	CategoryID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
	Archived   bool
}

// CreateProject inserts a new project under the given category.
// Returns an error if categoryID doesn't reference an existing row
// (enforced by the SQL foreign key, not the Go layer).
func (s *Store) CreateProject(ctx context.Context, name, categoryID string) (*Project, error) {
	now := time.Now()
	p := &Project{
		ID:         uuid.NewString(),
		Name:       name,
		CategoryID: categoryID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, category_id, created_at, updated_at, archived)
		 VALUES (?, ?, ?, ?, ?, 0)`,
		p.ID, p.Name, p.CategoryID, p.CreatedAt.Unix(), p.UpdatedAt.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
	return p, nil
}

// ListProjectsByCategory returns all live projects in the given category,
// sorted by name case-insensitively.
func (s *Store) ListProjectsByCategory(ctx context.Context, categoryID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, category_id, created_at, updated_at, deleted_at, archived
		 FROM projects
		 WHERE category_id = ? AND deleted_at IS NULL AND archived = 0
		 ORDER BY name COLLATE NOCASE`,
		categoryID,
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
		`SELECT id, name, category_id, created_at, updated_at, deleted_at, archived
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

	if err := r.Scan(&p.ID, &p.Name, &p.CategoryID, &createdAt, &updatedAt, &deletedAt, &archived); err != nil {
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
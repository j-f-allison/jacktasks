package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Category is a sub-label scoped to a project (or project-less when ProjectID is empty).
type Category struct {
	ID        string
	Name      string
	ProjectID string // empty when NULL (no-project category)
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	Archived  bool
}

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("not found")

// CreateCategory inserts a new category. projectID may be empty (stored as NULL).
func (s *Store) CreateCategory(ctx context.Context, name, projectID string) (*Category, error) {
	now := time.Now()
	c := &Category{
		ID:        uuid.NewString(),
		Name:      name,
		ProjectID: projectID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	var projArg any
	if projectID != "" {
		projArg = projectID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO categories (id, name, project_id, created_at, updated_at, archived)
		 VALUES (?, ?, ?, ?, ?, 0)`,
		c.ID, c.Name, projArg, c.CreatedAt.Unix(), c.UpdatedAt.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert category: %w", err)
	}
	return c, nil
}

// ListCategoriesByProject returns all live categories for the given project,
// sorted by name case-insensitively. Pass an empty projectID to list no-project categories.
func (s *Store) ListCategoriesByProject(ctx context.Context, projectID string) ([]Category, error) {
	var rows *sql.Rows
	var err error
	if projectID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, project_id, created_at, updated_at, deleted_at, archived
			 FROM categories
			 WHERE project_id IS NULL AND deleted_at IS NULL AND archived = 0
			 ORDER BY name COLLATE NOCASE`,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, project_id, created_at, updated_at, deleted_at, archived
			 FROM categories
			 WHERE project_id = ? AND deleted_at IS NULL AND archived = 0
			 ORDER BY name COLLATE NOCASE`,
			projectID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}
	defer rows.Close()

	var out []Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCategory returns the category with the given id. Returns ErrNotFound if no row matches.
func (s *Store) GetCategory(ctx context.Context, id string) (*Category, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, project_id, created_at, updated_at, deleted_at, archived
		 FROM categories WHERE id = ?`,
		id,
	)
	c, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateOrGetCategoryByName looks up a no-project category by name (project_id IS NULL).
// If found, returns it; otherwise inserts and returns a new one.
func (s *Store) CreateOrGetCategoryByName(ctx context.Context, name, projectID string) (*Category, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, project_id, created_at, updated_at, deleted_at, archived
		 FROM categories WHERE name = ? AND project_id IS NULL AND deleted_at IS NULL`,
		name,
	)
	c, err := scanCategory(row)
	if err == nil {
		return &c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lookup category: %w", err)
	}
	return s.CreateCategory(ctx, name, "")
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCategory(r rowScanner) (Category, error) {
	var c Category
	var createdAt, updatedAt int64
	var projectID sql.NullString
	var deletedAt sql.NullInt64
	var archived int

	if err := r.Scan(&c.ID, &c.Name, &projectID, &createdAt, &updatedAt, &deletedAt, &archived); err != nil {
		return c, err
	}
	if projectID.Valid {
		c.ProjectID = projectID.String
	}
	c.CreatedAt = time.Unix(createdAt, 0)
	c.UpdatedAt = time.Unix(updatedAt, 0)
	if deletedAt.Valid {
		t := time.Unix(deletedAt.Int64, 0)
		c.DeletedAt = &t
	}
	c.Archived = archived != 0
	return c, nil
}

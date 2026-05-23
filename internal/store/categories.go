package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Category represents a top-level grouping (e.g. "RELAC", "JDi").
type Category struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	Archived  bool
}

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("not found")

// CreateCategory inserts a new category and returns it fully populated.
func (s *Store) CreateCategory(ctx context.Context, name string) (*Category, error) {
	now := time.Now()
	c := &Category{
		ID:        uuid.NewString(),
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO categories (id, name, created_at, updated_at, archived)
		 VALUES (?, ?, ?, ?, 0)`,
		c.ID, c.Name, c.CreatedAt.Unix(), c.UpdatedAt.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert category: %w", err)
	}
	return c, nil
}

// ListCategories returns all live (not deleted, not archived) categories,
// sorted by name case-insensitively.
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, created_at, updated_at, deleted_at, archived
		 FROM categories
		 WHERE deleted_at IS NULL AND archived = 0
		 ORDER BY name COLLATE NOCASE`,
	)
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

// GetCategory returns the category with the given id, including
// soft-deleted and archived rows. Returns ErrNotFound if no row matches.
func (s *Store) GetCategory(ctx context.Context, id string) (*Category, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at, updated_at, deleted_at, archived
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

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCategory(r rowScanner) (Category, error) {
	var c Category
	var createdAt, updatedAt int64
	var deletedAt sql.NullInt64
	var archived int

	if err := r.Scan(&c.ID, &c.Name, &createdAt, &updatedAt, &deletedAt, &archived); err != nil {
		return c, err
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
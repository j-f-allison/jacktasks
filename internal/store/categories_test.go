package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.CreateCategory(ctx, "RELAC")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if c.Name != "RELAC" {
		t.Errorf("Name = %q, want %q", c.Name, "RELAC")
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}
	if c.DeletedAt != nil {
		t.Error("DeletedAt should be nil on create")
	}
	if c.Archived {
		t.Error("Archived should be false on create")
	}
}

func TestListCategories(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	list, err := s.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}

	for _, n := range []string{"JDi", "RELAC", "Apex"} {
		if _, err := s.CreateCategory(ctx, n); err != nil {
			t.Fatalf("create %q: %v", n, err)
		}
	}

	list, err = s.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"Apex", "JDi", "RELAC"}
	if len(list) != len(want) {
		t.Fatalf("got %d categories, want %d", len(list), len(want))
	}
	for i, c := range list {
		if c.Name != want[i] {
			t.Errorf("list[%d].Name = %q, want %q", i, c.Name, want[i])
		}
	}
}

func TestGetCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateCategory(ctx, "RELAC")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetCategory(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name {
		t.Errorf("got %+v, want id=%q name=%q", got, created.ID, created.Name)
	}

	if _, err := s.GetCategory(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got err %v, want ErrNotFound", err)
	}
}
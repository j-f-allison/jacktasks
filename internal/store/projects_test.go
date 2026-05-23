package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cat, err := s.CreateCategory(ctx, "RELAC")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	p, err := s.CreateProject(ctx, "memo", cat.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if p.Name != "memo" {
		t.Errorf("Name = %q, want %q", p.Name, "memo")
	}
	if p.CategoryID != cat.ID {
		t.Errorf("CategoryID = %q, want %q", p.CategoryID, cat.ID)
	}
}

func TestCreateProjectInvalidCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.CreateProject(ctx, "orphan", "no-such-category")
	if err == nil {
		t.Fatal("expected FK error for invalid category_id, got nil")
	}
}

func TestListProjectsByCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	relac, _ := s.CreateCategory(ctx, "RELAC")
	jdi, _ := s.CreateCategory(ctx, "JDi")

	list, err := s.ListProjectsByCategory(ctx, relac.ID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty, got %d", len(list))
	}

	for _, n := range []string{"memo", "outline", "annotations"} {
		if _, err := s.CreateProject(ctx, n, relac.ID); err != nil {
			t.Fatalf("create relac %q: %v", n, err)
		}
	}
	if _, err := s.CreateProject(ctx, "homework", jdi.ID); err != nil {
		t.Fatalf("create jdi: %v", err)
	}

	list, err = s.ListProjectsByCategory(ctx, relac.ID)
	if err != nil {
		t.Fatalf("list relac: %v", err)
	}
	want := []string{"annotations", "memo", "outline"}
	if len(list) != len(want) {
		t.Fatalf("got %d projects, want %d", len(list), len(want))
	}
	for i, p := range list {
		if p.Name != want[i] {
			t.Errorf("list[%d].Name = %q, want %q", i, p.Name, want[i])
		}
		if p.CategoryID != relac.ID {
			t.Errorf("list[%d].CategoryID = %q, want %q", i, p.CategoryID, relac.ID)
		}
	}

	jdiList, err := s.ListProjectsByCategory(ctx, jdi.ID)
	if err != nil {
		t.Fatalf("list jdi: %v", err)
	}
	if len(jdiList) != 1 || jdiList[0].Name != "homework" {
		t.Errorf("unexpected jdi projects: %+v", jdiList)
	}
}

func TestGetProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cat, _ := s.CreateCategory(ctx, "RELAC")
	created, err := s.CreateProject(ctx, "memo", cat.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetProject(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name || got.CategoryID != cat.ID {
		t.Errorf("got %+v, want id=%q name=%q cat=%q", got, created.ID, created.Name, cat.ID)
	}

	if _, err := s.GetProject(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got err %v, want ErrNotFound", err)
	}
}
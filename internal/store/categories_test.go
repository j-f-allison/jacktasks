package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	proj, err := s.CreateProject(ctx, "RELAC")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	c, err := s.CreateCategory(ctx, "Coding", proj.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if c.Name != "Coding" {
		t.Errorf("Name = %q, want %q", c.Name, "Coding")
	}
	if c.ProjectID != proj.ID {
		t.Errorf("ProjectID = %q, want %q", c.ProjectID, proj.ID)
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

func TestCreateCategoryNoProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.CreateCategory(ctx, "Misc", "")
	if err != nil {
		t.Fatalf("create no-project category: %v", err)
	}
	if c.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty", c.ProjectID)
	}
}

func TestListCategoriesByProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	relac, _ := s.CreateProject(ctx, "RELAC")
	jdi, _ := s.CreateProject(ctx, "JDi")

	list, err := s.ListCategoriesByProject(ctx, relac.ID)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty, got %d", len(list))
	}

	for _, n := range []string{"Coding", "Planning", "Admin"} {
		if _, err := s.CreateCategory(ctx, n, relac.ID); err != nil {
			t.Fatalf("create relac %q: %v", n, err)
		}
	}
	if _, err := s.CreateCategory(ctx, "Research", jdi.ID); err != nil {
		t.Fatalf("create jdi: %v", err)
	}

	list, err = s.ListCategoriesByProject(ctx, relac.ID)
	if err != nil {
		t.Fatalf("list relac: %v", err)
	}
	want := []string{"Admin", "Coding", "Planning"}
	if len(list) != len(want) {
		t.Fatalf("got %d categories, want %d", len(list), len(want))
	}
	for i, c := range list {
		if c.Name != want[i] {
			t.Errorf("list[%d].Name = %q, want %q", i, c.Name, want[i])
		}
		if c.ProjectID != relac.ID {
			t.Errorf("list[%d].ProjectID = %q, want %q", i, c.ProjectID, relac.ID)
		}
	}

	jdiList, err := s.ListCategoriesByProject(ctx, jdi.ID)
	if err != nil {
		t.Fatalf("list jdi: %v", err)
	}
	if len(jdiList) != 1 || jdiList[0].Name != "Research" {
		t.Errorf("unexpected jdi categories: %+v", jdiList)
	}
}

func TestGetCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	proj, _ := s.CreateProject(ctx, "RELAC")
	created, err := s.CreateCategory(ctx, "Coding", proj.ID)
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

func TestSetCategoryTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	proj, _ := s.CreateProject(ctx, "P")
	cat, err := s.CreateCategory(ctx, "Keybr", proj.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Initially no target.
	if cat.HasTarget() {
		t.Error("new category should have no target")
	}

	// Set a minute/day target with weekday mask.
	mins := 30
	mask := 31 // weekdays
	if err := s.SetCategoryTarget(ctx, cat.ID, &mins, "day", &mask); err != nil {
		t.Fatalf("set target: %v", err)
	}

	got, err := s.GetCategory(ctx, cat.ID)
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if !got.HasTarget() {
		t.Error("should have target after set")
	}
	if got.TargetPeriod != "day" {
		t.Errorf("period = %q, want %q", got.TargetPeriod, "day")
	}
	if got.TargetMinutes == nil || *got.TargetMinutes != 30 {
		t.Errorf("minutes = %v, want 30", got.TargetMinutes)
	}
	if got.ScheduleMask == nil || *got.ScheduleMask != 31 {
		t.Errorf("mask = %v, want 31", got.ScheduleMask)
	}

	// Set a presence-only weekly target.
	if err := s.SetCategoryTarget(ctx, cat.ID, nil, "week", nil); err != nil {
		t.Fatalf("set weekly target: %v", err)
	}
	got2, _ := s.GetCategory(ctx, cat.ID)
	if got2.TargetMinutes != nil {
		t.Errorf("presence-only should have nil minutes, got %v", got2.TargetMinutes)
	}
	if got2.TargetPeriod != "week" {
		t.Errorf("period = %q, want week", got2.TargetPeriod)
	}
	if got2.ScheduleMask != nil {
		t.Errorf("weekly target should have nil mask, got %v", got2.ScheduleMask)
	}

	// Clear the target.
	if err := s.SetCategoryTarget(ctx, cat.ID, nil, "", nil); err != nil {
		t.Fatalf("clear target: %v", err)
	}
	got3, _ := s.GetCategory(ctx, cat.ID)
	if got3.HasTarget() {
		t.Error("target should be cleared")
	}

	// ErrNotFound on unknown ID.
	if err := s.SetCategoryTarget(ctx, "nope", nil, "day", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCreateOrGetCategoryByName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// First call: inserts
	c1, err := s.CreateOrGetCategoryByName(ctx, "email", "")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if c1.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	// Second call with same name: returns existing row
	c2, err := s.CreateOrGetCategoryByName(ctx, "email", "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if c2.ID != c1.ID {
		t.Errorf("got id %q, want %q (should reuse)", c2.ID, c1.ID)
	}

	// Different name: new row
	c3, err := s.CreateOrGetCategoryByName(ctx, "coding", "")
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if c3.ID == c1.ID {
		t.Error("different name should produce different row")
	}
}

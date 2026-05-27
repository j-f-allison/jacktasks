package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.CreateProject(ctx, "memo")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if p.Name != "memo" {
		t.Errorf("Name = %q, want %q", p.Name, "memo")
	}
}

func TestListProjects(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	list, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty, got %d", len(list))
	}

	for _, n := range []string{"memo", "outline", "annotations"} {
		if _, err := s.CreateProject(ctx, n); err != nil {
			t.Fatalf("create %q: %v", n, err)
		}
	}

	list, err = s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"annotations", "memo", "outline"}
	if len(list) != len(want) {
		t.Fatalf("got %d projects, want %d", len(list), len(want))
	}
	for i, p := range list {
		if p.Name != want[i] {
			t.Errorf("list[%d].Name = %q, want %q", i, p.Name, want[i])
		}
	}
}

func TestGetProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateProject(ctx, "memo")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetProject(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name {
		t.Errorf("got %+v, want id=%q name=%q", got, created.ID, created.Name)
	}

	if _, err := s.GetProject(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got err %v, want ErrNotFound", err)
	}
}

func TestSetProjectRemindersList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.CreateProject(ctx, "thesis")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.RemindersListName != "" {
		t.Errorf("new project RemindersListName = %q, want empty", p.RemindersListName)
	}

	// Set a list name.
	if err := s.SetProjectRemindersList(ctx, p.ID, "Thesis Tasks"); err != nil {
		t.Fatalf("SetProjectRemindersList set: %v", err)
	}
	got, err := s.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if got.RemindersListName != "Thesis Tasks" {
		t.Errorf("RemindersListName = %q, want %q", got.RemindersListName, "Thesis Tasks")
	}

	// Clear the list name.
	if err := s.SetProjectRemindersList(ctx, p.ID, ""); err != nil {
		t.Fatalf("SetProjectRemindersList clear: %v", err)
	}
	got, err = s.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got.RemindersListName != "" {
		t.Errorf("RemindersListName after clear = %q, want empty", got.RemindersListName)
	}

	// Not found.
	if err := s.SetProjectRemindersList(ctx, "no-such-id", "X"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListProjectsIncludesRemindersListName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.CreateProject(ctx, "class")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.SetProjectRemindersList(ctx, p.ID, "Assignments"); err != nil {
		t.Fatalf("set reminders list: %v", err)
	}

	list, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d projects, want 1", len(list))
	}
	if list[0].RemindersListName != "Assignments" {
		t.Errorf("RemindersListName = %q, want Assignments", list[0].RemindersListName)
	}
}

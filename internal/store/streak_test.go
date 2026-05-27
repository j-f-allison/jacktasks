package store

import (
	"context"
	"testing"
	"time"

	"github.com/j-f-allison/jacktasks/internal/target"
)

// seedSession inserts a session for catID starting at startedAt with the given
// duration in seconds.
func seedSession(t *testing.T, s *Store, catID, projID string, startedAt time.Time, durSec int) {
	t.Helper()
	ctx := context.Background()
	_, err := s.CreateSession(ctx, CreateSessionInput{
		CategoryID:         catID,
		ProjectID:          projID,
		PlannedDurationMin: 30,
		ActualDurationSec:  durSec,
		StartedAt:          startedAt,
		EndedAt:            startedAt.Add(time.Duration(durSec) * time.Second),
		Status:             SessionCompleted,
		DeviceID:           "test-device",
	})
	if err != nil {
		t.Fatalf("seedSession: %v", err)
	}
}

// categoryCat creates a category and sets its target.
func categoryWithTarget(t *testing.T, s *Store, projID string, mins *int, period string, mask *int) Category {
	t.Helper()
	ctx := context.Background()
	cat, err := s.CreateCategory(ctx, "test-cat", projID)
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if period != "" {
		if err := s.SetCategoryTarget(ctx, cat.ID, mins, period, mask); err != nil {
			t.Fatalf("set target: %v", err)
		}
		cat, err = s.GetCategory(ctx, cat.ID)
		if err != nil {
			t.Fatalf("get after target set: %v", err)
		}
	}
	return *cat
}

func TestSumCategorySecondsBetween(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	// Reference time: 2024-01-10 noon local.
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)
	dayStart := time.Date(2024, 1, 10, 0, 0, 0, 0, time.Local).Unix()
	dayEnd := time.Date(2024, 1, 11, 0, 0, 0, 0, time.Local).Unix()

	// No sessions yet.
	n, err := s.SumCategorySecondsBetween(ctx, catID, dayStart, dayEnd)
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if n != 0 {
		t.Errorf("want 0, got %d", n)
	}

	// Add two sessions today.
	seedSession(t, s, catID, projID, now.Add(-2*time.Hour), 900)  // 15 min
	seedSession(t, s, catID, projID, now.Add(-1*time.Hour), 1200) // 20 min

	n, err = s.SumCategorySecondsBetween(ctx, catID, dayStart, dayEnd)
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if n != 2100 {
		t.Errorf("want 2100, got %d", n)
	}

	// Session outside the window not counted.
	yesterday := time.Date(2024, 1, 9, 12, 0, 0, 0, time.Local)
	seedSession(t, s, catID, projID, yesterday, 3600)
	n, err = s.SumCategorySecondsBetween(ctx, catID, dayStart, dayEnd)
	if err != nil {
		t.Fatalf("sum after yesterday session: %v", err)
	}
	if n != 2100 {
		t.Errorf("window boundary: want 2100, got %d", n)
	}
}

func TestCategoryActiveBetween(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	catID, projID := sessionFixtures(t, s)

	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.Local).Unix()
	end := time.Date(2024, 1, 11, 0, 0, 0, 0, time.Local).Unix()

	active, err := s.CategoryActiveBetween(ctx, catID, start, end)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if active {
		t.Error("should be inactive with no sessions")
	}

	seedSession(t, s, catID, projID, now, 1800)

	active, err = s.CategoryActiveBetween(ctx, catID, start, end)
	if err != nil {
		t.Fatalf("active after seed: %v", err)
	}
	if !active {
		t.Error("should be active after seeded session")
	}
}

func TestCategoryStreakNoTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")
	cat, _ := s.CreateCategory(ctx, "C", proj.ID)

	n, err := CategoryStreak(ctx, s, *cat, time.Now())
	if err != nil {
		t.Fatalf("streak: %v", err)
	}
	if n != 0 {
		t.Errorf("no-target streak should be 0, got %d", n)
	}
}

func TestDailyStreakMinuteGoal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	mins := 30 // 30 min = 1800 sec
	cat := categoryWithTarget(t, s, proj.ID, &mins, target.PeriodDay, nil)

	// Reference "today" = 2024-01-10 noon.
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)

	// No sessions: streak 0.
	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 0 {
		t.Errorf("empty: got streak %d err %v, want 0", n, err)
	}

	// Meet goal yesterday and day before.
	seedSession(t, s, cat.ID, proj.ID, now.AddDate(0, 0, -1).Add(time.Hour), 1800) // yesterday: exactly 30 min
	seedSession(t, s, cat.ID, proj.ID, now.AddDate(0, 0, -2).Add(time.Hour), 1800) // 2 days ago

	n, err = CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 2 {
		t.Errorf("2-day streak: got %d err %v, want 2", n, err)
	}

	// Day 3 ago missed: streak still 2.
	n, err = CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 2 {
		t.Errorf("after gap: got %d err %v, want 2", n, err)
	}
}

func TestDailyStreakScheduleMask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	// Weekdays-only target.
	mins := 20
	mask := target.MaskWeekdays
	cat := categoryWithTarget(t, s, proj.ID, &mins, target.PeriodDay, &mask)

	// Use a Wednesday as "today" = 2024-01-10 (Wednesday).
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local) // Wed

	// Seed Tue (Jan 9), Mon (Jan 8). Skip the weekend before (Jan 6 Sat, Jan 7 Sun).
	// Then Fri Jan 5 should also count (weekend is skipped, not breaking).
	seedSession(t, s, cat.ID, proj.ID, time.Date(2024, 1, 9, 10, 0, 0, 0, time.Local), 1200) // Tue: 20 min
	seedSession(t, s, cat.ID, proj.ID, time.Date(2024, 1, 8, 10, 0, 0, 0, time.Local), 1200) // Mon: 20 min
	// Sat Jan 6 and Sun Jan 7 are skipped.
	seedSession(t, s, cat.ID, proj.ID, time.Date(2024, 1, 5, 10, 0, 0, 0, time.Local), 1200) // Fri: 20 min

	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil {
		t.Fatalf("streak: %v", err)
	}
	// Tue, Mon, (Sat skipped), (Sun skipped), Fri = 3 days counted.
	if n != 3 {
		t.Errorf("want 3, got %d", n)
	}
}

func TestDailyStreakPresenceOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	cat := categoryWithTarget(t, s, proj.ID, nil, target.PeriodDay, nil)
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)

	// Any session counts, even 1 second.
	seedSession(t, s, cat.ID, proj.ID, now.AddDate(0, 0, -1).Add(time.Hour), 1)
	seedSession(t, s, cat.ID, proj.ID, now.AddDate(0, 0, -2).Add(time.Hour), 1)

	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 2 {
		t.Errorf("presence: got %d err %v, want 2", n, err)
	}
}

func TestCurrentDayDoesNotBreakStreak(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	mins := 30
	cat := categoryWithTarget(t, s, proj.ID, &mins, target.PeriodDay, nil)
	now := time.Date(2024, 1, 10, 8, 0, 0, 0, time.Local) // early morning, not yet done

	// Yesterday met, today not yet met.
	seedSession(t, s, cat.ID, proj.ID, now.AddDate(0, 0, -1).Add(time.Hour), 1800)

	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 1 {
		t.Errorf("in-progress day: got %d err %v, want 1", n, err)
	}
}

func TestWeeklyStreak(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	mins := 30
	cat := categoryWithTarget(t, s, proj.ID, &mins, target.PeriodWeek, nil)

	// "Today" = 2024-01-10 (Wednesday, week of Jan 8-14).
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)

	// Prev week (Jan 1-7): seed 30 min.
	seedSession(t, s, cat.ID, proj.ID, time.Date(2024, 1, 3, 10, 0, 0, 0, time.Local), 1800)
	// Week before that (Dec 25-31): seed 30 min.
	seedSession(t, s, cat.ID, proj.ID, time.Date(2023, 12, 27, 10, 0, 0, 0, time.Local), 1800)

	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 2 {
		t.Errorf("weekly 2-week streak: got %d err %v, want 2", n, err)
	}
}

func TestCurrentWeekDoesNotBreakStreak(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	mins := 30
	cat := categoryWithTarget(t, s, proj.ID, &mins, target.PeriodWeek, nil)

	// "Today" = Wednesday Jan 10. Previous week met, current week not yet.
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)
	seedSession(t, s, cat.ID, proj.ID, time.Date(2024, 1, 3, 10, 0, 0, 0, time.Local), 1800) // prev week

	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 1 {
		t.Errorf("in-progress week: got %d err %v, want 1", n, err)
	}
}

func TestWeeklyStreakPresenceOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj, _ := s.CreateProject(ctx, "P")

	cat := categoryWithTarget(t, s, proj.ID, nil, target.PeriodWeek, nil)
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.Local)

	seedSession(t, s, cat.ID, proj.ID, time.Date(2024, 1, 3, 10, 0, 0, 0, time.Local), 1)

	n, err := CategoryStreak(ctx, s, cat, now)
	if err != nil || n != 1 {
		t.Errorf("weekly presence: got %d err %v, want 1", n, err)
	}
}

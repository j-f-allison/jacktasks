package store

import (
	"context"
	"time"

	"github.com/j-f-allison/jacktasks/internal/target"
)

const maxStreakDays = 366
const maxStreakWeeks = 53

// CategoryStreak returns the current streak (number of consecutive periods in
// which the category's target was met) as of now.
//
// Rules:
//   - Daily: walks days backward. Days outside schedule_mask are skipped (neither
//     increment nor break the streak). The current (in-progress) day never breaks
//     the streak — only past unmet scheduled days do.
//   - Weekly: walks Monday-to-Sunday weeks backward (ISO). The current in-progress
//     week never breaks the streak.
//
// Returns 0 if cat has no target or the most recent qualifying period was missed.
func CategoryStreak(ctx context.Context, s *Store, cat Category, now time.Time) (int, error) {
	if !cat.HasTarget() {
		return 0, nil
	}

	if cat.TargetPeriod == target.PeriodDay {
		return dailyStreak(ctx, s, cat, now)
	}
	return weeklyStreak(ctx, s, cat, now)
}

// dailyStreak counts consecutive past days (up to maxStreakDays) in which the
// target was met, skipping days outside schedule_mask. The current day is not
// counted.
func dailyStreak(ctx context.Context, s *Store, cat Category, now time.Time) (int, error) {
	loc := now.Location()
	streak := 0

	// Start walking from yesterday.
	y, m, d := now.Date()
	current := time.Date(y, m, d-1, 0, 0, 0, 0, loc)

	for i := 0; i < maxStreakDays; i++ {
		dayStart := current.Unix()
		dayEnd := current.AddDate(0, 0, 1).Unix()

		// Bit 0 = Mon; time.Monday = 1 in Go, so subtract 1, handle Sunday (0→6).
		wd := int(current.Weekday()) - 1
		if wd < 0 {
			wd = 6
		}

		if !target.DayScheduled(cat.ScheduleMask, wd) {
			// Skip this day — move to the previous day without breaking streak.
			current = current.AddDate(0, 0, -1)
			continue
		}

		met, err := periodMet(ctx, s, cat, dayStart, dayEnd)
		if err != nil {
			return 0, err
		}
		if !met {
			break
		}
		streak++
		current = current.AddDate(0, 0, -1)
	}
	return streak, nil
}

// weeklyStreak counts consecutive past ISO weeks (Mon-Sun) in which the target
// was met, up to maxStreakWeeks. The current week is not counted.
func weeklyStreak(ctx context.Context, s *Store, cat Category, now time.Time) (int, error) {
	loc := now.Location()
	streak := 0

	// Start of the current week (Monday).
	thisWeekStart := StartOfWeekMonday(now, loc)

	// Walk backward from the previous week.
	weekEnd := thisWeekStart
	weekStart := weekEnd.AddDate(0, 0, -7)

	for i := 0; i < maxStreakWeeks; i++ {
		met, err := periodMet(ctx, s, cat, weekStart.Unix(), weekEnd.Unix())
		if err != nil {
			return 0, err
		}
		if !met {
			break
		}
		streak++
		weekEnd = weekStart
		weekStart = weekEnd.AddDate(0, 0, -7)
	}
	return streak, nil
}

// periodMet reports whether the category's target was met within [start, end).
func periodMet(ctx context.Context, s *Store, cat Category, start, end int64) (bool, error) {
	if cat.TargetMinutes == nil {
		// Presence-only: any session counts.
		return s.CategoryActiveBetween(ctx, cat.ID, start, end)
	}
	secs, err := s.SumCategorySecondsBetween(ctx, cat.ID, start, end)
	if err != nil {
		return false, err
	}
	return secs >= (*cat.TargetMinutes)*60, nil
}

// StartOfWeekMonday returns the Monday 00:00:00 of the ISO week containing t.
func StartOfWeekMonday(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.Date()
	wd := int(t.Weekday())
	// Go: Sun=0, Mon=1..Sat=6. We want Monday=0..Sunday=6 offset.
	offset := wd - 1
	if offset < 0 {
		offset = 6
	}
	return time.Date(y, m, d-offset, 0, 0, 0, 0, loc)
}

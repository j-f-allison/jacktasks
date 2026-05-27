package target_test

import (
	"testing"

	"github.com/j-f-allison/jacktasks/internal/target"
)

func pint(n int) *int { return &n }

func TestParse(t *testing.T) {
	tests := []struct {
		input        string
		wantMinutes  *int
		wantSessions *int
		wantPeriod   string
		wantMask     *int
		wantErr      bool
	}{
		// Clear cases.
		{"", nil, nil, "", nil, false},
		{"none", nil, nil, "", nil, false},
		{"NONE", nil, nil, "", nil, false},
		{"  none  ", nil, nil, "", nil, false},

		// Minute/day.
		{"30/day", pint(30), nil, "day", nil, false},
		{"30/DAY", pint(30), nil, "day", nil, false},
		{"5/day", pint(5), nil, "day", nil, false},

		// Session-count/day.
		{"3x/day", nil, pint(3), "day", nil, false},
		{"3X/DAY", nil, pint(3), "day", nil, false},
		{"1x/day", nil, pint(1), "day", nil, false},
		{"3x/day MTWTF", nil, pint(3), "day", pint(target.MaskWeekdays), false},

		// Presence-only day.
		{"/day", nil, nil, "day", nil, false},
		{"/DAY", nil, nil, "day", nil, false},

		// Minute/week.
		{"30/week", pint(30), nil, "week", nil, false},
		{"120/week", pint(120), nil, "week", nil, false},

		// Session-count/week.
		{"2x/week", nil, pint(2), "week", nil, false},

		// Presence-only week.
		{"/week", nil, nil, "week", nil, false},

		// With weekday masks.
		{"30/day MTWTF", pint(30), nil, "day", pint(target.MaskWeekdays), false},
		{"/day MTWTF", nil, nil, "day", pint(target.MaskWeekdays), false},
		{"20/day MWF", pint(20), nil, "day", pint(target.BitMon | target.BitWed | target.BitFri), false},
		{"20/day SS", pint(20), nil, "day", pint(target.BitSat | target.BitSun), false},
		{"20/day MTWTFSS", pint(20), nil, "day", pint(target.MaskEveryDay), false},

		// Error cases.
		{"30/fortnight", nil, nil, "", nil, true},
		{"0/day", nil, nil, "", nil, true},
		{"-1/day", nil, nil, "", nil, true},
		{"abc/day", nil, nil, "", nil, true},
		{"0x/day", nil, nil, "", nil, true},        // zero sessions
		{"-2x/day", nil, nil, "", nil, true},       // negative sessions
		{"x/day", nil, nil, "", nil, true},         // missing count
		{"abcx/day", nil, nil, "", nil, true},      // non-numeric count
		{"2x/week MTWTF", nil, nil, "", nil, true}, // weekday on week target
		{"30/week MTWTF", nil, nil, "", nil, true}, // weekday on week target
		{"extra tokens here", nil, nil, "", nil, true},
		{"30", nil, nil, "", nil, true}, // no slash
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotMin, gotSessions, gotPeriod, gotMask, err := target.Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse(%q) want error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("Parse(%q) unexpected error: %v", tt.input, err)
				return
			}
			if !eqIntPtr(gotMin, tt.wantMinutes) {
				t.Errorf("Parse(%q) minutes: got %v, want %v", tt.input, deref(gotMin), deref(tt.wantMinutes))
			}
			if !eqIntPtr(gotSessions, tt.wantSessions) {
				t.Errorf("Parse(%q) sessions: got %v, want %v", tt.input, deref(gotSessions), deref(tt.wantSessions))
			}
			if gotPeriod != tt.wantPeriod {
				t.Errorf("Parse(%q) period: got %q, want %q", tt.input, gotPeriod, tt.wantPeriod)
			}
			if !eqIntPtr(gotMask, tt.wantMask) {
				t.Errorf("Parse(%q) mask: got %v, want %v", tt.input, deref(gotMask), deref(tt.wantMask))
			}
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		minutes  *int
		sessions *int
		period   string
		mask     *int
		want     string
	}{
		{nil, nil, "", nil, ""},
		{pint(30), nil, "day", nil, "30 min/day"},
		{pint(30), nil, "day", pint(target.MaskWeekdays), "30 min/day, weekdays"},
		{pint(20), nil, "day", pint(target.MaskEveryDay), "20 min/day, every day"},
		{nil, nil, "day", pint(target.MaskWeekdays), "presence/day, weekdays"},
		{pint(30), nil, "week", nil, "30 min/week"},
		{nil, nil, "week", nil, "presence/week"},
		{pint(20), nil, "day", pint(target.BitMon | target.BitWed | target.BitFri), "20 min/day, Mon/Wed/Fri"},
		{nil, pint(3), "day", nil, "3 sessions/day"},
		{nil, pint(1), "day", nil, "1 session/day"},
		{nil, pint(3), "day", pint(target.MaskWeekdays), "3 sessions/day, weekdays"},
		{nil, pint(2), "week", nil, "2 sessions/week"},
	}

	for _, tt := range tests {
		got := target.Format(tt.minutes, tt.sessions, tt.period, tt.mask)
		if got != tt.want {
			t.Errorf("Format(%v, %v, %q, %v) = %q, want %q",
				deref(tt.minutes), deref(tt.sessions), tt.period, deref(tt.mask), got, tt.want)
		}
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	// Parse then format should produce a stable description (not necessarily the
	// same string, but semantically equivalent and formatted predictably).
	cases := []struct {
		input      string
		wantFormat string
	}{
		{"30/day", "30 min/day"},
		{"30/day MTWTF", "30 min/day, weekdays"},
		{"3x/day", "3 sessions/day"},
		{"1x/day MTWTF", "1 session/day, weekdays"},
		{"2x/week", "2 sessions/week"},
		{"/week", "presence/week"},
		{"none", ""},
		{"", ""},
	}
	for _, tc := range cases {
		min, sessions, per, mask, err := target.Parse(tc.input)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.input, err)
			continue
		}
		got := target.Format(min, sessions, per, mask)
		if got != tc.wantFormat {
			t.Errorf("Format(Parse(%q)) = %q, want %q", tc.input, got, tc.wantFormat)
		}
	}
}

func TestDayScheduled(t *testing.T) {
	tests := []struct {
		mask    *int
		weekday int // 0=Mon..6=Sun
		want    bool
	}{
		{nil, 0, true}, // nil mask = every day
		{nil, 5, true},
		{pint(target.MaskWeekdays), 0, true},  // Mon
		{pint(target.MaskWeekdays), 4, true},  // Fri
		{pint(target.MaskWeekdays), 5, false}, // Sat
		{pint(target.MaskWeekdays), 6, false}, // Sun
		{pint(target.BitWed), 2, true},
		{pint(target.BitWed), 1, false},
	}
	for _, tt := range tests {
		got := target.DayScheduled(tt.mask, tt.weekday)
		if got != tt.want {
			t.Errorf("DayScheduled(%v, %d) = %v, want %v", deref(tt.mask), tt.weekday, got, tt.want)
		}
	}
}

func eqIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

package target

import (
	"fmt"
	"strconv"
	"strings"
)

// PeriodDay and PeriodWeek are the valid target_period values.
const (
	PeriodDay  = "day"
	PeriodWeek = "week"
)

// Day-of-week bit positions: bit 0 = Monday, bit 6 = Sunday.
const (
	BitMon = 1 << 0
	BitTue = 1 << 1
	BitWed = 1 << 2
	BitThu = 1 << 3
	BitFri = 1 << 4
	BitSat = 1 << 5
	BitSun = 1 << 6

	MaskWeekdays = BitMon | BitTue | BitWed | BitThu | BitFri // 0b0011111 = 31
	MaskEveryDay = BitMon | BitTue | BitWed | BitThu | BitFri | BitSat | BitSun // 127
)

// Parse parses a compact target line into its components.
//
// Supported syntax (case-insensitive, whitespace-tolerant):
//
//	"none" or ""         → clear (all nil / "")
//	"30/day"             → 30 min/day, every day
//	"30/day MTWTF"       → 30 min/day, weekdays only
//	"/day"               → presence-only, every day
//	"/day MWF"           → presence-only, Mon/Wed/Fri
//	"30/week"            → 30 min/week
//	"/week"              → presence-only weekly
//
// Weekday tokens use letters M T W T F S S (Mon..Sun); duplicate entries are
// OR-ed. A weekday token on /week is rejected.
func Parse(s string) (minutes *int, period string, mask *int, err error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return nil, "", nil, nil
	}

	// Split on whitespace: first token is <n?>/period, optional second is weekday string.
	parts := strings.Fields(s)
	if len(parts) > 2 {
		return nil, "", nil, fmt.Errorf("too many tokens; expected \"<n>/day|week [MTWTFSS]\"")
	}

	main := strings.ToLower(parts[0])
	var weekdayToken string
	if len(parts) == 2 {
		weekdayToken = strings.ToUpper(parts[1])
	}

	// Parse <n?>/period.
	slashIdx := strings.LastIndex(main, "/")
	if slashIdx < 0 {
		return nil, "", nil, fmt.Errorf("missing '/'; use e.g. \"30/day\" or \"/week\"")
	}

	numPart := main[:slashIdx]
	perPart := main[slashIdx+1:]

	switch perPart {
	case PeriodDay, PeriodWeek:
	default:
		return nil, "", nil, fmt.Errorf("unknown period %q; use \"day\" or \"week\"", perPart)
	}

	if numPart != "" {
		n, parseErr := strconv.Atoi(numPart)
		if parseErr != nil || n <= 0 {
			return nil, "", nil, fmt.Errorf("minutes must be a positive integer, got %q", numPart)
		}
		minutes = &n
	}

	period = perPart

	// Weekday token only allowed on day targets.
	if weekdayToken != "" {
		if period != PeriodDay {
			return nil, "", nil, fmt.Errorf("weekday schedule only applies to daily targets")
		}
		m, maskErr := parseWeekdays(weekdayToken)
		if maskErr != nil {
			return nil, "", nil, maskErr
		}
		mask = &m
	}

	return minutes, period, mask, nil
}

// Format returns a human-readable description of a target, e.g.
// "30 min/day, weekdays" or "presence/week" or "" when no target is set.
// Intended for category-list annotations and confirmation echoes.
func Format(minutes *int, period string, mask *int) string {
	if period == "" {
		return ""
	}

	var parts []string

	// Minute goal or presence.
	if minutes != nil {
		parts = append(parts, fmt.Sprintf("%d min/%s", *minutes, period))
	} else {
		parts = append(parts, fmt.Sprintf("presence/%s", period))
	}

	// Weekday schedule (daily only).
	if period == PeriodDay && mask != nil {
		parts = append(parts, maskToString(*mask))
	}

	return strings.Join(parts, ", ")
}

// parseWeekdays converts a string of MTWTFSS letters (Mon..Sun) to a bitmask.
// Duplicate letters are OR-ed. Unknown letters return an error.
func parseWeekdays(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("weekday string is empty")
	}

	// Map each letter to its day — but 'T' is ambiguous (Tue and Thu),
	// as is 'S' (Sat and Sun). We resolve by position in the canonical
	// MTWTFSS sequence: odd T (index 1 in MTWTF) = Tue, even T (index 3) = Thu.
	// Similarly, S at position 5 = Sat, position 6 = Sun.
	// Strategy: match against the canonical string "MTWTFSS" greedily left-to-right.
	canonical := "MTWTFSS"
	canonicalBits := []int{BitMon, BitTue, BitWed, BitThu, BitFri, BitSat, BitSun}

	result := 0
	pos := 0 // position in canonical string
	for _, ch := range s {
		found := false
		for pos < len(canonical) {
			if rune(canonical[pos]) == ch {
				result |= canonicalBits[pos]
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			// Try allowing wrapping for individual characters not in order.
			// Reset search for this char from the beginning.
			for i, c := range canonical {
				if c == ch {
					result |= canonicalBits[i]
					found = true
					break
				}
			}
			if !found {
				return 0, fmt.Errorf("unknown day letter %q in %q", string(ch), s)
			}
		}
	}

	if result == 0 {
		return 0, fmt.Errorf("weekday string %q matched no days", s)
	}
	return result, nil
}

// maskToString returns a human-readable description of a schedule_mask.
func maskToString(mask int) string {
	switch mask {
	case MaskWeekdays:
		return "weekdays"
	case MaskEveryDay:
		return "every day"
	case BitSat | BitSun:
		return "weekends"
	}

	// Build abbreviated string.
	days := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	bits := []int{BitMon, BitTue, BitWed, BitThu, BitFri, BitSat, BitSun}
	var out []string
	for i, bit := range bits {
		if mask&bit != 0 {
			out = append(out, days[i])
		}
	}
	return strings.Join(out, "/")
}

// DayScheduled reports whether the given weekday (time.Weekday) is scheduled
// per the mask. Sunday = bit 6, Monday = bit 0 (ISO). Pass mask=nil for "every day".
func DayScheduled(mask *int, weekday int) bool {
	if mask == nil {
		return true
	}
	// weekday: 0=Mon..6=Sun (caller converts from time.Weekday where 0=Sun)
	return (*mask)&(1<<weekday) != 0
}

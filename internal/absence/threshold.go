package absence

import "time"

// Present returns how many team members are present on day d,
// given a map of how many people are already on absence per day.
// It assumes d is already truncated to the start of the day.
func Present(d time.Time, onAbsence map[time.Time]int, teamSize int) int {
	return teamSize - onAbsence[d]
}

// Color returns the CSS color token for a given presence count.
// Thresholds: below min → "red", just above → "orange", mid → "yellow", near-full → "green".
func Color(present, teamSize, minPresent int) string {
	if present <= minPresent {
		return "red"
	}
	span := teamSize - minPresent
	if span <= 0 {
		return "green"
	}
	ratio := float64(present-minPresent) / float64(span)
	switch {
	case ratio >= 0.75:
		return "green"
	case ratio >= 0.50:
		return "yellow"
	default:
		return "orange"
	}
}

// CheckRequest returns the subset of days in [start, end] where approving the
// new request would push coverage below minPresent. weekendCounts=false skips
// Saturday and Sunday from threshold evaluation.
func CheckRequest(start, end time.Time, onAbsence map[time.Time]int, teamSize, minPresent int, weekendCounts bool) []time.Time {
	var offending []time.Time
	curr := truncateDay(start)
	last := truncateDay(end)

	for !curr.After(last) {
		if !weekendCounts && isWeekend(curr) {
			curr = curr.AddDate(0, 0, 1)
			continue
		}
		present := Present(curr, onAbsence, teamSize) - 1 // -1 for the requester
		if present < minPresent {
			offending = append(offending, curr)
		}
		curr = curr.AddDate(0, 0, 1)
	}
	return offending
}

func isWeekend(d time.Time) bool {
	wd := d.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

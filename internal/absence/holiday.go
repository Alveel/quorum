package absence

import "time"

type HolidaySource string

const (
	HolidaySourceImported HolidaySource = "imported"
	HolidaySourceManual   HolidaySource = "manual"
)

type Holiday struct {
	Date   time.Time
	Name   string
	Source HolidaySource
}

// HolidaySet converts a holiday list (as returned by the store) into the map[time.Time]bool
// shape Coverage expects.
func HolidaySet(holidays []Holiday) map[time.Time]bool {
	out := make(map[time.Time]bool, len(holidays))
	for _, h := range holidays {
		out[truncateDay(h.Date)] = true
	}
	return out
}

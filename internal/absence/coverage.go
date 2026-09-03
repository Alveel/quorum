package absence

import (
	"sort"
	"time"
)

type RoleID int32

type Role struct {
	ID         RoleID
	Name       string
	MinPresent int
}

// Member carries roster identity + weekly working-day pattern + role membership.
// Coverage uses ID/Active/WorkingDays/RoleIDs; Email/Name are for admin display.
type Member struct {
	ID          string
	Email       string
	Name        string
	WorkingDays uint8 // bit i (time.Weekday() value, Sun=0..Sat=6) set = works that weekday
	Active      bool
	RoleIDs     []RoleID
}

// WorksOn reports whether m is scheduled to work on the weekday of d.
func (m Member) WorksOn(d time.Time) bool {
	return m.WorkingDays&(1<<uint(d.Weekday())) != 0
}

// HasRole reports whether m holds role id.
func (m Member) HasRole(id RoleID) bool {
	for _, r := range m.RoleIDs {
		if r == id {
			return true
		}
	}
	return false
}

type Settings struct {
	MinPresent     int    // global fallback threshold
	HolidayCountry string // ISO code, e.g. "nl"; empty disables sync
}

type RoleCoverage struct {
	Expected int
	Present  int
	Min      int
}

type DayCoverage struct {
	Date     time.Time
	Expected int
	Present  int
	Min      int // = globalMinPresent, set even on NoOneScheduled days
	PerRole  map[RoleID]RoleCoverage
	// Failing lists roles at-or-below their Min this day (<=). Drives Color/red and the
	// admin feasibility warning; not the same boundary OffendingDays uses for denial.
	Failing        []RoleID
	HasOverride    bool // any absence covering this day has Status == StatusOverridden
	NoOneScheduled bool // Expected == 0 (holiday, or nobody's pattern covers this day)
	Color          string
}

// Coverage is the single seam for all day-coverage math: heatmap coloring, the
// absence-request denial check (via OffendingDays), and the admin role-feasibility
// warning (via FeasibilityWarnings) all read from its result — nothing else computes
// present/expected counts. Fills every day in [from,to] inclusive, keyed by UTC midnight.
func Coverage(roster []Member, absences []Absence, holidays map[time.Time]bool, roles []Role, globalMinPresent int, from, to time.Time) map[time.Time]DayCoverage {
	from, to = truncateDay(from), truncateDay(to)

	absentOn := map[time.Time]map[string]bool{}
	overrideOn := map[time.Time]bool{}
	for _, a := range absences {
		if a.Status == StatusCancelled {
			continue
		}
		s, e := truncateDay(a.StartDate), truncateDay(a.EndDate)
		if s.Before(from) {
			s = from
		}
		if e.After(to) {
			e = to
		}
		for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
			if absentOn[d] == nil {
				absentOn[d] = map[string]bool{}
			}
			absentOn[d][a.UserID] = true
			if a.Status == StatusOverridden {
				overrideOn[d] = true
			}
		}
	}

	out := make(map[time.Time]DayCoverage)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if holidays[d] {
			out[d] = DayCoverage{Date: d, Min: globalMinPresent, NoOneScheduled: true, Color: "none"}
			continue
		}

		perRole := make(map[RoleID]RoleCoverage, len(roles))
		for _, r := range roles {
			perRole[r.ID] = RoleCoverage{Min: r.MinPresent}
		}

		var expected, present int
		for _, m := range roster {
			if !m.Active || !m.WorksOn(d) {
				continue
			}
			expected++
			absent := absentOn[d][m.ID]
			if !absent {
				present++
			}
			for _, rid := range m.RoleIDs {
				rc := perRole[rid]
				rc.Expected++
				if !absent {
					rc.Present++
				}
				perRole[rid] = rc
			}
		}

		if expected == 0 {
			out[d] = DayCoverage{Date: d, Min: globalMinPresent, NoOneScheduled: true, Color: "none"}
			continue
		}

		worst := colorFor(present, expected, globalMinPresent)
		var failing []RoleID
		for rid, rc := range perRole {
			if rc.Expected == 0 {
				continue
			}
			if c := colorFor(rc.Present, rc.Expected, rc.Min); colorRank[c] < colorRank[worst] {
				worst = c
			}
			if rc.Present <= rc.Min {
				failing = append(failing, rid)
			}
		}
		sort.Slice(failing, func(i, j int) bool { return failing[i] < failing[j] })

		out[d] = DayCoverage{
			Date: d, Expected: expected, Present: present, Min: globalMinPresent,
			PerRole: perRole, Failing: failing, HasOverride: overrideOn[d], Color: worst,
		}
	}
	return out
}

// OffendingDays returns the subset of [start,end] where cov — produced by Coverage with
// the candidate absence already merged into its absences input — shows global coverage or
// one of member's own roles dropping strictly below its minimum ("<", not the "<=" Failing/
// Color use: a day can be legally approved down to exactly Min and still render red). Days
// the member doesn't work per pattern, or holidays, never appear: Coverage already excludes
// the member from Expected/Present there regardless of any absence row, so a pre-existing
// shortage caused by other people can't be misattributed to this request.
func OffendingDays(cov map[time.Time]DayCoverage, member Member, start, end time.Time) []time.Time {
	var out []time.Time
	for d := truncateDay(start); !d.After(truncateDay(end)); d = d.AddDate(0, 0, 1) {
		dc, ok := cov[d]
		if !ok || dc.NoOneScheduled || !member.WorksOn(d) {
			continue
		}
		offending := dc.Present < dc.Min
		for _, rid := range member.RoleIDs {
			if rc, ok := dc.PerRole[rid]; ok && rc.Expected > 0 && rc.Present < rc.Min {
				offending = true
			}
		}
		if offending {
			out = append(out, d)
		}
	}
	return out
}

// FeasibilityWarnings scans an already-computed Coverage result (any absences set —
// Expected is absence-independent) and returns, per role, the dates where its Expected
// headcount is already short of its Min before anyone books leave. Gated on Expected>0 so
// a role with zero scheduled holders on an off-pattern day doesn't spuriously flood the
// admin page — same rule Failing/Color already use.
func FeasibilityWarnings(cov map[time.Time]DayCoverage) map[RoleID][]time.Time {
	out := map[RoleID][]time.Time{}
	for d, dc := range cov {
		if dc.NoOneScheduled {
			continue
		}
		for rid, rc := range dc.PerRole {
			if rc.Expected > 0 && rc.Expected < rc.Min {
				out[rid] = append(out[rid], d)
			}
		}
	}
	for rid := range out {
		sort.Slice(out[rid], func(i, j int) bool { return out[rid][i].Before(out[rid][j]) })
	}
	return out
}

func colorFor(present, expected, min int) string {
	if present <= min {
		return "red"
	}
	span := expected - min
	if span <= 0 {
		return "green"
	}
	ratio := float64(present-min) / float64(span)
	switch {
	case ratio >= 0.75:
		return "green"
	case ratio >= 0.50:
		return "yellow"
	default:
		return "orange"
	}
}

var colorRank = map[string]int{"red": 0, "orange": 1, "yellow": 2, "green": 3}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

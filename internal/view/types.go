package view

import (
	"time"

	"github.com/alveel/quorum/internal/absence"
)

type DayCell struct {
	Date           time.Time
	Present        int
	Expected       int
	Color          string // "green" | "yellow" | "orange" | "red" | "none"
	HasOverride    bool
	IsWeekend      bool
	NoOneScheduled bool
	Blank          bool // padding before month starts
}

type MonthData struct {
	Name string
	Year int
	Mon  time.Month
	Days []DayCell
}

type HeatmapData struct {
	Year       int
	PrevYear   int
	NextYear   int
	Months     []MonthData
	MinPresent int
}

type PageData struct {
	User       string
	IsAdmin    bool
	Heatmap    HeatmapData
	MyAbsences []absence.Absence
	Configured bool // false when the member has zero roles (or is inactive) — gates AbsenceForm
}

type RosterRow struct {
	ID          string
	DisplayName string
	Active      bool
	WorkingDays uint8
	Roles       []string // resolved role names
}

type RoleRow struct {
	ID              absence.RoleID
	Name            string
	MinPresent      int
	MemberCount     int
	InfeasibleDates []time.Time
}

type HolidayRow struct {
	Date   time.Time
	Name   string
	Source string
}

type AdminData struct {
	User           string
	Settings       absence.Settings
	Absences       []absence.Absence
	Roster         []RosterRow
	Roles          []RoleRow
	Holidays       []HolidayRow
	CountryOptions []string
}

type RoleOption struct {
	ID       absence.RoleID
	Name     string
	Selected bool
}

type MemberSettingsData struct {
	User         string // logged-in user, for nav
	IsAdmin      bool
	TargetUserID string
	IsSelf       bool
	DisplayName  string // admin-edit only
	Active       bool   // admin-edit only
	WorkingDays  uint8
	Roles        []RoleOption
}

type RoleDetailRow struct {
	Name     string
	Present  int
	Expected int
	Min      int
	Failing  bool
}

type DayDetailData struct {
	Date           string
	Present        int
	Expected       int
	NoOneScheduled bool
	PerRole        []RoleDetailRow
	Absences       []absence.Absence
}

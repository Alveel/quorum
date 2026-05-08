package view

import (
	"time"

	"github.com/alveel/quorum/internal/absence"
)

type DayCell struct {
	Date        time.Time
	Present     int
	Color       string // "green" | "yellow" | "orange" | "red"
	HasOverride bool
	IsWeekend   bool
	Blank       bool // padding before month starts
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
	TeamSize   int
}

type PageData struct {
	User       string
	IsAdmin    bool
	Heatmap    HeatmapData
	MyAbsences []absence.Absence
}

type AdminData struct {
	User     string
	Settings absence.Settings
	Absences []absence.Absence
}

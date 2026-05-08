package server

import (
	"testing"
	"time"

	"github.com/alveel/quorum/internal/absence"
	"github.com/alveel/quorum/internal/view"
)

func emptyPerDay() map[time.Time]int { return map[time.Time]int{} }

func defaultSettings() absence.Settings {
	return absence.Settings{TeamSize: 15, MinPresent: 8}
}

func countBlanks(days []view.DayCell) int {
	n := 0
	for _, d := range days {
		if d.Blank {
			n++
		}
	}
	return n
}

func countNonBlanks(days []view.DayCell) int {
	n := 0
	for _, d := range days {
		if !d.Blank {
			n++
		}
	}
	return n
}

func TestBuildHeatmap_AlwaysReturns12Months(t *testing.T) {
	h := buildHeatmap(2026, emptyPerDay(), defaultSettings())
	if len(h.Months) != 12 {
		t.Fatalf("want 12 months, got %d", len(h.Months))
	}
}

func TestBuildHeatmap_MetadataFields(t *testing.T) {
	h := buildHeatmap(2026, emptyPerDay(), absence.Settings{TeamSize: 12, MinPresent: 5})
	if h.Year != 2026 {
		t.Errorf("Year: want 2026, got %d", h.Year)
	}
	if h.PrevYear != 2025 {
		t.Errorf("PrevYear: want 2025, got %d", h.PrevYear)
	}
	if h.NextYear != 2027 {
		t.Errorf("NextYear: want 2027, got %d", h.NextYear)
	}
	if h.MinPresent != 5 {
		t.Errorf("MinPresent: want 5, got %d", h.MinPresent)
	}
	if h.TeamSize != 12 {
		t.Errorf("TeamSize: want 12, got %d", h.TeamSize)
	}
}

// 2024 Jan 1 = Monday → 0 blank cells before day 1.
func TestBuildHeatmap_BlankCellsMonday(t *testing.T) {
	h := buildHeatmap(2024, emptyPerDay(), defaultSettings())
	jan := h.Months[0]
	if jan.Days[0].Blank {
		t.Error("first cell should not be blank when month starts on Monday")
	}
	if jan.Days[0].Date.Day() != 1 {
		t.Errorf("first non-blank day: want day=1, got day=%d", jan.Days[0].Date.Day())
	}
	if countBlanks(jan.Days) != 0 {
		t.Errorf("want 0 leading blanks for Monday start, got %d", countBlanks(jan.Days))
	}
}

// 2026 Jan 1 = Thursday → 3 blank cells (Mon, Tue, Wed) before day 1.
func TestBuildHeatmap_BlankCellsThursday(t *testing.T) {
	h := buildHeatmap(2026, emptyPerDay(), defaultSettings())
	jan := h.Months[0]
	for i := 0; i < 3; i++ {
		if !jan.Days[i].Blank {
			t.Errorf("cell[%d] should be blank for Thursday start", i)
		}
	}
	if jan.Days[3].Blank {
		t.Error("cell[3] should be day 1, not blank")
	}
	if jan.Days[3].Date.Day() != 1 {
		t.Errorf("cell[3].Date.Day: want 1, got %d", jan.Days[3].Date.Day())
	}
}

// 2023 Jan 1 = Sunday → 6 blank cells (Mon–Sat) before day 1.
func TestBuildHeatmap_BlankCellsSunday(t *testing.T) {
	h := buildHeatmap(2023, emptyPerDay(), defaultSettings())
	jan := h.Months[0]
	for i := 0; i < 6; i++ {
		if !jan.Days[i].Blank {
			t.Errorf("cell[%d] should be blank for Sunday start", i)
		}
	}
	if jan.Days[6].Blank {
		t.Error("cell[6] should be day 1, not blank")
	}
}

// 2024 is a leap year → February has 29 days.
func TestBuildHeatmap_LeapDay(t *testing.T) {
	h := buildHeatmap(2024, emptyPerDay(), defaultSettings())
	feb := h.Months[1]
	nonBlanks := countNonBlanks(feb.Days)
	if nonBlanks != 29 {
		t.Errorf("2024 Feb: want 29 non-blank cells, got %d", nonBlanks)
	}
}

// In 2026, Jan 3 = Saturday, Jan 4 = Sunday → IsWeekend=true; Jan 5 Mon → false.
func TestBuildHeatmap_WeekendFlag(t *testing.T) {
	h := buildHeatmap(2026, emptyPerDay(), defaultSettings())
	jan := h.Months[0]
	for _, cell := range jan.Days {
		if cell.Blank {
			continue
		}
		day := cell.Date.Day()
		wd := cell.Date.Weekday()
		isWE := wd == time.Saturday || wd == time.Sunday
		if cell.IsWeekend != isWE {
			t.Errorf("Jan %d (%s): IsWeekend=%v, want %v", day, wd, cell.IsWeekend, isWE)
		}
	}
}

// 14 people have absence on Jan 5 → present=1 < min=8 → Color="red".
func TestBuildHeatmap_CellColorFromPerDay(t *testing.T) {
	jan5 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	perDay := map[time.Time]int{jan5: 14}
	h := buildHeatmap(2026, perDay, defaultSettings())
	jan := h.Months[0]
	for _, cell := range jan.Days {
		if !cell.Blank && cell.Date.Day() == 5 {
			if cell.Color != "red" {
				t.Errorf("Jan 5 (1 present): want Color=red, got %q", cell.Color)
			}
			if cell.Present != 1 {
				t.Errorf("Jan 5: want Present=1, got %d", cell.Present)
			}
			return
		}
	}
	t.Fatal("Jan 5 cell not found")
}

// Each month name should be the 3-char abbreviation.
func TestBuildHeatmap_MonthNames(t *testing.T) {
	want := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	h := buildHeatmap(2026, emptyPerDay(), defaultSettings())
	for i, m := range h.Months {
		if m.Name != want[i] {
			t.Errorf("month[%d]: want %q, got %q", i, want[i], m.Name)
		}
	}
}

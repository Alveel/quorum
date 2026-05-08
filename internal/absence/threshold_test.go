package absence

import (
	"testing"
	"time"
)

func day(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

func TestPresent_EmptyMap(t *testing.T) {
	p := Present(day(2026, 1, 5), map[time.Time]int{}, 15)
	if p != 15 {
		t.Fatalf("want 15, got %d", p)
	}
}

func TestPresent_SomeOnAbsence(t *testing.T) {
	m := map[time.Time]int{day(2026, 1, 5): 3}
	p := Present(day(2026, 1, 5), m, 15)
	if p != 12 {
		t.Fatalf("want 12, got %d", p)
	}
}

func TestColor_Boundaries(t *testing.T) {
	// team 15, min 8 → span 7
	cases := []struct {
		present int
		want    string
	}{
		{7, "red"},
		{8, "red"}, // ≤ min
		{9, "orange"},
		{13, "yellow"},
		{14, "green"},
		{15, "green"},
	}
	for _, c := range cases {
		got := Color(c.present, 15, 8)
		if got != c.want {
			t.Errorf("Color(%d, 15, 8) = %q, want %q", c.present, got, c.want)
		}
	}
}

func TestCheckRequest_DeniesWhenBelowMin(t *testing.T) {
	// 14 people already have absence on the 5th → 1 present
	onVac := map[time.Time]int{day(2026, 7, 7): 14}
	offending := CheckRequest(day(2026, 7, 7), day(2026, 7, 7), onVac, 15, 8, true)
	if len(offending) == 0 {
		t.Fatal("expected denial, got none")
	}
}

func TestCheckRequest_AllowsWhenAboveMin(t *testing.T) {
	// 6 have absence → 9 present; requester would bring to 8 = exactly min → allowed (< min means deny)
	onVac := map[time.Time]int{day(2026, 7, 7): 6}
	offending := CheckRequest(day(2026, 7, 7), day(2026, 7, 7), onVac, 15, 8, true)
	if len(offending) != 0 {
		t.Fatalf("expected no denial, got %v", offending)
	}
}

func TestCheckRequest_SkipsWeekends(t *testing.T) {
	// Saturday 2026-07-04: fill all slots
	onVac := map[time.Time]int{day(2026, 7, 4): 14}
	offending := CheckRequest(day(2026, 7, 4), day(2026, 7, 4), onVac, 15, 8, false)
	if len(offending) != 0 {
		t.Fatalf("weekend should be skipped, got %v", offending)
	}
}

func TestCheckRequest_MultiDayRange(t *testing.T) {
	onVac := map[time.Time]int{
		day(2026, 7, 6): 2,  // Mon: 13 present − 1 = 12 > 8 ✓
		day(2026, 7, 7): 14, // Tue: 1 present − 1 = 0 < 8 ✗
		day(2026, 7, 8): 3,  // Wed: 12 present − 1 = 11 > 8 ✓
	}
	offending := CheckRequest(day(2026, 7, 6), day(2026, 7, 8), onVac, 15, 8, true)
	if len(offending) != 1 || offending[0] != day(2026, 7, 7) {
		t.Fatalf("expected only 7 Jul offending, got %v", offending)
	}
}

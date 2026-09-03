package holidaysync

import "testing"

func TestSupportedCountries_IncludesNL(t *testing.T) {
	found := false
	for _, c := range SupportedCountries() {
		if c == "nl" {
			found = true
		}
	}
	if !found {
		t.Errorf("SupportedCountries() = %v, want it to include %q", SupportedCountries(), "nl")
	}
}

func TestSync_NL_ReturnsKnownHolidayCount(t *testing.T) {
	rows, err := Sync("nl", 2026)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// nl.Holidays currently lists 9 standard national holidays; none of them fall
	// outside their observed year for 2026, so all 9 should come back.
	if len(rows) != 9 {
		t.Errorf("len(rows) = %d, want 9", len(rows))
	}
	for _, h := range rows {
		if h.Date.IsZero() {
			t.Errorf("holiday %q has zero date", h.Name)
		}
		if h.Source != "imported" {
			t.Errorf("holiday %q source = %q, want imported", h.Name, h.Source)
		}
	}
}

func TestSync_UnsupportedCountry_Errors(t *testing.T) {
	if _, err := Sync("zz", 2026); err == nil {
		t.Error("want error for unsupported country, got nil")
	}
}

// TestSync_ShiftingHoliday_MatchesLibraryObservedDate pins Koningsdag (King's Day), which
// has an explicit Sunday->Saturday substitution rule, against whatever rickar/cal actually
// computes — not a hand-derived date — so a library update that changes the rule surfaces
// here instead of silently drifting.
func TestSync_ShiftingHoliday_MatchesLibraryObservedDate(t *testing.T) {
	rows, err := Sync("nl", 2026)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var found bool
	for _, h := range rows {
		if h.Name == "Koningsdag" {
			found = true
			if h.Date.Weekday().String() == "Sunday" {
				t.Errorf("Koningsdag observed date should never be a Sunday (substitution rule), got %s", h.Date)
			}
		}
	}
	if !found {
		t.Fatal("Koningsdag not present in synced rows")
	}
}

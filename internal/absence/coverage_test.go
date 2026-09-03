package absence

import (
	"testing"
	"time"
)

func day(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

const monFri = 62   // Mon..Fri
const allWeek = 127 // Sun..Sat

func absOn(userID string, d time.Time, status Status) Absence {
	return Absence{UserID: userID, StartDate: d, EndDate: d, Status: status}
}

func TestCoverage_ColorBoundaries_MatchOldSemantics(t *testing.T) {
	// 9 members, min=1, span=8. Boundaries: red <=1, orange <50%, yellow >=50%<75%, green >=75%.
	cases := []struct {
		present int
		want    string
	}{
		{1, "red"},
		{3, "orange"}, // (3-1)/8 = 0.25
		{5, "yellow"}, // (5-1)/8 = 0.5
		{7, "green"},  // (7-1)/8 = 0.75
		{9, "green"},  // all present
	}
	d := day(2026, 1, 5) // Monday
	for _, c := range cases {
		roster := make([]Member, 9)
		var absences []Absence
		for i := range roster {
			roster[i] = Member{ID: string(rune('a' + i)), Active: true, WorkingDays: allWeek}
			if i >= c.present {
				absences = append(absences, absOn(roster[i].ID, d, StatusApproved))
			}
		}
		cov := Coverage(roster, absences, nil, nil, 1, d, d)
		dc := cov[d]
		if dc.Color != c.want {
			t.Errorf("present=%d: Color=%q, want %q", c.present, dc.Color, c.want)
		}
		if dc.Present != c.present {
			t.Errorf("present=%d: dc.Present=%d, want %d", c.present, dc.Present, c.present)
		}
	}
}

func TestCoverage_MultiRole_IndependentQuotas(t *testing.T) {
	d := day(2026, 1, 5) // Monday
	roleA, roleB := RoleID(1), RoleID(2)
	roster := []Member{
		{ID: "a1", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleA}},
		{ID: "a2", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleA}},
		{ID: "b1", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleB}},
		{ID: "b2", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleB}},
	}
	roles := []Role{{ID: roleA, Name: "A", MinPresent: 1}, {ID: roleB, Name: "B", MinPresent: 1}}
	absences := []Absence{absOn("a1", d, StatusApproved)} // roleA down to 1/2, hits its min

	cov := Coverage(roster, absences, nil, roles, 1, d, d)
	dc := cov[d]

	if dc.PerRole[roleA].Present != 1 || dc.PerRole[roleA].Expected != 2 {
		t.Errorf("roleA coverage = %+v, want present=1 expected=2", dc.PerRole[roleA])
	}
	if dc.PerRole[roleB].Present != 2 || dc.PerRole[roleB].Expected != 2 {
		t.Errorf("roleB coverage = %+v, want present=2 expected=2", dc.PerRole[roleB])
	}
	if dc.Color != "red" {
		t.Errorf("Color = %q, want red (worst-of roleA)", dc.Color)
	}
	foundA, foundB := false, false
	for _, rid := range dc.Failing {
		if rid == roleA {
			foundA = true
		}
		if rid == roleB {
			foundB = true
		}
	}
	if !foundA {
		t.Error("Failing should include roleA")
	}
	if foundB {
		t.Error("Failing should not include roleB")
	}
}

func TestCoverage_MemberWithMultipleRoles_CountsInBoth(t *testing.T) {
	d := day(2026, 1, 5)
	roleA, roleB := RoleID(1), RoleID(2)
	roster := []Member{
		{ID: "dual", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleA, roleB}},
	}
	roles := []Role{{ID: roleA, Name: "A", MinPresent: 0}, {ID: roleB, Name: "B", MinPresent: 0}}
	absences := []Absence{absOn("dual", d, StatusApproved)}

	cov := Coverage(roster, absences, nil, roles, 0, d, d)
	dc := cov[d]

	if dc.PerRole[roleA].Expected != 1 || dc.PerRole[roleA].Present != 0 {
		t.Errorf("roleA = %+v, want expected=1 present=0", dc.PerRole[roleA])
	}
	if dc.PerRole[roleB].Expected != 1 || dc.PerRole[roleB].Present != 0 {
		t.Errorf("roleB = %+v, want expected=1 present=0", dc.PerRole[roleB])
	}
}

func TestCoverage_Holiday_NoOneScheduled_NotRed(t *testing.T) {
	d := day(2026, 1, 1)
	roster := []Member{{ID: "a", Active: true, WorkingDays: monFri}}
	holidays := map[time.Time]bool{d: true}

	cov := Coverage(roster, nil, holidays, nil, 100, d, d) // absurdly high min to prove it's not consulted
	dc := cov[d]

	if !dc.NoOneScheduled {
		t.Error("want NoOneScheduled=true on a holiday")
	}
	if dc.Color != "none" {
		t.Errorf("Color = %q, want %q", dc.Color, "none")
	}
	if dc.Color == "red" {
		t.Error("holiday must never render red")
	}
}

func TestCoverage_WeekendPattern_ExcludesNonWorkingMembers(t *testing.T) {
	sat := day(2026, 1, 3) // Saturday
	roster := []Member{
		{ID: "weekday", Active: true, WorkingDays: monFri},
		{ID: "everyday", Active: true, WorkingDays: allWeek},
	}
	cov := Coverage(roster, nil, nil, nil, 0, sat, sat)
	dc := cov[sat]
	if dc.Expected != 1 {
		t.Errorf("Expected = %d, want 1 (only the everyday member works Saturday)", dc.Expected)
	}
}

func TestCoverage_InactiveMemberExcluded(t *testing.T) {
	d := day(2026, 1, 5)
	roster := []Member{
		{ID: "gone", Active: false, WorkingDays: allWeek},
		{ID: "here", Active: true, WorkingDays: allWeek},
	}
	cov := Coverage(roster, nil, nil, nil, 0, d, d)
	dc := cov[d]
	if dc.Expected != 1 {
		t.Errorf("Expected = %d, want 1 (inactive member excluded)", dc.Expected)
	}
}

func TestCoverage_AbsentMemberReducesPresentNotExpected(t *testing.T) {
	d := day(2026, 1, 5)
	roster := []Member{
		{ID: "a", Active: true, WorkingDays: monFri},
		{ID: "b", Active: true, WorkingDays: monFri},
	}
	absences := []Absence{absOn("a", d, StatusApproved)}
	cov := Coverage(roster, absences, nil, nil, 0, d, d)
	dc := cov[d]
	if dc.Expected != 2 {
		t.Errorf("Expected = %d, want 2 (absence doesn't change who's scheduled)", dc.Expected)
	}
	if dc.Present != 1 {
		t.Errorf("Present = %d, want 1", dc.Present)
	}
}

func TestCoverage_HasOverride_SetFromOverriddenAbsence(t *testing.T) {
	approvedDay := day(2026, 1, 5)
	overriddenDay := day(2026, 1, 6)
	roster := []Member{{ID: "a", Active: true, WorkingDays: allWeek}, {ID: "b", Active: true, WorkingDays: allWeek}}
	absences := []Absence{
		absOn("a", approvedDay, StatusApproved),
		absOn("a", overriddenDay, StatusOverridden),
	}
	cov := Coverage(roster, absences, nil, nil, 0, approvedDay, overriddenDay)
	if cov[approvedDay].HasOverride {
		t.Error("approved-only day should not have HasOverride set")
	}
	if !cov[overriddenDay].HasOverride {
		t.Error("day with an overridden absence should have HasOverride set")
	}
}

func TestOffendingDays_ScopedToGlobalAndRequesterOwnRoles(t *testing.T) {
	d := day(2026, 1, 5)
	roleA, roleB := RoleID(1), RoleID(2)
	requester := Member{ID: "req", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleA}}
	roster := []Member{
		requester,
		{ID: "b1", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleB}},
		{ID: "b2", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleB}},
	}
	roles := []Role{{ID: roleA, Name: "A", MinPresent: 0}, {ID: roleB, Name: "B", MinPresent: 5}} // roleB always short
	cov := Coverage(roster, nil, nil, roles, 0, d, d)

	if len(cov[d].Failing) == 0 {
		t.Fatal("test setup broken: roleB should be Failing that day")
	}
	offending := OffendingDays(cov, requester, d, d)
	if len(offending) != 0 {
		t.Errorf("OffendingDays = %v, want none (requester doesn't hold the short-staffed role)", offending)
	}
}

func TestOffendingDays_ExcludesNonWorkingDays(t *testing.T) {
	sat := day(2026, 1, 3) // Saturday
	requester := Member{ID: "req", Active: true, WorkingDays: monFri}
	roster := []Member{
		requester,
		{ID: "sat-worker", Active: true, WorkingDays: allWeek},
	}
	// sat-worker absent on the one day they're scheduled -> global present=0 < min -> red.
	absences := []Absence{absOn("sat-worker", sat, StatusApproved)}
	cov := Coverage(roster, absences, nil, nil, 5, sat, sat)
	if cov[sat].Color != "red" {
		t.Fatal("test setup broken: Saturday should be red")
	}
	offending := OffendingDays(cov, requester, sat, sat)
	if len(offending) != 0 {
		t.Errorf("OffendingDays = %v, want none (requester's pattern excludes Saturday)", offending)
	}
}

func TestOffendingDays_ExcludesHolidays(t *testing.T) {
	d := day(2026, 1, 1)
	requester := Member{ID: "req", Active: true, WorkingDays: allWeek}
	holidays := map[time.Time]bool{d: true}
	cov := Coverage([]Member{requester}, nil, holidays, nil, 0, d, d)
	offending := OffendingDays(cov, requester, d, d)
	if len(offending) != 0 {
		t.Errorf("OffendingDays = %v, want none (holiday)", offending)
	}
}

func TestFailingVsOffending_SameDay_DifferentThresholds(t *testing.T) {
	d := day(2026, 1, 5)
	requester := Member{ID: "req", Active: true, WorkingDays: monFri}
	roster := []Member{
		requester,
		{ID: "b", Active: true, WorkingDays: monFri},
		{ID: "c", Active: true, WorkingDays: monFri},
		{ID: "d", Active: true, WorkingDays: monFri},
		{ID: "e", Active: true, WorkingDays: monFri},
	}
	// 3 absent -> present=2, min=2: exactly at the boundary.
	absences := []Absence{
		absOn("c", d, StatusApproved),
		absOn("d", d, StatusApproved),
		absOn("e", d, StatusApproved),
	}
	cov := Coverage(roster, absences, nil, nil, 2, d, d)
	dc := cov[d]
	if dc.Present != 2 {
		t.Fatalf("test setup broken: Present=%d, want 2", dc.Present)
	}
	if dc.Color != "red" {
		t.Errorf("Color = %q, want red at exactly Min (<=)", dc.Color)
	}
	offending := OffendingDays(cov, requester, d, d)
	if len(offending) != 0 {
		t.Errorf("OffendingDays = %v, want none — exactly Min is approvable (<)", offending)
	}
}

func TestFeasibilityWarnings_GatedOnExpectedGreaterThanZero(t *testing.T) {
	weekday := day(2026, 1, 5) // Monday
	sat := day(2026, 1, 3)     // Saturday
	roleC := RoleID(3)
	roster := []Member{{ID: "solo", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleC}}}
	roles := []Role{{ID: roleC, Name: "C", MinPresent: 2}}

	cov := Coverage(roster, nil, nil, roles, 0, sat, weekday)
	warnings := FeasibilityWarnings(cov)

	for _, warnDay := range warnings[roleC] {
		if warnDay.Equal(sat) {
			t.Error("Saturday should not be warned about — role C has zero scheduled holders that day")
		}
	}
	found := false
	for _, warnDay := range warnings[roleC] {
		if warnDay.Equal(weekday) {
			found = true
		}
	}
	if !found {
		t.Error("Monday should be warned about — role C is short (1 scheduled < min 2)")
	}
}

func TestFeasibilityWarnings_FlagsStructuralShortage(t *testing.T) {
	d := day(2026, 1, 5)
	roleC := RoleID(3)
	roster := []Member{
		{ID: "m1", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleC}},
		{ID: "m2", Active: true, WorkingDays: monFri, RoleIDs: []RoleID{roleC}},
	}
	roles := []Role{{ID: roleC, Name: "C", MinPresent: 3}}
	cov := Coverage(roster, nil, nil, roles, 0, d, d)
	warnings := FeasibilityWarnings(cov)
	if len(warnings[roleC]) != 1 {
		t.Errorf("want 1 warning day for role C, got %d", len(warnings[roleC]))
	}
}

func TestMember_WorksOn_BitmaskDefault62IsMonFri(t *testing.T) {
	m := Member{WorkingDays: 62}
	want := map[time.Weekday]bool{
		time.Sunday: false, time.Monday: true, time.Tuesday: true, time.Wednesday: true,
		time.Thursday: true, time.Friday: true, time.Saturday: false,
	}
	// Jan 2026: 1=Thu .. pick a full week Jan 4 (Sun) .. Jan 10 (Sat).
	for i := 0; i < 7; i++ {
		d := day(2026, 1, 4+i)
		if got := m.WorksOn(d); got != want[d.Weekday()] {
			t.Errorf("WorksOn(%s): got %v, want %v", d.Weekday(), got, want[d.Weekday()])
		}
	}
}

func TestHolidaySet_ConvertsListToMap(t *testing.T) {
	d := day(2026, 1, 1)
	set := HolidaySet([]Holiday{{Date: d, Name: "New Year", Source: HolidaySourceImported}})
	if !set[d] {
		t.Error("holiday date should be in the set")
	}
	if set[day(2026, 1, 2)] {
		t.Error("unrelated date should not be in the set")
	}
}

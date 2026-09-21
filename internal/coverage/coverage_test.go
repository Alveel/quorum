package coverage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alveel/quorum/internal/absence"
)

const monFri = 62 // bits 1..5 of time.Weekday

var wed = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

type fakeStore struct {
	settings absence.Settings
	roster   []absence.Member
	roles    []absence.Role
	holidays []absence.Holiday
	absences []absence.Absence
	err      error

	gotFrom, gotTo time.Time
}

func (f *fakeStore) GetSettings(context.Context) (absence.Settings, error) {
	return f.settings, f.err
}
func (f *fakeStore) ListRoster(context.Context) ([]absence.Member, error) { return f.roster, f.err }
func (f *fakeStore) ListRoles(context.Context) ([]absence.Role, error)    { return f.roles, f.err }
func (f *fakeStore) ListHolidays(context.Context) ([]absence.Holiday, error) {
	return f.holidays, f.err
}
func (f *fakeStore) ListAbsencesInRange(_ context.Context, from, to time.Time) ([]absence.Absence, error) {
	f.gotFrom, f.gotTo = from, to
	return f.absences, f.err
}

func twoMemberStore() *fakeStore {
	return &fakeStore{
		settings: absence.Settings{MinPresent: 1},
		roster: []absence.Member{
			{ID: "a", Active: true, WorkingDays: monFri},
			{ID: "b", Active: true, WorkingDays: monFri},
		},
	}
}

func TestForRange_CountsStoredAbsences(t *testing.T) {
	st := twoMemberStore()
	st.absences = []absence.Absence{
		{UserID: "a", StartDate: wed, EndDate: wed, Status: absence.StatusApproved},
	}

	snap, err := New(st).ForRange(context.Background(), wed, wed)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Days[wed]; got.Present != 1 || got.Expected != 2 {
		t.Errorf("present/expected = %d/%d, want 1/2", got.Present, got.Expected)
	}
	if st.gotFrom != wed || st.gotTo != wed {
		t.Errorf("store queried [%v,%v], want [%v,%v]", st.gotFrom, st.gotTo, wed, wed)
	}
}

// An extra absence is counted but not listed: it hasn't been written yet, so nothing
// should render it as though it had.
func TestForRange_CountsExtraButDoesNotListIt(t *testing.T) {
	st := twoMemberStore()
	candidate := absence.Absence{UserID: "a", StartDate: wed, EndDate: wed, Status: absence.StatusApproved}

	snap, err := New(st).ForRange(context.Background(), wed, wed, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Days[wed].Present; got != 1 {
		t.Errorf("present = %d, want 1 (extra absence not counted)", got)
	}
	if len(snap.Absences) != 0 {
		t.Errorf("Absences = %v, want empty (extra is not a stored row)", snap.Absences)
	}
}

// Passing extra must not write through to the caller's stored-absence slice.
func TestForRange_ExtraDoesNotMutateStoredSlice(t *testing.T) {
	st := twoMemberStore()
	st.absences = make([]absence.Absence, 1, 4) // spare capacity: a naive append would scribble
	st.absences[0] = absence.Absence{UserID: "a", StartDate: wed, EndDate: wed, Status: absence.StatusApproved}

	_, err := New(st).ForRange(context.Background(), wed, wed,
		absence.Absence{UserID: "b", StartDate: wed, EndDate: wed, Status: absence.StatusApproved})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.absences) != 1 || st.absences[0].UserID != "a" {
		t.Errorf("stored slice was mutated: %v", st.absences)
	}
}

func TestForRange_StoreErrorIsWrapped(t *testing.T) {
	st := twoMemberStore()
	st.err = errors.New("db down")

	if _, err := New(st).ForRange(context.Background(), wed, wed); err == nil {
		t.Fatal("want error, got nil")
	} else if !errors.Is(err, st.err) {
		t.Errorf("error does not wrap the store error: %v", err)
	}
}

func TestSnapshotMember(t *testing.T) {
	snap := Snapshot{Roster: []absence.Member{
		{ID: "a", Active: true, WorkingDays: monFri, RoleIDs: []absence.RoleID{1}},
	}}

	if got := snap.Member("a"); got.ID != "a" || !got.Active {
		t.Errorf("Member(a) = %+v, want the active roster entry", got)
	}
	// An unknown id must read as unconfigured, not as a member in good standing.
	if got := snap.Member("nobody"); got.Active || len(got.RoleIDs) != 0 {
		t.Errorf("Member(nobody) = %+v, want the zero Member", got)
	}
}

package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/absence"
)

type fakeStore struct {
	settings           absence.Settings
	settingsErr        error
	absencesInRange    []absence.Absence
	absencesInRangeErr error
	onDay              []absence.Absence
	onDayErr           error
	myAbsences         []absence.Absence
	myVacErr           error
	allActive          []absence.Absence
	allActiveErr       error
	createVac          absence.Absence
	createVacErr       error
	createOvr          absence.Absence
	createOvrErr       error
	cancelErr          error
	updateSettingErr   error
	upsertErr          error
	upsertCalled       bool
	upsertID           string
	upsertEmail        string
	hasOverlap         bool
	hasOverlapErr      error

	roster                          []absence.Member
	rosterErr                       error
	member                          absence.Member
	memberErr                       error
	upsertRosterErr                 error
	updateMemberSettingsErr         error
	updateMemberSettingsUserID      string
	updateMemberSettingsWorkingDays uint8
	updateMemberSettingsRoleIDs     []absence.RoleID
	updateMemberSettingsActorID     string

	roles         []absence.Role
	rolesErr      error
	createRole    absence.Role
	createRoleErr error
	updateRoleErr error
	deleteRoleErr error

	holidays          []absence.Holiday
	holidaysErr       error
	createHoliday     absence.Holiday
	createHolidayErr  error
	deleteHolidayErr  error
	importedCount     int
	importHolidaysErr error
}

func (f *fakeStore) GetSettings(_ context.Context) (absence.Settings, error) {
	return f.settings, f.settingsErr
}

func (f *fakeStore) UpdateSetting(_ context.Context, _ string, _ any, _ string) error {
	return f.updateSettingErr
}

func (f *fakeStore) ListAbsencesInRange(_ context.Context, _, _ time.Time) ([]absence.Absence, error) {
	return f.absencesInRange, f.absencesInRangeErr
}

func (f *fakeStore) AbsenceOnDay(_ context.Context, _ time.Time) ([]absence.Absence, error) {
	return f.onDay, f.onDayErr
}

func (f *fakeStore) ListMyAbsences(_ context.Context, _ string) ([]absence.Absence, error) {
	return f.myAbsences, f.myVacErr
}

func (f *fakeStore) ListAllActive(_ context.Context) ([]absence.Absence, error) {
	return f.allActive, f.allActiveErr
}

func (f *fakeStore) CreateAbsence(_ context.Context, _, _, _ string, _, _ time.Time) (absence.Absence, error) {
	return f.createVac, f.createVacErr
}

func (f *fakeStore) CreateOverride(_ context.Context, _, _, _ string, _, _ time.Time, _ string) (absence.Absence, error) {
	return f.createOvr, f.createOvrErr
}

func (f *fakeStore) CancelAbsence(_ context.Context, _ uuid.UUID, _ string) error {
	return f.cancelErr
}

func (f *fakeStore) HasOverlap(_ context.Context, _ string, _, _ time.Time) (bool, error) {
	return f.hasOverlap, f.hasOverlapErr
}

func (f *fakeStore) UpsertUser(_ context.Context, id, email, _ string) error {
	f.upsertCalled = true
	f.upsertID = id
	f.upsertEmail = email
	return f.upsertErr
}

func (f *fakeStore) GetMember(_ context.Context, _ string) (absence.Member, error) {
	return f.member, f.memberErr
}

func (f *fakeStore) ListRoster(_ context.Context) ([]absence.Member, error) {
	return f.roster, f.rosterErr
}

func (f *fakeStore) UpsertRosterMember(_ context.Context, _, _ string, _ bool, _ string) error {
	return f.upsertRosterErr
}

func (f *fakeStore) UpdateMemberSettings(_ context.Context, userID string, wd uint8, roleIDs []absence.RoleID, actorID string) error {
	f.updateMemberSettingsUserID = userID
	f.updateMemberSettingsWorkingDays = wd
	f.updateMemberSettingsRoleIDs = roleIDs
	f.updateMemberSettingsActorID = actorID
	return f.updateMemberSettingsErr
}

func (f *fakeStore) ListRoles(_ context.Context) ([]absence.Role, error) {
	return f.roles, f.rolesErr
}

func (f *fakeStore) CreateRole(_ context.Context, _ string, _ int, _ string) (absence.Role, error) {
	return f.createRole, f.createRoleErr
}

func (f *fakeStore) UpdateRole(_ context.Context, _ absence.RoleID, _ string, _ int, _ string) error {
	return f.updateRoleErr
}

func (f *fakeStore) DeleteRole(_ context.Context, _ absence.RoleID, _ string) error {
	return f.deleteRoleErr
}

func (f *fakeStore) ListHolidays(_ context.Context) ([]absence.Holiday, error) {
	return f.holidays, f.holidaysErr
}

func (f *fakeStore) CreateManualHoliday(_ context.Context, _ time.Time, _, _ string) (absence.Holiday, error) {
	return f.createHoliday, f.createHolidayErr
}

func (f *fakeStore) DeleteHoliday(_ context.Context, _ time.Time, _ string) error {
	return f.deleteHolidayErr
}

func (f *fakeStore) ImportHolidays(_ context.Context, _ []absence.Holiday, _ string) (int, error) {
	return f.importedCount, f.importHolidaysErr
}

// --- roster fixtures shared across tests ---

// testUserMember is the dev-auth-bypass identity (see newTestServer), active, Mon-Fri,
// holding role id 1.
func testUserMember() absence.Member {
	return absence.Member{ID: "testuser", Active: true, WorkingDays: 62, RoleIDs: []absence.RoleID{1}}
}

// fifteenMemberRoster returns 15 active Mon-Fri members with no roles — for tests that
// don't care about role gating.
func fifteenMemberRoster() []absence.Member {
	roster := make([]absence.Member, 15)
	for i := range roster {
		roster[i] = absence.Member{ID: fmt.Sprintf("user%d", i), Active: true, WorkingDays: 62}
	}
	return roster
}

// oneRoleRoster is fifteenMemberRoster() with the first slot replaced by testUserMember(),
// so the dev-bypass caller is both roster member and gate-passing requester.
func oneRoleRoster() []absence.Member {
	roster := fifteenMemberRoster()
	roster[0] = testUserMember()
	return roster
}

// oneRole is the role definition matching testUserMember()'s RoleIDs.
func oneRole() []absence.Role {
	return []absence.Role{{ID: 1, Name: "role1", MinPresent: 0}}
}

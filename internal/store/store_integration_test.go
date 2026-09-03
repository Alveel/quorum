//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/absence"
)

func day(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

func insertUser(t *testing.T, st *Store, id, email string) {
	t.Helper()
	if err := st.UpsertUser(context.Background(), id, email, id); err != nil {
		t.Fatalf("insertUser: %v", err)
	}
}

// --- UpsertUser ---

func TestUpsertUser_Insert(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.UpsertUser(ctx, "alice", "alice@example.com", "Alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var email string
	err := testPool.QueryRow(ctx, `SELECT email FROM users WHERE id = 'alice'`).Scan(&email)
	if err != nil {
		t.Fatalf("query after upsert: %v", err)
	}
	if email != "alice@example.com" {
		t.Errorf("email: want alice@example.com, got %q", email)
	}
}

func TestUpsertUser_UpdateDoesNotDuplicate(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	st.UpsertUser(ctx, "alice", "old@example.com", "Alice")    //nolint:errcheck
	st.UpsertUser(ctx, "alice", "new@example.com", "Alice V2") //nolint:errcheck

	var count int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE id = 'alice'`).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("want 1 row, got %d", count)
	}

	var email string
	testPool.QueryRow(ctx, `SELECT email FROM users WHERE id = 'alice'`).Scan(&email) //nolint:errcheck
	if email != "new@example.com" {
		t.Errorf("email not updated: want new@example.com, got %q", email)
	}
}

// TestUpsertUser_NeverOverwritesDisplayNameOnUpdate is the regression test for the bug
// where every login clobbered an admin-entered roster display name back to whatever the
// auth headers happened to pass (today, always the user id).
func TestUpsertUser_NeverOverwritesDisplayNameOnUpdate(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.UpsertUser(ctx, "alice", "alice@example.com", "Alice Admin-Set Name"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Simulate a subsequent login passing a different displayName argument.
	if err := st.UpsertUser(ctx, "alice", "alice@example.com", "alice"); err != nil {
		t.Fatalf("update: %v", err)
	}

	var displayName string
	testPool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = 'alice'`).Scan(&displayName) //nolint:errcheck
	if displayName != "Alice Admin-Set Name" {
		t.Errorf("display_name = %q, want it unchanged by the update call", displayName)
	}
}

// --- GetSettings / UpdateSetting ---

func TestGetSettings_ReturnsMinPresentAndHolidayCountry(t *testing.T) {
	st := testStore(t)
	s, err := st.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.MinPresent != 8 {
		t.Errorf("MinPresent: want 8, got %d", s.MinPresent)
	}
	if s.HolidayCountry != "nl" {
		t.Errorf("HolidayCountry: want nl, got %q", s.HolidayCountry)
	}
}

func TestUpdateSetting_PersistsAndAudits(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.UpdateSetting(ctx, "holiday_country", "be", "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, _ := st.GetSettings(ctx)
	if s.HolidayCountry != "be" {
		t.Errorf("HolidayCountry after update: want be, got %q", s.HolidayCountry)
	}

	var action, targetID string
	err := testPool.QueryRow(ctx, `
		SELECT action, target_id FROM audit_log WHERE actor_id = 'admin' ORDER BY at DESC LIMIT 1
	`).Scan(&action, &targetID)
	if err != nil {
		t.Fatalf("audit_log query: %v", err)
	}
	if action != "update_setting" {
		t.Errorf("audit action: want update_setting, got %q", action)
	}
	if targetID != "holiday_country" {
		t.Errorf("audit target_id: want holiday_country, got %q", targetID)
	}
}

// --- ListAbsencesInRange ---

func TestListAbsencesInRange_NoAbsences_Empty(t *testing.T) {
	st := testStore(t)
	rows, err := st.ListAbsencesInRange(context.Background(), day(2026, 7, 1), day(2026, 7, 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("want 0 rows, got %d", len(rows))
	}
}

func TestListAbsencesInRange_SingleAbsence_ReturnsRowWithUserName(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")
	st.UpsertRosterMember(ctx, "alice", "Alice", true, "admin")                   //nolint:errcheck
	st.CreateAbsence(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 5)) //nolint:errcheck

	rows, err := st.ListAbsencesInRange(ctx, day(2026, 6, 29), day(2026, 7, 7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].UserName != "Alice" {
		t.Errorf("UserName: want Alice, got %q", rows[0].UserName)
	}
}

func TestListAbsencesInRange_CancelledExcluded(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, _ := st.CreateAbsence(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))
	st.CancelAbsence(ctx, v.ID, "alice") //nolint:errcheck

	rows, _ := st.ListAbsencesInRange(ctx, day(2026, 7, 1), day(2026, 7, 1))
	if len(rows) != 0 {
		t.Errorf("cancelled absence should not be returned, got %d rows", len(rows))
	}
}

func TestListAbsencesInRange_OverriddenIncluded(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	st.CreateOverride(ctx, "alice", "admin", "", day(2026, 7, 1), day(2026, 7, 1), "reason") //nolint:errcheck

	rows, _ := st.ListAbsencesInRange(ctx, day(2026, 7, 1), day(2026, 7, 1))
	if len(rows) != 1 {
		t.Errorf("overridden absence should be returned, got %d rows", len(rows))
	}
}

func TestListAbsencesInRange_OutsideRangeExcluded(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")
	st.CreateAbsence(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 5)) //nolint:errcheck

	rows, _ := st.ListAbsencesInRange(ctx, day(2026, 8, 1), day(2026, 8, 5))
	if len(rows) != 0 {
		t.Errorf("absence outside range should be excluded, got %d rows", len(rows))
	}
}

// --- CreateAbsence ---

func TestCreateAbsence_ReturnsAbsenceWithID(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, err := st.CreateAbsence(ctx, "alice", "alice", "holiday", day(2026, 7, 1), day(2026, 7, 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Error("ID should be non-nil UUID")
	}
	if !v.StartDate.Equal(day(2026, 7, 1)) {
		t.Errorf("StartDate: want 2026-07-01, got %s", v.StartDate)
	}
}

func TestCreateAbsence_WritesAuditLog(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, _ := st.CreateAbsence(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))

	var action, targetID string
	err := testPool.QueryRow(ctx, `
		SELECT action, target_id FROM audit_log WHERE actor_id = 'alice' ORDER BY at DESC LIMIT 1
	`).Scan(&action, &targetID)
	if err != nil {
		t.Fatalf("audit_log query: %v", err)
	}
	if action != "create_absence" {
		t.Errorf("action: want create_absence, got %q", action)
	}
	if targetID != v.ID.String() {
		t.Errorf("target_id: want %s, got %q", v.ID, targetID)
	}
}

// --- CancelAbsence ---

func TestCancelAbsence_OwnAbsence_Succeeds(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, _ := st.CreateAbsence(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))
	if err := st.CancelAbsence(ctx, v.ID, "alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status string
	testPool.QueryRow(ctx, `SELECT status FROM absence WHERE id = $1`, v.ID).Scan(&status) //nolint:errcheck
	if status != "cancelled" {
		t.Errorf("status: want cancelled, got %q", status)
	}
}

func TestCancelAbsence_OtherUsersAbsence_Fails(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")
	insertUser(t, st, "bob", "bob@example.com")

	v, _ := st.CreateAbsence(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))
	err := st.CancelAbsence(ctx, v.ID, "bob")
	if err == nil {
		t.Fatal("expected error when bob tries to cancel alice's absence")
	}

	var status string
	testPool.QueryRow(ctx, `SELECT status FROM absence WHERE id = $1`, v.ID).Scan(&status) //nolint:errcheck
	if status != "approved" {
		t.Errorf("status should still be approved, got %q", status)
	}
}

func TestCancelAbsence_NonexistentID_Fails(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	fakeID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
	err := st.CancelAbsence(ctx, fakeID, "alice")
	if err == nil {
		t.Fatal("expected error for nonexistent absence")
	}
}

// --- roster ---

func TestListRoster_MemberWithNoRoles_ReturnsEmptySliceNotNil(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	roster, err := st.ListRoster(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roster) != 1 {
		t.Fatalf("want 1 roster member, got %d", len(roster))
	}
	if roster[0].RoleIDs == nil {
		t.Error("RoleIDs should be an empty slice, not nil, for a member with no roles")
	}
	if len(roster[0].RoleIDs) != 0 {
		t.Errorf("want 0 roles, got %d", len(roster[0].RoleIDs))
	}
	if !roster[0].Active {
		t.Error("new user should default to Active=true")
	}
	if roster[0].WorkingDays != 62 {
		t.Errorf("WorkingDays: want default 62, got %d", roster[0].WorkingDays)
	}
}

func TestGetMember_ReturnsCurrentRoleIDs(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")
	role, err := st.CreateRole(ctx, "DBA", 1, "admin")
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := st.UpdateMemberSettings(ctx, "alice", 31, []absence.RoleID{role.ID}, "alice"); err != nil {
		t.Fatalf("UpdateMemberSettings: %v", err)
	}

	m, err := st.GetMember(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.WorkingDays != 31 {
		t.Errorf("WorkingDays: want 31, got %d", m.WorkingDays)
	}
	if len(m.RoleIDs) != 1 || m.RoleIDs[0] != role.ID {
		t.Errorf("RoleIDs: want [%d], got %v", role.ID, m.RoleIDs)
	}
}

func TestUpsertRosterMember_CreateThenEdit_PreservesWorkingDays(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.UpsertRosterMember(ctx, "alice", "Alice", true, "admin"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.UpdateMemberSettings(ctx, "alice", 15, nil, "alice"); err != nil {
		t.Fatalf("set pattern: %v", err)
	}
	// Admin edits display name / active only — must not reset the pattern back to default.
	if err := st.UpsertRosterMember(ctx, "alice", "Alice Renamed", false, "admin"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	m, err := st.GetMember(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "Alice Renamed" {
		t.Errorf("Name: want %q, got %q", "Alice Renamed", m.Name)
	}
	if m.Active {
		t.Error("Active: want false after edit")
	}
	if m.WorkingDays != 15 {
		t.Errorf("WorkingDays: want 15 (preserved), got %d", m.WorkingDays)
	}
}

func TestUpdateMemberSettings_ReplacesRolesAtomically(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")
	r1, _ := st.CreateRole(ctx, "A", 0, "admin")
	r2, _ := st.CreateRole(ctx, "B", 0, "admin")

	if err := st.UpdateMemberSettings(ctx, "alice", 62, []absence.RoleID{r1.ID, r2.ID}, "alice"); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if err := st.UpdateMemberSettings(ctx, "alice", 62, []absence.RoleID{r2.ID}, "alice"); err != nil {
		t.Fatalf("second update: %v", err)
	}

	m, _ := st.GetMember(ctx, "alice")
	if len(m.RoleIDs) != 1 || m.RoleIDs[0] != r2.ID {
		t.Errorf("RoleIDs: want [%d], got %v", r2.ID, m.RoleIDs)
	}
}

// --- roles ---

func TestCreateRole_UpdateRole_DeleteRole_CascadesUserRoles(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	role, err := st.CreateRole(ctx, "DBA", 2, "admin")
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := st.UpdateMemberSettings(ctx, "alice", 62, []absence.RoleID{role.ID}, "alice"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	if err := st.UpdateRole(ctx, role.ID, "DBA Lead", 3, "admin"); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	roles, _ := st.ListRoles(ctx)
	if len(roles) != 1 || roles[0].Name != "DBA Lead" || roles[0].MinPresent != 3 {
		t.Errorf("roles after update = %+v", roles)
	}

	if err := st.DeleteRole(ctx, role.ID, "admin"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	m, _ := st.GetMember(ctx, "alice")
	if len(m.RoleIDs) != 0 {
		t.Errorf("RoleIDs after role deletion: want empty (cascade), got %v", m.RoleIDs)
	}
}

// --- holidays ---

func TestCreateManualHoliday_DuplicateDate_ReturnsDistinctError(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if _, err := st.CreateManualHoliday(ctx, day(2026, 5, 1), "Company day", "admin"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := st.CreateManualHoliday(ctx, day(2026, 5, 1), "Another name", "admin")
	if err == nil {
		t.Fatal("want error for duplicate date")
	}
	if err != ErrDuplicateHoliday {
		t.Errorf("want ErrDuplicateHoliday, got %v", err)
	}
}

func TestImportHolidays_UpsertIsIdempotent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	rows := []absence.Holiday{
		{Date: day(2026, 1, 1), Name: "New Year", Source: absence.HolidaySourceImported},
	}
	if _, err := st.ImportHolidays(ctx, rows, "system"); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := st.ImportHolidays(ctx, rows, "system"); err != nil {
		t.Fatalf("second import: %v", err)
	}

	holidays, _ := st.ListHolidays(ctx)
	if len(holidays) != 1 {
		t.Errorf("want 1 holiday after re-import, got %d", len(holidays))
	}
}

func TestImportHolidays_DoesNotClobberManualEntry(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if _, err := st.CreateManualHoliday(ctx, day(2026, 1, 1), "Custom closure", "admin"); err != nil {
		t.Fatalf("create manual: %v", err)
	}
	rows := []absence.Holiday{
		{Date: day(2026, 1, 1), Name: "New Year", Source: absence.HolidaySourceImported},
	}
	if _, err := st.ImportHolidays(ctx, rows, "system"); err != nil {
		t.Fatalf("import: %v", err)
	}

	holidays, _ := st.ListHolidays(ctx)
	if len(holidays) != 1 {
		t.Fatalf("want 1 holiday, got %d", len(holidays))
	}
	if holidays[0].Name != "Custom closure" || holidays[0].Source != absence.HolidaySourceManual {
		t.Errorf("manual holiday was clobbered by import: %+v", holidays[0])
	}
}

func TestDeleteHoliday_WorksForBothSources(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	st.CreateManualHoliday(ctx, day(2026, 5, 1), "Manual", "admin") //nolint:errcheck
	st.ImportHolidays(ctx, []absence.Holiday{                       //nolint:errcheck
		{Date: day(2026, 1, 1), Name: "Imported", Source: absence.HolidaySourceImported},
	}, "system")

	if err := st.DeleteHoliday(ctx, day(2026, 5, 1), "admin"); err != nil {
		t.Fatalf("delete manual: %v", err)
	}
	if err := st.DeleteHoliday(ctx, day(2026, 1, 1), "admin"); err != nil {
		t.Fatalf("delete imported: %v", err)
	}

	holidays, _ := st.ListHolidays(ctx)
	if len(holidays) != 0 {
		t.Errorf("want 0 holidays after deleting both, got %d", len(holidays))
	}
}

func TestListHolidays_OrderedByDate(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	st.CreateManualHoliday(ctx, day(2026, 12, 25), "Christmas", "admin") //nolint:errcheck
	st.CreateManualHoliday(ctx, day(2026, 1, 1), "New Year", "admin")    //nolint:errcheck

	holidays, err := st.ListHolidays(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(holidays) != 2 {
		t.Fatalf("want 2 holidays, got %d", len(holidays))
	}
	if !holidays[0].Date.Equal(day(2026, 1, 1)) {
		t.Errorf("first holiday: want Jan 1, got %s", holidays[0].Date)
	}
}

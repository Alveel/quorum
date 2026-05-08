//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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

	st.UpsertUser(ctx, "alice", "old@example.com", "Alice")
	st.UpsertUser(ctx, "alice", "new@example.com", "Alice")

	var count int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE id = 'alice'`).Scan(&count)
	if count != 1 {
		t.Errorf("want 1 row, got %d", count)
	}

	var email string
	testPool.QueryRow(ctx, `SELECT email FROM users WHERE id = 'alice'`).Scan(&email)
	if email != "new@example.com" {
		t.Errorf("email not updated: want new@example.com, got %q", email)
	}
}

// --- GetSettings / UpdateSetting ---

func TestGetSettings_ReturnsDefaults(t *testing.T) {
	st := testStore(t)
	s, err := st.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.MinPresent != 8 {
		t.Errorf("MinPresent: want 8, got %d", s.MinPresent)
	}
	if s.TeamSize != 15 {
		t.Errorf("TeamSize: want 15, got %d", s.TeamSize)
	}
	if s.WeekendCounts {
		t.Error("WeekendCounts: want false, got true")
	}
}

func TestUpdateSetting_PersistsAndAudits(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.UpdateSetting(ctx, "team_size", 20, "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, _ := st.GetSettings(ctx)
	if s.TeamSize != 20 {
		t.Errorf("TeamSize after update: want 20, got %d", s.TeamSize)
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
	if targetID != "team_size" {
		t.Errorf("audit target_id: want team_size, got %q", targetID)
	}
}

// --- VacationsPerDay ---

func TestVacationsPerDay_NoVacations_AllZero(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	m, err := st.VacationsPerDay(ctx, day(2026, 7, 1), day(2026, 7, 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for d, count := range m {
		if count != 0 {
			t.Errorf("%s: want 0, got %d", d.Format("2006-01-02"), count)
		}
	}
}

func TestVacationsPerDay_SingleVacation(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	st.CreateVacation(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 5))

	m, err := st.VacationsPerDay(ctx, day(2026, 6, 29), day(2026, 7, 7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for d := day(2026, 7, 1); !d.After(day(2026, 7, 5)); d = d.AddDate(0, 0, 1) {
		if m[d] != 1 {
			t.Errorf("%s: want 1, got %d", d.Format("2006-01-02"), m[d])
		}
	}
	if m[day(2026, 6, 30)] != 0 {
		t.Errorf("Jun 30 (outside range): want 0, got %d", m[day(2026, 6, 30)])
	}
	if m[day(2026, 7, 6)] != 0 {
		t.Errorf("Jul 6 (outside range): want 0, got %d", m[day(2026, 7, 6)])
	}
}

func TestVacationsPerDay_CancelledIgnored(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, _ := st.CreateVacation(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))
	st.CancelVacation(ctx, v.ID, "alice")

	m, _ := st.VacationsPerDay(ctx, day(2026, 7, 1), day(2026, 7, 1))
	if m[day(2026, 7, 1)] != 0 {
		t.Errorf("cancelled vacation should not count, got %d", m[day(2026, 7, 1)])
	}
}

func TestVacationsPerDay_OverriddenCounts(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	st.CreateOverride(ctx, "alice", "admin", "", day(2026, 7, 1), day(2026, 7, 1), "reason")

	m, _ := st.VacationsPerDay(ctx, day(2026, 7, 1), day(2026, 7, 1))
	if m[day(2026, 7, 1)] != 1 {
		t.Errorf("overridden vacation should count as 1, got %d", m[day(2026, 7, 1)])
	}
}

// --- CreateVacation ---

func TestCreateVacation_ReturnsVacationWithID(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, err := st.CreateVacation(ctx, "alice", "alice", "holiday", day(2026, 7, 1), day(2026, 7, 5))
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

func TestCreateVacation_WritesAuditLog(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, _ := st.CreateVacation(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))

	var action, targetID string
	err := testPool.QueryRow(ctx, `
		SELECT action, target_id FROM audit_log WHERE actor_id = 'alice' ORDER BY at DESC LIMIT 1
	`).Scan(&action, &targetID)
	if err != nil {
		t.Fatalf("audit_log query: %v", err)
	}
	if action != "create_vacation" {
		t.Errorf("action: want create_vacation, got %q", action)
	}
	if targetID != v.ID.String() {
		t.Errorf("target_id: want %s, got %q", v.ID, targetID)
	}
}

// --- CancelVacation ---

func TestCancelVacation_OwnVacation_Succeeds(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")

	v, _ := st.CreateVacation(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))
	if err := st.CancelVacation(ctx, v.ID, "alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var status string
	testPool.QueryRow(ctx, `SELECT status FROM leave WHERE id = $1`, v.ID).Scan(&status)
	if status != "cancelled" {
		t.Errorf("status: want cancelled, got %q", status)
	}
}

func TestCancelVacation_OtherUsersVacation_Fails(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	insertUser(t, st, "alice", "alice@example.com")
	insertUser(t, st, "bob", "bob@example.com")

	v, _ := st.CreateVacation(ctx, "alice", "alice", "", day(2026, 7, 1), day(2026, 7, 1))
	err := st.CancelVacation(ctx, v.ID, "bob")
	if err == nil {
		t.Fatal("expected error when bob tries to cancel alice's vacation")
	}

	var status string
	testPool.QueryRow(ctx, `SELECT status FROM leave WHERE id = $1`, v.ID).Scan(&status)
	if status != "approved" {
		t.Errorf("status should still be approved, got %q", status)
	}
}

func TestCancelVacation_NonexistentID_Fails(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	fakeID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
	err := st.CancelVacation(ctx, fakeID, "alice")
	if err == nil {
		t.Fatal("expected error for nonexistent vacation")
	}
}

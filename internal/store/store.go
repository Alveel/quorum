package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/alveel/quorum/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/alveel/quorum/internal/absence"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dbURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// RunMigrations applies pending migrations using the embedded SQL files.
// Uses pgx/v5 stdlib wrapper so golang-migrate's postgres driver works without
// a separate pgx-specific migrate driver.
func RunMigrations(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("open sql db for migrations: %w", err)
	}
	defer db.Close() //nolint:errcheck

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create migrate driver: %w", err)
	}
	defer driver.Close() //nolint:errcheck

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migrate source: %w", err)
	}
	defer src.Close() //nolint:errcheck

	m, err := migrate.NewWithInstance(
		"iofs", src,
		"postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}

	err = m.Up()
	if err == nil || errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return fmt.Errorf("run migrations: %w", err)
}

// UpsertUser inserts a user row on first sight of an authenticated request. On subsequent
// logins it refreshes email only — display_name is intentionally left untouched on UPDATE
// so an admin-entered roster name (or the placeholder set at INSERT) survives future logins;
// there is no real display-name source from the auth headers to overwrite it with anyway.
func (s *Store) UpsertUser(ctx context.Context, id, email, displayName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		  SET email = EXCLUDED.email
	`, id, email, displayName)
	return err
}

// GetSettings returns all settings as a Settings struct.
func (s *Store) GetSettings(ctx context.Context) (absence.Settings, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return absence.Settings{}, err
	}
	defer rows.Close()

	m := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var val json.RawMessage
		if err := rows.Scan(&key, &val); err != nil {
			return absence.Settings{}, err
		}
		m[key] = val
	}

	var s2 absence.Settings
	if v, ok := m["min_present"]; ok {
		if err := json.Unmarshal(v, &s2.MinPresent); err != nil {
			return absence.Settings{}, fmt.Errorf("unmarshal min_present: %w", err)
		}
	}
	if v, ok := m["holiday_country"]; ok {
		if err := json.Unmarshal(v, &s2.HolidayCountry); err != nil {
			return absence.Settings{}, fmt.Errorf("unmarshal holiday_country: %w", err)
		}
	}
	return s2, rows.Err()
}

// UpdateSetting writes a single settings key and appends an audit log entry.
func (s *Store) UpdateSetting(ctx context.Context, key string, value any, actorID string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		UPDATE settings SET value = $1, updated_at = now(), updated_by = $2
		WHERE key = $3
	`, raw, actorID, key)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, target_id, payload)
		VALUES ($1, 'update_setting', $2, $3)
	`, actorID, key, json.RawMessage(raw))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateAbsence inserts a new absence with status='approved'.
func (s *Store) CreateAbsence(ctx context.Context, userID, createdBy, note string, start, end time.Time) (absence.Absence, error) {
	return s.insertAbsence(ctx, userID, createdBy, note, start, end, absence.StatusApproved, "")
}

// CreateOverride inserts a absence bypassing threshold, with status='overridden'.
func (s *Store) CreateOverride(ctx context.Context, userID, actorID, note string, start, end time.Time, reason string) (absence.Absence, error) {
	v, err := s.insertAbsence(ctx, userID, actorID, note, start, end, absence.StatusOverridden, reason)
	return v, err
}

func (s *Store) insertAbsence(ctx context.Context, userID, createdBy, note string, start, end time.Time, status absence.Status, overrideReason string) (absence.Absence, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return absence.Absence{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var v absence.Absence
	err = tx.QueryRow(ctx, `
		INSERT INTO absence (user_id, start_date, end_date, note, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, start_date, end_date, note, status, created_at, created_by
	`, userID, start, end, note, status, createdBy).Scan(
		&v.ID, &v.UserID, &v.StartDate, &v.EndDate, &v.Note, &v.Status, &v.CreatedAt, &v.CreatedBy,
	)
	if err != nil {
		return absence.Absence{}, err
	}

	payload, _ := json.Marshal(map[string]string{ //nolint:errcheck // map[string]string is always JSON-serializable
		"user_id": userID,
		"status":  string(status),
		"reason":  overrideReason,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, target_id, payload)
		VALUES ($1, $2, $3, $4)
	`, createdBy, "create_absence", v.ID.String(), json.RawMessage(payload))
	if err != nil {
		return absence.Absence{}, err
	}

	return v, tx.Commit(ctx)
}

// CancelAbsence marks a absence as cancelled. Only the owning user can cancel.
func (s *Store) CancelAbsence(ctx context.Context, id uuid.UUID, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE absence SET status = 'cancelled'
		WHERE id = $1 AND user_id = $2 AND status IN ('approved', 'overridden')
	`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("absence not found or not cancellable")
	}
	return nil
}

// ListMyAbsences returns non-cancelled absences for a user, newest first.
func (s *Store) ListMyAbsences(ctx context.Context, userID string) ([]absence.Absence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, start_date, end_date, note, status, created_at, created_by
		FROM absence
		WHERE user_id = $1 AND status != 'cancelled'
		ORDER BY start_date DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAbsences(rows)
}

// ListAllActive returns all non-cancelled absences sorted by start date.
func (s *Store) ListAllActive(ctx context.Context) ([]absence.Absence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.id, v.user_id, u.display_name, v.start_date, v.end_date, v.note, v.status, v.created_at, v.created_by
		FROM absence v
		JOIN users u ON u.id = v.user_id
		WHERE v.status IN ('approved', 'overridden')
		ORDER BY v.start_date
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAbsencesWithName(rows)
}

// AbsenceOnDay returns all active absences covering a specific date, with user display name.
func (s *Store) AbsenceOnDay(ctx context.Context, date time.Time) ([]absence.Absence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.id, v.user_id, u.display_name, v.start_date, v.end_date, v.note, v.status, v.created_at, v.created_by
		FROM absence v
		JOIN users u ON u.id = v.user_id
		WHERE v.status IN ('approved', 'overridden')
		  AND $1 BETWEEN v.start_date AND v.end_date
		ORDER BY v.start_date, v.user_id
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAbsencesWithName(rows)
}

// ListAbsencesInRange returns all active absences overlapping [from,to], with user display
// name. Used by Coverage() callers (heatmap, denial check) which need per-member/per-role
// attribution, not just a per-day count.
func (s *Store) ListAbsencesInRange(ctx context.Context, from, to time.Time) ([]absence.Absence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.id, v.user_id, u.display_name, v.start_date, v.end_date, v.note, v.status, v.created_at, v.created_by
		FROM absence v
		JOIN users u ON u.id = v.user_id
		WHERE v.status IN ('approved', 'overridden')
		  AND v.start_date <= $2 AND v.end_date >= $1
		ORDER BY v.start_date
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAbsencesWithName(rows)
}

// HasOverlap checks if a user already has a non-cancelled absence
// overlapping the given date range.
func (s *Store) HasOverlap(ctx context.Context, userID string, start, end time.Time) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM absence
			WHERE user_id = $1
		  		AND status != 'cancelled'
		  		AND end_date >= $2
				AND start_date <= $3
		)
	`, userID, start, end).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// --- roster ---

const rosterSelect = `
	SELECT u.id, u.email, u.display_name, u.active, u.working_days,
	       COALESCE(array_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '{}')
	FROM users u
	LEFT JOIN user_roles ur ON ur.user_id = u.id
`

// GetMember returns roster identity + pattern + role membership for one user.
func (s *Store) GetMember(ctx context.Context, userID string) (absence.Member, error) {
	row := s.pool.QueryRow(ctx, rosterSelect+`
		WHERE u.id = $1
		GROUP BY u.id, u.email, u.display_name, u.active, u.working_days
	`, userID)
	return scanMember(row)
}

// ListRoster returns every user with their pattern and role membership, ordered by id.
func (s *Store) ListRoster(ctx context.Context) ([]absence.Member, error) {
	rows, err := s.pool.Query(ctx, rosterSelect+`
		GROUP BY u.id, u.email, u.display_name, u.active, u.working_days
		ORDER BY u.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []absence.Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertRosterMember creates or updates the admin-owned identity fields (display name,
// active flag) for a member. It never touches working_days/roles — those are owned by
// UpdateMemberSettings — so a new row gets the column default pattern (Mon-Fri, no roles)
// and an existing row keeps whatever pattern it already has.
func (s *Store) UpsertRosterMember(ctx context.Context, id, displayName string, active bool, actorID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, display_name, active)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		  SET display_name = EXCLUDED.display_name, active = EXCLUDED.active
	`, id, displayName, active)
	if err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actorID, "upsert_roster_member", id, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateMemberSettings replaces a member's working-day pattern and role assignments in one
// transaction. Shared by self-service (/settings) and admin-edit (/admin/roster/{id}) —
// actorID differs from userID when an admin edits someone else.
func (s *Store) UpdateMemberSettings(ctx context.Context, userID string, workingDays uint8, roleIDs []absence.RoleID, actorID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `UPDATE users SET working_days = $1 WHERE id = $2`, int16(workingDays), userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, rid := range roleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, userID, int32(rid)); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]any{ //nolint:errcheck
		"working_days": workingDays,
		"role_ids":     roleIDs,
	})
	if err := insertAudit(ctx, tx, actorID, "update_member_settings", userID, payload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanMember(row rowScanner) (absence.Member, error) {
	var m absence.Member
	var workingDays int16
	var roleIDs []int32
	if err := row.Scan(&m.ID, &m.Email, &m.Name, &m.Active, &workingDays, &roleIDs); err != nil {
		return absence.Member{}, err
	}
	m.WorkingDays = uint8(workingDays)
	m.RoleIDs = make([]absence.RoleID, len(roleIDs))
	for i, r := range roleIDs {
		m.RoleIDs[i] = absence.RoleID(r)
	}
	return m, nil
}

// --- roles ---

// ListRoles returns every role, ordered by id.
func (s *Store) ListRoles(ctx context.Context) ([]absence.Role, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, min_present FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []absence.Role
	for rows.Next() {
		var r absence.Role
		if err := rows.Scan(&r.ID, &r.Name, &r.MinPresent); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRole inserts a new role and returns it with its assigned id.
func (s *Store) CreateRole(ctx context.Context, name string, minPresent int, actorID string) (absence.Role, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return absence.Role{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var r absence.Role
	if err := tx.QueryRow(ctx, `
		INSERT INTO roles (name, min_present) VALUES ($1, $2)
		RETURNING id, name, min_present
	`, name, minPresent).Scan(&r.ID, &r.Name, &r.MinPresent); err != nil {
		return absence.Role{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "create_role", fmt.Sprint(r.ID), nil); err != nil {
		return absence.Role{}, err
	}
	return r, tx.Commit(ctx)
}

// UpdateRole updates a role's name and quota.
func (s *Store) UpdateRole(ctx context.Context, id absence.RoleID, name string, minPresent int, actorID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `UPDATE roles SET name = $1, min_present = $2 WHERE id = $3`, name, minPresent, int32(id))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found")
	}
	if err := insertAudit(ctx, tx, actorID, "update_role", fmt.Sprint(id), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteRole removes a role. Membership rows cascade via ON DELETE CASCADE.
func (s *Store) DeleteRole(ctx context.Context, id absence.RoleID, actorID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `DELETE FROM roles WHERE id = $1`, int32(id))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role not found")
	}
	if err := insertAudit(ctx, tx, actorID, "delete_role", fmt.Sprint(id), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- holidays ---

var ErrDuplicateHoliday = errors.New("a holiday already exists on that date")

// ListHolidays returns every holiday, ordered by date.
func (s *Store) ListHolidays(ctx context.Context) ([]absence.Holiday, error) {
	rows, err := s.pool.Query(ctx, `SELECT date, name, source FROM holidays ORDER BY date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []absence.Holiday
	for rows.Next() {
		var h absence.Holiday
		if err := rows.Scan(&h.Date, &h.Name, &h.Source); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CreateManualHoliday adds a single admin-entered holiday. Returns ErrDuplicateHoliday if
// the date already has a holiday (of either source) so the handler can 422 instead of 500.
func (s *Store) CreateManualHoliday(ctx context.Context, date time.Time, name, actorID string) (absence.Holiday, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return absence.Holiday{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `
		INSERT INTO holidays (date, name, source) VALUES ($1, $2, 'manual')
		ON CONFLICT (date) DO NOTHING
	`, date, name)
	if err != nil {
		return absence.Holiday{}, err
	}
	if tag.RowsAffected() == 0 {
		return absence.Holiday{}, ErrDuplicateHoliday
	}
	if err := insertAudit(ctx, tx, actorID, "create_manual_holiday", date.Format("2006-01-02"), nil); err != nil {
		return absence.Holiday{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return absence.Holiday{}, err
	}
	return absence.Holiday{Date: date, Name: name, Source: absence.HolidaySourceManual}, nil
}

// DeleteHoliday removes a holiday on a given date, regardless of source.
func (s *Store) DeleteHoliday(ctx context.Context, date time.Time, actorID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx, `DELETE FROM holidays WHERE date = $1`, date)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("holiday not found")
	}
	if err := insertAudit(ctx, tx, actorID, "delete_holiday", date.Format("2006-01-02"), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ImportHolidays bulk-upserts synced holiday rows. A row already present with
// source='manual' is left untouched — re-syncing never clobbers an admin-entered override
// on the same date. Returns the number of rows written (imported or refreshed).
func (s *Store) ImportHolidays(ctx context.Context, rows []absence.Holiday, actorID string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var count int
	for _, h := range rows {
		tag, err := tx.Exec(ctx, `
			INSERT INTO holidays (date, name, source) VALUES ($1, $2, 'imported')
			ON CONFLICT (date) DO UPDATE SET name = EXCLUDED.name, source = 'imported'
			WHERE holidays.source = 'imported'
		`, h.Date, h.Name)
		if err != nil {
			return 0, err
		}
		count += int(tag.RowsAffected())
	}
	payload, _ := json.Marshal(map[string]any{"count": count}) //nolint:errcheck
	if err := insertAudit(ctx, tx, actorID, "import_holidays", "", payload); err != nil {
		return 0, err
	}
	return count, tx.Commit(ctx)
}

// --- helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

type rowsScanner interface {
	rowScanner
	Next() bool
	Err() error
}

func insertAudit(ctx context.Context, tx pgx.Tx, actorID, action, targetID string, payload json.RawMessage) error {
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, target_id, payload)
		VALUES ($1, $2, $3, $4)
	`, actorID, action, targetID, payload)
	return err
}

func scanAbsences(rows rowsScanner) ([]absence.Absence, error) {
	var out []absence.Absence
	for rows.Next() {
		var v absence.Absence
		if err := rows.Scan(&v.ID, &v.UserID, &v.StartDate, &v.EndDate, &v.Note, &v.Status, &v.CreatedAt, &v.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanAbsencesWithName(rows rowsScanner) ([]absence.Absence, error) {
	var out []absence.Absence
	for rows.Next() {
		var v absence.Absence
		if err := rows.Scan(&v.ID, &v.UserID, &v.UserName, &v.StartDate, &v.EndDate, &v.Note, &v.Status, &v.CreatedAt, &v.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

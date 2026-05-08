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
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return fmt.Errorf("run migrations: %w", err)
}

// UpsertUser inserts or updates a user row on every authenticated request.
func (s *Store) UpsertUser(ctx context.Context, id, email, displayName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		  SET email = EXCLUDED.email,
		      display_name = EXCLUDED.display_name
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
	if v, ok := m["team_size"]; ok {
		if err := json.Unmarshal(v, &s2.TeamSize); err != nil {
			return absence.Settings{}, fmt.Errorf("unmarshal team_size: %w", err)
		}
	}
	if v, ok := m["weekend_counts"]; ok {
		if err := json.Unmarshal(v, &s2.WeekendCounts); err != nil {
			return absence.Settings{}, fmt.Errorf("unmarshal weekend_counts: %w", err)
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

// AbsencesOnDay returns all active absences covering a specific date, with user display name.
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

// AbsencePerDay returns a map of date → on-absence count for the given range.
// Used by both threshold checking and heatmap rendering.
func (s *Store) AbsencePerDay(ctx context.Context, from, to time.Time) (map[time.Time]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT gs::date, COUNT(v.id)
		FROM generate_series($1::date, $2::date, '1 day'::interval) gs
		LEFT JOIN absence v
		  ON v.status IN ('approved', 'overridden')
		  AND gs BETWEEN v.start_date AND v.end_date
		GROUP BY gs
		ORDER BY gs
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[time.Time]int)
	for rows.Next() {
		var d time.Time
		var count int
		if err := rows.Scan(&d, &count); err != nil {
			return nil, err
		}
		m[d.UTC().Truncate(24*time.Hour)] = count
	}
	return m, rows.Err()
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

// --- helpers ---

type rowScanner interface {
	Scan(dest ...any) error
	Next() bool
	Err() error
}

func scanAbsences(rows rowScanner) ([]absence.Absence, error) {
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

func scanAbsencesWithName(rows rowScanner) ([]absence.Absence, error) {
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

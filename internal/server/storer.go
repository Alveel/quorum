package server

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/absence"
)

// Storer is the subset of store.Store methods used by HTTP handlers.
// Declared consumer-side so handlers can be tested with a fake.
type Storer interface {
	// settings
	GetSettings(ctx context.Context) (absence.Settings, error)
	UpdateSetting(ctx context.Context, key string, value any, actorID string) error

	// absences
	ListAbsencesInRange(ctx context.Context, from, to time.Time) ([]absence.Absence, error)
	AbsenceOnDay(ctx context.Context, date time.Time) ([]absence.Absence, error)
	ListMyAbsences(ctx context.Context, userID string) ([]absence.Absence, error)
	ListAllActive(ctx context.Context) ([]absence.Absence, error)
	CreateAbsence(ctx context.Context, userID, createdBy, note string, start, end time.Time) (absence.Absence, error)
	CreateOverride(ctx context.Context, userID, actorID, note string, start, end time.Time, reason string) (absence.Absence, error)
	CancelAbsence(ctx context.Context, id uuid.UUID, userID string) error
	HasOverlap(ctx context.Context, userID string, start, end time.Time) (bool, error)

	// users / roster
	UpsertUser(ctx context.Context, id, email, displayName string) error
	GetMember(ctx context.Context, userID string) (absence.Member, error)
	ListRoster(ctx context.Context) ([]absence.Member, error)
	UpsertRosterMember(ctx context.Context, id, displayName string, active bool, actorID string) error
	UpdateMemberSettings(ctx context.Context, userID string, workingDays uint8, roleIDs []absence.RoleID, actorID string) error

	// roles
	ListRoles(ctx context.Context) ([]absence.Role, error)
	CreateRole(ctx context.Context, name string, minPresent int, actorID string) (absence.Role, error)
	UpdateRole(ctx context.Context, id absence.RoleID, name string, minPresent int, actorID string) error
	DeleteRole(ctx context.Context, id absence.RoleID, actorID string) error

	// holidays
	ListHolidays(ctx context.Context) ([]absence.Holiday, error)
	CreateManualHoliday(ctx context.Context, date time.Time, name, actorID string) (absence.Holiday, error)
	DeleteHoliday(ctx context.Context, date time.Time, actorID string) error
	ImportHolidays(ctx context.Context, rows []absence.Holiday, actorID string) (int, error)
}

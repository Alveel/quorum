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
	GetSettings(ctx context.Context) (absence.Settings, error)
	UpdateSetting(ctx context.Context, key string, value any, actorID string) error
	AbsencePerDay(ctx context.Context, from, to time.Time) (map[time.Time]int, error)
	AbsenceOnDay(ctx context.Context, date time.Time) ([]absence.Absence, error)
	ListMyAbsences(ctx context.Context, userID string) ([]absence.Absence, error)
	ListAllActive(ctx context.Context) ([]absence.Absence, error)
	CreateAbsence(ctx context.Context, userID, createdBy, note string, start, end time.Time) (absence.Absence, error)
	CreateOverride(ctx context.Context, userID, actorID, note string, start, end time.Time, reason string) (absence.Absence, error)
	CancelAbsence(ctx context.Context, id uuid.UUID, userID string) error
	UpsertUser(ctx context.Context, id, email, displayName string) error
	HasOverlap(ctx context.Context, userID string, start, end time.Time) (bool, error)
}

package server

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/vacation"
)

// Storer is the subset of store.Store methods used by HTTP handlers.
// Declared consumer-side so handlers can be tested with a fake.
type Storer interface {
	GetSettings(ctx context.Context) (vacation.Settings, error)
	UpdateSetting(ctx context.Context, key string, value any, actorID string) error
	VacationsPerDay(ctx context.Context, from, to time.Time) (map[time.Time]int, error)
	VacationsOnDay(ctx context.Context, date time.Time) ([]vacation.Vacation, error)
	ListMyVacations(ctx context.Context, userID string) ([]vacation.Vacation, error)
	ListAllActive(ctx context.Context) ([]vacation.Vacation, error)
	CreateVacation(ctx context.Context, userID, createdBy, note string, start, end time.Time) (vacation.Vacation, error)
	CreateOverride(ctx context.Context, userID, actorID, note string, start, end time.Time, reason string) (vacation.Vacation, error)
	CancelVacation(ctx context.Context, id uuid.UUID, userID string) error
	UpsertUser(ctx context.Context, id, email, displayName string) error
	HasOverlap(ctx context.Context, userID string, start, end time.Time) (bool, error)
}

package server

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/vacation"
)

type fakeStore struct {
	settings      vacation.Settings
	settingsErr   error
	perDay        map[time.Time]int
	perDayErr     error
	onDay         []vacation.Vacation
	onDayErr      error
	myVacations   []vacation.Vacation
	myVacErr      error
	allActive     []vacation.Vacation
	allActiveErr  error
	createVac     vacation.Vacation
	createVacErr  error
	createOvr     vacation.Vacation
	createOvrErr  error
	cancelErr     error
	upsertErr     error
	upsertCalled  bool
	upsertID      string
	upsertEmail   string
	hasOverlap    bool
	hasOverlapErr error
}

func (f *fakeStore) GetSettings(_ context.Context) (vacation.Settings, error) {
	return f.settings, f.settingsErr
}

func (f *fakeStore) UpdateSetting(_ context.Context, _ string, _ any, _ string) error {
	return nil
}

func (f *fakeStore) VacationsPerDay(_ context.Context, _, _ time.Time) (map[time.Time]int, error) {
	if f.perDay == nil {
		return map[time.Time]int{}, f.perDayErr
	}
	return f.perDay, f.perDayErr
}

func (f *fakeStore) VacationsOnDay(_ context.Context, _ time.Time) ([]vacation.Vacation, error) {
	return f.onDay, f.onDayErr
}

func (f *fakeStore) ListMyVacations(_ context.Context, _ string) ([]vacation.Vacation, error) {
	return f.myVacations, f.myVacErr
}

func (f *fakeStore) ListAllActive(_ context.Context) ([]vacation.Vacation, error) {
	return f.allActive, f.allActiveErr
}

func (f *fakeStore) CreateVacation(_ context.Context, _, _, _ string, _, _ time.Time) (vacation.Vacation, error) {
	return f.createVac, f.createVacErr
}

func (f *fakeStore) CreateOverride(_ context.Context, _, _, _ string, _, _ time.Time, _ string) (vacation.Vacation, error) {
	return f.createOvr, f.createOvrErr
}

func (f *fakeStore) CancelVacation(_ context.Context, _ uuid.UUID, _ string) error {
	return f.cancelErr
}

func (f *fakeStore) UpsertUser(_ context.Context, id, email, _ string) error {
	f.upsertCalled = true
	f.upsertID = id
	f.upsertEmail = email
	return f.upsertErr
}

func (f *fakeStore) HasOverlap(_ context.Context, _ string, _, _ time.Time) (bool, error) {
	return f.hasOverlap, f.hasOverlapErr
}

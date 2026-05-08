package server

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/absence"
)

type fakeStore struct {
	settings      absence.Settings
	settingsErr   error
	perDay        map[time.Time]int
	perDayErr     error
	onDay         []absence.Absence
	onDayErr      error
	myAbsences    []absence.Absence
	myVacErr      error
	allActive     []absence.Absence
	allActiveErr  error
	createVac     absence.Absence
	createVacErr  error
	createOvr     absence.Absence
	createOvrErr  error
	cancelErr     error
	upsertErr     error
	upsertCalled  bool
	upsertID      string
	upsertEmail   string
	hasOverlap    bool
	hasOverlapErr error
}

func (f *fakeStore) GetSettings(_ context.Context) (absence.Settings, error) {
	return f.settings, f.settingsErr
}

func (f *fakeStore) UpdateSetting(_ context.Context, _ string, _ any, _ string) error {
	return nil
}

func (f *fakeStore) AbsencePerDay(_ context.Context, _, _ time.Time) (map[time.Time]int, error) {
	if f.perDay == nil {
		return map[time.Time]int{}, f.perDayErr
	}
	return f.perDay, f.perDayErr
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

func (f *fakeStore) UpsertUser(_ context.Context, id, email, _ string) error {
	f.upsertCalled = true
	f.upsertID = id
	f.upsertEmail = email
	return f.upsertErr
}

func (f *fakeStore) HasOverlap(_ context.Context, _ string, _, _ time.Time) (bool, error) {
	return f.hasOverlap, f.hasOverlapErr
}

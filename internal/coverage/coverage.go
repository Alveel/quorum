// Package coverage loads everything absence.Coverage needs and computes it, so a
// caller asks for a date range rather than assembling five store reads, a holiday-set
// conversion and a seven-argument call.
package coverage

import (
	"context"
	"fmt"
	"time"

	"github.com/alveel/quorum/internal/absence"
)

// Store is the subset of the persistence layer a Snapshot is built from. Declared
// consumer-side, so this package is testable with a five-method fake.
type Store interface {
	GetSettings(ctx context.Context) (absence.Settings, error)
	ListRoster(ctx context.Context) ([]absence.Member, error)
	ListRoles(ctx context.Context) ([]absence.Role, error)
	ListHolidays(ctx context.Context) ([]absence.Holiday, error)
	ListAbsencesInRange(ctx context.Context, from, to time.Time) ([]absence.Absence, error)
}

// Snapshot is one read of everything needed to talk about coverage over [From,To]:
// the computed per-day result, plus the inputs it was computed from, which callers
// also render.
type Snapshot struct {
	From, To time.Time
	Days     map[time.Time]absence.DayCoverage
	Roster   []absence.Member
	Roles    []absence.Role
	Holidays []absence.Holiday
	// Absences holds the stored rows in range. Any extra absences passed to ForRange
	// are counted in Days but deliberately not listed here: they aren't persisted, so
	// nothing should render them as though they were.
	Absences []absence.Absence
	Settings absence.Settings
}

// Member returns the roster entry for id, or the zero Member if nobody matches. The
// zero value reads as inactive with no roles, so an unknown id is treated as
// unconfigured rather than as a member in good standing.
func (s Snapshot) Member(id string) absence.Member {
	for _, m := range s.Roster {
		if m.ID == id {
			return m
		}
	}
	return absence.Member{}
}

// Querier builds Snapshots from a Store.
type Querier struct{ store Store }

func New(store Store) *Querier { return &Querier{store: store} }

// ForRange reads every coverage input and computes day coverage across [from,to]
// inclusive.
//
// Any extra absences are merged into the stored ones before computing. That is how a
// request that hasn't been written yet gets evaluated against the coverage it would
// produce: the caller passes the candidate rather than reproducing the merge.
func (q *Querier) ForRange(ctx context.Context, from, to time.Time, extra ...absence.Absence) (Snapshot, error) {
	settings, err := q.store.GetSettings(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("get settings: %w", err)
	}
	roster, err := q.store.ListRoster(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list roster: %w", err)
	}
	roles, err := q.store.ListRoles(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list roles: %w", err)
	}
	holidays, err := q.store.ListHolidays(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list holidays: %w", err)
	}
	absences, err := q.store.ListAbsencesInRange(ctx, from, to)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list absences: %w", err)
	}

	counted := absences
	if len(extra) > 0 {
		counted = make([]absence.Absence, 0, len(absences)+len(extra))
		counted = append(counted, absences...)
		counted = append(counted, extra...)
	}

	return Snapshot{
		From:     from,
		To:       to,
		Days:     absence.Coverage(roster, counted, absence.HolidaySet(holidays), roles, settings.MinPresent, from, to),
		Roster:   roster,
		Roles:    roles,
		Holidays: holidays,
		Absences: absences,
		Settings: settings,
	}, nil
}

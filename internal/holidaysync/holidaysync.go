// Package holidaysync computes a country's public holidays for a given year using an
// offline calendar library. It is kept separate from internal/absence so the domain
// package stays free of the external dependency — internal/absence only ever consumes
// the resulting []absence.Holiday, never rickar/cal itself.
package holidaysync

import (
	"fmt"
	"sort"

	cal "github.com/rickar/cal/v2"
	"github.com/rickar/cal/v2/nl"

	"github.com/alveel/quorum/internal/absence"
)

// countrySelectors maps an ISO country code to its holiday definitions. Add a country by
// importing its subpackage and adding one entry here — no schema/architecture change.
var countrySelectors = map[string][]*cal.Holiday{
	"nl": nl.Holidays,
}

// SupportedCountries returns the ISO codes currently wired in, sorted.
func SupportedCountries() []string {
	out := make([]string, 0, len(countrySelectors))
	for k := range countrySelectors {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Sync computes a country's public holidays for a year. Pure/offline — no network access,
// safe to call from an admin-triggered request handler.
func Sync(country string, year int) ([]absence.Holiday, error) {
	defs, ok := countrySelectors[country]
	if !ok {
		return nil, fmt.Errorf("holidaysync: unsupported country %q", country)
	}
	out := make([]absence.Holiday, 0, len(defs))
	for _, h := range defs {
		actual, observed := h.Calc(year)
		if actual.IsZero() {
			continue // not observed in this year (StartYear/EndYear/Except)
		}
		d := observed
		if d.IsZero() {
			d = actual
		}
		out = append(out, absence.Holiday{Date: d, Name: h.Name, Source: absence.HolidaySourceImported})
	}
	return out, nil
}

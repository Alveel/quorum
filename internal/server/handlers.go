package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/absence"
	"github.com/alveel/quorum/internal/auth"
	"github.com/alveel/quorum/internal/config"
	"github.com/alveel/quorum/internal/locale"
	"github.com/alveel/quorum/internal/view"
)

type handlers struct {
	cfg   config.Config
	store Storer
}

func (h *handlers) index(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	year := time.Now().Year()
	if y := r.URL.Query().Get("year"); y != "" {
		if n, err := strconv.Atoi(y); err == nil {
			year = n
		}
	}

	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		http.Error(w, "load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	perDay, err := h.store.AbsencePerDay(r.Context(), yearStart, yearEnd)
	if err != nil {
		http.Error(w, "load heatmap: "+err.Error(), http.StatusInternalServerError)
		return
	}

	myAbsence, err := h.store.ListMyAbsences(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "load absence: "+err.Error(), http.StatusInternalServerError)
		return
	}

	heatmap := buildHeatmap(year, perDay, settings)
	page := view.PageData{
		User:       u.ID,
		IsAdmin:    u.Admin,
		Heatmap:    heatmap,
		MyAbsences: myAbsence,
	}
	if err := view.IndexPage(page).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
}

func (h *handlers) createAbsence(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	start, end, err := parseDateRange(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err2 := view.FormError(locale.T(r.Context(), err.Error()), nil).Render(r.Context(), w); err2 != nil {
			slog.Debug("render", "err", err2)
		}
		return
	}

	overlap, err := h.store.HasOverlap(r.Context(), u.ID, start, end)
	if err != nil {
		http.Error(w, "check overlap: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if overlap {
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err := view.FormError(locale.T(r.Context(), "err_overlap"), nil).Render(r.Context(), w); err != nil {
			slog.Debug("render", "err", err)
		}
		return
	}

	note := r.FormValue("note")

	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	perDay, err := h.store.AbsencePerDay(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	offending := absence.CheckRequest(start, end, perDay, settings.TeamSize, settings.MinPresent, settings.WeekendCounts)
	if len(offending) > 0 {
		dates := make([]string, len(offending))
		for i, d := range offending {
			dates[i] = locale.FormatDate(r.Context(), d)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err := view.FormError(
			locale.TP(r.Context(), "err_coverage", len(offending), map[string]any{
				"Min":   settings.MinPresent,
				"Count": len(offending),
			}),
			dates,
		).Render(r.Context(), w); err != nil {
			slog.Debug("render", "err", err)
		}
		return
	}

	if _, err := h.store.CreateAbsence(r.Context(), u.ID, u.ID, note, start, end); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// OOB-swap: update heatmap + my-absence list in one response.
	year := start.Year()
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	perDay, err = h.store.AbsencePerDay(r.Context(), yearStart, yearEnd)
	if err != nil {
		slog.Warn("oob refresh: AbsencePerDay", "err", err)
	}
	myAbsences, err2 := h.store.ListMyAbsences(r.Context(), u.ID)
	if err2 != nil {
		slog.Warn("oob refresh: ListMyAbsences", "err", err2)
	}

	// OOB elements appended after primary response content.
	if err := view.FormSuccess().Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
	if err := view.HeatmapOOB(buildHeatmap(year, perDay, settings)).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
	if err := view.MyAbsencesOOB(myAbsences).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
}

func (h *handlers) cancelAbsence(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.CancelAbsence(r.Context(), id, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		slog.Warn("oob refresh: GetSettings", "err", err)
	}
	perDay, err := h.store.AbsencePerDay(r.Context(), yearStart, yearEnd)
	if err != nil {
		slog.Warn("oob refresh: AbsencePerDay", "err", err)
	}
	myAbsences, err := h.store.ListMyAbsences(r.Context(), u.ID)
	if err != nil {
		slog.Warn("oob refresh: ListMyAbsences", "err", err)
	}

	if err := view.MyAbsences(myAbsences).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
	if err := view.HeatmapOOB(buildHeatmap(now.Year(), perDay, settings)).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
}

func (h *handlers) dayDetail(w http.ResponseWriter, r *http.Request) {
	dateStr := chi.URLParam(r, "date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}

	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	absences, err := h.store.AbsenceOnDay(r.Context(), date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	present := settings.TeamSize - len(absences)
	if err := view.DayDetail(locale.FormatDate(r.Context(), date), absences, present, settings.TeamSize).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
}

func (h *handlers) adminPage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		http.Error(w, "load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	absences, err := h.store.ListAllActive(r.Context())
	if err != nil {
		http.Error(w, "load absences: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := view.AdminPage(view.AdminData{
		User:     u.ID,
		Settings: settings,
		Absences: absences,
	}).Render(r.Context(), w); err != nil {
		slog.Debug("render", "err", err)
	}
}

func (h *handlers) adminSettings(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if v := r.FormValue("min_present"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "invalid min_present: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.store.UpdateSetting(r.Context(), "min_present", n, u.ID); err != nil {
			http.Error(w, "update min_present: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if v := r.FormValue("team_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "invalid team_size: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.store.UpdateSetting(r.Context(), "team_size", n, u.ID); err != nil {
			http.Error(w, "update team_size: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	wc := r.FormValue("weekend_counts") == "true"
	if err := h.store.UpdateSetting(r.Context(), "weekend_counts", wc, u.ID); err != nil {
		http.Error(w, "update weekend_counts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *handlers) adminOverride(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	targetUserID := r.FormValue("user_id")
	reason := r.FormValue("reason")
	note := r.FormValue("note")
	start, end, err := parseDateRange(r)
	if err != nil {
		http.Error(w, locale.T(r.Context(), err.Error()), http.StatusUnprocessableEntity)
		return
	}
	if _, err := h.store.CreateOverride(r.Context(), targetUserID, u.ID, note, start, end, reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// --- helpers ---

func parseDateRange(r *http.Request) (time.Time, time.Time, error) {
	if err := r.ParseForm(); err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err := time.Parse("2006-01-02", r.FormValue("start_date"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("err_invalid_start")
	}
	end, err := time.Parse("2006-01-02", r.FormValue("end_date"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("err_invalid_end")
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("err_end_before_start")
	}
	return start, end, nil
}

func buildHeatmap(year int, perDay map[time.Time]int, s absence.Settings) view.HeatmapData {
	months := make([]view.MonthData, 12)
	for i := range months {
		m := time.Month(i + 1)
		first := time.Date(year, m, 1, 0, 0, 0, 0, time.UTC)
		// Blank cells to align Monday as first column (ISO week).
		startWD := int(first.Weekday()+6) % 7 // Mon=0 … Sun=6
		var days []view.DayCell
		for j := 0; j < startWD; j++ {
			days = append(days, view.DayCell{Blank: true})
		}
		for d := first; d.Month() == m; d = d.AddDate(0, 0, 1) {
			present := absence.Present(d, perDay, s.TeamSize)
			days = append(days, view.DayCell{
				Date:      d,
				Present:   present,
				Color:     absence.Color(present, s.TeamSize, s.MinPresent),
				IsWeekend: d.Weekday() == time.Saturday || d.Weekday() == time.Sunday,
			})
		}
		months[i] = view.MonthData{
			Name: m.String()[:3],
			Year: year,
			Mon:  m,
			Days: days,
		}
	}
	return view.HeatmapData{
		Year:       year,
		PrevYear:   year - 1,
		NextYear:   year + 1,
		Months:     months,
		MinPresent: s.MinPresent,
		TeamSize:   s.TeamSize,
	}
}

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/alveel/vacation-coverage/internal/auth"
	"github.com/alveel/vacation-coverage/internal/config"
	"github.com/alveel/vacation-coverage/internal/vacation"
	"github.com/alveel/vacation-coverage/internal/view"
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
	perDay, err := h.store.VacationsPerDay(r.Context(), yearStart, yearEnd)
	if err != nil {
		http.Error(w, "load heatmap: "+err.Error(), http.StatusInternalServerError)
		return
	}

	myVacations, err := h.store.ListMyVacations(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "load vacations: "+err.Error(), http.StatusInternalServerError)
		return
	}

	heatmap := buildHeatmap(year, perDay, settings)
	page := view.PageData{
		User:        u.ID,
		IsAdmin:     u.Admin,
		Heatmap:     heatmap,
		MyVacations: myVacations,
	}
	view.IndexPage(page).Render(r.Context(), w)
}

func (h *handlers) createVacation(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	start, end, err := parseDateRange(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		view.FormError(err.Error(), nil).Render(r.Context(), w)
		return
	}

	overlap, err := h.store.HasOverlap(r.Context(), u.ID, start, end)
	if err != nil {
		http.Error(w, "check overlap: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if overlap {
		w.WriteHeader(http.StatusUnprocessableEntity)
		view.FormError("You already have a vacation overlapping this date range", nil).Render(r.Context(), w)
		return
	}

	note := r.FormValue("note")

	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	perDay, err := h.store.VacationsPerDay(r.Context(), start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	offending := vacation.CheckRequest(start, end, perDay, settings.TeamSize, settings.MinPresent, settings.WeekendCounts)
	if len(offending) > 0 {
		dates := make([]string, len(offending))
		for i, d := range offending {
			dates[i] = d.Format("Mon 02 Jan 2006")
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		view.FormError(
			fmt.Sprintf("Coverage would drop below minimum (%d) on %d day(s). Discuss with your team.", settings.MinPresent, len(offending)),
			dates,
		).Render(r.Context(), w)
		return
	}

	if _, err := h.store.CreateVacation(r.Context(), u.ID, u.ID, note, start, end); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// OOB-swap: update heatmap + my-vacations list in one response.
	year := start.Year()
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	perDay, _ = h.store.VacationsPerDay(r.Context(), yearStart, yearEnd)
	myVacations, _ := h.store.ListMyVacations(r.Context(), u.ID)

	// OOB elements appended after primary response content.
	view.FormSuccess().Render(r.Context(), w)
	view.HeatmapOOB(buildHeatmap(year, perDay, settings)).Render(r.Context(), w)
	view.MyVacationsOOB(myVacations).Render(r.Context(), w)
}

func (h *handlers) cancelVacation(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.CancelVacation(r.Context(), id, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	settings, _ := h.store.GetSettings(r.Context())
	perDay, _ := h.store.VacationsPerDay(r.Context(), yearStart, yearEnd)
	myVacations, _ := h.store.ListMyVacations(r.Context(), u.ID)

	view.MyVacations(myVacations).Render(r.Context(), w)
	view.HeatmapOOB(buildHeatmap(now.Year(), perDay, settings)).Render(r.Context(), w)
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

	vacations, err := h.store.VacationsOnDay(r.Context(), date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	present := settings.TeamSize - len(vacations)
	view.DayDetail(date.Format("Mon 02 Jan 2006"), vacations, present, settings.TeamSize).Render(r.Context(), w)
}

func (h *handlers) adminPage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	settings, _ := h.store.GetSettings(r.Context())
	vacations, _ := h.store.ListAllActive(r.Context())
	view.AdminPage(view.AdminData{
		User:      u.ID,
		Settings:  settings,
		Vacations: vacations,
	}).Render(r.Context(), w)
}

func (h *handlers) adminSettings(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if v := r.FormValue("min_present"); v != "" {
		n, _ := strconv.Atoi(v)
		h.store.UpdateSetting(r.Context(), "min_present", n, u.ID)
	}
	if v := r.FormValue("team_size"); v != "" {
		n, _ := strconv.Atoi(v)
		h.store.UpdateSetting(r.Context(), "team_size", n, u.ID)
	}
	wc := r.FormValue("weekend_counts") == "true"
	h.store.UpdateSetting(r.Context(), "weekend_counts", wc, u.ID)
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
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
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
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start date")
	}
	end, err := time.Parse("2006-01-02", r.FormValue("end_date"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end date")
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end date must be on or after start date")
	}
	return start, end, nil
}

func buildHeatmap(year int, perDay map[time.Time]int, s vacation.Settings) view.HeatmapData {
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
			present := vacation.Present(d, perDay, s.TeamSize)
			days = append(days, view.DayCell{
				Date:      d,
				Present:   present,
				Color:     vacation.Color(present, s.TeamSize, s.MinPresent),
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

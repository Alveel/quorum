package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/alveel/quorum/internal/absence"
	"github.com/alveel/quorum/internal/auth"
	"github.com/alveel/quorum/internal/config"
	"github.com/alveel/quorum/internal/coverage"
	"github.com/alveel/quorum/internal/locale"
	"github.com/alveel/quorum/internal/view"
)

type handlers struct {
	cfg      config.Config
	store    Storer
	coverage *coverage.Querier
}

func (h *handlers) index(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	year := time.Now().Year()
	if y := r.URL.Query().Get("year"); y != "" {
		if n, err := strconv.Atoi(y); err == nil {
			year = n
		}
	}

	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	snap, err := h.coverage.ForRange(r.Context(), yearStart, yearEnd)
	if err != nil {
		http.Error(w, "load coverage: "+err.Error(), http.StatusInternalServerError)
		return
	}

	myAbsence, err := h.store.ListMyAbsences(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "load absence: "+err.Error(), http.StatusInternalServerError)
		return
	}

	heatmap := buildHeatmap(year, snap.Days, snap.Settings.MinPresent)

	me := snap.Member(u.ID)
	page := view.PageData{
		User:       u.ID,
		IsAdmin:    u.Admin,
		Heatmap:    heatmap,
		MyAbsences: myAbsence,
		Configured: me.Active && len(me.RoleIDs) > 0,
	}
	if err := view.IndexPage(page).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "index", "err", err)
	}
}

func (h *handlers) createAbsence(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())

	me, err := h.store.GetMember(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "load member: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !me.Active || len(me.RoleIDs) == 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err := view.FormError(locale.T(r.Context(), "err_not_configured"), nil).Render(r.Context(), w); err != nil {
			slog.Debug("render", "handler", "createAbsence", "err", err)
		}
		return
	}

	start, end, err := parseDateRange(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err2 := view.FormError(locale.T(r.Context(), err.Error()), nil).Render(r.Context(), w); err2 != nil {
			slog.Debug("render", "handler", "createAbsence", "err", err2)
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
			slog.Debug("render", "handler", "createAbsence", "err", err)
		}
		return
	}

	note := r.FormValue("note")

	// Evaluate the request against the coverage it would produce, by counting it
	// before it exists.
	candidate := absence.Absence{UserID: u.ID, StartDate: start, EndDate: end, Status: absence.StatusApproved}
	snap, err := h.coverage.ForRange(r.Context(), start, end, candidate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	offending := absence.OffendingDays(snap.Days, me, start, end)
	if len(offending) > 0 {
		dates := make([]string, len(offending))
		for i, d := range offending {
			dates[i] = locale.FormatDate(r.Context(), d)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err := view.FormError(
			locale.TP(r.Context(), "err_coverage", len(offending), map[string]any{
				"Min":   snap.Settings.MinPresent,
				"Count": len(offending),
			}),
			dates,
		).Render(r.Context(), w); err != nil {
			slog.Debug("render", "handler", "createAbsence", "err", err)
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
	yearSnap, err := h.coverage.ForRange(r.Context(), yearStart, yearEnd)
	if err != nil {
		slog.Warn("oob refresh: coverage", "err", err)
	}
	myAbsences, err2 := h.store.ListMyAbsences(r.Context(), u.ID)
	if err2 != nil {
		slog.Warn("oob refresh: ListMyAbsences", "err", err2)
	}

	// OOB elements appended after primary response content.
	if err := view.FormSuccess().Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "createAbsence", "err", err)
	}
	if err := view.HeatmapOOB(buildHeatmap(year, yearSnap.Days, yearSnap.Settings.MinPresent)).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "createAbsence", "err", err)
	}
	if err := view.MyAbsencesOOB(myAbsences).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "createAbsence", "err", err)
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
	snap, err := h.coverage.ForRange(r.Context(), yearStart, yearEnd)
	if err != nil {
		slog.Warn("oob refresh: coverage", "err", err)
	}
	myAbsences, err := h.store.ListMyAbsences(r.Context(), u.ID)
	if err != nil {
		slog.Warn("oob refresh: ListMyAbsences", "err", err)
	}

	if err := view.MyAbsences(myAbsences).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "cancelAbsence", "err", err)
	}
	if err := view.HeatmapOOB(buildHeatmap(now.Year(), snap.Days, snap.Settings.MinPresent)).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "cancelAbsence", "err", err)
	}
}

func (h *handlers) dayDetail(w http.ResponseWriter, r *http.Request) {
	dateStr := chi.URLParam(r, "date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}

	snap, err := h.coverage.ForRange(r.Context(), date, date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dc := snap.Days[date]

	failing := make(map[absence.RoleID]bool, len(dc.Failing))
	for _, rid := range dc.Failing {
		failing[rid] = true
	}
	var perRole []view.RoleDetailRow
	for _, ro := range snap.Roles {
		rc := dc.PerRole[ro.ID]
		if rc.Expected == 0 {
			continue
		}
		perRole = append(perRole, view.RoleDetailRow{
			Name: ro.Name, Present: rc.Present, Expected: rc.Expected, Min: rc.Min, Failing: failing[ro.ID],
		})
	}
	sort.Slice(perRole, func(i, j int) bool { return perRole[i].Name < perRole[j].Name })

	data := view.DayDetailData{
		Date:           locale.FormatDate(r.Context(), date),
		Present:        dc.Present,
		Expected:       dc.Expected,
		NoOneScheduled: dc.NoOneScheduled,
		PerRole:        perRole,
		Absences:       snap.Absences,
	}
	if err := view.DayDetail(data).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "dayDetail", "err", err)
	}
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

// parseWorkingDays reads repeated "wd" form values (weekday ints 0-6, time.Weekday scale)
// into a bitmask.
func parseWorkingDays(r *http.Request) (uint8, error) {
	var wd uint8
	for _, v := range r.Form["wd"] {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 6 {
			return 0, fmt.Errorf("invalid working day value %q", v)
		}
		wd |= 1 << uint(n)
	}
	return wd, nil
}

// parseRoleIDs reads repeated "role_id" form values and validates each against the
// currently-defined roles, so a stale/unknown id (e.g. a role deleted mid-edit) 400s
// instead of being silently written.
func (h *handlers) parseRoleIDs(ctx context.Context, r *http.Request) ([]absence.RoleID, error) {
	roles, err := h.store.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	valid := make(map[absence.RoleID]bool, len(roles))
	for _, ro := range roles {
		valid[ro.ID] = true
	}
	var out []absence.RoleID
	for _, v := range r.Form["role_id"] {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid role_id %q", v)
		}
		rid := absence.RoleID(n)
		if !valid[rid] {
			return nil, fmt.Errorf("unknown role_id %d", n)
		}
		out = append(out, rid)
	}
	return out, nil
}

func buildMemberSettingsData(loggedInUser auth.User, isSelf bool, targetID string, member absence.Member, roles []absence.Role) view.MemberSettingsData {
	opts := make([]view.RoleOption, len(roles))
	for i, ro := range roles {
		opts[i] = view.RoleOption{ID: ro.ID, Name: ro.Name, Selected: member.HasRole(ro.ID)}
	}
	return view.MemberSettingsData{
		User:         loggedInUser.ID,
		IsAdmin:      loggedInUser.Admin,
		TargetUserID: targetID,
		IsSelf:       isSelf,
		DisplayName:  member.Name,
		Active:       member.Active,
		WorkingDays:  member.WorkingDays,
		Roles:        opts,
	}
}

func buildHeatmap(year int, cov map[time.Time]absence.DayCoverage, minPresent int) view.HeatmapData {
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
			dc := cov[d]
			days = append(days, view.DayCell{
				Date:           d,
				Present:        dc.Present,
				Expected:       dc.Expected,
				Color:          dc.Color,
				HasOverride:    dc.HasOverride,
				IsWeekend:      d.Weekday() == time.Saturday || d.Weekday() == time.Sunday,
				NoOneScheduled: dc.NoOneScheduled,
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
		MinPresent: minPresent,
	}
}

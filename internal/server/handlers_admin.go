package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/alveel/quorum/internal/absence"
	"github.com/alveel/quorum/internal/auth"
	"github.com/alveel/quorum/internal/holidaysync"
	"github.com/alveel/quorum/internal/locale"
	"github.com/alveel/quorum/internal/store"
	"github.com/alveel/quorum/internal/view"
)

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
	roster, roles, holidays, err := h.loadCoverageInputs(r.Context())
	if err != nil {
		http.Error(w, "load coverage inputs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	holidayRows, err := h.store.ListHolidays(r.Context())
	if err != nil {
		http.Error(w, "load holidays: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Feasibility warnings over a ~180-day lookahead: does any role's scheduled headcount
	// already fall short of its quota before anyone even books leave?
	today := time.Now().UTC().Truncate(24 * time.Hour)
	windowEnd := today.AddDate(0, 0, 180)
	cov := absence.Coverage(roster, nil, holidays, roles, settings.MinPresent, today, windowEnd)
	warnings := absence.FeasibilityWarnings(cov)

	roleNameByID := make(map[absence.RoleID]string, len(roles))
	for _, ro := range roles {
		roleNameByID[ro.ID] = ro.Name
	}

	rosterRows := make([]view.RosterRow, len(roster))
	for i, m := range roster {
		roleNames := make([]string, len(m.RoleIDs))
		for j, rid := range m.RoleIDs {
			roleNames[j] = roleNameByID[rid]
		}
		rosterRows[i] = view.RosterRow{
			ID: m.ID, DisplayName: m.Name, Active: m.Active, WorkingDays: m.WorkingDays, Roles: roleNames,
		}
	}

	memberCount := make(map[absence.RoleID]int)
	for _, m := range roster {
		for _, rid := range m.RoleIDs {
			memberCount[rid]++
		}
	}
	roleRows := make([]view.RoleRow, len(roles))
	for i, ro := range roles {
		roleRows[i] = view.RoleRow{
			ID: ro.ID, Name: ro.Name, MinPresent: ro.MinPresent,
			MemberCount: memberCount[ro.ID], InfeasibleDates: warnings[ro.ID],
		}
	}

	holidayRowsView := make([]view.HolidayRow, len(holidayRows))
	for i, hr := range holidayRows {
		holidayRowsView[i] = view.HolidayRow{Date: hr.Date, Name: hr.Name, Source: string(hr.Source)}
	}

	if err := view.AdminPage(view.AdminData{
		User:           u.ID,
		Settings:       settings,
		Absences:       absences,
		Roster:         rosterRows,
		Roles:          roleRows,
		Holidays:       holidayRowsView,
		CountryOptions: holidaysync.SupportedCountries(),
	}).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "adminPage", "err", err)
	}
}

func (h *handlers) adminSettings(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var minPresent *int
	if v := r.FormValue("min_present"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "invalid min_present: "+err.Error(), http.StatusBadRequest)
			return
		}
		minPresent = &n
	}
	country := r.FormValue("holiday_country")
	if country != "" {
		valid := false
		for _, c := range holidaysync.SupportedCountries() {
			if c == country {
				valid = true
				break
			}
		}
		if !valid {
			http.Error(w, "unsupported holiday_country", http.StatusBadRequest)
			return
		}
	}

	if minPresent != nil {
		if err := h.store.UpdateSetting(r.Context(), "min_present", *minPresent, u.ID); err != nil {
			http.Error(w, "update min_present: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if country != "" {
		if err := h.store.UpdateSetting(r.Context(), "holiday_country", country, u.ID); err != nil {
			http.Error(w, "update holiday_country: "+err.Error(), http.StatusInternalServerError)
			return
		}
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

// --- roster ---

func (h *handlers) adminEditMemberPage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	targetID := chi.URLParam(r, "id")
	member, err := h.store.GetMember(r.Context(), targetID)
	if err != nil {
		http.Error(w, "load member: "+err.Error(), http.StatusInternalServerError)
		return
	}
	roles, err := h.store.ListRoles(r.Context())
	if err != nil {
		http.Error(w, "load roles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := buildMemberSettingsData(u, false, targetID, member, roles)
	if err := view.MemberSettingsPage(data).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "adminEditMemberPage", "err", err)
	}
}

func (h *handlers) adminCreateRosterMember(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	displayName := r.FormValue("display_name")
	if displayName == "" {
		displayName = id
	}
	if err := h.store.UpsertRosterMember(r.Context(), id, displayName, true, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *handlers) adminUpdateMember(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	targetID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	displayName := r.FormValue("display_name")
	active := r.FormValue("active") == "true"
	wd, err := parseWorkingDays(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	roleIDs, err := h.parseRoleIDs(r.Context(), r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.store.UpsertRosterMember(r.Context(), targetID, displayName, active, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.store.UpdateMemberSettings(r.Context(), targetID, wd, roleIDs, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// --- roles ---

func (h *handlers) adminCreateRole(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	minPresent, err := strconv.Atoi(r.FormValue("min_present"))
	if err != nil {
		http.Error(w, "invalid min_present", http.StatusBadRequest)
		return
	}
	if _, err := h.store.CreateRole(r.Context(), name, minPresent, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *handlers) adminUpdateRole(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	minPresent, err := strconv.Atoi(r.FormValue("min_present"))
	if err != nil {
		http.Error(w, "invalid min_present", http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateRole(r.Context(), absence.RoleID(id), name, minPresent, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *handlers) adminDeleteRole(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteRole(r.Context(), absence.RoleID(id), u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// --- holidays ---

func (h *handlers) adminAddHoliday(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", r.FormValue("date"))
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if _, err := h.store.CreateManualHoliday(r.Context(), date, name, u.ID); err != nil {
		if errors.Is(err, store.ErrDuplicateHoliday) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *handlers) adminDeleteHoliday(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	date, err := time.Parse("2006-01-02", chi.URLParam(r, "date"))
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteHoliday(r.Context(), date, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *handlers) adminSyncHolidays(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	settings, err := h.store.GetSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if settings.HolidayCountry == "" {
		http.Error(w, "holiday_country not configured", http.StatusUnprocessableEntity)
		return
	}
	year := time.Now().Year()
	if v := r.FormValue("year"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			year = n
		}
	}
	rows, err := holidaysync.Sync(settings.HolidayCountry, year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if _, err := h.store.ImportHolidays(r.Context(), rows, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

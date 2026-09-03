package server

import (
	"log/slog"
	"net/http"

	"github.com/alveel/quorum/internal/auth"
	"github.com/alveel/quorum/internal/view"
)

func (h *handlers) memberSettings(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	member, err := h.store.GetMember(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "load member: "+err.Error(), http.StatusInternalServerError)
		return
	}
	roles, err := h.store.ListRoles(r.Context())
	if err != nil {
		http.Error(w, "load roles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := buildMemberSettingsData(u, true, u.ID, member, roles)
	if err := view.MemberSettingsPage(data).Render(r.Context(), w); err != nil {
		slog.Debug("render", "handler", "memberSettings", "err", err)
	}
}

// updateMemberSettings always writes to the authenticated caller's own row. The target id
// is deliberately never read from the submitted form — a form field would let a member
// post someone else's id and edit their settings (IDOR).
func (h *handlers) updateMemberSettings(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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
	if err := h.store.UpdateMemberSettings(r.Context(), u.ID, wd, roleIDs, u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

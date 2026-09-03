package server

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/alveel/quorum/internal/auth"
	"github.com/alveel/quorum/internal/config"
	"github.com/alveel/quorum/internal/locale"
)

func New(cfg config.Config, st Storer, staticFS fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health probes — no auth required.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Static assets — FS is already sub-rooted at "static/", so strip the URL prefix.
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Language switching — no auth required.
	r.Get("/lang/{code}", locale.SetLang)

	h := &handlers{cfg: cfg, store: st}

	// All other routes require authentication.
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(cfg))
		r.Use(locale.Middleware)
		r.Use(upsertUserMiddleware(st))

		r.Get("/", h.index)
		r.Get("/day/{date}", h.dayDetail)
		r.Post("/absences", h.createAbsence)
		r.Delete("/absences/{id}", h.cancelAbsence)

		r.Get("/settings", h.memberSettings)
		r.Post("/settings", h.updateMemberSettings)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAdmin)
			r.Get("/admin", h.adminPage)
			r.Post("/admin/settings", h.adminSettings)
			r.Post("/admin/override", h.adminOverride)

			r.Get("/admin/roster/{id}", h.adminEditMemberPage)
			r.Post("/admin/roster", h.adminCreateRosterMember)
			r.Post("/admin/roster/{id}", h.adminUpdateMember)

			r.Post("/admin/roles", h.adminCreateRole)
			r.Post("/admin/roles/{id}", h.adminUpdateRole)
			r.Post("/admin/roles/{id}/delete", h.adminDeleteRole)

			r.Post("/admin/holidays", h.adminAddHoliday)
			r.Post("/admin/holidays/{date}/delete", h.adminDeleteHoliday)
			r.Post("/admin/holidays/sync", h.adminSyncHolidays)
		})
	})

	return r
}

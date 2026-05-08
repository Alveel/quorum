package server

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/alveel/vacation-coverage/internal/auth"
	"github.com/alveel/vacation-coverage/internal/config"
	"github.com/alveel/vacation-coverage/internal/locale"
)

func New(cfg config.Config, st Storer, staticFS fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
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
		r.Post("/vacations", h.createVacation)
		r.Delete("/vacations/{id}", h.cancelVacation)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAdmin)
			r.Get("/admin", h.adminPage)
			r.Post("/admin/settings", h.adminSettings)
			r.Post("/admin/override", h.adminOverride)
		})
	})

	return r
}

package server

import (
	"log/slog"
	"net/http"

	"github.com/alveel/vacation-coverage/internal/auth"
)

func upsertUserMiddleware(st Storer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.FromContext(r.Context())
			if err := st.UpsertUser(r.Context(), u.ID, u.Email, u.ID); err != nil {
				slog.Error("upsertUser failed", "user", u.ID, "err", err)
			}
			next.ServeHTTP(w, r)
		})
	}
}

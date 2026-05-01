package server

import (
	"net/http"

	"github.com/alveel/vacation-coverage/internal/auth"
	"github.com/alveel/vacation-coverage/internal/store"
)

func upsertUserMiddleware(st *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.FromContext(r.Context())
			_ = st.UpsertUser(r.Context(), u.ID, u.Email, u.ID)
			next.ServeHTTP(w, r)
		})
	}
}

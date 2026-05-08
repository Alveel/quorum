package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/alveel/quorum/internal/config"
)

type User struct {
	ID     string
	Email  string
	Groups []string
	Admin  bool
}

type contextKey struct{}

// Middleware injects an auth.User into the context from oauth-proxy forwarded headers.
// In dev (DEV_AUTH_BYPASS=true), synthesizes identity from DEV_USER / DEV_ADMIN env vars.
func Middleware(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var u User
			if cfg.DevAuthBypass {
				u = User{
					ID:    cfg.DevUser,
					Email: cfg.DevUser + "@dev.local",
					Admin: cfg.DevAdmin,
				}
			} else {
				u = User{
					ID:     r.Header.Get("X-Forwarded-User"),
					Email:  r.Header.Get("X-Forwarded-Email"),
					Groups: splitTrim(r.Header.Get("X-Forwarded-Groups"), ","),
				}
				u.Admin = isAdmin(u.Groups, cfg.AdminGroups)
			}
			if u.ID == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), contextKey{}, u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func FromContext(ctx context.Context) User {
	u, _ := ctx.Value(contextKey{}).(User)
	return u
}

// RequireAdmin is a middleware that returns 403 for non-admins.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !FromContext(r.Context()).Admin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAdmin(userGroups, adminGroups []string) bool {
	for _, ug := range userGroups {
		for _, ag := range adminGroups {
			if ug == ag {
				return true
			}
		}
	}
	return false
}

func splitTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

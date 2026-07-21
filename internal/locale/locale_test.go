package locale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// findCookie returns the value of the named Set-Cookie, or "" if absent.
func findCookie(resp *http.Response, name string) string {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestSetLang(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		referer    string
		wantCookie string
		wantLoc    string
	}{
		{"valid nl", "/lang/nl", "/admin", "nl", "/admin"},
		{"valid en", "/lang/en", "/", "en", "/"},
		{"invalid falls back to en", "/lang/xx", "/", "en", "/"},
		{"empty referer redirects to root", "/lang/nl", "", "nl", "/"},
	}
	// Route through chi so chi.URLParam(r, "code") resolves.
	router := chi.NewRouter()
	router.Get("/lang/{code}", SetLang)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.path, nil)
			if tt.referer != "" {
				r.Header.Set("Referer", tt.referer)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)

			resp := w.Result()
			if got := findCookie(resp, "lang"); got != tt.wantCookie {
				t.Errorf("cookie lang = %q, want %q", got, tt.wantCookie)
			}
			if got := resp.Header.Get("Location"); got != tt.wantLoc {
				t.Errorf("Location = %q, want %q", got, tt.wantLoc)
			}
			if resp.StatusCode != http.StatusSeeOther {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
			}
		})
	}
}

func TestMiddlewareLang(t *testing.T) {
	Init()
	tests := []struct {
		name   string
		cookie string
		want   string
	}{
		{"nl cookie", "nl", "nl"},
		{"no cookie", "", "en"},
		{"garbage cookie", "fr", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = Lang(r.Context())
			}))
			r := httptest.NewRequest("GET", "/", nil)
			if tt.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "lang", Value: tt.cookie})
			}
			h.ServeHTTP(httptest.NewRecorder(), r)
			if got != tt.want {
				t.Errorf("Lang = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	// 2026-06-22 is a Monday.
	d := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	ctx := func(lang string) context.Context {
		return context.WithValue(context.Background(), langKey{}, lang)
	}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"full en", FormatDate(ctx("en"), d), "Mon 22 Jun 2026"},
		{"full nl", FormatDate(ctx("nl"), d), "ma 22 jun 2026"},
		{"short en", FormatDateShort(ctx("en"), d), "22 Jun"},
		{"short nl", FormatDateShort(ctx("nl"), d), "22 jun"},
		{"medium en", FormatDateMedium(ctx("en"), d), "22 Jun 2026"},
		{"medium nl", FormatDateMedium(ctx("nl"), d), "22 jun 2026"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("= %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestTPPlural(t *testing.T) {
	Init()
	tests := []struct {
		count int
		want  string
	}{
		{1, "on 1 day"},
		{2, "on 2 days"},
	}
	for _, tt := range tests {
		var got string
		h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = TP(r.Context(), "err_coverage", tt.count, map[string]any{
				"Min": 8, "Count": tt.count,
			})
		}))
		r := httptest.NewRequest("GET", "/", nil) // no cookie → en
		h.ServeHTTP(httptest.NewRecorder(), r)
		if !strings.Contains(got, tt.want) {
			t.Errorf("count=%d: %q missing %q", tt.count, got, tt.want)
		}
	}
}

func TestTranslates(t *testing.T) {
	Init()
	for _, tt := range []struct{ lang, want string }{
		{"en", "Admin"},
		{"nl", "Beheer"},
	} {
		var got string
		h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = T(r.Context(), "nav_admin")
		}))
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: "lang", Value: tt.lang})
		h.ServeHTTP(httptest.NewRecorder(), r)
		if got != tt.want {
			t.Errorf("T(nav_admin) lang=%s = %q, want %q", tt.lang, got, tt.want)
		}
	}
}

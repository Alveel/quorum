package locale

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFS embed.FS

type localizerKey struct{}
type langKey struct{}

var bundle *i18n.Bundle

func Init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	if _, err := bundle.LoadMessageFileFS(localeFS, "locales/en.json"); err != nil {
		panic("locale: load en.json: " + err.Error())
	}
	if _, err := bundle.LoadMessageFileFS(localeFS, "locales/nl.json"); err != nil {
		panic("locale: load nl.json: " + err.Error())
	}
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bundle == nil {
			next.ServeHTTP(w, r)
			return
		}
		lang := "en"
		if c, err := r.Cookie("lang"); err == nil && c.Value == "nl" {
			lang = "nl"
		}
		loc := i18n.NewLocalizer(bundle, lang)
		ctx := context.WithValue(r.Context(), localizerKey{}, loc)
		ctx = context.WithValue(ctx, langKey{}, lang)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// T returns the translated message for messageID. Falls back to messageID on error.
func T(ctx context.Context, messageID string, data ...map[string]any) string {
	loc, _ := ctx.Value(localizerKey{}).(*i18n.Localizer)
	if loc == nil {
		return messageID
	}
	cfg := &i18n.LocalizeConfig{MessageID: messageID}
	if len(data) > 0 {
		cfg.TemplateData = data[0]
	}
	s, err := loc.Localize(cfg)
	if err != nil {
		return messageID
	}
	return s
}

// TP is like T but selects one/other plural form based on count.
func TP(ctx context.Context, messageID string, count int, data map[string]any) string {
	loc, _ := ctx.Value(localizerKey{}).(*i18n.Localizer)
	if loc == nil {
		return messageID
	}
	s, err := loc.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		PluralCount:  count,
		TemplateData: data,
	})
	if err != nil {
		return messageID
	}
	return s
}

// SetLang handles GET /lang/{code} — sets lang cookie, redirects back.
func SetLang(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code != "nl" {
		code = "en"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "lang",
		Value:    code,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
	ref := r.Referer()
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

// FormatDate formats t as a full date string (weekday + day + month + year) per active language.
func FormatDate(ctx context.Context, t time.Time) string {
	if lang, _ := ctx.Value(langKey{}).(string); lang == "nl" {
		return fmt.Sprintf("%s %02d %s %d", nlDays[t.Weekday()], t.Day(), nlMonths[t.Month()], t.Year())
	}
	return t.Format("Mon 02 Jan 2006")
}

// FormatDateShort formats t as day + month only (no weekday, no year) per active language.
func FormatDateShort(ctx context.Context, t time.Time) string {
	if lang, _ := ctx.Value(langKey{}).(string); lang == "nl" {
		return fmt.Sprintf("%02d %s", t.Day(), nlMonths[t.Month()])
	}
	return t.Format("02 Jan")
}

// FormatDateMedium formats t as day + month + year (no weekday) per active language.
func FormatDateMedium(ctx context.Context, t time.Time) string {
	if lang, _ := ctx.Value(langKey{}).(string); lang == "nl" {
		return fmt.Sprintf("%02d %s %d", t.Day(), nlMonths[t.Month()], t.Year())
	}
	return t.Format("02 Jan 2006")
}

var nlDays = [...]string{"zo", "ma", "di", "wo", "do", "vr", "za"}
var nlMonths = [...]string{"", "jan", "feb", "mrt", "apr", "mei", "jun",
	"jul", "aug", "sep", "okt", "nov", "dec"}

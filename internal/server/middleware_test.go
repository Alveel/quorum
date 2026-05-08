package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alveel/quorum/internal/auth"
	"github.com/alveel/quorum/internal/config"
)

// injectUser wraps a handler with auth.Middleware in dev bypass mode.
func injectUser(cfg config.Config, next http.Handler) http.Handler {
	return auth.Middleware(cfg)(next)
}

func TestUpsertUserMiddleware_CallsUpsertWithCorrectArgs(t *testing.T) {
	st := &fakeStore{}
	cfg := config.Config{DevAuthBypass: true, DevUser: "alice", DevAdmin: false}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := injectUser(cfg, upsertUserMiddleware(st)(inner))

	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !st.upsertCalled {
		t.Fatal("UpsertUser was not called")
	}
	if st.upsertID != "alice" {
		t.Errorf("UpsertUser ID: want alice, got %q", st.upsertID)
	}
}

func TestUpsertUserMiddleware_ContinuesOnStoreError(t *testing.T) {
	st := &fakeStore{upsertErr: errors.New("db down")}
	cfg := config.Config{DevAuthBypass: true, DevUser: "alice"}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := injectUser(cfg, upsertUserMiddleware(st)(inner))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler not called after UpsertUser error")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

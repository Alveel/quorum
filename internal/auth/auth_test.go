package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alveel/vacation-coverage/internal/config"
)

func TestMiddleware_DevBypass(t *testing.T) {
	cfg := config.Config{DevAuthBypass: true, DevUser: "alice", DevAdmin: true}
	called := false
	h := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if u.ID != "alice" {
			t.Errorf("want ID=alice, got %q", u.ID)
		}
		if !u.Admin {
			t.Error("want Admin=true")
		}
		called = true
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("handler not called")
	}
}

func TestMiddleware_ForwardedHeaders(t *testing.T) {
	cfg := config.Config{AdminGroups: []string{"ops"}}
	called := false
	h := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if u.ID != "bob" {
			t.Errorf("want ID=bob, got %q", u.ID)
		}
		if u.Email != "bob@example.com" {
			t.Errorf("want email=bob@example.com, got %q", u.Email)
		}
		if !u.Admin {
			t.Error("want Admin=true (bob is in ops group)")
		}
		called = true
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-User", "bob")
	req.Header.Set("X-Forwarded-Email", "bob@example.com")
	req.Header.Set("X-Forwarded-Groups", "devs, ops, readers")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatal("handler not called")
	}
}

func TestMiddleware_NoIdentity_Returns401(t *testing.T) {
	cfg := config.Config{}
	h := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when no identity")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

func TestMiddleware_DevBypassRefusesWithoutExplicitTrue(t *testing.T) {
	// DevAuthBypass=false even with DevUser set — must not bypass.
	cfg := config.Config{DevAuthBypass: false, DevUser: "mallory"}
	h := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401 when bypass disabled, got %d", rec.Code)
	}
}

func TestRequireAdmin_Allows(t *testing.T) {
	cfg := config.Config{DevAuthBypass: true, DevUser: "admin", DevAdmin: true}
	called := false
	h := Middleware(cfg)(RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("admin handler not called")
	}
}

func TestRequireAdmin_Blocks(t *testing.T) {
	cfg := config.Config{DevAuthBypass: true, DevUser: "user", DevAdmin: false}
	h := Middleware(cfg)(RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("non-admin should be blocked")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rec.Code)
	}
}

func TestIsAdmin_GroupIntersection(t *testing.T) {
	cases := []struct {
		userGroups  []string
		adminGroups []string
		want        bool
	}{
		{[]string{"devs", "ops"}, []string{"ops"}, true},
		{[]string{"devs"}, []string{"ops"}, false},
		{nil, []string{"ops"}, false},
		{[]string{"ops"}, nil, false},
	}
	for _, c := range cases {
		got := isAdmin(c.userGroups, c.adminGroups)
		if got != c.want {
			t.Errorf("isAdmin(%v, %v) = %v, want %v", c.userGroups, c.adminGroups, got, c.want)
		}
	}
}

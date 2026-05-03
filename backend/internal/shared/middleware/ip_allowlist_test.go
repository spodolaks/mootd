package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type silentLogger struct{}

func (silentLogger) Printf(string, ...any) {}

func TestAdminIPAllowlist_AllowAllWhenEmpty(t *testing.T) {
	mw := AdminIPAllowlist(nil, silentLogger{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	r.RemoteAddr = "203.0.113.42:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Fatal("handler should have been invoked when allowlist is empty")
	}
}

func TestAdminIPAllowlist_DenyOutside(t *testing.T) {
	mw := AdminIPAllowlist([]string{"10.0.0.0/8"}, silentLogger{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should NOT have been invoked")
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	r.RemoteAddr = "203.0.113.42:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAdminIPAllowlist_AllowInside(t *testing.T) {
	mw := AdminIPAllowlist([]string{"10.0.0.0/8"}, silentLogger{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	r.RemoteAddr = "10.5.6.7:54321"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Errorf("expected handler to fire for 10.5.6.7, got status %d", w.Code)
	}
}

func TestAdminIPAllowlist_BareIPUpgraded(t *testing.T) {
	mw := AdminIPAllowlist([]string{"203.0.113.42"}, silentLogger{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	r.RemoteAddr = "203.0.113.42:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Error("bare IP should have been upgraded to /32 and matched")
	}
}

func TestAdminIPAllowlist_HonoursXForwardedFor(t *testing.T) {
	mw := AdminIPAllowlist([]string{"10.0.0.0/8"}, silentLogger{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	// Caddy front-end at a public IP, real client behind it.
	r.RemoteAddr = "172.16.0.1:8080"
	r.Header.Set("X-Forwarded-For", "10.5.6.7, 172.16.0.1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Errorf("XFF left-most should have matched 10.5.6.7, got %d", w.Code)
	}
}

func TestAdminIPAllowlist_DropsUnparseable(t *testing.T) {
	// Bad entry should be ignored; remaining valid entry still works.
	mw := AdminIPAllowlist([]string{"not-a-cidr", "10.0.0.0/8"}, silentLogger{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	r.RemoteAddr = "10.5.6.7:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Errorf("expected match on 10.0.0.0/8 after dropping bad entry, got status %d", w.Code)
	}
}

func TestAdminIPAllowlist_IPv6(t *testing.T) {
	mw := AdminIPAllowlist([]string{"::1/128"}, silentLogger{})
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	r.RemoteAddr = "[::1]:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Errorf("IPv6 loopback should have matched, got status %d", w.Code)
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mootd/backend/internal/admin"
	jwtutil "mootd/backend/internal/shared/jwt"
)

const adminTestSecret = "admin-test-secret-at-least-32-chars-long!"
const userTestSecret = "user-test-secret-at-least-32-chars-long!!"

func okHandler(seen *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := admin.AdminIDFromContext(r.Context()); ok {
			*seen = id
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAdminAuth_AcceptsValidToken(t *testing.T) {
	token, err := admin.GenerateToken("adm_1", []string{"admin"}, true, adminTestSecret, 5*time.Minute)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var seen string
	h := RequireAdminAuth(adminTestSecret)(okHandler(&seen))

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if seen != "adm_1" {
		t.Errorf("admin ID not propagated: got %q", seen)
	}
}

func TestRequireAdminAuth_RejectsMissingHeader(t *testing.T) {
	h := RequireAdminAuth(adminTestSecret)(okHandler(new(string)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestRequireAdminAuth_RejectsNonBearer(t *testing.T) {
	h := RequireAdminAuth(adminTestSecret)(okHandler(new(string)))
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil)
	req.Header.Set("Authorization", "Basic abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestRequireAdminAuth_RejectsUserToken(t *testing.T) {
	// Issue a user-side token (iss="mootd") with the user secret — this
	// is the exact thing the separation is meant to prevent.
	userToken, err := jwtutil.GenerateToken("user_1", "user@example.com", userTestSecret, 5*time.Minute)
	if err != nil {
		t.Fatalf("user token gen: %v", err)
	}
	h := RequireAdminAuth(adminTestSecret)(okHandler(new(string)))
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401 — user token must NOT pass admin middleware", rec.Code)
	}
}

func TestRequireAdminAuth_RejectsWrongSecret(t *testing.T) {
	// Token signed with one admin secret but validated under another.
	badToken, err := admin.GenerateToken("adm_1", []string{"admin"}, false, "different-secret-at-least-32-chars!", 5*time.Minute)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	h := RequireAdminAuth(adminTestSecret)(okHandler(new(string)))
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+badToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestRequireAdminAuth_RejectsExpired(t *testing.T) {
	token, err := admin.GenerateToken("adm_1", []string{"admin"}, false, adminTestSecret, -time.Minute)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	h := RequireAdminAuth(adminTestSecret)(okHandler(new(string)))
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401 (expired token must be rejected)", rec.Code)
	}
}

func TestRequireAdminAuth_ResponseBody(t *testing.T) {
	h := RequireAdminAuth(adminTestSecret)(okHandler(new(string)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) {
		t.Errorf("expected JSON error body, got %q", body)
	}
}

func TestAdminContextAccessors(t *testing.T) {
	token, _ := admin.GenerateToken("adm_42", []string{"admin", "engineer"}, true, adminTestSecret, 5*time.Minute)
	var seenID string
	var seenRoles []string
	var seenMFA bool
	h := RequireAdminAuth(adminTestSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenID, _ = admin.AdminIDFromContext(r.Context())
		seenRoles, _ = admin.AdminRolesFromContext(r.Context())
		seenMFA = admin.AdminMFAVerifiedFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seenID != "adm_42" {
		t.Errorf("id: got %q", seenID)
	}
	if len(seenRoles) != 2 {
		t.Errorf("roles: got %v", seenRoles)
	}
	if !seenMFA {
		t.Errorf("mfa: got false, want true")
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwtutil "mootd/backend/internal/shared/jwt"
)

const testSecret = "test-secret-for-middleware-tests-min32!"

func TestAuth_ValidToken(t *testing.T) {
	token, err := jwtutil.GenerateToken("user42", "u@test.com", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	var capturedUserID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := UserIDFromContext(r.Context())
		if !ok {
			t.Error("UserIDFromContext returned false")
		}
		capturedUserID = uid
		w.WriteHeader(http.StatusOK)
	})

	handler := Auth(testSecret)(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedUserID != "user42" {
		t.Errorf("userID = %q, want %q", capturedUserID, "user42")
	}
}

func TestAuth_MissingHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	handler := Auth(testSecret)(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	handler := Auth(testSecret)(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-here")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuth_ExpiredToken(t *testing.T) {
	token, err := jwtutil.GenerateToken("user42", "u@test.com", testSecret, -time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	handler := Auth(testSecret)(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

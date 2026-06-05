package user

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"mootd/backend/internal/shared/middleware"
)

func authedDelete(userID string) *http.Request {
	r := httptest.NewRequest(http.MethodDelete, "/v1/user/profile", nil)
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	return r.WithContext(ctx)
}

// deleteAccount only touches the cascade + request context, never the repo, so
// a nil repo is sufficient here.
func testHandler(cascade CascadeFn) *Handler {
	return NewHandler(log.New(io.Discard, "", 0), nil, cascade)
}

func TestDeleteAccount_Success(t *testing.T) {
	var gotUser string
	h := testHandler(func(_ context.Context, userID string) error {
		gotUser = userID
		return nil
	})
	rec := httptest.NewRecorder()
	h.deleteAccount(rec, authedDelete("u1"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if gotUser != "u1" {
		t.Errorf("cascade user = %q, want u1", gotUser)
	}
}

func TestDeleteAccount_CascadeNotConfigured(t *testing.T) {
	h := testHandler(nil)
	rec := httptest.NewRecorder()
	h.deleteAccount(rec, authedDelete("u1"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDeleteAccount_CascadeError(t *testing.T) {
	h := testHandler(func(_ context.Context, _ string) error {
		return errors.New("boom")
	})
	rec := httptest.NewRecorder()
	h.deleteAccount(rec, authedDelete("u1"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestDeleteAccount_Unauthorized(t *testing.T) {
	called := false
	h := testHandler(func(_ context.Context, _ string) error {
		called = true
		return nil
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/user/profile", nil) // no UserIDKey
	h.deleteAccount(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Error("cascade ran for an unauthenticated request")
	}
}

// TestDeleteAccount_DecoupledFromRequestCancellation proves the #96 fix: a
// client disconnect must not abort erasure. We cancel the request context up
// front; the cascade must still run on a live (non-cancelled) context.
func TestDeleteAccount_DecoupledFromRequestCancellation(t *testing.T) {
	var ctxErrAtCall error
	called := false
	h := testHandler(func(ctx context.Context, _ string) error {
		called = true
		ctxErrAtCall = ctx.Err()
		return nil
	})

	req := authedDelete("u1")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.deleteAccount(rec, req)

	if !called {
		t.Fatal("cascade was not called")
	}
	if ctxErrAtCall != nil {
		t.Errorf("cascade ran on a cancelled context (%v); erasure must be decoupled from the request (#96)", ctxErrAtCall)
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

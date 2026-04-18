package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_GeneratesWhenHeaderMissing(t *testing.T) {
	var gotID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID == "" {
		t.Fatal("expected a generated request ID, got empty")
	}
	if rec.Header().Get(RequestIDHeader) != gotID {
		t.Errorf("response header = %q, want %q", rec.Header().Get(RequestIDHeader), gotID)
	}
}

func TestRequestID_HonoursInboundHeader(t *testing.T) {
	var gotID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "client-abc-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID != "client-abc-123" {
		t.Errorf("context ID = %q, want %q", gotID, "client-abc-123")
	}
	if rec.Header().Get(RequestIDHeader) != "client-abc-123" {
		t.Errorf("response header = %q, want inbound value echoed", rec.Header().Get(RequestIDHeader))
	}
}

func TestRequestID_RejectsOversizedInboundHeader(t *testing.T) {
	var gotID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	huge := strings.Repeat("a", 10_000)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, huge)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID == "" || gotID == huge {
		t.Fatalf("expected a fresh request ID for oversized input, got %q (len=%d)", gotID, len(gotID))
	}
	if len(gotID) > 64 {
		t.Errorf("generated ID len=%d, want <=64", len(gotID))
	}
}

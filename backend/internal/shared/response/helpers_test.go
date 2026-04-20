package response

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if body := rec.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", body, `{"status":"ok"}`)
	}
}

func TestDecodeJSONBody_Valid(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err := DecodeJSONBody(rec, req, &dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dst.Name != "test" {
		t.Errorf("name = %q, want %q", dst.Name, "test")
	}
}

func TestDecodeJSONBody_EmptyBody(t *testing.T) {
	var dst struct{ Name string }

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	rec := httptest.NewRecorder()

	err := DecodeJSONBody(rec, req, &dst)
	if err == nil || err.Error() != "empty request body" {
		t.Errorf("err = %v, want 'empty request body'", err)
	}
}

func TestDecodeJSONBody_InvalidJSON(t *testing.T) {
	var dst struct{ Name string }

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()

	err := DecodeJSONBody(rec, req, &dst)
	if err == nil || err.Error() != "invalid json body" {
		t.Errorf("err = %v, want 'invalid json body'", err)
	}
}

func TestDecodeJSONBody_OversizedBody(t *testing.T) {
	// Generate a body larger than 1MB
	bigBody := `{"name":"` + strings.Repeat("x", 2<<20) + `"}`
	var dst struct{ Name string }

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(bigBody))
	rec := httptest.NewRecorder()

	err := DecodeJSONBody(rec, req, &dst)
	if err == nil {
		t.Error("expected error for oversized body, got nil")
	}
}

func TestDecodeJSONBodyWithLimit_AcceptsUnderCap(t *testing.T) {
	// Body that would exceed the default 1 MiB cap but fits within a
	// caller-supplied 4 MiB cap — simulates the moodboard save with a
	// rendered collage PNG.
	bigValue := strings.Repeat("x", 2<<20) // 2 MiB string
	body := `{"payload":"` + bigValue + `"}`
	var dst struct{ Payload string }

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	if err := DecodeJSONBodyWithLimit(rec, req, &dst, 4<<20); err != nil {
		t.Fatalf("unexpected error under raised cap: %v", err)
	}
	if len(dst.Payload) != len(bigValue) {
		t.Errorf("decoded length = %d, want %d", len(dst.Payload), len(bigValue))
	}
}

func TestDecodeJSONBodyWithLimit_RejectsOverCap(t *testing.T) {
	body := `{"payload":"` + strings.Repeat("x", 4<<20) + `"}` // 4 MiB content
	var dst struct{ Payload string }

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	// Cap at 2 MiB — the 4 MiB body must be rejected.
	if err := DecodeJSONBodyWithLimit(rec, req, &dst, 2<<20); err == nil {
		t.Error("expected error for body exceeding custom cap, got nil")
	}
}

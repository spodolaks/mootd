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

package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecover_CatchesPanicAndReturns500(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	handler := Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/kaboom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("body = %q, want JSON error", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "PANIC") || !strings.Contains(buf.String(), "boom") {
		t.Errorf("log = %q, want PANIC + boom", buf.String())
	}
}

func TestRecover_IncludesRequestIDInLog(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	// RequestID must sit outside Recover so the context is populated before the panic.
	handler := RequestID(Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})))

	req := httptest.NewRequest(http.MethodGet, "/kaboom", nil)
	req.Header.Set(RequestIDHeader, "req-xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(buf.String(), "reqID=req-xyz") {
		t.Errorf("log = %q, want reqID=req-xyz", buf.String())
	}
}

func TestRecover_NoPanicIsPassthrough(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	handler := Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if buf.Len() != 0 {
		t.Errorf("log should be empty on happy path, got %q", buf.String())
	}
}

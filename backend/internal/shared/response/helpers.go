// Package response provides shared HTTP response helpers used across all domain handlers.
package response

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// WriteJSON encodes payload as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.Printf("response: write JSON body failed (status=%d, %d bytes): %v", status, len(body), err)
	}
}

// WriteJSONErr writes an error response with the canonical
// {error, requestId} shape (mootd#38). The requestId echoes the
// X-Request-ID response header so a customer-supplied "the app
// crashed at 14:23" report joins instantly to the matching log
// line.
//
// Prefer this over `WriteJSON(w, status, map[string]string{
// "error": ...})` for new code. Existing call sites can stay on
// WriteJSON until touched — every response also carries the
// X-Request-ID header (set by the RequestID middleware) so
// correlation works either way.
//
// We read the request ID off the response writer's headers
// (set unconditionally by the RequestID middleware earlier in
// the chain) rather than the request context, which would
// create an import cycle with shared/middleware.
//
// `extra` lets callers attach error-specific fields (e.g.
// missingPermission, requireMfa). nil is fine.
func WriteJSONErr(w http.ResponseWriter, status int, message string, extra map[string]any) {
	body := map[string]any{"error": message}
	if reqID := w.Header().Get("X-Request-ID"); reqID != "" {
		body["requestId"] = reqID
	}
	for k, v := range extra {
		body[k] = v
	}
	WriteJSON(w, status, body)
}

// DefaultMaxBodyBytes is the ceiling applied by DecodeJSONBody. Sized
// deliberately small — most endpoints accept a handful of fields and don't
// need more than this to do their job. Endpoints that legitimately carry
// large payloads (e.g. moodboard save, which ships a base64 PNG render of
// the collage) must use DecodeJSONBodyWithLimit and declare their own cap.
const DefaultMaxBodyBytes int64 = 1 << 20

// DecodeJSONBody decodes a JSON request body into dst, enforcing the default
// 1 MiB cap. It disallows unknown fields, rejects empty bodies, and requires
// exactly one JSON object.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	return DecodeJSONBodyWithLimit(w, r, dst, DefaultMaxBodyBytes)
}

// DecodeJSONBodyWithLimit is DecodeJSONBody with a caller-supplied byte cap.
// Use it on endpoints that accept large payloads (image uploads, bulk writes)
// so the tight default stays in force everywhere else — an oversized body
// hits the MaxBytesReader before any handler logic runs, which is why a too-
// small cap surfaces as an opaque 400 with no handler-level logging.
func DecodeJSONBodyWithLimit(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	limitedBody := http.MaxBytesReader(w, r.Body, maxBytes)
	defer limitedBody.Close()

	dec := json.NewDecoder(limitedBody)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty request body")
		}
		return errors.New("invalid json body")
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}

	return nil
}

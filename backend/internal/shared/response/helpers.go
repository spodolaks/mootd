// Package response provides shared HTTP response helpers used across all domain handlers.
package response

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// ErrorCode is a stable, machine-readable error code (mootd#41).
// Clients (RN app, admin frontend) switch on this — never on
// the human-readable `error` message — to decide retry / display
// behaviour. Adding a new code is a non-breaking change; renaming
// or removing one breaks every client and shouldn't ship without
// a contract review.
type ErrorCode string

const (
	// CodeInvalidToken — JWT missing, malformed, or expired
	// (and refresh path didn't recover).
	CodeInvalidToken ErrorCode = "INVALID_TOKEN"

	// CodeMissingField — request body missing a required field
	// or violating a basic shape constraint.
	CodeMissingField ErrorCode = "MISSING_FIELD"

	// CodeInvalidInput — request validated structurally but
	// values are out of range / disallowed.
	CodeInvalidInput ErrorCode = "INVALID_INPUT"

	// CodeQuotaExceeded — per-user budget cap hit
	// (mootd-admin#30). Carries `retryAfter` (seconds).
	CodeQuotaExceeded ErrorCode = "QUOTA_EXCEEDED"

	// CodeRateLimited — per-IP or per-user rate limiter
	// triggered. Distinct from QuotaExceeded: this is
	// short-window protection, not a daily/monthly cap.
	CodeRateLimited ErrorCode = "RATE_LIMITED"

	// CodeUpstreamTimeout — LLM / detection service didn't
	// respond inside the per-call deadline.
	CodeUpstreamTimeout ErrorCode = "UPSTREAM_TIMEOUT"

	// CodeUpstreamError — upstream returned 5xx / unparseable
	// payload. Distinguishes transport failure from valid
	// upstream "this won't work" responses.
	CodeUpstreamError ErrorCode = "UPSTREAM_ERROR"

	// CodeNotFound — entity does not exist or caller doesn't
	// own it (we 404 owner-mismatch as a privacy hardening).
	CodeNotFound ErrorCode = "NOT_FOUND"

	// CodeForbidden — auth succeeded but the action is denied
	// (RBAC, ownership, MFA-required, etc).
	CodeForbidden ErrorCode = "FORBIDDEN"

	// CodeConflict — write would violate a uniqueness/state
	// constraint (e.g. starting a 2nd active A/B test).
	CodeConflict ErrorCode = "CONFLICT"

	// CodeServiceUnavailable — a soft-dependency isn't wired
	// (e.g. async outfit gen requested without Redis). Distinct
	// from UpstreamError: this is misconfiguration, not a flaky
	// dependency.
	CodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"

	// CodeInternal — anything not classified above. Surface in
	// logs with full detail; keep the wire message generic.
	CodeInternal ErrorCode = "INTERNAL"
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
// missingPermission, requireMfa, retryAfter). nil is fine.
func WriteJSONErr(w http.ResponseWriter, status int, message string, extra map[string]any) {
	WriteJSONErrWithCode(w, status, "", message, extra)
}

// WriteJSONErrWithCode is WriteJSONErr with the canonical
// {error, code, requestId} shape (mootd#41). The code field is
// stable across UI copy changes — clients switch on it for
// retry / display decisions. Pass empty code "" to omit it
// (matches WriteJSONErr's behaviour).
//
// Migration policy: new error sites MUST pass a code. Existing
// WriteJSON({error}) sites can be migrated when touched; the
// requestId header still works for correlation either way.
func WriteJSONErrWithCode(w http.ResponseWriter, status int, code ErrorCode, message string, extra map[string]any) {
	body := map[string]any{"error": message}
	if code != "" {
		body["code"] = string(code)
	}
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

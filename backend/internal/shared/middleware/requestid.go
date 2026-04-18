package middleware

import (
	"context"
	"net/http"

	"mootd/backend/internal/shared/id"
)

// RequestIDKey is the context key under which the per-request correlation ID is stored.
const RequestIDKey contextKey = "requestID"

// RequestIDHeader is the HTTP header used to accept an inbound request ID and
// emit the chosen one back to the caller.
const RequestIDHeader = "X-Request-ID"

// RequestID attaches a correlation ID to every request. When the caller passes
// one via the X-Request-ID header it is honoured (subject to a length cap to
// keep log lines bounded); otherwise a fresh UUIDv4 is generated. The ID is
// stored in the request context and echoed back in the response header so
// clients and server-side logs can be joined.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(RequestIDHeader)
		// Cap to 64 chars so a malicious client can't poison logs with megabyte IDs.
		if reqID == "" || len(reqID) > 64 {
			reqID = id.Generate()
		}
		w.Header().Set(RequestIDHeader, reqID)
		ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext extracts the correlation ID from the request context.
// Returns an empty string when no ID is set (e.g. middleware not wired).
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(RequestIDKey).(string); ok {
		return v
	}
	return ""
}

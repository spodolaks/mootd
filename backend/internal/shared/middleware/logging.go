// Package middleware contains shared HTTP middleware used across all domain routes.
package middleware

import (
	"log"
	"net/http"
	"time"
)

// statusRecorder wraps http.ResponseWriter so the logging middleware can see the
// final status code, which net/http does not expose on its own.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Logging returns a middleware that logs each request's correlation ID, method,
// path, status, and duration. It relies on RequestID being wired earlier in the
// chain; when it isn't, the reqID field is empty rather than a fresh value, so
// logs still line up with response headers.
func Logging(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			reqID := RequestIDFromContext(r.Context())
			logger.Printf("reqID=%s %s %s status=%d dur=%s",
				reqID, r.Method, r.URL.Path, rec.status, time.Since(start).String())
		})
	}
}

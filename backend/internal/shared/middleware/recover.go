package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// Recover catches panics from downstream handlers, logs them with the request
// ID and stack trace, and returns a generic 500. This is the single most
// important middleware for observability of unexpected errors — it is the
// natural integration point for Sentry/DataDog/etc., which can be added by
// swapping the logger call for a reporter.Capture call.
func Recover(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				reqID := RequestIDFromContext(r.Context())
				userID, _ := UserIDFromContext(r.Context())
				logger.Printf("PANIC reqID=%s userID=%s method=%s path=%s err=%v\n%s",
					reqID, userID, r.Method, r.URL.Path, rec, debug.Stack())
				// Don't clobber a response that already started writing.
				// http.ResponseWriter has no standard "was written?" signal, so we
				// best-effort write a 500 and rely on the client tolerating a
				// mid-stream connection reset on the unlucky case.
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-ID", reqID)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

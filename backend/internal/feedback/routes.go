package feedback

import "net/http"

// Middleware is the standard middleware signature used across the codebase.
type Middleware = func(http.Handler) http.Handler

// RegisterRoutes registers feedback endpoints. authMiddleware is required —
// every event must be attributed to a real user. limit is optional extra
// rate-limiting (nil is fine; the global limiter still applies).
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware Middleware, limit Middleware) {
	submit := http.Handler(http.HandlerFunc(h.Submit))
	if limit != nil {
		submit = limit(submit)
	}
	mux.Handle("/v1/outfits/feedback", authMiddleware(submit))
}

package admin

import "net/http"

// Middleware is the standard middleware signature used across the codebase.
type Middleware = func(http.Handler) http.Handler

// RegisterRoutes registers the admin auth endpoints on mux.
//
// authLimit applies IP-scoped rate limiting to the login + refresh paths
// to blunt credential spraying and refresh-token brute force. Pass nil
// (when Redis is unavailable) to skip — the global in-memory limiter
// still applies at a coarser granularity.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authLimit Middleware) {
	wrap := func(h http.Handler) http.Handler {
		if authLimit == nil {
			return h
		}
		return authLimit(h)
	}

	mux.Handle("/admin/v1/auth/login", wrap(http.HandlerFunc(h.Login)))
	mux.Handle("/admin/v1/auth/refresh", wrap(http.HandlerFunc(h.Refresh)))
}

package admin

import "net/http"

// Middleware is the standard middleware signature used across the codebase.
type Middleware = func(http.Handler) http.Handler

// RegisterRoutes registers the admin endpoints on mux.
//
//   - authLimit: IP-scoped rate limiter wrapped around the unauthenticated
//     auth endpoints (login + refresh). Pass nil to skip when Redis is
//     unavailable; the global in-memory limiter still applies.
//   - requireAdmin: the JWT-validating admin auth middleware. Wraps every
//     protected endpoint. Required (not optional) — an admin handler
//     without auth is a bug.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authLimit Middleware, requireAdmin Middleware) {
	wrap := func(h http.Handler) http.Handler {
		if authLimit == nil {
			return h
		}
		return authLimit(h)
	}

	// Unauthenticated (rate-limited).
	mux.Handle("/admin/v1/auth/login", wrap(http.HandlerFunc(h.Login)))
	mux.Handle("/admin/v1/auth/refresh", wrap(http.HandlerFunc(h.Refresh)))

	// Authenticated. Every handler below sits behind requireAdmin —
	// adding a new route here without wrapping is a bug we'd want to
	// catch in code review, hence the explicit wrap on each line.
	mux.Handle("/admin/v1/me", requireAdmin(http.HandlerFunc(h.Me)))
	mux.Handle("/admin/v1/users", requireAdmin(http.HandlerFunc(h.ListUsers)))
}

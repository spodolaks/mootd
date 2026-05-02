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
	mux.Handle("/admin/v1/build-info", requireAdmin(http.HandlerFunc(h.BuildInfoHandler)))
	mux.Handle("/admin/v1/users", requireAdmin(http.HandlerFunc(h.ListUsers)))
	// /admin/v1/users/{id} — net/http (pre-1.22) doesn't pattern-match
	// path variables, so we register a prefix and the handler trims it.
	mux.Handle("/admin/v1/users/", requireAdmin(http.HandlerFunc(h.GetUser)))
	mux.Handle("/admin/v1/overview", requireAdmin(http.HandlerFunc(h.Overview)))
	mux.Handle("/admin/v1/traces", requireAdmin(http.HandlerFunc(h.ListTraces)))
	mux.Handle("/admin/v1/traces/summary", requireAdmin(http.HandlerFunc(h.TracesSummaryHandler)))
	// /admin/v1/traces/{id} — prefix route. Go's mux gives the longer
	// `/traces/summary` exact pattern priority, so this catches only
	// real ids (anything after /traces/ that isn't "summary").
	mux.Handle("/admin/v1/traces/", requireAdmin(http.HandlerFunc(h.GetTrace)))
	mux.Handle("/admin/v1/audit", requireAdmin(http.HandlerFunc(h.ListAudit)))
	mux.Handle("/admin/v1/search", requireAdmin(http.HandlerFunc(h.Search)))
	// Detection-run archive (P1-04). Same prefix-route trick as
	// /users/ — handler dispatches on the trailing path segment
	// (input-image vs the bare detail).
	mux.Handle("/admin/v1/detection-runs/", requireAdmin(http.HandlerFunc(h.GetDetectionRun)))

	// Eval suite (P3-04 / mootd-admin#27). Prefix-routed, same
	// pattern: /sets, /runs, /runs/{id} all dispatched in
	// EvalsRouter.
	mux.Handle("/admin/v1/evals/", requireAdmin(http.HandlerFunc(h.EvalsRouter)))
}

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

	// Authenticated. Every handler below sits behind requireAdmin
	// (JWT-validating). RBAC permission gates layer on top via
	// RequirePermission(perm) — see permissions.go for the
	// role→permission map.
	//
	// Endpoints intentionally without a permission gate:
	//   /me, /build-info, /search   — every authenticated admin
	//                                  needs these for navigation.
	//   /audit                      — admins reviewing their own
	//                                  actions; gating it behind a
	//                                  separate perm would defeat
	//                                  the audit trail's purpose.
	mux.Handle("/admin/v1/me", requireAdmin(http.HandlerFunc(h.Me)))
	mux.Handle("/admin/v1/build-info", requireAdmin(http.HandlerFunc(h.BuildInfoHandler)))
	mux.Handle("/admin/v1/search", requireAdmin(http.HandlerFunc(h.Search)))
	mux.Handle("/admin/v1/audit", requireAdmin(http.HandlerFunc(h.ListAudit)))

	// User surfaces — readable by anyone with users:read; PII
	// fields gated *inside* the handler with HasPermissionFromContext
	// (because PII reveal is per-row, not per-route).
	mux.Handle("/admin/v1/users", requireAdmin(RequirePermission(PermUsersRead)(http.HandlerFunc(h.ListUsers))))
	mux.Handle("/admin/v1/users/", requireAdmin(RequirePermission(PermUsersRead)(http.HandlerFunc(h.GetUser))))

	mux.Handle("/admin/v1/overview", requireAdmin(RequirePermission(PermSpendRead)(http.HandlerFunc(h.Overview))))

	// Traces firehose + detail.
	mux.Handle("/admin/v1/traces", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.ListTraces))))
	mux.Handle("/admin/v1/traces/summary", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.TracesSummaryHandler))))
	mux.Handle("/admin/v1/traces/", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.GetTrace))))

	// Model routing — write-only sensitive surface. PUT requires
	// routing:write; GET is allowed for anyone with users:read so
	// they can see what's configured.
	mux.Handle("/admin/v1/model-routing", requireAdmin(http.HandlerFunc(h.ModelRouting)))

	// Weekly report — preview wide, send narrow.
	mux.Handle("/admin/v1/reports/weekly", requireAdmin(RequirePermission(PermSpendRead)(http.HandlerFunc(h.WeeklyReport))))
	mux.Handle("/admin/v1/reports/weekly/send", requireAdmin(RequirePermission(PermReportsSend)(http.HandlerFunc(h.WeeklyReport))))

	// Detection-run archive (P1-04). Reading is users:read;
	// the rerun POST requires detections:rerun (gated inside the
	// handler since the dispatcher is shared with GET endpoints).
	mux.Handle("/admin/v1/detection-runs/", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.GetDetectionRun))))

	// Eval suite (P3-04). Read = traces:read; start a run =
	// detections:rerun (write-side perm). Method dispatch inside
	// the handler — start-run perm checked there.
	mux.Handle("/admin/v1/evals/", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.EvalsRouter))))
}

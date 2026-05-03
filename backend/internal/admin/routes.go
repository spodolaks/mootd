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
//
// Optional outer wrappers (P5-03 / mootd-admin#36): caller may
// install IP-allowlist middleware ahead of these routes by
// chaining at the app-level mux instead of here. The middleware
// is in shared/middleware/ip_allowlist.go and is wired in
// internal/app/app.go from ADMIN_ALLOWED_IPS.
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

	// MFA enrollment (P5-02 / mootd-admin#35) — authenticated;
	// every admin must be able to enroll their own MFA without
	// elevated permission. Rate-limited via authLimit so a
	// botched enrollment can't be used as a TOTP-guess oracle.
	mux.Handle("/admin/v1/auth/mfa/setup", requireAdmin(http.HandlerFunc(h.MFASetup)))
	mux.Handle("/admin/v1/auth/mfa/verify", requireAdmin(http.HandlerFunc(h.MFAVerify)))

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
	// PII reveal audit (P5-04 / mootd-admin#37). Internal perm
	// gate inside the handler so a missing perm returns the
	// uniform {error, missingPermission} body the FE expects.
	mux.Handle("/admin/v1/audit/pii-reveal", requireAdmin(http.HandlerFunc(h.AuditPIIReveal)))
	// Session replay (P5-05 / mootd-admin#38). Recording is
	// permission-free — every authenticated admin's actions
	// flow into the log. Reading the log requires sessions:view.
	mux.Handle("/admin/v1/sessions/events", requireAdmin(http.HandlerFunc(h.RecordSessionEvent)))
	mux.Handle("/admin/v1/sessions", requireAdmin(RequirePermission(PermSessionsView)(http.HandlerFunc(h.ListSessions))))
	mux.Handle("/admin/v1/sessions/", requireAdmin(RequirePermission(PermSessionsView)(http.HandlerFunc(h.GetSession))))

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

	// Prompt templates (P3-01 / mootd-admin#24). Read =
	// prompts:read; create + promote require prompts:write
	// (gated inline since the dispatcher serves both).
	mux.Handle("/admin/v1/prompts", requireAdmin(RequirePermission(PermPromptsRead)(http.HandlerFunc(h.PromptTemplatesRouter))))
	mux.Handle("/admin/v1/prompts/", requireAdmin(RequirePermission(PermPromptsRead)(http.HandlerFunc(h.PromptTemplatesRouter))))

	// Funnels (P2-04 / mootd-admin#21). Read = traces:read;
	// POST gated inline.
	mux.Handle("/admin/v1/funnels", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.FunnelsRouter))))
	mux.Handle("/admin/v1/funnels/", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.FunnelsRouter))))
}

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
	//   /me, /build-info            — every authenticated admin
	//                                  needs these for navigation.
	//   /audit                      — admins reviewing their own
	//                                  actions; gating it behind a
	//                                  separate perm would defeat
	//                                  the audit trail's purpose.
	mux.Handle("/admin/v1/me", requireAdmin(http.HandlerFunc(h.Me)))
	mux.Handle("/admin/v1/build-info", requireAdmin(http.HandlerFunc(h.BuildInfoHandler)))
	// /search is an email lookup (its only result kind today is user-by-email),
	// so it requires users:pii — without a gate any admin, including roles with
	// zero user permissions, could enumerate customer emails (#140).
	mux.Handle("/admin/v1/search", requireAdmin(RequirePermission(PermUsersPII)(http.HandlerFunc(h.Search))))
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

	// Model routing — sensitive config surface. The dispatcher serves
	// both GET and PUT, so the route is gated at spend:read (the floor
	// the sibling cost surfaces — /overview, /reports/weekly — use), which
	// previously had no permission gate at all and was reachable by any
	// authenticated admin (#145). The mutating PUT keeps its stricter
	// inline routing:write check in updateModelRouting, so this floor does
	// not weaken the write path: routing:write is admin-only and admin also
	// holds spend:read.
	mux.Handle("/admin/v1/model-routing", requireAdmin(RequirePermission(PermSpendRead)(http.HandlerFunc(h.ModelRouting))))

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

	// Retention cohorts (P2-05 / mootd-admin#22). Read-only,
	// computed live from the events collection. traces:read
	// matches the funnels gate — both are pass-through analyses
	// of the events stream.
	mux.Handle("/admin/v1/retention", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.GetCohortRetention))))

	// HITL queue + per-item actions (singleItemDetection #34, #35).
	// Read = traces:read on the queue + detail; mutating actions
	// (approve / reject / patch / regenerate) gate inline on
	// traces:rerun. Returns 503 when DETECTION_BACKEND != singleitem
	// or SINGLEITEM_BASE_URL is empty.
	mux.Handle("/admin/v1/hitl-queue", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.HitlQueue))))
	mux.Handle("/admin/v1/items/", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.HitlItemsRouter))))

	// Training trials (singleItemDetection #36). Same orchestrator
	// proxy as the HITL queue (reuses hitlProxy). Read = traces:read
	// for the trial poll + image blobs; the process POST gates inline
	// on traces:rerun. Returns 503 when SINGLEITEM_BASE_URL is empty.
	mux.Handle("/admin/v1/training/", requireAdmin(RequirePermission(PermTracesRead)(http.HandlerFunc(h.TrainingRouter))))

	// Archetype-default wardrobe items (cold-start fix). Read =
	// prompts:read (shared with the Prompts list endpoint); create
	// / patch / delete gated inline on defaults:write — scoped
	// narrower than prompts:write so curators can edit defaults
	// without inheriting prompt-template / A-B-test authoring.
	mux.Handle("/admin/v1/archetype-defaults", requireAdmin(RequirePermission(PermPromptsRead)(http.HandlerFunc(h.ArchetypeDefaultsRouter))))
	mux.Handle("/admin/v1/archetype-defaults/", requireAdmin(RequirePermission(PermPromptsRead)(http.HandlerFunc(h.ArchetypeDefaultsRouter))))
	// "Seed this user's wardrobe with their archetype defaults"
	// — admin-triggered helper for ops + cold-start onboarding.
	mux.Handle("/admin/v1/users/seed-from-archetype/", requireAdmin(RequirePermission(PermUsersRead)(http.HandlerFunc(h.SeedWardrobeRouter))))
}

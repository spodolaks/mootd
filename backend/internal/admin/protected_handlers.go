package admin

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"mootd/backend/internal/buildinfo"
	"mootd/backend/internal/shared/response"
)

// BuildInfo is the wire shape returned by GET /admin/v1/build-info.
// Mirrors the BuildInfo schema in admin-api.yaml; kept hand-written
// rather than imported from gen/ to avoid the Id-vs-ID convention
// fight (per backend/internal/admin/gen/README.md).
type BuildInfo struct {
	Version     string `json:"version"`
	SHA         string `json:"sha"`
	Environment string `json:"environment"`
	BuiltAt     string `json:"builtAt,omitempty"`
}

// BuildInfoHandler handles GET /admin/v1/build-info.
//
// Returns the compile-time identity of the running backend (version,
// SHA, environment, build timestamp). Cacheable — values only change
// on deploy, so the admin UI fetches once per session and shows the
// result in the sidebar footer.
func (h *Handler) BuildInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	env := os.Getenv("ENVIRONMENT")
	switch env {
	case "":
		env = "development"
	case "development", "staging", "production":
		// supported, leave as-is
	default:
		// Anything else collapses to "development" so the spec enum holds.
		env = "development"
	}

	response.WriteJSON(w, http.StatusOK, BuildInfo{
		Version:     buildinfo.Version,
		SHA:         buildinfo.SHA,
		Environment: env,
		BuiltAt:     buildinfo.BuiltAt,
	})
}

// Me handles GET /admin/v1/me.
//
// Returns the authenticated admin's identity + roles + MFA state.
// Used by the admin frontend to verify the current session is still
// valid (and to render the email/role badge in the dashboard). Any
// non-PII state belongs here; PII reveal endpoints will be separate.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adminID, ok := AdminIDFromContext(r.Context())
	if !ok {
		// Should never happen — middleware enforces auth before we get here.
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	a, err := h.repo.FindByID(ctx, adminID)
	if err != nil || a == nil {
		// Token is valid but the admin record was deleted — rare, but
		// the right answer is 401, not 500. Same generic message as
		// the auth middleware so a deleted admin can't be told apart
		// from a forged token.
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Best-effort last-active bump on any /me hit. The frontend pings
	// this on focus, so it doubles as a presence signal — admins who
	// haven't opened the panel for a week will show up clearly in the
	// users list (when we eventually surface admin presence there).
	if err := h.repo.UpdateLastActive(ctx, a.ID, time.Now().UTC()); err != nil {
		h.logger.Printf("admin /me: update last-active: %v", err)
	}

	response.WriteJSON(w, http.StatusOK, AdminInfo{
		ID:    a.ID,
		Email: a.Email,
		Roles: a.RolesAsStrings(),
	})
}

// ListUsers handles GET /admin/v1/users.
//
// Cursor pagination, search on email, optional active-in-30d filter.
// Per-user counts are computed inline (one indexed CountDocuments per
// collection per user) — fine at our user volume; a future phase
// migrates to user_daily_rollup for the cost columns.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.usersRepo == nil {
		// Wiring bug — fail loud rather than serve empty pages.
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "users repository not configured"})
		return
	}

	q := parseUsersQuery(r.URL.Query())

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	summaries, nextCursor, err := h.usersRepo.ListSummaries(ctx, q)
	if err != nil {
		h.logger.Printf("admin /users: list failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// PII redaction. Phase 0: every admin is full PII (RoleAdmin), so
	// nothing redacted yet. P5-04 reads the role list from context
	// and redacts when users:pii is missing.
	// Stub the hook here so the future change is one-line:
	//
	//   roles, _ := middleware.AdminRolesFromContext(r.Context())
	//   if !hasPermission(roles, "users:pii") {
	//       for i := range summaries { summaries[i].Email = redactEmail(summaries[i].Email) }
	//   }

	response.WriteJSON(w, http.StatusOK, UsersListResponse{
		Users:      summaries,
		NextCursor: nextCursor,
	})
}

// Overview handles GET /admin/v1/overview.
//
// Dashboard data: today's spend + LLM call count + approx DAU + the
// last 10 LLM calls for the recent-activity feed. Lightweight enough
// to call on every dashboard mount without rate-limiting; the
// frontend's TanStack Query cache (30s stale) handles re-mount churn.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.overviewRepo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "overview repository not configured"})
		return
	}

	period := resolvePeriod(r.URL.Query().Get("period"))

	// 8s budget for the whole page — daily series can be the slowest
	// query; we want it bounded.
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	now := time.Now().UTC()
	start, end := periodWindow(period, now)
	priorEnd := start
	priorStart := priorEnd.Add(-end.Sub(start)) // window of equal length immediately before

	// Headline period.
	spend, count, err := h.overviewRepo.PeriodMetrics(ctx, start, end)
	if err != nil {
		h.logger.Printf("admin /overview: period metrics: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Prior period — same shape, used for WoW deltas. Best-effort:
	// failure here just elides the delta on the frontend.
	priorSpend, priorCount, err := h.overviewRepo.PeriodMetrics(ctx, priorStart, priorEnd)
	if err != nil {
		h.logger.Printf("admin /overview: prior metrics: %v", err)
		priorSpend, priorCount = 0, 0
	}

	dau, err := h.overviewRepo.ApproxDAU(ctx, now.Add(-24*time.Hour))
	if err != nil {
		// DAU is a heuristic anyway — log + serve the rest of the page.
		h.logger.Printf("admin /overview: dau: %v", err)
		dau = 0
	}
	priorDau, err := h.overviewRepo.ApproxDAU(ctx, now.Add(-48*time.Hour))
	if err != nil {
		h.logger.Printf("admin /overview: prior dau: %v", err)
		priorDau = 0
	}
	// ApproxDAU(48h) gives us [now-48h, now]; subtract today's slice
	// to get the prior 24h-only count.
	if priorDau > dau {
		priorDau -= dau
	} else {
		priorDau = 0
	}

	// Daily series — 30 entries each, zero-filled.
	spendSeries, countSeries, dauSeries, err := h.overviewRepo.DailySeries(ctx, now)
	if err != nil {
		h.logger.Printf("admin /overview: daily series: %v", err)
		// Sparklines are nice-to-have; don't fail the page on a
		// series error.
		spendSeries, countSeries, dauSeries = nil, nil, nil
	}

	calls, err := h.overviewRepo.RecentLLMCalls(ctx, 10)
	if err != nil {
		h.logger.Printf("admin /overview: recent calls: %v", err)
		calls = nil
	}

	// Cache metrics — best-effort; nil when no Anthropic activity in
	// the period (which is fine, frontend hides the tile).
	cacheMetrics, err := h.overviewRepo.CacheMetricsFor(ctx, start, end)
	if err != nil {
		h.logger.Printf("admin /overview: cache metrics: %v", err)
		cacheMetrics = nil
	}

	// Resolve user IDs → emails for the recent-calls feed.
	if len(calls) > 0 {
		ids := make([]string, 0, len(calls))
		seen := make(map[string]struct{}, len(calls))
		for _, c := range calls {
			if c.UserID == "" {
				continue
			}
			if _, dup := seen[c.UserID]; dup {
				continue
			}
			seen[c.UserID] = struct{}{}
			ids = append(ids, c.UserID)
		}
		emails, err := h.overviewRepo.EmailsForUserIDs(ctx, ids)
		if err != nil {
			h.logger.Printf("admin /overview: email resolve: %v", err)
		} else {
			for i := range calls {
				if e, ok := emails[calls[i].UserID]; ok {
					calls[i].UserEmail = e
				}
			}
		}
	}

	response.WriteJSON(w, http.StatusOK, OverviewMetrics{
		Period:          period,
		SpendUSD:        spend,
		CallCount:       count,
		DauApprox:       dau,
		SpendUSDPrior:   priorSpend,
		CallCountPrior:  priorCount,
		DauPrior:        priorDau,
		SpendSeries:     spendSeries,
		CallCountSeries: countSeries,
		DauSeries:       dauSeries,
		LastCalls:       calls,
		CacheMetrics:    cacheMetrics,
		GeneratedAt:     now,
	})
}

// GetUser handles GET /admin/v1/users/{id} and dispatches on
// sub-path:
//
//   /admin/v1/users/{id}            → user detail (P1-06)
//   /admin/v1/users/{id}/wardrobe   → wardrobe page (mootd-admin#11)
//
// Until Go 1.22 path variables, this dispatch lives inside the
// handler instead of the mux. Adding new sub-paths (outfits /
// moodboards / etc.) drops one more case into the switch below.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.usersRepo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "users repository not configured"})
		return
	}

	// Path: /admin/v1/users/{id}[/{sub}]
	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/users/")
	if rest == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}
	id, sub := rest, ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		id, sub = rest[:idx], rest[idx+1:]
	}

	switch sub {
	case "":
		h.getUserDetail(w, r, id)
	case "wardrobe":
		h.getUserWardrobe(w, r, id)
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown user sub-resource"})
	}
}

func (h *Handler) getUserDetail(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	detail, err := h.usersRepo.FindDetail(ctx, id)
	if err != nil {
		h.logger.Printf("admin /users/{id}: repo failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if detail == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	// PII redaction hook — same pattern as ListUsers; phase-0 every
	// admin has full PII so nothing redacted yet.

	response.WriteJSON(w, http.StatusOK, detail)
}

// getUserWardrobe handles GET /admin/v1/users/{id}/wardrobe.
// Cursor-paginated, 50-row default. Returns 404 if the user
// doesn't exist (caught upstream by FindDetail's null check —
// here we only verify the id is non-empty).
func (h *Handler) getUserWardrobe(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	items, nextCursor, err := h.usersRepo.ListWardrobe(ctx, id, cursor, limit)
	if err != nil {
		h.logger.Printf("admin /users/%s/wardrobe: repo failed: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, UserWardrobePage{
		Items:      items,
		NextCursor: nextCursor,
	})
}

// ListTraces handles GET /admin/v1/traces.
//
// Cursor pagination, filters by userId / model / feature / status /
// minCost / date range. The endpoint is the foundation for the
// admin's /traces page (full firehose) and the user-detail drawer
// (which calls it with userId pre-filled).
//
// Per [DATA_MODEL.md](docs/DATA_MODEL.md), llm_calls indexes cover
// the per-filter sort patterns — the per-page query is one indexed
// scan, not a collection scan, regardless of how the filters
// combine.
func (h *Handler) ListTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tracesRepo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "traces repository not configured"})
		return
	}

	v := r.URL.Query()
	q := parseTracesQuery(v)

	// CSV export takes a different code path: no pagination, audit
	// log, and a streamed text/csv response. Cap at maxExportRows so
	// a no-filter export can't OOM the server.
	if v.Get("format") == "csv" {
		h.exportTracesCSV(w, r, q)
		return
	}

	if l := v.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			q.Limit = n
		}
	}
	q.Cursor = v.Get("cursor")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, err := h.tracesRepo.List(ctx, q)
	if err != nil {
		h.logger.Printf("admin /traces: list failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	response.WriteJSON(w, http.StatusOK, page)
}

// GetTrace handles GET /admin/v1/traces/{id}.
//
// Single-call detail for the admin prompt viewer (P1-12 /
// mootd-admin#17). Returns the full llm_calls row including the
// archived prompt + user message + raw response + wardrobe item
// IDs. Resolves user email server-side so the FE can render it
// directly under the masked-email convention.
func (h *Handler) GetTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tracesRepo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "traces repository not configured"})
		return
	}

	// Path: /admin/v1/traces/{id}. The id is everything after the
	// "/traces/" segment minus a trailing "/summary" — that
	// shadowing is handled by registering /traces/summary explicitly
	// on the mux, which has higher specificity than this prefix.
	id := strings.TrimPrefix(r.URL.Path, "/admin/v1/traces/")
	if id == "" || strings.Contains(id, "/") {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid trace id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	detail, err := h.tracesRepo.FindDetail(ctx, id)
	if err != nil {
		h.logger.Printf("admin /traces/{id}: repo failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if detail == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "trace not found"})
		return
	}

	// Best-effort email join — same pattern as Overview's recent-
	// calls feed. Single point lookup, never blocks the response on
	// failure.
	if h.overviewRepo != nil && detail.UserID != "" {
		if emails, err := h.overviewRepo.EmailsForUserIDs(ctx, []string{detail.UserID}); err == nil {
			if e, ok := emails[detail.UserID]; ok {
				detail.UserEmail = e
			}
		}
	}

	response.WriteJSON(w, http.StatusOK, detail)
}

// TracesSummaryHandler handles GET /admin/v1/traces/summary.
//
// Aggregate over the same filter as /admin/v1/traces — total count,
// total cost, mean and approximate p95 latency. Pagination params
// are ignored. Powers the strip above the firehose table.
func (h *Handler) TracesSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tracesRepo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "traces repository not configured"})
		return
	}

	q := parseTracesQuery(r.URL.Query())

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	summary, err := h.tracesRepo.Summary(ctx, q)
	if err != nil {
		h.logger.Printf("admin /traces/summary: aggregate failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, summary)
}

// maxExportRows caps a single CSV export. Bigger sets need to be
// fetched via the API directly; the UI never hands an admin a
// half-million-row CSV.
const maxExportRows = 50_000

// exportTracesCSV streams a CSV of every llm_calls row matching the
// filter. Audited — exporting customer data is a sensitive action.
func (h *Handler) exportTracesCSV(w http.ResponseWriter, r *http.Request, q TracesQuery) {
	// Cursor + limit don't apply to CSV — clear them defensively in
	// case a caller stitched them in.
	q.Cursor = ""
	q.Limit = 0

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	rows, err := h.tracesRepo.IterAll(ctx, q, maxExportRows)
	if err != nil {
		h.logger.Printf("admin /traces export: iter failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Audit. Best-effort — if the audit write fails we still serve
	// the CSV; the alternative (denying the export over an audit
	// hiccup) is worse for operators. Email lookup is best-effort
	// too: empty email in the audit row beats blocking the export.
	if h.repo != nil {
		adminID, _ := AdminIDFromContext(r.Context())
		var adminEmail string
		if a, _ := h.repo.FindByID(r.Context(), adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(r.Context(), h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "traces.export",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"format":   "csv",
				"rowCount": len(rows),
				"filter": map[string]any{
					"userId":  q.UserID,
					"model":   q.Model,
					"feature": q.Feature,
					"status":  q.Status,
					"minCost": q.MinCostUSD,
					"from":    timeStr(q.From),
					"to":      timeStr(q.To),
				},
			},
		})
	}

	filename := fmt.Sprintf("traces-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"id", "createdAt", "userId", "provider", "model", "feature",
		"status", "costUsd", "durationMs",
	})
	for _, c := range rows {
		_ = writer.Write([]string{
			c.ID,
			c.CreatedAt.UTC().Format(time.RFC3339Nano),
			c.UserID,
			c.Provider,
			c.Model,
			c.Feature,
			c.Status,
			strconv.FormatFloat(c.CostUSD, 'f', -1, 64),
			strconv.FormatInt(c.DurationMs, 10),
		})
	}
	writer.Flush()
}

// parseTracesQuery hoists URL query parsing out of the handler. Note
// that pagination params (cursor / limit) are NOT read here — they
// only apply to the JSON list path and the caller stitches them in.
func parseTracesQuery(v url.Values) TracesQuery {
	return TracesQuery{
		UserID:     v.Get("userId"),
		Model:      v.Get("model"),
		Feature:    v.Get("feature"),
		Status:     v.Get("status"),
		MinCostUSD: parseFloat0(v.Get("minCost")),
		From:       parseTimePtr(v.Get("from")),
		To:         parseTimePtr(v.Get("to")),
	}
}

func timeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Search handles GET /admin/v1/search.
//
// Cross-collection lookup behind the Cmd+K palette + (future) global
// search bar (mootd-admin#92). Today: user-by-email only. Returns
// up to 10 hits per kind.
//
// Audit: every query writes a row with action=search.users and the
// query string in metadata. Search reveals identifying data (emails)
// — that's an admin action worth recording.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.usersRepo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "users repository not configured"})
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// Spec says <2 chars returns empty — keeps the FE debounce
	// behaviour tolerant (a single keystroke shouldn't 400).
	if len(q) < 2 {
		response.WriteJSON(w, http.StatusOK, SearchResponse{Hits: []SearchHit{}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hits, err := h.usersRepo.SearchUsers(ctx, q, 10)
	if err != nil {
		h.logger.Printf("admin /search: users failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Audit. Best-effort — the search itself succeeded, lost audit
	// rows surface in monitoring, never a 5xx to the caller.
	if h.repo != nil {
		adminID, _ := AdminIDFromContext(r.Context())
		var adminEmail string
		if a, _ := h.repo.FindByID(r.Context(), adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(r.Context(), h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "search.users",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"query":    q,
				"hitCount": len(hits),
			},
		})
	}

	response.WriteJSON(w, http.StatusOK, SearchResponse{Hits: hits})
}

// ListAudit handles GET /admin/v1/audit.
//
// Paginated audit-log feed. Filterable by action / adminId /
// targetUserId / date range. Foundation for the admin /audit page
// (mootd-admin#95). Closes the loop on every audit row written by
// /traces export and (future) PII reveals + role changes.
func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.repo == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit repository not configured"})
		return
	}

	v := r.URL.Query()
	q := AuditQuery{
		Action:       v.Get("action"),
		AdminID:      v.Get("adminId"),
		TargetUserID: v.Get("targetUserId"),
		From:         parseTimePtr(v.Get("from")),
		To:           parseTimePtr(v.Get("to")),
		Cursor:       v.Get("cursor"),
	}
	if l := v.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			q.Limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	entries, nextCursor, err := h.repo.ListAudit(ctx, q)
	if err != nil {
		h.logger.Printf("admin /audit: list failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	response.WriteJSON(w, http.StatusOK, AuditPage{
		Entries:    entries,
		NextCursor: nextCursor,
	})
}

// parseUsersQuery hoists URL query parsing out of the handler so it
// stays trivially testable + the handler reads as a sequence of
// repository calls.
func parseUsersQuery(v url.Values) UsersQuery {
	q := UsersQuery{
		Search: v.Get("q"),
		Tier:   v.Get("tier"),
		Sort:   v.Get("sort"),
		Cursor: v.Get("cursor"),
	}
	if v.Get("active") == "true" {
		q.Active = true
	}
	if l := v.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			q.Limit = n
		}
	}
	return q
}

package admin

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
		GeneratedAt:     now,
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
	q := TracesQuery{
		UserID:     v.Get("userId"),
		Model:      v.Get("model"),
		Feature:    v.Get("feature"),
		Status:     v.Get("status"),
		MinCostUSD: parseFloat0(v.Get("minCost")),
		From:       parseTimePtr(v.Get("from")),
		To:         parseTimePtr(v.Get("to")),
		Cursor:     v.Get("cursor"),
	}
	if l := v.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			q.Limit = n
		}
	}

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

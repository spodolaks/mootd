package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
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
	// Prior-period DAU is a half-open window [now-48h, now-24h). The
	// previous implementation did `ApproxDAU(48h) - ApproxDAU(24h)`,
	// which over-subtracted any user active in both windows and
	// inflated the WoW delta (closes mootd#36). The new
	// ApproxDAUBetween query carries a documented data-model caveat
	// — see its godoc for the systematic-undercount tradeoff.
	priorDau, err := h.overviewRepo.ApproxDAUBetween(ctx, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	if err != nil {
		h.logger.Printf("admin /overview: prior dau: %v", err)
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

	// Per-case method dispatch — the GET-only handlers gate
	// inline so /budget can also accept PUT without splitting into
	// a separate top-level mux registration.
	switch sub {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getUserDetail(w, r, id)
	case "wardrobe":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getUserWardrobe(w, r, id)
	case "moodboards":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getUserMoodboards(w, r, id)
	case "outfits":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getUserOutfits(w, r, id)
	case "spend":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getUserSpend(w, r, id)
	case "budget":
		switch r.Method {
		case http.MethodGet:
			h.getUserBudget(w, r, id)
		case http.MethodPut:
			h.updateUserBudget(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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

// getUserMoodboards handles GET /admin/v1/users/{id}/moodboards.
// Cursor-paginated, 25-row default. Returns 200 with empty items
// when the user has no saved moodboards (vs 404 — empty is a valid
// state for new users).
func (h *Handler) getUserMoodboards(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	items, nextCursor, err := h.usersRepo.ListMoodboards(ctx, id, cursor, limit)
	if err != nil {
		h.logger.Printf("admin /users/%s/moodboards: repo failed: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, UserMoodboardsPage{
		Items:      items,
		NextCursor: nextCursor,
	})
}

// getUserOutfits handles GET /admin/v1/users/{id}/outfits.
//
// Cursor-paginated, 15 batches per page (each batch has 3-4
// candidates so 15 batches = ~45-60 outfits per page render).
// Returns 200 with an empty array for new users.
func (h *Handler) getUserOutfits(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 15
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	batches, nextCursor, err := h.usersRepo.ListOutfitBatches(ctx, id, cursor, limit)
	if err != nil {
		h.logger.Printf("admin /users/%s/outfits: repo failed: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, UserOutfitsPage{
		Batches:    batches,
		NextCursor: nextCursor,
	})
}

// getUserSpend handles GET /admin/v1/users/{id}/spend.
//
// Returns 30-day per-feature spend breakdown, zero-filled. The
// repo aggregates with a single $group; this handler is mostly a
// transport-and-timeout wrapper.
func (h *Handler) getUserSpend(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	breakdown, err := h.usersRepo.SpendBreakdown(ctx, id, time.Now().UTC())
	if err != nil {
		h.logger.Printf("admin /users/%s/spend: repo failed: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, breakdown)
}

// getUserBudget handles GET /admin/v1/users/{id}/budget.
// Falls back to system defaults when the user has no override —
// the response carries `isDefault: true` so the FE can render
// the values as placeholders rather than as a saved override.
func (h *Handler) getUserBudget(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// When the budgets repo isn't wired, return the static defaults
	// read-only. The FE detects this via isDefault + the absence of
	// a setBy/setAt and disables the edit button accordingly.
	if h.budgets == nil {
		response.WriteJSON(w, http.StatusOK, UserBudget{
			UserID:     id,
			DailyUSD:   DefaultDailyBudgetUSD,
			MonthlyUSD: DefaultMonthlyBudgetUSD,
			IsDefault:  true,
		})
		return
	}

	budget, _, err := h.budgets.GetForUser(ctx, id)
	if err != nil {
		h.logger.Printf("admin /users/%s/budget GET: repo failed: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Live-state hydration (P4-02 / mootd-admin#30). Best-effort —
	// a Redis blip when reading these is logged but doesn't fail
	// the GET. Caller still sees the saved cap, just without
	// today's spend.
	if h.budgetState != nil {
		if today, terr := h.budgetState.TodaySpend(ctx, id); terr != nil {
			h.logger.Printf("admin /users/%s/budget GET: spend read: %v", id, terr)
		} else {
			budget.TodaySpendUSD = today
		}
		if suspended, terr := h.budgetState.IsSuspended(ctx, id); terr != nil {
			h.logger.Printf("admin /users/%s/budget GET: suspend read: %v", id, terr)
		} else if suspended {
			// Tracker doesn't surface the exact "until" timestamp
			// today (Redis just stores the TTL on the key), so we
			// fall back to "+24h from now" as a soft display.
			// Tightening the tracker to read the stored value is
			// a small follow-up — not on the critical path for
			// "is the user suspended?" which the boolean answers.
			until := time.Now().UTC().Add(24 * time.Hour)
			budget.SuspendedUntil = &until
		}
	}

	response.WriteJSON(w, http.StatusOK, budget)
}

// updateUserBudget handles PUT /admin/v1/users/{id}/budget.
// Validates body, upserts the row, writes an audit entry, and
// echoes the resulting UserBudget. Reason is required — every
// budget edit ends up in the audit log with rationale.
func (h *Handler) updateUserBudget(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}
	if h.budgets == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "budgets repository not configured"})
		return
	}

	var body struct {
		DailyUSD   float64 `json:"dailyUSD"`
		MonthlyUSD float64 `json:"monthlyUSD"`
		Reason     string  `json:"reason"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Validation. Spec mins/maxes are mirrored here defensively —
	// the spec is documentation; the wire is the truth.
	switch {
	case body.DailyUSD < 0 || body.DailyUSD > 1000:
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "dailyUSD must be between 0 and 1000"})
		return
	case body.MonthlyUSD < 0 || body.MonthlyUSD > 10000:
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "monthlyUSD must be between 0 and 10000"})
		return
	case body.MonthlyUSD < body.DailyUSD:
		// A monthly cap below the daily cap is almost always a
		// fat-finger — daily would never bind. Reject with a
		// pointed message so the admin knows why.
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "monthlyUSD must be >= dailyUSD"})
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "reason is required"})
		return
	}
	if len(reason) > 500 {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "reason exceeds 500 characters"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Verify the user exists. UsersRepo's FindDetail does this for
	// us; reusing it here keeps the not-found path consistent with
	// the rest of /users/{id}/* handlers.
	detail, err := h.usersRepo.FindDetail(ctx, id)
	if err != nil {
		h.logger.Printf("admin /users/%s/budget PUT: user lookup: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if detail == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	// Read prior to capture diff for the audit log.
	prior, _, _ := h.budgets.GetForUser(ctx, id)

	adminID, _ := AdminIDFromContext(r.Context())
	now := time.Now().UTC()

	updated := UserBudget{
		UserID:     id,
		DailyUSD:   body.DailyUSD,
		MonthlyUSD: body.MonthlyUSD,
		SetBy:      adminID,
		SetAt:      &now,
		Reason:     reason,
	}
	if err := h.budgets.Upsert(ctx, updated); err != nil {
		h.logger.Printf("admin /users/%s/budget PUT: upsert: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Audit. Same fire-and-forget pattern as elsewhere — admin
	// budget changes are sensitive (raising a cap is essentially
	// authorising spend) so a missing audit row is a real cost,
	// but blocking the write on Mongo wobbles is worse for ops.
	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:           generateAuditID(),
			AdminID:      adminID,
			AdminEmail:   adminEmail,
			Action:       "budget.update",
			TargetUserID: id,
			TargetEntity: "user_budget",
			At:           now,
			IP:           clientIP(r),
			UserAgent:    r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"reason":         reason,
				"priorDailyUSD":  prior.DailyUSD,
				"priorMonthlyUSD": prior.MonthlyUSD,
				"priorIsDefault": prior.IsDefault,
				"newDailyUSD":    body.DailyUSD,
				"newMonthlyUSD":  body.MonthlyUSD,
			},
		})
	}

	response.WriteJSON(w, http.StatusOK, updated)
}

// EvalsRouter is the prefix dispatcher for /admin/v1/evals/*.
// Cases:
//
//   - GET  /admin/v1/evals/sets       → list discovered sets
//   - GET  /admin/v1/evals/runs       → paginated runs (no cases)
//   - POST /admin/v1/evals/runs       → kick off async run, returns 202
//   - GET  /admin/v1/evals/runs/{id}  → full run with cases
//
// (P3-04 / mootd-admin#27.)
func (h *Handler) EvalsRouter(w http.ResponseWriter, r *http.Request) {
	if h.evalsRepo == nil || h.evalsLoader == nil || h.evalsRunner == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "eval suite not wired"})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/evals/")
	first, runID := rest, ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		first, runID = rest[:idx], rest[idx+1:]
	}

	switch {
	case first == "sets" && runID == "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listEvalSets(w, r)
	case first == "runs" && runID == "":
		switch r.Method {
		case http.MethodGet:
			h.listEvalRuns(w, r)
		case http.MethodPost:
			h.startEvalRun(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case first == "runs" && runID != "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getEvalRun(w, r, runID)
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown evals sub-resource"})
	}
}

func (h *Handler) listEvalSets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	sets, err := h.evalsLoader.List(ctx)
	if err != nil {
		h.logger.Printf("admin /evals/sets: loader: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"sets": sets})
}

func (h *Handler) listEvalRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cursor := r.URL.Query().Get("cursor")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, next, err := h.evalsRepo.List(ctx, cursor, limit)
	if err != nil {
		h.logger.Printf("admin /evals/runs list: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if rows == nil {
		rows = []EvalRun{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"runs":       rows,
		"nextCursor": next,
	})
}

func (h *Handler) startEvalRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EvalSetID     string `json:"evalSetId"`
		PromptVersion string `json:"promptVersion"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.EvalSetID) == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "evalSetId is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	adminID, _ := AdminIDFromContext(r.Context())

	runID, err := h.evalsRunner.Start(ctx, body.EvalSetID, body.PromptVersion, adminID)
	if err != nil {
		// Loader errors (set not found, no cases) translate to 400;
		// Mongo errors (create failed) translate to 500. Crude
		// substring match because the runner returns wrapped errors
		// with stable prefixes.
		if strings.Contains(err.Error(), "load tuples") || strings.Contains(err.Error(), "no cases") || strings.Contains(err.Error(), "invalid eval set id") {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.logger.Printf("admin /evals/runs POST: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Audit: who kicked off which run against which set. The
	// metadata helps later when correlating regressions to who
	// was experimenting that day.
	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "eval.start",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"runId":         runID,
				"evalSetId":     body.EvalSetID,
				"promptVersion": body.PromptVersion,
			},
		})
	}

	response.WriteJSON(w, http.StatusAccepted, map[string]any{
		"runId":  runID,
		"status": EvalStatusPending,
	})
}

func (h *Handler) getEvalRun(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	run, err := h.evalsRepo.Get(ctx, id)
	if err != nil {
		h.logger.Printf("admin /evals/runs/%s: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if run == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}
	response.WriteJSON(w, http.StatusOK, run)
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

// GetDetectionRun is the prefix-route dispatcher for everything
// under /admin/v1/detection-runs/. Cases:
//
//   - GET  /admin/v1/detection-runs/versions          → list distinct labels
//   - GET  /admin/v1/detection-runs/{id}              → run detail
//   - GET  /admin/v1/detection-runs/{id}/input-image  → archived photo
//   - POST /admin/v1/detection-runs/{id}/rerun        → admin rerun (P1-10)
//
// The "versions" keyword takes precedence over the {id} parse.
// runIDs are random and won't collide.
func (h *Handler) GetDetectionRun(w http.ResponseWriter, r *http.Request) {
	if h.detectionRuns == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "detection_runs archive not wired"})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/detection-runs/")
	if rest == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing detection-run id"})
		return
	}

	// Top-level keyword routes (no {id}).
	if rest == "versions" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listDetectionVersions(w, r)
		return
	}

	id, sub := rest, ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		id, sub = rest[:idx], rest[idx+1:]
	}

	switch sub {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getDetectionRunDetail(w, r, id)
	case "input-image":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getDetectionRunInputImage(w, r, id)
	case "rerun":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.rerunDetectionRun(w, r, id)
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-resource"})
	}
}

func (h *Handler) getDetectionRunDetail(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	run, err := h.detectionRuns.FindRun(ctx, id)
	if err != nil {
		h.logger.Printf("admin /detection-runs/%s: repo failed: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if run == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "detection run not found"})
		return
	}

	// Best-effort email join + input-image URL stitching.
	if h.overviewRepo != nil && run.UserID != "" {
		if emails, err := h.overviewRepo.EmailsForUserIDs(ctx, []string{run.UserID}); err == nil {
			if e, ok := emails[run.UserID]; ok {
				run.UserEmail = e
			}
		}
	}
	if run.InputImageContentType != "" {
		// Path-only — caller composes against the admin API base.
		run.InputImageURL = "/admin/v1/detection-runs/" + id + "/input-image"
	}

	response.WriteJSON(w, http.StatusOK, run)
}

func (h *Handler) getDetectionRunInputImage(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	data, contentType, err := h.detectionRuns.GetInputImage(ctx, id)
	if err != nil {
		// Most likely "not found" from the GridFS layer — translate
		// to 404 without leaking the internal error type.
		h.logger.Printf("admin /detection-runs/%s/input-image: %v", id, err)
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "input image not found"})
		return
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// listDetectionVersions handles
// GET /admin/v1/detection-runs/versions. Returns the distinct,
// non-empty `detectionVersion` strings ever persisted across
// detection_runs. Powers the dropdown in the admin rerun modal —
// when the list is empty the FE falls back to a free-text input
// so admins can establish the first version themselves.
func (h *Handler) listDetectionVersions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	versions, err := h.detectionRuns.ListVersions(ctx)
	if err != nil {
		h.logger.Printf("admin /detection-runs/versions: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if versions == nil {
		versions = []string{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// rerunDetectionRun handles
// POST /admin/v1/detection-runs/{id}/rerun. Replays the archived
// photo from the original run through the detection pipeline,
// persists a child detection_runs row with parent_run_id +
// created_by + detection_version set, and writes an audit entry.
//
// Body is optional — { "detectionVersion": "..." } sets the label
// on the child row. Today the upstream detection service is
// versionless so the label is descriptive only; when versioning
// lands we'll honour it as a real override.
//
// Synchronous on the wire: detection currently takes 5–30 seconds,
// well within an HTTP-friendly window. If detection grows past
// ~60 seconds we'll switch to the same async-job pattern as
// /v1/outfits/generate.
func (h *Handler) rerunDetectionRun(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing detection-run id"})
		return
	}

	// Decode optional body. We deliberately do NOT use the strict
	// decoder (response.DecodeJSON) because the spec marks the body
	// optional — an empty POST is valid and means "rerun, no version
	// label." We tolerate body absence + parse errors equally.
	var body struct {
		DetectionVersion string `json:"detectionVersion"`
	}
	if r.Body != nil {
		// Best-effort decode — we ignore EOF (no body) + decode errors.
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	// Detection itself is the slow part — 5–30s round-trip per run.
	// 90s gives us comfortable headroom over the wardrobe handler's
	// own timeouts.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	adminID, _ := AdminIDFromContext(r.Context())

	newRunID, err := h.detectionRuns.Rerun(ctx, id, adminID, strings.TrimSpace(body.DetectionVersion))
	if err != nil {
		// Best-effort error mapping: the rerun helper wraps the
		// not-found case with ErrRunNotFound. We can't import the
		// wardrobe package from here (one-way dep), so we sniff the
		// string. Other failures are 500.
		switch {
		case strings.Contains(err.Error(), "detection run not found"):
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "detection run or archived photo not found"})
		default:
			h.logger.Printf("admin rerun detection run %s: %v", id, err)
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "rerun failed"})
		}
		return
	}

	// Audit. Same fire-and-forget pattern as the trace export — we
	// log + continue if Mongo wobbles.
	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(r.Context(), adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(r.Context(), h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "detection.rerun",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"originalRunId":    id,
				"newRunId":         newRunID,
				"detectionVersion": body.DetectionVersion,
			},
		})
	}

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"runId":       newRunID,
		"parentRunId": id,
	})
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

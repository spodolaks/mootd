package admin

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/config"
	jwtutil "mootd/backend/internal/shared/jwt"
	"mootd/backend/internal/shared/response"
)

// Handler serves admin authentication endpoints (/admin/v1/auth/*) and
// the small set of always-available endpoints that just describe the
// current admin (/admin/v1/me). Domain endpoints under /admin/v1/users,
// /admin/v1/traces, etc. live in their own handlers within this package.
type Handler struct {
	logger        *log.Logger
	repo          Repository
	usersRepo     UsersRepository
	overviewRepo  OverviewRepository
	tracesRepo    TracesRepository
	detectionRuns DetectionRunRepository  // optional — when nil, /detection-runs returns 503
	budgets       UserBudgetsRepository   // optional — when nil, /users/{id}/budget returns defaults read-only
	budgetState   BudgetStateReader       // optional — when nil, /users/{id}/budget GET omits live spend
	evalsRepo     EvalsRepository         // optional — when nil, /evals/* returns 503
	evalsLoader   EvalSetLoader           // optional — pairs with evalsRepo
	evalsRunner   *EvalRunner             // optional — pairs with evalsRepo
	routingRepo   RoutingRepository       // optional — when nil, /model-routing returns 503
	routingCache  *CachedRoutingReader    // optional — cleared on PUT to invalidate
	routingProviders []string             // boot-time provider names; populated alongside routingRepo
	secret        string
}

// BudgetStateReader exposes today's per-user spend + suspension
// state from the budget tracker. Defined as an interface so admin/
// doesn't import the budget package — same one-way-dep pattern as
// elsewhere.
type BudgetStateReader interface {
	TodaySpend(ctx context.Context, userID string) (float64, error)
	IsSuspended(ctx context.Context, userID string) (bool, error)
}

// WithDetectionRuns wires the detection-run archive reader. Optional —
// keeps NewHandler's signature stable for tests; production app.go
// opts in once the wardrobe-side repo is up.
func (h *Handler) WithDetectionRuns(r DetectionRunRepository) *Handler {
	h.detectionRuns = r
	return h
}

// WithUserBudgets wires the per-user budget repo. Optional —
// when not wired, GET returns the system defaults (read-only) and
// PUT returns 503. Production app.go always wires it.
func (h *Handler) WithUserBudgets(r UserBudgetsRepository) *Handler {
	h.budgets = r
	return h
}

// WithBudgetState wires the live-spend reader (P4-02 /
// mootd-admin#30). When set, /users/{id}/budget GET includes
// `todaySpendUSD` and `suspendedUntil`. When unset, those fields
// are absent — same graceful-degradation pattern as the rest of
// the optional deps.
func (h *Handler) WithBudgetState(s BudgetStateReader) *Handler {
	h.budgetState = s
	return h
}

// WithRouting wires the model-routing repo + cache + the
// boot-time list of available providers (P4-05 / mootd-admin#33).
// All three travel together — `providers` populates the dropdown
// in the admin UI and the validator on PUT. `cache` is the same
// reader instance the outfit service uses; the PUT handler
// invalidates it on writes so admins see their edit reflected
// immediately.
func (h *Handler) WithRouting(repo RoutingRepository, cache *CachedRoutingReader, providers []string) *Handler {
	h.routingRepo = repo
	h.routingCache = cache
	h.routingProviders = append([]string{}, providers...)
	return h
}

// WithEvalSuite wires the eval suite (P3-04 / mootd-admin#27).
// All three pieces (repo, loader, runner) move together — there's
// no useful partial state. When unset, /admin/v1/evals/* returns
// 503 and the FE shows "eval suite not wired in this build."
func (h *Handler) WithEvalSuite(repo EvalsRepository, loader EvalSetLoader, runner *EvalRunner) *Handler {
	h.evalsRepo = repo
	h.evalsLoader = loader
	h.evalsRunner = runner
	return h
}

// NewHandler constructs a Handler.
//
// usersRepo + overviewRepo + tracesRepo are required for the
// dashboard's protected endpoints. Pass nil only in auth-only test
// setups; production wiring (app/app.go) always supplies all three.
func NewHandler(
	logger *log.Logger,
	repo Repository,
	usersRepo UsersRepository,
	overviewRepo OverviewRepository,
	tracesRepo TracesRepository,
	jwtSecret string,
) *Handler {
	return &Handler{
		logger:       logger,
		repo:         repo,
		usersRepo:    usersRepo,
		overviewRepo: overviewRepo,
		tracesRepo:   tracesRepo,
		secret:       jwtSecret,
	}
}

// Login handles POST /admin/v1/auth/login.
//
// Request body: { email, password, totp? }
//
// TOTP is accepted from Phase 0 to lock in the shape; the field is only
// *verified* when the admin doc's MFAEnforced flag is true (landing in
// P5-02). Until then, a supplied TOTP is ignored and an absent TOTP is
// accepted without warning.
//
// Response: 200 OK { accessToken, refreshToken, expiresAt, admin }
// Response: 400 — missing fields
// Response: 401 — wrong credentials
//
// The 401 message is intentionally identical for "email not found" and
// "wrong password" so the endpoint can't be used as a credential
// oracle.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	admin, err := h.repo.FindByEmail(ctx, email)
	if err != nil {
		h.logger.Printf("admin login: find failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// Unify missing-admin and password-mismatch outcomes so the caller
	// can't tell whether the email is valid. VerifyPassword on an empty
	// hash still consumes argon2 CPU so timing is also constant-ish.
	invalid := admin == nil
	if !invalid {
		if err := VerifyPassword(admin.PasswordHash, req.Password); err != nil {
			invalid = true
		}
	} else {
		// Burn a hash operation anyway so a negative lookup isn't
		// 50 ms faster than a positive one.
		_ = VerifyPassword("$argon2id$v=19$m=65536,t=3,p=4$YWFhYWFhYWFhYWFhYWFhYQ$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", req.Password)
	}
	if invalid {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}
	if admin.DisabledAt != nil {
		// Same opaque 401 — disabled accounts shouldn't reveal their
		// state to an attacker guessing emails.
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	// MFA: verified=false always at Phase 0. Phase 5 (P5-02) will
	// validate req.TOTP against admin.MFASecret here and set this to
	// true only on success.
	mfaVerified := false

	access, err := GenerateToken(admin.ID, admin.RolesAsStrings(), mfaVerified, h.secret, config.DefaultAdminJWTExpiry)
	if err != nil {
		h.logger.Printf("admin login: generate token: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	rawRefresh, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		h.logger.Printf("admin login: generate refresh: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	refreshHash := jwtutil.HashRefreshToken(rawRefresh)
	now := time.Now().UTC()
	err = h.repo.SaveRefreshToken(ctx, RefreshToken{
		ID:        refreshHash,
		AdminID:   admin.ID,
		ExpiresAt: now.Add(config.DefaultAdminRefreshExpiry),
		CreatedAt: now,
		UserAgent: r.Header.Get("User-Agent"),
		IP:        clientIP(r),
	})
	if err != nil {
		h.logger.Printf("admin login: save refresh: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Best-effort: bump last-active. A failure here is observability-
	// only; the login itself still succeeds.
	if err := h.repo.UpdateLastActive(ctx, admin.ID, now); err != nil {
		h.logger.Printf("admin login: update last-active: %v", err)
	}

	response.WriteJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresAt:    now.Add(config.DefaultAdminJWTExpiry).Format(time.RFC3339),
		Admin: AdminInfo{
			ID:    admin.ID,
			Email: admin.Email,
			Roles: admin.RolesAsStrings(),
		},
	})
}

// Refresh handles POST /admin/v1/auth/refresh.
//
// The refresh token is single-use: successful validation immediately
// revokes the presented token and issues a new pair. Presenting the
// same token twice (e.g. due to a retry) fails the second attempt, so
// clients must handle the error and re-authenticate from scratch.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RefreshRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.RefreshToken == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "refreshToken is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	hash := jwtutil.HashRefreshToken(req.RefreshToken)
	existing, err := h.repo.FindRefreshToken(ctx, hash)
	if err != nil {
		h.logger.Printf("admin refresh: find failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if existing == nil {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	admin, err := h.repo.FindByID(ctx, existing.AdminID)
	if err != nil || admin == nil || admin.DisabledAt != nil {
		if err != nil {
			h.logger.Printf("admin refresh: find admin failed: %v", err)
		}
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	now := time.Now().UTC()
	// Single-use: revoke before issuing the replacement. If the save of
	// the new pair below fails, the admin has to log in again — better
	// than leaving the old token valid.
	if err := h.repo.RevokeRefreshToken(ctx, hash, now); err != nil {
		h.logger.Printf("admin refresh: revoke old: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	access, err := GenerateToken(admin.ID, admin.RolesAsStrings(), false, h.secret, config.DefaultAdminJWTExpiry)
	if err != nil {
		h.logger.Printf("admin refresh: generate token: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	rawRefresh, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		h.logger.Printf("admin refresh: generate refresh: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	newHash := jwtutil.HashRefreshToken(rawRefresh)
	err = h.repo.SaveRefreshToken(ctx, RefreshToken{
		ID:        newHash,
		AdminID:   admin.ID,
		ExpiresAt: now.Add(config.DefaultAdminRefreshExpiry),
		CreatedAt: now,
		UserAgent: r.Header.Get("User-Agent"),
		IP:        clientIP(r),
	})
	if err != nil {
		h.logger.Printf("admin refresh: save new refresh: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := h.repo.UpdateLastActive(ctx, admin.ID, now); err != nil {
		h.logger.Printf("admin refresh: update last-active: %v", err)
	}

	response.WriteJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresAt:    now.Add(config.DefaultAdminJWTExpiry).Format(time.RFC3339),
		Admin: AdminInfo{
			ID:    admin.ID,
			Email: admin.Email,
			Roles: admin.RolesAsStrings(),
		},
	})
}

// clientIP extracts the best-guess client IP for audit purposes. Prefers
// X-Forwarded-For's left-most entry (set by Caddy / Cloudflare) and
// falls back to RemoteAddr. Not used for auth decisions — only for
// logging, so a spoofed header here is a diagnostic inconvenience, not
// a security issue.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if ra := r.RemoteAddr; ra != "" {
		if idx := strings.LastIndexByte(ra, ':'); idx > 0 {
			return ra[:idx]
		}
		return ra
	}
	return ""
}

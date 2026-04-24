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

// Handler serves admin authentication endpoints: /admin/v1/auth/login
// and /admin/v1/auth/refresh. Everything else under /admin/v1/* is
// handled by other packages and wrapped in the RequireAdminAuth
// middleware.
type Handler struct {
	logger *log.Logger
	repo   Repository
	secret string
}

// NewHandler constructs a Handler.
func NewHandler(logger *log.Logger, repo Repository, jwtSecret string) *Handler {
	return &Handler{logger: logger, repo: repo, secret: jwtSecret}
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

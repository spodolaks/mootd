package auth

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

// Handler handles authentication endpoints.
type Handler struct {
	logger    *log.Logger
	repo      Repository
	jwtSecret string
}

// NewHandler creates a new auth Handler.
func NewHandler(logger *log.Logger, repo Repository, jwtSecret string) *Handler {
	return &Handler{
		logger:    logger,
		repo:      repo,
		jwtSecret: jwtSecret,
	}
}

// MockLogin handles POST /v1/auth/mock-login.
//
// Returns a signed mootd JWT for development use without real credentials.
//
// Request body:
//
//	{ "provider": "google" }
//
// Response: 200 OK — { accessToken, expiresAt, user, mode: "mock" }
// Response: 400 — { error: "unsupported provider" }
func (h *Handler) MockLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MockLoginRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "google"
	}
	if provider != "google" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
		return
	}

	token, err := jwtutil.GenerateToken("user_mock_001", "dev.user@mootd.local", h.jwtSecret, config.DefaultJWTExpiry)
	if err != nil {
		h.logger.Printf("mock-login: generate token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	rawRefresh, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		h.logger.Printf("mock-login: generate refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	refreshHash := jwtutil.HashRefreshToken(rawRefresh)
	if err := h.repo.SaveRefreshToken(r.Context(), "user_mock_001", refreshHash, time.Now().Add(30*24*time.Hour)); err != nil {
		h.logger.Printf("mock-login: save refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	response.WriteJSON(w, http.StatusOK, MockLoginResponse{
		AccessToken:  token,
		RefreshToken: rawRefresh,
		ExpiresAt:    time.Now().UTC().Add(config.DefaultJWTExpiry).Format(time.RFC3339),
		User: AuthUser{
			ID:        "user_mock_001",
			Email:     "dev.user@mootd.local",
			Name:      "MOOTD Dev User",
			AvatarURL: "https://api.dicebear.com/9.x/initials/svg?seed=MD",
		},
		Mode: "mock",
	})
}

// Google handles POST /v1/auth/google.
//
// Verifies the Google access token, upserts the user in MongoDB using only
// verified data, then returns a signed mootd JWT.
//
// Request body:
//
//	{ "accessToken": "<google access token>" }
//
// Response: 200 OK — { accessToken (mootd JWT), expiresAt, user, mode: "api" }
// Response: 400 — { error: "accessToken is required" }
// Response: 401 — { error: "invalid Google token" }
// Response: 500 — { error: "failed to save user" | "failed to issue token" }
func (h *Handler) Google(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GoogleAuthRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AccessToken == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "accessToken is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Verify with Google — do not trust client-supplied profile data.
	googleUser, err := verifyGoogleToken(ctx, req.AccessToken)
	if err != nil {
		h.logger.Printf("google auth: token verification failed: %v", err)
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid Google token"})
		return
	}

	// Upsert user with verified data only.
	if err := h.repo.UpsertByGoogleID(ctx, googleUser.Sub, googleUser.Email, googleUser.Name, googleUser.Picture); err != nil {
		h.logger.Printf("google auth: upsert user failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save user"})
		return
	}

	token, err := jwtutil.GenerateToken(googleUser.Sub, googleUser.Email, h.jwtSecret, config.DefaultJWTExpiry)
	if err != nil {
		h.logger.Printf("google auth: generate token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	rawRefresh, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		h.logger.Printf("google auth: generate refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	refreshHash := jwtutil.HashRefreshToken(rawRefresh)
	if err := h.repo.SaveRefreshToken(ctx, googleUser.Sub, refreshHash, time.Now().Add(30*24*time.Hour)); err != nil {
		h.logger.Printf("google auth: save refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	response.WriteJSON(w, http.StatusOK, GoogleAuthResponse{
		AccessToken:  token,
		RefreshToken: rawRefresh,
		ExpiresAt:    time.Now().UTC().Add(config.DefaultJWTExpiry).Format(time.RFC3339),
		User: AuthUser{
			ID:        googleUser.Sub,
			Email:     googleUser.Email,
			Name:      googleUser.Name,
			AvatarURL: googleUser.Picture,
		},
		Mode: "api",
	})
}

// Refresh handles POST /v1/auth/refresh.
//
// Validates the incoming refresh token, rotates it, and returns a new JWT + refresh token pair.
//
// Request body:
//
//	{ "refreshToken": "<opaque refresh token>" }
//
// Response: 200 OK — { accessToken, refreshToken, expiresAt, user }
// Response: 400 — { error: "refreshToken is required" }
// Response: 401 — { error: "invalid or expired refresh token" }
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
	user, err := h.repo.FindByRefreshToken(ctx, hash)
	if err != nil {
		h.logger.Printf("refresh: find by refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if user == nil {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	// Generate new JWT.
	newToken, err := jwtutil.GenerateToken(user.ID, user.Email, h.jwtSecret, config.DefaultJWTExpiry)
	if err != nil {
		h.logger.Printf("refresh: generate token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	// Rotate refresh token.
	newRawRefresh, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		h.logger.Printf("refresh: generate refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	newRefreshHash := jwtutil.HashRefreshToken(newRawRefresh)
	if err := h.repo.SaveRefreshToken(ctx, user.ID, newRefreshHash, time.Now().Add(30*24*time.Hour)); err != nil {
		h.logger.Printf("refresh: save refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}

	response.WriteJSON(w, http.StatusOK, RefreshResponse{
		AccessToken:  newToken,
		RefreshToken: newRawRefresh,
		ExpiresAt:    time.Now().UTC().Add(config.DefaultJWTExpiry).Format(time.RFC3339),
		User: AuthUser{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			AvatarURL: user.AvatarURL,
		},
	})
}

// Logout handles POST /v1/auth/logout.
//
// Revokes the caller's refresh token server-side by clearing the matching
// refreshTokenHash on the user document. The endpoint is self-authenticating:
// possession of a valid refresh token is sufficient, so it also works when the
// access token has expired.
//
// Request body:
//
//	{ "refreshToken": "<opaque refresh token>" }
//
// Responses always return 204 No Content — whether or not the token matched —
// so the endpoint can't be used as a refresh-token oracle.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LogoutRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.RefreshToken == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hash := jwtutil.HashRefreshToken(req.RefreshToken)
	if _, err := h.repo.ClearRefreshTokenByHash(ctx, hash); err != nil {
		h.logger.Printf("logout: clear refresh token failed: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

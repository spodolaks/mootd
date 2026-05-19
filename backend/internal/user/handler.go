package user

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"mootd/backend/internal/shared/gender"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// CascadeFn deletes every piece of data owned by userID across the domains the
// user package doesn't import directly (wardrobe, moodboard, etc). It is wired
// in app.go so that user stays free of cross-domain imports.
type CascadeFn func(ctx context.Context, userID string) error

// Handler handles user profile endpoints.
type Handler struct {
	logger  *log.Logger
	repo    Repository
	cascade CascadeFn
}

// NewHandler creates a new user Handler. cascade may be nil in contexts where
// account deletion is not wired; the DELETE endpoint will then respond 503.
func NewHandler(logger *log.Logger, repo Repository, cascade CascadeFn) *Handler {
	return &Handler{logger: logger, repo: repo, cascade: cascade}
}

// Profile routes GET, PUT, and DELETE requests to the appropriate sub-handler.
// The authenticated user's ID is resolved from the JWT via Auth middleware.
func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getProfile(w, r)
	case http.MethodPut:
		h.updateProfile(w, r)
	case http.MethodDelete:
		h.deleteAccount(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getProfile handles GET /v1/user/profile.
//
// Response: 200 OK — full UserDocument
// Response: 401 — missing/invalid JWT
// Response: 404 — user not found
func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	user, err := h.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.logger.Printf("get profile: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch user"})
		return
	}

	response.WriteJSON(w, http.StatusOK, user)
}

// updateProfile handles PUT /v1/user/profile.
//
// Request body: { "name": "...", "avatarUrl": "..." } (at least one field required)
// Response: 200 OK — updated UserDocument
// Response: 400 — no fields to update
// Response: 401 — missing/invalid JWT
// Response: 404 — user not found
func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req UpdateProfileRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Gender != nil && !gender.ValidUser(*req.Gender) {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "gender must be male, female, or unisex"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	updated, err := h.repo.Update(ctx, userID, req)
	if err != nil {
		switch {
		case err.Error() == "no fields to update":
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no fields to update"})
		case errors.Is(err, mongo.ErrNoDocuments):
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		default:
			h.logger.Printf("update profile: %v", err)
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		}
		return
	}

	response.WriteJSON(w, http.StatusOK, updated)
}

// deleteAccount handles DELETE /v1/user/profile.
//
// Erases all data owned by the authenticated user: wardrobe items and their
// images, moodboards, and the user document itself. The cascade is orchestrated
// by a CascadeFn wired at app startup, so this handler stays free of
// cross-domain imports.
//
// Response: 204 No Content on success
// Response: 401 — missing/invalid JWT
// Response: 500 — cascade or deletion failure
// Response: 503 — cascade not configured
func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	if h.cascade == nil {
		h.logger.Printf("delete account: cascade not configured")
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account deletion unavailable"})
		return
	}

	// Generous timeout — GridFS cleanup on a large wardrobe takes time, and a
	// partial deletion is worse than a slow one.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := h.cascade(ctx, userID); err != nil {
		h.logger.Printf("delete account: cascade for user %s failed: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete account"})
		return
	}

	h.logger.Printf("delete account: user %s erased", userID)
	w.WriteHeader(http.StatusNoContent)
}

package user

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// Handler handles user profile endpoints.
type Handler struct {
	logger *log.Logger
	repo   Repository
}

// NewHandler creates a new user Handler.
func NewHandler(logger *log.Logger, repo Repository) *Handler {
	return &Handler{logger: logger, repo: repo}
}

// Profile routes GET and PUT requests to the appropriate sub-handler.
// The authenticated user's ID is resolved from the JWT via Auth middleware.
func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getProfile(w, r)
	case http.MethodPut:
		h.updateProfile(w, r)
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

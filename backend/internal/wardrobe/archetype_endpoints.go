package wardrobe

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// ArchetypeFillerSeeder takes the user's "I have this IRL" tap on
// a filler in a moodboard and turns it into a real wardrobe item.
// Defined as an interface here so wardrobe/ can drive the endpoint
// without importing admin/ — the app/-side adapter is the single
// place that touches both repos.
//
// Idempotent: a default already promoted into this user's wardrobe
// returns the existing wi_<hex> id rather than minting a duplicate.
type ArchetypeFillerSeeder interface {
	SeedForUser(ctx context.Context, userID, defaultID string) (string, error)
}

// ArchetypeEndpointsConfig wires the optional dependencies needed
// to serve /v1/wardrobe/items/from-archetype-default and
// /v1/wardrobe/archetype-rejections. Both are nil-safe — when the
// admin defaults plumbing isn't wired (legacy boot, repo init
// failed) the endpoints return 503.
type ArchetypeEndpointsConfig struct {
	// Seeder copies one curated default into the user's wardrobe
	// when they tap "I have this IRL".
	Seeder ArchetypeFillerSeeder
	// Rejections records "not in my wardrobe" so subsequent outfit
	// generations exclude that default for this user.
	Rejections ArchetypeRejectionsRepository
}

// WithArchetypeEndpoints attaches the from-archetype-default + reject
// endpoints to the handler. Returns the receiver for builder-style
// chaining at boot.
func (h *Handler) WithArchetypeEndpoints(cfg ArchetypeEndpointsConfig) *Handler {
	h.archetypeSeeder = cfg.Seeder
	h.archetypeRejections = cfg.Rejections
	return h
}

// FromArchetypeDefaultRequest is the body for
// POST /v1/wardrobe/items/from-archetype-default.
type FromArchetypeDefaultRequest struct {
	DefaultID string `json:"defaultId"`
}

// ArchetypeRejectionRequest is the body for
// POST /v1/wardrobe/archetype-rejections.
type ArchetypeRejectionRequest struct {
	DefaultID string `json:"defaultId"`
}

// FromArchetypeDefaultResponse mirrors the wardrobe item shape so
// the FE can drop the new row into its local list without a follow-
// up GET.
type FromArchetypeDefaultResponse struct {
	Item ClothingItem `json:"item"`
}

// FromArchetypeDefault handles POST /v1/wardrobe/items/from-archetype-default.
//
// Body: { defaultId: "ad_<hex>" }
//
// Promotes the curated default into the user's wardrobe and returns
// the resulting wi_<hex> ClothingItem. Idempotent — calling twice for
// the same default returns the same wardrobe row. Also clears any
// matching "rejection" so a previously-dismissed item that the user
// now claims doesn't sit in the reject list.
func (h *Handler) FromArchetypeDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.archetypeSeeder == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "archetype defaults not wired",
		})
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req FromArchetypeDefaultRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defaultID := strings.TrimSpace(req.DefaultID)
	if !strings.HasPrefix(defaultID, "ad_") {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "defaultId must look like ad_<hex>",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	newID, err := h.archetypeSeeder.SeedForUser(ctx, userID, defaultID)
	if err != nil {
		h.logger.Printf("wardrobe: from-archetype-default user=%s default=%s: %v", userID, defaultID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "seed failed: " + err.Error()})
		return
	}

	// Re-load the seeded row so we return the canonical shape rather
	// than reconstructing from the request. Cheap (single Mongo
	// fetch) and keeps the response truthful when the seeder
	// idempotent-returned an existing row.
	items, err := h.repo.FindByUser(ctx, userID)
	if err != nil {
		h.logger.Printf("wardrobe: from-archetype-default reload user=%s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "seed succeeded but reload failed"})
		return
	}
	var found *ClothingItem
	for i := range items {
		if items[i].ID == newID {
			found = &items[i]
			break
		}
	}
	if found == nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("seeded id %s not found post-write", newID),
		})
		return
	}

	// User just claimed this item — they own it now, so clear any
	// prior "not in wardrobe" rejection so the data model stays
	// consistent (mootd#75). Best-effort: a failure here doesn't
	// roll back the seed; we log and move on.
	if h.archetypeRejections != nil {
		if err := h.archetypeRejections.Delete(ctx, userID, defaultID); err != nil {
			h.logger.Printf("wardrobe: from-archetype-default user=%s clear stale rejection %s: %v (continuing)",
				userID, defaultID, err)
		}
	}

	h.logger.Printf("wardrobe: user %s claimed default %s as %s", userID, defaultID, newID)
	response.WriteJSON(w, http.StatusOK, FromArchetypeDefaultResponse{Item: *found})
}

// ArchetypeRejection handles POST /v1/wardrobe/archetype-rejections.
//
// Body: { defaultId: "ad_<hex>" }
//
// Records that this user explicitly does NOT have the curated
// default; the outfit-generation filler loader excludes it on
// subsequent runs. Idempotent — re-rejecting is a 200/no-op.
func (h *Handler) ArchetypeRejection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.archetypeRejections == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "archetype defaults not wired",
		})
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req ArchetypeRejectionRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defaultID := strings.TrimSpace(req.DefaultID)
	if !strings.HasPrefix(defaultID, "ad_") {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "defaultId must look like ad_<hex>",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.archetypeRejections.Add(ctx, userID, defaultID); err != nil {
		h.logger.Printf("wardrobe: archetype-rejection user=%s default=%s: %v", userID, defaultID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "rejection failed"})
		return
	}

	h.logger.Printf("wardrobe: user %s rejected default %s", userID, defaultID)
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"defaultId": defaultID,
		"rejected":  true,
	})
}

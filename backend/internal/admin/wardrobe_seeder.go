package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// WardrobeSeeder copies archetype defaults into a user's
// wardrobe. The admin package owns the contract (so the seed
// endpoint lives next to the curation surface) but the
// implementation reaches into wardrobe-side state — wired in
// app.go via an adapter so admin/ stays free of a wardrobe
// import.
type WardrobeSeeder interface {
	// Seed copies the defaults for `archetypeName` into the
	// user's wardrobe. Returns the number of items seeded
	// (which can be < len(defaults) if some are duplicates of
	// existing wardrobe items — the caller's choice).
	Seed(ctx context.Context, userID, archetypeName string) (int, error)
}

// WithWardrobeSeeder wires the seed adapter so the admin
// /users/seed-from-archetype endpoint becomes available.
func (h *Handler) WithWardrobeSeeder(s WardrobeSeeder) *Handler {
	h.wardrobeSeeder = s
	return h
}

// SeedWardrobeRouter handles POST /admin/v1/users/seed-from-archetype/{userId}.
//
// Body:
//
//	{ "archetype": "rebel", "notes": "support ticket #1247" }
//
// notes is required and lands in the audit row alongside the
// archetype + count seeded. Powers two flows:
//
//  1. Manual seeding by ops when a user reports an empty
//     wardrobe.
//  2. The "promote a fresh sign-up" path the mobile app may
//     call via admin (future feature).
//
// Permission: users:purge — same gate as the destructive purge
// endpoint, since seeding modifies the user's owned data
// without their direct input. Audited.
func (h *Handler) SeedWardrobeRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermUsersPurge) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermUsersPurge,
		})
		return
	}
	if h.wardrobeSeeder == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wardrobe seeder not wired"})
		return
	}

	userID := strings.TrimPrefix(r.URL.Path, "/admin/v1/users/seed-from-archetype/")
	userID = strings.TrimSpace(userID)
	if userID == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	var body struct {
		Archetype string `json:"archetype"`
		Notes     string `json:"notes"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Archetype) == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "archetype is required"})
		return
	}
	if strings.TrimSpace(body.Notes) == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "notes is required (audit-trail rationale)"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	count, err := h.wardrobeSeeder.Seed(ctx, userID, body.Archetype)
	if err != nil {
		if errors.Is(err, ErrUserNotFoundForSeed) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.logger.Printf("admin /users/seed-from-archetype/%s (archetype=%s): %v", userID, body.Archetype, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if h.repo != nil {
		adminID, _ := AdminIDFromContext(r.Context())
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:           generateAuditID(),
			AdminID:      adminID,
			AdminEmail:   adminEmail,
			Action:       "wardrobe.seed_from_archetype",
			TargetUserID: userID,
			TargetEntity: "users/" + userID,
			Metadata: map[string]any{
				"archetype":  body.Archetype,
				"notes":      body.Notes,
				"seededCount": count,
			},
			At:        time.Now().UTC(),
			IP:        clientIP(r),
			UserAgent: r.Header.Get("User-Agent"),
		})
	}

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"userId":      userID,
		"archetype":   body.Archetype,
		"seededCount": count,
	})
}

// ErrUserNotFoundForSeed is returned by a WardrobeSeeder when
// the user can't be found. Sentinel so the handler can map to
// 404 cleanly.
var ErrUserNotFoundForSeed = errors.New("admin: user not found for archetype seed")

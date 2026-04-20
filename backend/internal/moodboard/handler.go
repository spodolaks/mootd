package moodboard

import (
	"context"
	"log"
	"net/http"
	"time"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/shared/id"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/pagination"
	"mootd/backend/internal/shared/response"
	"mootd/backend/internal/wardrobe"
)

// SaveEventFn is called after a moodboard is successfully saved. Implementations
// typically append a feedback.Event so later training jobs can reconstruct
// preference pairs (saved outfit vs the rejected members of GeneratedBatch).
//
// It is best-effort: errors are logged but never fail the user's save, and the
// handler skips the hook entirely when nil. Wiring happens in app.go so the
// moodboard package stays free of a direct feedback-package import.
type SaveEventFn func(ctx context.Context, userID string, req SaveRequest, saved SavedMoodBoard)

// Handler handles moodboard HTTP endpoints.
type Handler struct {
	logger        *log.Logger
	repo          Repository
	wardrobeRepo  wardrobeRepository
	profileUpdate profileUpdater
	onSave        SaveEventFn
}

// wardrobeRepository is the subset of the wardrobe repo needed to snapshot items.
type wardrobeRepository interface {
	FindByUser(ctx context.Context, userID string) ([]wardrobe.ClothingItem, error)
}

// profileUpdater reads and updates the user's archetype profile.
type profileUpdater interface {
	GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error)
	UpdateArchetypeProfile(ctx context.Context, userID string, profile map[string]float64) error
}

// ProfileUpdaterFunc adapts functions to satisfy profileUpdater.
type ProfileUpdaterFunc struct {
	GetFn    func(ctx context.Context, userID string) (map[string]float64, error)
	UpdateFn func(ctx context.Context, userID string, profile map[string]float64) error
}

func (f ProfileUpdaterFunc) GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error) {
	return f.GetFn(ctx, userID)
}
func (f ProfileUpdaterFunc) UpdateArchetypeProfile(ctx context.Context, userID string, profile map[string]float64) error {
	return f.UpdateFn(ctx, userID, profile)
}

// NewHandler creates a Handler.
// profileUpdate may be nil if archetype tracking is not configured.
// onSave may be nil; when provided it runs after a successful save and is
// where the feedback event is emitted (see app.go for the wiring).
func NewHandler(logger *log.Logger, repo Repository, wardrobeRepo wardrobeRepository, profileUpdate profileUpdater, onSave SaveEventFn) *Handler {
	return &Handler{logger: logger, repo: repo, wardrobeRepo: wardrobeRepo, profileUpdate: profileUpdate, onSave: onSave}
}

// Save handles POST /v1/moodboards.
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req SaveRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	date := req.Date
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	// Resolve item IDs to snapshots for permanent display.
	snapshots := h.resolveSnapshots(r.Context(), userID, req.Outfit.Items)

	outfit := req.Outfit
	outfit.Snapshots = snapshots

	board := SavedMoodBoard{
		ID:        id.Generate(),
		UserID:    userID,
		Outfit:    outfit,
		Date:      date,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.repo.Save(r.Context(), board); err != nil {
		h.logger.Printf("moodboard: save for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save moodboard"})
		return
	}

	// Update user's archetype profile based on this outfit choice.
	if h.profileUpdate != nil && len(outfit.ArchetypeScores) > 0 {
		h.updateUserProfile(r.Context(), userID, outfit.ArchetypeScores)
	}

	// Emit the feedback event last so a downstream failure never rolls back
	// the user's save. The hook itself is expected to swallow errors.
	if h.onSave != nil {
		h.onSave(r.Context(), userID, req, board)
	}

	response.WriteJSON(w, http.StatusCreated, board)
}

// updateUserProfile blends the outfit's archetype scores into the user's profile
// using exponential moving average (alpha=0.3).
func (h *Handler) updateUserProfile(ctx context.Context, userID string, outfitScores map[string]float64) {
	existing, err := h.profileUpdate.GetArchetypeProfile(ctx, userID)
	if err != nil {
		h.logger.Printf("moodboard: read archetype profile for %s: %v", userID, err)
		existing = nil
	}

	var merged archetype.Scores
	if len(existing) > 0 {
		merged = archetype.Merge(existing, outfitScores, 0.3)
	} else {
		// First save — use outfit scores as initial profile.
		merged = outfitScores
	}

	if err := h.profileUpdate.UpdateArchetypeProfile(ctx, userID, merged); err != nil {
		h.logger.Printf("moodboard: update archetype profile for %s: %v", userID, err)
	}
}

func (h *Handler) resolveSnapshots(ctx context.Context, userID string, itemIDs []string) []OutfitItem {
	items, err := h.wardrobeRepo.FindByUser(ctx, userID)
	if err != nil {
		h.logger.Printf("moodboard: resolve snapshots: %v", err)
		return nil
	}

	lookup := make(map[string]wardrobe.ClothingItem, len(items))
	for _, item := range items {
		lookup[item.ID] = item
	}

	snapshots := make([]OutfitItem, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		if item, ok := lookup[itemID]; ok {
			snapshots = append(snapshots, OutfitItem{
				ID:          item.ID,
				Category:    item.Category,
				Label:       item.Label,
				ImageURL:    item.ImageURL,
				PngImageURL: item.PngImageURL,
			})
		}
	}
	return snapshots
}

// List handles GET /v1/moodboards.
// Supports cursor-based pagination via ?limit=N&cursor=<token>.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	limit, cursor := pagination.ParseParams(r, 10, 100)

	boards, err := h.repo.FindByUserPaginated(r.Context(), userID, limit, cursor)
	if err != nil {
		h.logger.Printf("moodboard: list for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch moodboards"})
		return
	}

	var nextCursor *string
	if len(boards) > limit {
		boards = boards[:limit]
		last := boards[limit-1]
		nc := pagination.EncodeCursor(last.CreatedAt, last.ID)
		nextCursor = &nc
	}

	if boards == nil {
		boards = []SavedMoodBoard{}
	}

	response.WriteJSON(w, http.StatusOK, ListResponse{MoodBoards: boards, NextCursor: nextCursor})
}


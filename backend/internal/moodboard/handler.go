package moodboard

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/shared/id"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/pagination"
	"mootd/backend/internal/shared/response"
	"mootd/backend/internal/wardrobe"
)

// maxBoardImageBytes caps how large a rendered collage PNG we accept per
// save. Well above a typical ~600KB retina capture so legitimate uploads go
// through, tight enough that a malicious or broken client can't bloat
// GridFS. Decoded bytes, not base64 length.
const maxBoardImageBytes = 5 * 1024 * 1024

// maxSaveRequestBytes caps the raw POST body. It must comfortably exceed
// maxBoardImageBytes once base64 overhead (~33%) and the surrounding JSON
// fields are included, otherwise the JSON decoder's MaxBytesReader trips
// before handler logic runs — a symptom we hit in #18 when the shared
// 1 MiB default rejected every web-captured save. 10 MiB gives a ~3 MiB
// headroom above the decoded-image cap.
const maxSaveRequestBytes = 10 * 1024 * 1024

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
	// Use the larger per-endpoint cap so the rendered collage PNG doesn't
	// trip the shared 1 MiB default. See maxSaveRequestBytes for rationale.
	if err := response.DecodeJSONBodyWithLimit(w, r, &req, maxSaveRequestBytes); err != nil {
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

	boardID := id.Generate()

	// Persist the rendered collage image first (best-effort). On success we
	// stamp the board with its image URL so a single insert covers both the
	// doc and its render reference. On failure we log and fall through — the
	// calendar can still render from Outfit.Snapshots.
	var imageURL string
	if req.BoardImage != "" {
		if data, ct, err := decodeBoardImage(req.BoardImage); err != nil {
			h.logger.Printf("moodboard: decode board image for user %s: %v (saving without render)", userID, err)
		} else if len(data) > maxBoardImageBytes {
			h.logger.Printf("moodboard: board image %d bytes exceeds cap %d for user %s (saving without render)", len(data), maxBoardImageBytes, userID)
		} else if err := h.repo.SaveImage(r.Context(), boardID, data, ct); err != nil {
			h.logger.Printf("moodboard: save board image for user %s board %s: %v (saving without render)", userID, boardID, err)
		} else {
			imageURL = "/v1/moodboards/" + boardID + "/image"
		}
	}

	board := SavedMoodBoard{
		ID:        boardID,
		UserID:    userID,
		Outfit:    outfit,
		Date:      date,
		ImageURL:  imageURL,
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

// ServeImage handles GET /v1/moodboards/{id}/image.
//
// Like the wardrobe item-image endpoint this is unauthenticated — the
// moodboard ID is a UUID, and gating behind auth would require the RN
// `expo-image` component to send the Bearer token, which we don't wire
// today. That's a known P1 from the earlier security review; we reapply
// the same tradeoff here so behaviour stays consistent until signed URLs
// land.
func (h *Handler) ServeImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path: /v1/moodboards/{id}/image
	path := strings.TrimPrefix(r.URL.Path, "/v1/moodboards/")
	boardID := strings.TrimSuffix(path, "/image")
	if boardID == "" || boardID == path {
		http.NotFound(w, r)
		return
	}

	data, contentType, err := h.repo.GetImage(r.Context(), boardID)
	if err != nil {
		if errors.Is(err, mongo.ErrFileNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Printf("moodboard: serve image %s: %v", boardID, err)
		http.Error(w, "failed to read image", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

// decodeBoardImage accepts either a raw base64 string or a full
// "data:image/png;base64,..." URL and returns the decoded bytes plus the
// MIME type advertised in the prefix (defaulting to image/png). Any parse
// failure surfaces as an error so the caller can fall back to saving
// without a render.
func decodeBoardImage(raw string) ([]byte, string, error) {
	contentType := "image/png"
	payload := raw
	if strings.HasPrefix(raw, "data:") {
		comma := strings.Index(raw, ",")
		if comma < 0 {
			return nil, "", errors.New("malformed data URL (missing comma)")
		}
		header := raw[5:comma]
		payload = raw[comma+1:]
		// header is e.g. "image/png;base64"
		if semi := strings.Index(header, ";"); semi >= 0 {
			ct := strings.TrimSpace(header[:semi])
			if ct != "" {
				contentType = ct
			}
		} else if header != "" {
			contentType = header
		}
	}

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

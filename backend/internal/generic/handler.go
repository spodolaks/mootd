package generic

import (
	"context"
	"log"
	"net/http"
	"time"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/shared/id"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
	"mootd/backend/internal/wardrobe"
)

// wardrobeProvider reads wardrobe items for prediction.
type wardrobeProvider interface {
	FindByUser(ctx context.Context, userID string) ([]wardrobe.ClothingItem, error)
}

// profileProvider reads user archetype profile.
type profileProvider interface {
	GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error)
}

// Handler handles generic item endpoints.
type Handler struct {
	logger       *log.Logger
	repo         Repository
	wardrobeRepo wardrobeProvider
	profile      profileProvider
}

// NewHandler creates a Handler.
func NewHandler(
	logger *log.Logger,
	repo Repository,
	wardrobeRepo wardrobeProvider,
	profile profileProvider,
) *Handler {
	return &Handler{logger: logger, repo: repo, wardrobeRepo: wardrobeRepo, profile: profile}
}

// ListItems handles GET /v1/generic/items.
// Returns generic items matching the user's archetype profile.
func (h *Handler) ListItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	items, err := h.wardrobeRepo.FindByUser(r.Context(), userID)
	if err != nil {
		h.logger.Printf("generic: fetch wardrobe for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch wardrobe"})
		return
	}

	// Compute effective archetype scores.
	traits := make([]archetype.ItemTraits, len(items))
	for i, item := range items {
		traits[i] = archetype.ItemTraits{
			Category:       item.Category,
			Color:          item.Traits["color"],
			ColorSecondary: item.Traits["color_secondary"],
			Fabric:         item.Traits["fabric"],
			Style:          item.Traits["style"],
			Occasion:       item.Traits["occasion"],
			OverallStyle:   item.Traits["overall_style"],
			Details:        item.Traits["details"],
		}
	}
	wardrobeScores := archetype.ScoreItems(traits)

	// Merge with stored profile if available.
	effectiveScores := wardrobeScores
	if h.profile != nil {
		stored, _ := h.profile.GetArchetypeProfile(r.Context(), userID)
		if len(stored) > 0 {
			effectiveScores = archetype.Merge(stored, wardrobeScores, 0.6)
		}
	}

	// Count items per category group.
	catCounts := map[string]int{}
	for _, item := range items {
		catCounts[categoryGroup(item.Category)]++
	}

	limit := MinWardrobeSize - len(items)
	if limit <= 0 {
		limit = 0
	}
	if limit > MaxGenericItems {
		limit = MaxGenericItems
	}

	genericItems, err := h.repo.FindMatching(r.Context(), effectiveScores, catCounts, limit)
	if err != nil {
		h.logger.Printf("generic: find matching for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch generic items"})
		return
	}

	if genericItems == nil {
		genericItems = []GenericItem{}
	}

	response.WriteJSON(w, http.StatusOK, ListResponse{Items: genericItems})
}

// TriggerGeneration handles POST /v1/generic/trigger.
// Runs prediction and creates generic items for missing wardrobe slots.
func (h *Handler) TriggerGeneration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	items, err := h.wardrobeRepo.FindByUser(r.Context(), userID)
	if err != nil {
		h.logger.Printf("generic: fetch wardrobe for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch wardrobe"})
		return
	}

	if len(items) >= MinWardrobeSize {
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "wardrobe sufficient"})
		return
	}

	// Compute scores.
	traits := make([]archetype.ItemTraits, len(items))
	for i, item := range items {
		traits[i] = archetype.ItemTraits{
			Category:       item.Category,
			Color:          item.Traits["color"],
			ColorSecondary: item.Traits["color_secondary"],
			Fabric:         item.Traits["fabric"],
			Style:          item.Traits["style"],
			Occasion:       item.Traits["occasion"],
			OverallStyle:   item.Traits["overall_style"],
			Details:        item.Traits["details"],
		}
	}
	scores := archetype.ScoreItems(traits)

	// Predict missing items.
	predictions := PredictMissingItems(items, scores, MinWardrobeSize)
	if len(predictions) == 0 {
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "no predictions needed"})
		return
	}

	topArch := archetype.TopN(scores, 1)
	primaryArchetype := ""
	if len(topArch) > 0 {
		primaryArchetype = topArch[0].Name
	}

	// Create or reuse generic items for each prediction.
	created := 0
	reused := 0
	for _, pred := range predictions {
		dedupKey := DedupKey(pred.SourceArchetype, pred.Category, pred.Label)

		item := GenericItem{
			ID:               id.Generate(),
			Category:         pred.Category,
			Label:            pred.Label,
			Description:      pred.Label, // TODO: enrich with Ollama in Phase 2
			Traits:           pred.Traits,
			ArchetypeScores:  scores,
			PrimaryArchetype: primaryArchetype,
			DedupKey:         dedupKey,
			CreatedAt:        time.Now().UTC(),
		}

		_, isNew, err := h.repo.FindOrCreate(r.Context(), item)
		if err != nil {
			h.logger.Printf("generic: create item %q: %v", pred.Label, err)
			continue
		}
		if isNew {
			created++
		} else {
			reused++
		}
	}

	h.logger.Printf("generic: trigger for user %s: %d predicted, %d created, %d reused",
		userID, len(predictions), created, reused)

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"predicted":   len(predictions),
		"created":     created,
		"reused":      reused,
	})
}

package brands

import (
	"log"
	"net/http"
	"strings"

	"mootd/backend/internal/shared/response"
)

// Handler handles brand HTTP endpoints.
type Handler struct {
	logger *log.Logger
	repo   Repository
}

// NewHandler creates a Handler.
func NewHandler(logger *log.Logger, repo Repository) *Handler {
	return &Handler{logger: logger, repo: repo}
}

// Save handles POST /v1/brands.
// Body: { "name": "Nike" }
// Response 200: { "name": "Nike" }
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body SaveBrandRequest
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	if err := h.repo.Save(r.Context(), name); err != nil {
		h.logger.Printf("brands: save %q: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save brand"})
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"name": name})
}

// Search handles GET /v1/brands?q=query.
// Response 200: { "brands": ["Nike", "Nikita", ...] }
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	brands, err := h.repo.Search(r.Context(), query)
	if err != nil {
		h.logger.Printf("brands: search %q: %v", query, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "search failed"})
		return
	}

	response.WriteJSON(w, http.StatusOK, SearchResponse{Brands: brands})
}

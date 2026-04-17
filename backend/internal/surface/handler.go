package surface

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Handler serves surface images to the frontend. Metadata isn't exposed —
// the LLM consumes it via the outfit service, and clients only ever need
// the rendered pixels.
type Handler struct {
	logger *log.Logger
	repo   Repository
}

// NewHandler constructs a surface HTTP handler.
func NewHandler(logger *log.Logger, repo Repository) *Handler {
	return &Handler{logger: logger, repo: repo}
}

// RegisterRoutes wires the public image-serving route. No auth: surface IDs
// are opaque and images are non-sensitive, matching the wardrobe image route.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/surfaces/", h.ServeImage)
}

// ServeImage handles GET /v1/surfaces/{id}/image.
func (h *Handler) ServeImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/surfaces/")
	id := strings.TrimSuffix(path, "/image")
	if id == "" || id == path {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	data, contentType, err := h.repo.GetImage(r.Context(), id)
	if err != nil {
		if errors.Is(err, mongo.ErrFileNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.logger.Printf("surface: serve image for %s: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := w.Write(data); err != nil {
		h.logger.Printf("surface: serve image for %s: write body failed: %v", id, err)
	}
}

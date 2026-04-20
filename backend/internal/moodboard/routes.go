package moodboard

import (
	"net/http"
	"strings"
)

// RegisterRoutes registers moodboard routes on mux.
//
//   - /v1/moodboards         — auth-required list / save
//   - /v1/moodboards/{id}/image — public, matches the wardrobe-image pattern
//     (UUID in the path; auth would require the frontend image component to
//     attach a Bearer token, which is tracked as a separate item).
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/v1/moodboards", authMiddleware(http.HandlerFunc(h.dispatch)))
	mux.HandleFunc("/v1/moodboards/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/image") {
			h.ServeImage(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.Save(w, r)
	case http.MethodGet:
		h.List(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

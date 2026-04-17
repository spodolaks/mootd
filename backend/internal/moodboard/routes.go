package moodboard

import "net/http"

// RegisterRoutes registers moodboard routes on mux. /v1/moodboards is auth-only.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/v1/moodboards", authMiddleware(http.HandlerFunc(h.dispatch)))
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

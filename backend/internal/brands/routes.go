package brands

import "net/http"

// RegisterRoutes registers brand routes on mux.
// Both endpoints require auth to prevent unauthenticated writes/reads.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/v1/brands", authMiddleware(http.HandlerFunc(h.Brands)))
}

// Brands dispatches GET (search) and POST (save) on /v1/brands.
func (h *Handler) Brands(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.Save(w, r)
	case http.MethodGet:
		h.Search(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

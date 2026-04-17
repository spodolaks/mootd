package generic

import "net/http"

// RegisterRoutes registers generic item routes on mux. All require auth.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/v1/generic/items", authMiddleware(http.HandlerFunc(h.ListItems)))
	mux.Handle("/v1/generic/trigger", authMiddleware(http.HandlerFunc(h.TriggerGeneration)))
}

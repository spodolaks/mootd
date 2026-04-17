package outfit

import "net/http"

// RegisterRoutes registers outfit routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/v1/outfits", authMiddleware(http.HandlerFunc(h.Generate)))
	mux.Handle("/v1/outfits/generate", authMiddleware(http.HandlerFunc(h.SubmitGenerate)))
	mux.Handle("/v1/outfits/jobs/", authMiddleware(http.HandlerFunc(h.PollJob)))
}

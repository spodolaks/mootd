package user

import "net/http"

// RegisterRoutes registers the user domain routes on mux.
// Protected routes are wrapped with authMiddleware.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("/v1/user/profile", authMiddleware(http.HandlerFunc(h.Profile)))
}

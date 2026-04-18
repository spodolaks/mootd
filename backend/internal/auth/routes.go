package auth

import "net/http"

// RegisterRoutes registers the auth domain routes on mux.
// When enableMockLogin is false (e.g. production), the mock-login endpoint is not registered.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, enableMockLogin bool) {
	if enableMockLogin {
		mux.HandleFunc("/v1/auth/mock-login", h.MockLogin)
	}
	mux.HandleFunc("/v1/auth/google", h.Google)
	mux.HandleFunc("/v1/auth/refresh", h.Refresh)
	mux.HandleFunc("/v1/auth/logout", h.Logout)
}

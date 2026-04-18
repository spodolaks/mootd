package auth

import "net/http"

// Middleware is the standard middleware signature used across the codebase.
type Middleware = func(http.Handler) http.Handler

// RegisterRoutes registers the auth domain routes on mux.
//
// authLimit is wrapped around every unauthenticated auth endpoint so that
// refresh-token spraying, credential enumeration, and logout spam are all
// rate-limited by IP. Pass nil (e.g. when Redis is unavailable) to skip.
//
// When enableMockLogin is false (e.g. production), the mock-login endpoint is
// not registered at all.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, enableMockLogin bool, authLimit Middleware) {
	wrap := func(h http.Handler) http.Handler {
		if authLimit == nil {
			return h
		}
		return authLimit(h)
	}

	if enableMockLogin {
		mux.Handle("/v1/auth/mock-login", wrap(http.HandlerFunc(h.MockLogin)))
	}
	mux.Handle("/v1/auth/google", wrap(http.HandlerFunc(h.Google)))
	mux.Handle("/v1/auth/refresh", wrap(http.HandlerFunc(h.Refresh)))
	mux.Handle("/v1/auth/logout", wrap(http.HandlerFunc(h.Logout)))
}

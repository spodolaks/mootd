// Package middleware contains shared HTTP middleware used across all domain routes.
package middleware

import "net/http"

// CORS returns a middleware that adds Cross-Origin Resource Sharing headers.
// Set CORS_ALLOWED_ORIGINS (comma-separated) in the environment to configure allowed origins.
// Pass "*" to allow all origins (development only).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		originSet[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" {
				if _, ok := originSet[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			// X-Trial-Id / X-Describer / X-Request-Id carry the training-trial
			// routing metadata on POST /admin/v1/training/process (#36). Without
			// them in the allowlist the browser's preflight blocks the upload.
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Trial-Id, X-Describer, X-Request-Id")

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

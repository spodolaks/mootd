// Package middleware contains shared HTTP middleware used across all domain routes.
package middleware

import (
	"context"
	"net/http"
	"strings"

	jwtutil "mootd/backend/internal/shared/jwt"
)

type contextKey string

// UserIDKey is the context key under which the authenticated user's ID is stored.
const UserIDKey contextKey = "userID"

// Auth returns a middleware that validates a Bearer JWT from the Authorization header.
// On success it stores the user ID in the request context; on failure it responds 401.
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"missing or invalid authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := jwtutil.ValidateToken(tokenString, jwtSecret)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.RegisteredClaims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the authenticated user ID from the request context.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(UserIDKey).(string)
	return id, ok
}

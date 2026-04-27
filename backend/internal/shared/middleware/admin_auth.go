package middleware

import (
	"net/http"
	"strings"

	"mootd/backend/internal/admin"
)

// RequireAdminAuth validates an admin JWT (issuer=mootd-admin, signed
// with ADMIN_JWT_SECRET) and decorates the request context with the
// admin identity via admin.ContextWithAuth. On failure, responds 401
// with a generic error body so the endpoint can't be used as a token
// oracle.
//
// Context-shape ownership lives in the admin package — see
// admin/context.go for AdminIDFromContext, AdminRolesFromContext,
// AdminMFAVerifiedFromContext. This file is the HTTP-layer adapter
// only; it never adds keys of its own and never reads them either.
//
// Never call this with the user-side JWT secret. The issuer check
// inside admin.ValidateToken is defense-in-depth; the config-layer
// guard that refuses matching JWT_SECRET / ADMIN_JWT_SECRET is the
// primary line.
func RequireAdminAuth(adminJWTSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeAdminUnauthorized(w)
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := admin.ValidateToken(tokenString, adminJWTSecret)
			if err != nil {
				writeAdminUnauthorized(w)
				return
			}

			ctx := admin.ContextWithAuth(r.Context(), claims.Subject, claims.Roles, claims.MFAVerified)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAdminUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	// Distinct error message from the user-side Auth middleware so
	// logs make it trivial to tell which side rejected a request, but
	// the body exposes no information the caller could use to tell
	// "missing header" from "bad token" from "expired token."
	http.Error(w, `{"error":"admin authorization required"}`, http.StatusUnauthorized)
}

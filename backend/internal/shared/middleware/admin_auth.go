package middleware

import (
	"context"
	"net/http"
	"strings"

	"mootd/backend/internal/admin"
)

// adminContextKey is a private type so admin keys can't be confused with
// user keys by accidental string match.
type adminContextKey string

const (
	// AdminIDKey holds the authenticated admin's ID.
	AdminIDKey adminContextKey = "adminID"
	// AdminRolesKey holds the authenticated admin's role list.
	AdminRolesKey adminContextKey = "adminRoles"
	// AdminMFAVerifiedKey is true when the current session has a fresh
	// MFA-verified token. Phase-5 privileged actions gate on this bit.
	AdminMFAVerifiedKey adminContextKey = "adminMFAVerified"
)

// RequireAdminAuth validates an admin JWT (issuer=mootd-admin, signed
// with ADMIN_JWT_SECRET). On success, stuffs the admin's ID, roles, and
// MFA state into the request context. On failure, responds 401 with a
// generic error body so the endpoint can't be used as a token oracle.
//
// Never call this with the user-side JWT secret — the issuer check
// will still reject, but sharing the secret at all is a configuration
// bug (the config.Load function fails loudly if the two secrets match).
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

			ctx := r.Context()
			ctx = context.WithValue(ctx, AdminIDKey, claims.Subject)
			ctx = context.WithValue(ctx, AdminRolesKey, claims.Roles)
			ctx = context.WithValue(ctx, AdminMFAVerifiedKey, claims.MFAVerified)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminIDFromContext extracts the admin ID set by RequireAdminAuth.
func AdminIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(AdminIDKey).(string)
	return id, ok
}

// AdminRolesFromContext extracts the admin's role list.
func AdminRolesFromContext(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(AdminRolesKey).([]string)
	return roles, ok
}

// AdminMFAVerifiedFromContext returns whether the current session has a
// fresh MFA-verified token. Phase-5 privileged-action handlers gate on
// this; Phase-0 handlers may ignore it.
func AdminMFAVerifiedFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(AdminMFAVerifiedKey).(bool)
	if !ok {
		return false
	}
	return v
}

func writeAdminUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	// Distinct error message from the user-side Auth middleware so
	// logs make it trivial to tell which side rejected a request, but
	// the body exposes no information the caller could use to tell
	// "missing header" from "bad token" from "expired token."
	http.Error(w, `{"error":"admin authorization required"}`, http.StatusUnauthorized)
}

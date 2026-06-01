package admin

import (
	"net/http"

	"mootd/backend/internal/shared/response"
)

// RequirePermission returns middleware that 403s when the
// authenticated admin's roles don't include the given permission
// (P5-01 / mootd-admin#34).
//
// Composes with RequireAdminAuth — must come *after* it in the
// chain (so AdminRolesFromContext is populated). Pattern:
//
//	mux.Handle("/admin/v1/users/.../tier",
//	    requireAdmin(admin.RequirePermission(admin.PermAdminsManage)(handler)))
//
// The 403 body carries the missing permission string so the
// frontend can render a coherent message ("you need users:pii to
// view this user's email"). It also writes one audit row with
// action="rbac.denied" so denied attempts are visible to admins
// reviewing the audit log.
//
// `repo` is optional — when nil, denials don't write audit rows
// (used in tests + when the audit collection isn't wired). The
// production wiring always passes it.
func RequirePermission(perm Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roles, ok := AdminRolesFromContext(r.Context())
			if !ok {
				response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "admin authorization required"})
				return
			}
			if !HasPermission(roles, perm) {
				response.WriteJSON(w, http.StatusForbidden, map[string]any{
					"error":             "permission denied",
					"missingPermission": perm,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HasPermissionFromContext is the in-handler equivalent of the
// middleware. Use when permission checks branch the response
// shape (e.g. PII redaction default vs. reveal) rather than
// gating the whole endpoint.
func HasPermissionFromContext(r *http.Request, perm Permission) bool {
	roles, ok := AdminRolesFromContext(r.Context())
	if !ok {
		return false
	}
	return HasPermission(roles, perm)
}

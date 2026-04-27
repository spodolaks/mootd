package admin

import "context"

// contextKey is private so user-side context keys (or arbitrary
// strings) can never collide with admin keys by accident.
type contextKey string

const (
	keyAdminID          contextKey = "admin.id"
	keyAdminRoles       contextKey = "admin.roles"
	keyAdminMFAVerified contextKey = "admin.mfa"
)

// ContextWithAuth attaches the validated admin identity to a request
// context. Called by middleware.RequireAdminAuth after token
// validation; never called from a handler. Lives in the admin package
// so the package owns its own context shape — the middleware
// (`shared/middleware`) is a thin wrapper that doesn't need to import
// the admin types beyond ValidateToken.
func ContextWithAuth(ctx context.Context, adminID string, roles []string, mfaVerified bool) context.Context {
	ctx = context.WithValue(ctx, keyAdminID, adminID)
	ctx = context.WithValue(ctx, keyAdminRoles, roles)
	ctx = context.WithValue(ctx, keyAdminMFAVerified, mfaVerified)
	return ctx
}

// AdminIDFromContext extracts the authenticated admin's ID. Returns
// false when the context wasn't decorated by RequireAdminAuth — useful
// to detect bug paths where a handler is registered without the auth
// wrapper.
func AdminIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(keyAdminID).(string)
	return id, ok
}

// AdminRolesFromContext extracts the authenticated admin's role list.
func AdminRolesFromContext(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(keyAdminRoles).([]string)
	return roles, ok
}

// AdminMFAVerifiedFromContext returns true only when the current
// session has a fresh MFA-verified token. P5 privileged actions gate
// on this; Phase-0 handlers may ignore it.
func AdminMFAVerifiedFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(keyAdminMFAVerified).(bool)
	if !ok {
		return false
	}
	return v
}

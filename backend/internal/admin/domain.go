// Package admin owns authentication and authorization for the mootd admin
// panel (see https://github.com/spodolaks/mootd-admin). It is deliberately
// separate from the user-facing auth package — admin sessions use a
// different JWT issuer, a different signing secret, a shorter token
// lifetime, and a different persistence layout (admins have roles, MFA
// state, and an append-only refresh-token history instead of the
// in-document refresh-token hash the user doc carries).
//
// Admin auth lives in the same Go backend as user auth but never shares
// data or control flow with it. A user token presented to admin
// middleware is rejected; an admin token presented to user middleware is
// rejected. The only shared code is the HMAC signing primitive (HS256)
// via golang-jwt.
package admin

import "time"

// Role is an admin-permission bucket. The Phase-0 cut keeps only `admin`
// (all permissions); support/engineer/readonly land in P5-01.
type Role string

const (
	// RoleAdmin grants all permissions.
	RoleAdmin Role = "admin"
	// RoleEngineer grants traces + prompts + detection rerun; no PII access.
	RoleEngineer Role = "engineer"
	// RoleSupport grants user/moodboard reads and a narrow PII scope; no LLM
	// cost/prompt access.
	RoleSupport Role = "support"
	// RoleReadonly grants read on everything; cannot mutate or rerun.
	RoleReadonly Role = "readonly"
	// RoleCurator grants only prompts:read — enough for the
	// archetype-defaults curation surface (Defaults page) and the
	// read-only Prompts view. No traces, no users, no spend. Used
	// for contributors whose only job is curating per-archetype
	// defaults.
	RoleCurator Role = "curator"
)

// IsValidRole reports whether r is one of the five recognised roles.
func IsValidRole(r Role) bool {
	switch r {
	case RoleAdmin, RoleEngineer, RoleSupport, RoleReadonly, RoleCurator:
		return true
	}
	return false
}

// Admin is the MongoDB representation of an administrator. Stored in the
// dedicated `admins` collection; never joined to the `users` collection.
type Admin struct {
	ID           string    `bson:"_id"`
	Email        string    `bson:"email"`
	PasswordHash string    `bson:"passwordHash"` // argon2id encoded string
	// MFA slots below are populated from P5-02 onwards. Carried in Phase 0
	// so the schema is stable and claim checks can key off MFAEnforced
	// without a later migration.
	MFASecret        string    `bson:"mfaSecret,omitempty"`        // base32 TOTP seed
	MFAEnforced      bool      `bson:"mfaEnforced,omitempty"`      // require TOTP on login
	MFARecoveryCodes []string  `bson:"mfaRecoveryCodes,omitempty"` // sha256 hex of each
	MFALastTOTPStep  int64     `bson:"mfaLastTotpStep,omitempty"`  // highest consumed TOTP step; blocks replay within the validity window (#108 B4)
	Roles            []Role    `bson:"roles"`
	CreatedAt        time.Time `bson:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt"`
	LastActiveAt     time.Time `bson:"lastActiveAt,omitempty"`
	DisabledAt       *time.Time `bson:"disabledAt,omitempty"`
}

// RolesAsStrings returns admin's roles as a []string — convenient for JWT
// claims and JSON payloads where we don't want to leak the domain type.
func (a *Admin) RolesAsStrings() []string {
	out := make([]string, len(a.Roles))
	for i, r := range a.Roles {
		out[i] = string(r)
	}
	return out
}

// RefreshToken is the MongoDB representation of an admin refresh token
// record. Stored in its own collection (`admin_refresh_tokens`) so we can
// keep a full history for audit (which IP issued the token, when it was
// rotated, when revoked). The raw token is never stored — only its
// sha256 digest.
type RefreshToken struct {
	ID        string    `bson:"_id"` // sha256 hex of the raw token
	AdminID   string    `bson:"adminId"`
	ExpiresAt time.Time `bson:"expiresAt"`
	CreatedAt time.Time `bson:"createdAt"`
	RevokedAt *time.Time `bson:"revokedAt,omitempty"`
	UserAgent string    `bson:"userAgent,omitempty"`
	IP        string    `bson:"ip,omitempty"`
}

// ── HTTP wire types ─────────────────────────────────────────────────────

// LoginRequest is the body for POST /admin/v1/auth/login.
//
// TOTP is accepted from Phase 0 even though MFA validation is gated by the
// admin's MFAEnforced flag and the Phase-5 enforcement. Carrying the field
// day-zero means no breaking change when enforcement turns on.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp,omitempty"`
}

// LoginResponse is returned from a successful login or refresh.
type LoginResponse struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    string    `json:"expiresAt"`
	Admin        AdminInfo `json:"admin"`
}

// RefreshRequest is the body for POST /admin/v1/auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// AdminInfo is the serialisable public view of an admin — no password
// hash, no MFA secret, no recovery codes.
type AdminInfo struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions,omitempty"` // P5-01 / mootd-admin#34
	// LastActiveAt is the previous lastActiveAt value (the
	// timestamp before this /me hit bumped it). Used by the
	// dashboard's "since last visit" callout (mootd-admin#97).
	// Empty string when never set (first login).
	LastActiveAt string `json:"lastActiveAt,omitempty"`
}

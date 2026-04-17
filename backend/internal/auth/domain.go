// Package auth handles Google OAuth authentication and mootd JWT issuance.
package auth

// MockLoginRequest is the request body for POST /v1/auth/mock-login.
type MockLoginRequest struct {
	Provider string `json:"provider"`
}

// AuthUser represents an authenticated user returned in auth responses.
type AuthUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// MockLoginResponse is the response body for POST /v1/auth/mock-login.
type MockLoginResponse struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	ExpiresAt    string   `json:"expiresAt"`
	User         AuthUser `json:"user"`
	Mode         string   `json:"mode"`
}

// GoogleAuthRequest is the request body for POST /v1/auth/google.
// The backend verifies the access token with Google directly;
// client-supplied profile fields are intentionally not accepted.
type GoogleAuthRequest struct {
	AccessToken string `json:"accessToken"`
}

// GoogleAuthResponse is the response body for POST /v1/auth/google.
type GoogleAuthResponse struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	ExpiresAt    string   `json:"expiresAt"`
	User         AuthUser `json:"user"`
	Mode         string   `json:"mode"`
}

// RefreshRequest is the request body for POST /v1/auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// RefreshResponse is the response body for POST /v1/auth/refresh.
type RefreshResponse struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	ExpiresAt    string   `json:"expiresAt"`
	User         AuthUser `json:"user"`
}

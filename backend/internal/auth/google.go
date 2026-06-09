package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// googleHTTPClient is used for all Google API calls, with an explicit timeout
// to prevent hanging on slow/unresponsive endpoints.
var googleHTTPClient = &http.Client{Timeout: 15 * time.Second}

// Google API endpoints. Declared as vars (not consts) so tests can point them
// at a local httptest server.
var (
	googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"
	googleUserInfoURL  = "https://openidconnect.googleapis.com/v1/userinfo"
)

// googleUserInfo holds the verified user fields returned by Google's userinfo endpoint.
type googleUserInfo struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// googleTokenInfo holds the fields we care about from Google's tokeninfo
// endpoint. `aud` and `azp` identify the OAuth client the token was minted for
// — the audience binding that prevents token-substitution (confused-deputy)
// account takeover.
type googleTokenInfo struct {
	Aud   string `json:"aud"`
	Azp   string `json:"azp"`
	Sub   string `json:"sub"`
	Email string `json:"email"`
}

// verifyGoogleToken validates a Google access token and returns the associated
// user profile. It enforces two independent checks:
//
//  1. Audience binding — the token MUST have been issued to one of mootd's own
//     OAuth client IDs (allowedClientIDs). Without this, ANY valid Google
//     access token — including one minted for an unrelated, attacker-controlled
//     OAuth app — would be accepted, letting the attacker take over the matching
//     mootd account (users are keyed by the Google subject).
//  2. Profile retrieval — fetched from userinfo with the same token; the subject
//     is cross-checked against the tokeninfo subject as defence in depth.
//
// Client-supplied profile fields from the request body are never trusted; only
// the data returned by Google here is used.
func verifyGoogleToken(ctx context.Context, accessToken string, allowedClientIDs []string) (*googleUserInfo, error) {
	if len(allowedClientIDs) == 0 {
		// Fail closed: with no allowlist we cannot bind the audience, so we
		// must not accept the token rather than silently trusting any client.
		return nil, fmt.Errorf("no Google OAuth client IDs configured; refusing to validate token")
	}

	ti, err := fetchGoogleTokenInfo(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if !audienceAllowed(ti, allowedClientIDs) {
		return nil, fmt.Errorf("google token audience not allowed (aud=%q azp=%q)", ti.Aud, ti.Azp)
	}

	info, err := fetchGoogleUserInfo(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	// Defence in depth: tokeninfo and userinfo must describe the same subject.
	if ti.Sub != "" && info.Sub != "" && ti.Sub != info.Sub {
		return nil, fmt.Errorf("google token subject mismatch between tokeninfo and userinfo")
	}
	if info.Sub == "" || info.Email == "" {
		return nil, fmt.Errorf("incomplete user info from Google")
	}
	return info, nil
}

// audienceAllowed reports whether the token was minted for one of our client
// IDs. We accept a match on either aud or azp: for Google access tokens the
// client ID is reported in aud, but azp is checked too to be robust across flows.
func audienceAllowed(ti *googleTokenInfo, allowed []string) bool {
	for _, id := range allowed {
		if id == "" {
			continue
		}
		if ti.Aud == id || ti.Azp == id {
			return true
		}
	}
	return false
}

// fetchGoogleTokenInfo calls Google's tokeninfo endpoint to retrieve the
// token's audience/subject. A non-200 means the token is invalid or expired.
func fetchGoogleTokenInfo(ctx context.Context, accessToken string) (*googleTokenInfo, error) {
	endpoint := googleTokenInfoURL + "?access_token=" + url.QueryEscape(accessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build tokeninfo request: %w", err)
	}

	resp, err := googleHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google tokeninfo call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google rejected token with status %d", resp.StatusCode)
	}

	var ti googleTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&ti); err != nil {
		return nil, fmt.Errorf("decode google tokeninfo: %w", err)
	}
	return &ti, nil
}

// fetchGoogleUserInfo calls Google's userinfo endpoint to retrieve the profile
// fields (name, picture) used for display.
func fetchGoogleUserInfo(ctx context.Context, accessToken string) (*googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := googleHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google userinfo call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google rejected token with status %d", resp.StatusCode)
	}

	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode google userinfo: %w", err)
	}
	return &info, nil
}

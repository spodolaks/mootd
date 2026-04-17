package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// googleHTTPClient is used for all Google API calls, with an explicit timeout
// to prevent hanging on slow/unresponsive endpoints.
var googleHTTPClient = &http.Client{Timeout: 15 * time.Second}

// googleUserInfo holds the verified user fields returned by Google's userinfo endpoint.
type googleUserInfo struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// verifyGoogleToken calls Google's userinfo endpoint to validate the access token
// and return the associated user profile. Returns an error if the token is invalid.
func verifyGoogleToken(ctx context.Context, accessToken string) (*googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
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
	if info.Sub == "" || info.Email == "" {
		return nil, fmt.Errorf("incomplete user info from Google")
	}
	return &info, nil
}

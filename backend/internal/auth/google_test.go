package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const mootdClientID = "991290253393-test.apps.googleusercontent.com"

// googleStub stands in for Google's tokeninfo + userinfo endpoints and points
// the package URL vars at itself for the duration of a test.
func googleStub(t *testing.T, tokenInfoStatus int, tokenInfoBody string, userInfoStatus int, userInfoBody string) {
	t.Helper()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(tokenInfoStatus)
		_, _ = w.Write([]byte(tokenInfoBody))
	}))
	userSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(userInfoStatus)
		_, _ = w.Write([]byte(userInfoBody))
	}))

	prevToken, prevUser := googleTokenInfoURL, googleUserInfoURL
	googleTokenInfoURL = tokenSrv.URL
	googleUserInfoURL = userSrv.URL

	t.Cleanup(func() {
		googleTokenInfoURL = prevToken
		googleUserInfoURL = prevUser
		tokenSrv.Close()
		userSrv.Close()
	})
}

func TestVerifyGoogleToken(t *testing.T) {
	allowed := []string{mootdClientID}

	tests := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		userStatus  int
		userBody    string
		allowed     []string
		wantErr     bool
		wantSub     string
	}{
		{
			name:        "valid token for our client is accepted",
			tokenStatus: http.StatusOK,
			tokenBody:   `{"aud":"` + mootdClientID + `","sub":"google-123","email":"u@example.com"}`,
			userStatus:  http.StatusOK,
			userBody:    `{"sub":"google-123","email":"u@example.com","name":"U","picture":"p"}`,
			allowed:     allowed,
			wantErr:     false,
			wantSub:     "google-123",
		},
		{
			// The core vulnerability: a perfectly valid Google token minted for
			// some OTHER OAuth app must be rejected, not turned into a session.
			name:        "valid token for a different client is rejected",
			tokenStatus: http.StatusOK,
			tokenBody:   `{"aud":"attacker-999.apps.googleusercontent.com","sub":"victim-456","email":"victim@example.com"}`,
			userStatus:  http.StatusOK,
			userBody:    `{"sub":"victim-456","email":"victim@example.com","name":"Victim"}`,
			allowed:     allowed,
			wantErr:     true,
		},
		{
			name:        "azp match (empty aud) is accepted",
			tokenStatus: http.StatusOK,
			tokenBody:   `{"aud":"","azp":"` + mootdClientID + `","sub":"google-123","email":"u@example.com"}`,
			userStatus:  http.StatusOK,
			userBody:    `{"sub":"google-123","email":"u@example.com","name":"U"}`,
			allowed:     allowed,
			wantErr:     false,
			wantSub:     "google-123",
		},
		{
			name:        "subject mismatch between tokeninfo and userinfo is rejected",
			tokenStatus: http.StatusOK,
			tokenBody:   `{"aud":"` + mootdClientID + `","sub":"google-123","email":"u@example.com"}`,
			userStatus:  http.StatusOK,
			userBody:    `{"sub":"someone-else","email":"u@example.com","name":"U"}`,
			allowed:     allowed,
			wantErr:     true,
		},
		{
			name:        "invalid/expired token (tokeninfo 400) is rejected",
			tokenStatus: http.StatusBadRequest,
			tokenBody:   `{"error":"invalid_token"}`,
			userStatus:  http.StatusOK,
			userBody:    `{"sub":"google-123","email":"u@example.com"}`,
			allowed:     allowed,
			wantErr:     true,
		},
		{
			name:        "empty allowlist fails closed",
			tokenStatus: http.StatusOK,
			tokenBody:   `{"aud":"` + mootdClientID + `","sub":"google-123","email":"u@example.com"}`,
			userStatus:  http.StatusOK,
			userBody:    `{"sub":"google-123","email":"u@example.com"}`,
			allowed:     nil,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			googleStub(t, tc.tokenStatus, tc.tokenBody, tc.userStatus, tc.userBody)

			info, err := verifyGoogleToken(context.Background(), "any-access-token", tc.allowed)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (info=%+v)", info)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Sub != tc.wantSub {
				t.Fatalf("sub = %q, want %q", info.Sub, tc.wantSub)
			}
		})
	}
}

func TestAudienceAllowed(t *testing.T) {
	allowed := []string{"a.apps.googleusercontent.com", "b.apps.googleusercontent.com"}
	cases := []struct {
		aud, azp string
		want     bool
	}{
		{"a.apps.googleusercontent.com", "", true},
		{"", "b.apps.googleusercontent.com", true},
		{"evil.apps.googleusercontent.com", "evil.apps.googleusercontent.com", false},
		{"", "", false},
	}
	for _, c := range cases {
		got := audienceAllowed(&googleTokenInfo{Aud: c.aud, Azp: c.azp}, allowed)
		if got != c.want {
			t.Errorf("audienceAllowed(aud=%q,azp=%q) = %v, want %v", c.aud, c.azp, got, c.want)
		}
	}
}

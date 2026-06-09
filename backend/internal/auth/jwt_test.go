package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"mootd/backend/internal/config"
	jwtutil "mootd/backend/internal/shared/jwt"
)

const (
	testSecret = "test-secret-min-32-characters-long!"
	testUserID = "user_abc_123"
	testEmail  = "alice@example.com"
)

// TestGenerateToken_ClaimsRoundTrip covers the happy-path contract used by
// auth handlers: issue an access token with config.DefaultJWTExpiry, then
// parse it back and verify sub/iss/exp are populated correctly.
func TestGenerateToken_ClaimsRoundTrip(t *testing.T) {
	before := time.Now().UTC()
	tok, err := jwtutil.GenerateToken(testUserID, testEmail, testSecret, config.DefaultJWTExpiry)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	after := time.Now().UTC()

	claims, err := jwtutil.ValidateToken(tok, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if claims.Subject != testUserID {
		t.Errorf("Subject = %q, want %q", claims.Subject, testUserID)
	}
	if claims.Issuer != "mootd" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "mootd")
	}
	if claims.Email != testEmail {
		t.Errorf("Email = %q, want %q", claims.Email, testEmail)
	}

	// exp must land inside [before+expiry, after+expiry] — allow a small slop.
	expMin := before.Add(config.DefaultJWTExpiry).Add(-time.Second)
	expMax := after.Add(config.DefaultJWTExpiry).Add(time.Second)
	exp := claims.ExpiresAt.Time
	if exp.Before(expMin) || exp.After(expMax) {
		t.Errorf("ExpiresAt = %v, want within [%v, %v]", exp, expMin, expMax)
	}
}

// TestValidateToken_TamperedSignature ensures a modified signature fails
// validation — the cryptographic guarantee the HS256 scheme is there to give.
func TestValidateToken_TamperedSignature(t *testing.T) {
	tok, err := jwtutil.GenerateToken(testUserID, testEmail, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token shape: %d parts", len(parts))
	}
	// Flip a character in the signature segment.
	sig := []byte(parts[2])
	if sig[0] == 'A' {
		sig[0] = 'B'
	} else {
		sig[0] = 'A'
	}
	tampered := parts[0] + "." + parts[1] + "." + string(sig)

	if _, err := jwtutil.ValidateToken(tampered, testSecret); err == nil {
		t.Fatal("expected error for tampered signature, got nil")
	}
}

// TestValidateToken_Expired ensures tokens past their exp are rejected.
func TestValidateToken_Expired(t *testing.T) {
	tok, err := jwtutil.GenerateToken(testUserID, testEmail, testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, err := jwtutil.ValidateToken(tok, testSecret); err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

// TestValidateToken_WrongIssuer ensures a valid-but-foreign token (correct
// signature under the same secret, wrong `iss`) is rejected outright. This is
// the issuer separation that stops an admin or third-party token from being
// accepted on the user path.
func TestValidateToken_WrongIssuer(t *testing.T) {
	now := time.Now().UTC()
	foreign := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtutil.Claims{
		Email: testEmail,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   testUserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			Issuer:    "evil-corp",
		},
	})
	signed, err := foreign.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign foreign token: %v", err)
	}

	if _, err := jwtutil.ValidateToken(signed, testSecret); err == nil {
		t.Fatal("expected error for foreign issuer, got nil")
	}
}

// TestGenerateRefreshToken_UniqueAndHashable covers the refresh token flow:
// two consecutive generations must differ, and the hash must be stable.
func TestGenerateRefreshToken_UniqueAndHashable(t *testing.T) {
	a, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken a: %v", err)
	}
	b, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken b: %v", err)
	}
	if a == b {
		t.Fatal("two consecutive refresh tokens were identical")
	}
	if len(a) != 64 { // 32 bytes hex-encoded
		t.Errorf("refresh token hex length = %d, want 64", len(a))
	}

	h1 := jwtutil.HashRefreshToken(a)
	h2 := jwtutil.HashRefreshToken(a)
	if h1 != h2 {
		t.Fatal("HashRefreshToken not deterministic")
	}
	if h1 == a {
		t.Fatal("hash equals plaintext — HashRefreshToken is a no-op?")
	}
	if jwtutil.HashRefreshToken(b) == h1 {
		t.Fatal("distinct tokens hashed to the same digest")
	}
}

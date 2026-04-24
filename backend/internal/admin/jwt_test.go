package admin

import (
	"strings"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

const testSecret = "admin-test-secret-at-least-32-chars-long!"
const userSecret = "user-test-secret-at-least-32-chars-long!!"

func TestGenerateToken_RoundTrip(t *testing.T) {
	token, err := GenerateToken("adm_1", []string{"admin"}, true, testSecret, 5*time.Minute)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.Subject != "adm_1" {
		t.Errorf("subject: got %q want adm_1", claims.Subject)
	}
	if claims.Issuer != Issuer {
		t.Errorf("issuer: got %q want %q", claims.Issuer, Issuer)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "admin" {
		t.Errorf("roles: got %v want [admin]", claims.Roles)
	}
	if !claims.MFAVerified {
		t.Errorf("mfa_verified: got false want true")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, _ := GenerateToken("adm_1", []string{"admin"}, false, testSecret, 5*time.Minute)
	if _, err := ValidateToken(token, userSecret); err == nil {
		t.Fatalf("expected error validating with wrong secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	token, _ := GenerateToken("adm_1", []string{"admin"}, false, testSecret, -1*time.Minute)
	if _, err := ValidateToken(token, testSecret); err == nil {
		t.Fatalf("expected error validating expired token")
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	// Hand-craft a token with iss="mootd" (user issuer) but signed with
	// the admin secret. RequireAdminAuth's issuer check must catch it
	// even if the signature verifies.
	now := time.Now().UTC()
	claims := Claims{
		Roles:       []string{"admin"},
		MFAVerified: false,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Subject:   "adm_1",
			IssuedAt:  jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(5 * time.Minute)),
			Issuer:    "mootd", // ← wrong issuer
		},
	}
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = ValidateToken(signed, testSecret)
	if err == nil {
		t.Fatalf("expected error for wrong-issuer token")
	}
	if !strings.Contains(err.Error(), "wrong issuer") {
		t.Errorf("error %q did not mention wrong issuer", err)
	}
}

func TestValidateToken_GarbageString(t *testing.T) {
	if _, err := ValidateToken("not.a.jwt", testSecret); err == nil {
		t.Fatalf("expected error for garbage token")
	}
}

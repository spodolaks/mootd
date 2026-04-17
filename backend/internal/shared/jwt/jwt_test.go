package jwt

import (
	"testing"
	"time"
)

const testSecret = "test-secret-for-jwt-unit-tests-min32!"

func TestGenerateAndValidate(t *testing.T) {
	token, err := GenerateToken("user123", "user@example.com", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if claims.Subject != "user123" {
		t.Errorf("subject = %q, want %q", claims.Subject, "user123")
	}
	if claims.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", claims.Email, "user@example.com")
	}
	if claims.Issuer != "mootd" {
		t.Errorf("issuer = %q, want %q", claims.Issuer, "mootd")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, err := GenerateToken("user123", "user@example.com", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	_, err = ValidateToken(token, "wrong-secret-wrong-secret-wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	token, err := GenerateToken("user123", "user@example.com", testSecret, -time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	_, err = ValidateToken(token, testSecret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateToken_InvalidString(t *testing.T) {
	_, err := ValidateToken("not-a-jwt", testSecret)
	if err == nil {
		t.Fatal("expected error for invalid token string, got nil")
	}
}

// Package jwt provides JWT token generation and validation utilities shared across domains.
package jwt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is the value placed in the `iss` claim of every user-side JWT minted
// by GenerateToken; ValidateToken rejects tokens with any other issuer. Mirrors
// the admin-side issuer separation (see internal/admin/jwt.go).
const Issuer = "mootd"

// Claims holds the payload embedded in a mootd JWT.
type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed HS256 JWT. The Subject (sub) is set to userID.
func GenerateToken(userID, email, secret string, expiry time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			Issuer:    Issuer,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// GenerateRefreshToken generates a cryptographically random 32-byte hex string
// suitable for use as a refresh token.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashRefreshToken returns the SHA-256 hex digest of a raw refresh token.
func HashRefreshToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ValidateToken parses and validates a signed JWT, returning its claims on success.
func ValidateToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	if claims.Issuer != Issuer {
		return nil, fmt.Errorf("invalid token issuer: %q", claims.Issuer)
	}
	return claims, nil
}

package admin

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is the JWT `iss` claim value for all admin tokens. A user-side
// token (iss="mootd") cannot pass admin validation; an admin-side token
// cannot pass user validation. Distinct issuers + distinct signing
// secrets together give a belt-and-braces guarantee.
const Issuer = "mootd-admin"

// Claims is the payload of an admin JWT. The MFAVerified flag lets
// Phase-5 privileged-action middleware require a fresh TOTP without
// re-issuing the whole token; MFA-verified tokens simply have the bit
// set until the next refresh.
type Claims struct {
	Roles       []string `json:"roles"`
	MFAVerified bool     `json:"mfa_verified"`
	jwt.RegisteredClaims
}

// GenerateToken signs an HS256 JWT for the given admin. Called from the
// login and refresh handlers; never called outside this package.
func GenerateToken(adminID string, roles []string, mfaVerified bool, secret string, expiry time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		Roles:       roles,
		MFAVerified: mfaVerified,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			Issuer:    Issuer,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken parses and validates an admin JWT. Returns an error when:
//   - the signature doesn't verify under `secret`;
//   - the token is expired or not-yet-valid;
//   - the `iss` claim is not exactly "mootd-admin" (prevents user tokens
//     from sneaking in if somehow the same secret was shared — still
//     defense in depth alongside the config-layer check that refuses
//     matching secrets).
func ValidateToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("admin: unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("admin: invalid token claims")
	}
	if claims.Issuer != Issuer {
		// A token signed with the admin secret but carrying the user
		// issuer would be a bug in token generation; refuse it loudly
		// rather than letting the ambiguity leak into the request
		// context.
		return nil, fmt.Errorf("admin: wrong issuer %q (expected %q)", claims.Issuer, Issuer)
	}
	return claims, nil
}

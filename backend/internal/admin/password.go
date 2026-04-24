package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters — recommended 2026 defaults from OWASP Password
// Storage Cheat Sheet. 64 MiB memory × 3 iterations × 4 lanes gives
// ~60 ms per hash on modest hardware, which is the sweet spot between
// user-facing login latency and brute-force resistance.
const (
	argonMemoryKiB  uint32 = 64 * 1024 // 64 MiB
	argonIterations uint32 = 3
	argonParallel   uint8  = 4
	argonSaltLen    uint32 = 16
	argonKeyLen     uint32 = 32
)

// ErrInvalidHash is returned when a stored hash doesn't match the
// expected PHC-style format. Callers should treat it as a corrupted
// record and NOT as a failed-credential signal (which must be
// indistinguishable from a mismatched password for a real admin).
var ErrInvalidHash = errors.New("admin: invalid password hash format")

// HashPassword produces a PHC-format argon2id hash suitable for storage
// in Admin.PasswordHash. The salt is drawn fresh per call from crypto/rand.
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt-b64>$<hash-b64>
//
// Callers check strength separately (minimum length, HaveIBeenPwned
// lookup) — this function only hashes, it does not judge.
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("admin: password is empty")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("admin: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, argonIterations, argonMemoryKiB, argonParallel, argonKeyLen)

	// PHC string format — matches the de-facto standard used by
	// passlib, libsodium-bindings, and most argon2 libraries so a
	// future migration (e.g. to a dedicated auth service) can read
	// these hashes without transcoding.
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemoryKiB,
		argonIterations,
		argonParallel,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// VerifyPassword returns nil when plain matches the stored hash, a
// non-nil error otherwise. The error is intentionally opaque — callers
// must not surface its contents to clients because different error
// classes (malformed hash vs wrong password) would leak information.
func VerifyPassword(hash, plain string) error {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidHash
	}
	if version != argon2.Version {
		return fmt.Errorf("admin: argon2 version mismatch (have=%d, want=%d)", version, argon2.Version)
	}

	var memKiB, iters uint32
	var lanes uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memKiB, &iters, &lanes); err != nil {
		return ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return ErrInvalidHash
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return ErrInvalidHash
	}

	got := argon2.IDKey([]byte(plain), salt, iters, memKiB, lanes, uint32(len(expected)))
	// Constant-time comparison so timing never leaks whether the first
	// bytes matched.
	if subtle.ConstantTimeCompare(got, expected) != 1 {
		return errors.New("admin: password mismatch")
	}
	return nil
}

package admin

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

func TestGenerateTOTPSecret_Length(t *testing.T) {
	s, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	// 20 raw bytes encode to 32 base32 chars (no padding).
	if len(s) != 32 {
		t.Errorf("len = %d, want 32", len(s))
	}
}

func TestVerifyTOTP_AcceptsCurrent(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()

	// Compute the expected code by mimicking what an authenticator
	// would do, then verify it.
	expected := generateAt(t, secret, now)
	if !VerifyTOTP(secret, expected, now) {
		t.Error("verify should accept the just-generated code")
	}
}

func TestVerifyTOTP_AcceptsSkew(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()

	// Code from the previous step (-30s) should still verify
	// because we tolerate ±1 step.
	prev := generateAt(t, secret, now.Add(-30*time.Second))
	if !VerifyTOTP(secret, prev, now) {
		t.Error("verify should accept previous step within skew tolerance")
	}
}

func TestVerifyTOTP_RejectsTooOld(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now()
	// Two steps back is outside ±1 tolerance.
	old := generateAt(t, secret, now.Add(-90*time.Second))
	if VerifyTOTP(secret, old, now) {
		t.Error("verify should reject a code more than 1 step old")
	}
}

func TestVerifyTOTP_RejectsBadCode(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	// Not asserting on "000000": it's a valid code ~1-in-a-million of the
	// time and would flake — the non-numeric/length checks below carry
	// the rejection coverage.
	if VerifyTOTP(secret, "abc", time.Now()) {
		t.Error("verify should reject a non-numeric code")
	}
	if VerifyTOTP(secret, "1234567", time.Now()) {
		t.Error("verify should reject a 7-digit code")
	}
}

func TestRecoveryCodes_Roundtrip(t *testing.T) {
	plain, hashes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != recoveryCodeCount || len(hashes) != recoveryCodeCount {
		t.Fatalf("expected %d codes, got plain=%d hashes=%d", recoveryCodeCount, len(plain), len(hashes))
	}

	// Each plaintext should hash to its corresponding stored
	// hash. Verifies the pair stays aligned.
	for i, p := range plain {
		if HashRecoveryCode(p) != hashes[i] {
			t.Errorf("plain[%d] doesn't hash to hashes[%d]", i, i)
		}
	}

	// Hash should be the same regardless of case + dashes.
	want := hashes[0]
	if HashRecoveryCode(strings.ReplaceAll(plain[0], "-", "")) != want {
		t.Error("hash should ignore dashes")
	}
	if HashRecoveryCode(strings.ToUpper(plain[0])) != want {
		t.Error("hash should be case-insensitive")
	}
}

func TestConsumeRecoveryCode_Match(t *testing.T) {
	plain, hashes, _ := GenerateRecoveryCodes()
	ok, remaining := ConsumeRecoveryCode(hashes, plain[3])
	if !ok {
		t.Fatal("expected match for plain[3]")
	}
	if len(remaining) != recoveryCodeCount-1 {
		t.Errorf("expected %d remaining, got %d", recoveryCodeCount-1, len(remaining))
	}
	// The same code shouldn't match again.
	ok2, _ := ConsumeRecoveryCode(remaining, plain[3])
	if ok2 {
		t.Error("a one-time code should not match twice")
	}
}

func TestConsumeRecoveryCode_NoMatch(t *testing.T) {
	_, hashes, _ := GenerateRecoveryCodes()
	ok, remaining := ConsumeRecoveryCode(hashes, "totally-fake")
	if ok {
		t.Error("non-matching code should return false")
	}
	if len(remaining) != recoveryCodeCount {
		t.Errorf("non-match should leave list unchanged, got %d", len(remaining))
	}
}

// generateAt is a test helper that produces what an authenticator
// app would show at the given time. Mirrors the algorithm in
// hotp() so we can verify VerifyTOTP without coupling tests to
// specific clock instants.
func generateAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	step := uint64(at.Unix() / totpPeriod)
	return hotp(key, step)
}

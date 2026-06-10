package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mfaVerifyReq builds a POST /admin/v1/auth/mfa/verify request whose
// context carries the authenticated admin identity, mirroring what
// RequireAdminAuth installs in production. mfaVerified controls the
// session step-up claim.
func mfaVerifyReq(adminID, body string, mfaVerified bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/auth/mfa/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := ContextWithAuth(req.Context(), adminID, []string{string(RoleAdmin)}, mfaVerified)
	return req.WithContext(ctx)
}

func decodeVerify(rec *httptest.ResponseRecorder) map[string]any {
	var m map[string]any
	_ = json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&m)
	return m
}

// First-time enrollment (admin not yet enrolled) must succeed even on a
// session that is NOT MFA-verified — every admin has to be able to
// enroll their first factor without external help.
func TestMFAVerify_FirstEnroll_Allowed(t *testing.T) {
	h, repo := newTestHandler(t)

	secret, _ := GenerateTOTPSecret()
	code := generateAt(t, secret, time.Now())
	body := `{"secret":"` + secret + `","code":"` + code + `"}`

	rec := httptest.NewRecorder()
	h.MFAVerify(rec, mfaVerifyReq("adm_1", body, false /* not MFA-verified */))

	if rec.Code != http.StatusOK {
		t.Fatalf("first enroll should succeed without step-up; got %d body=%s", rec.Code, rec.Body)
	}
	out := decodeVerify(rec)
	if codes, ok := out["recoveryCodes"].([]any); !ok || len(codes) == 0 {
		t.Fatalf("expected recovery codes in response, got %v", out)
	}
	// Enrollment must have actually persisted.
	a, _ := repo.FindByID(context.Background(), "adm_1")
	if a == nil || !a.MFAEnforced || a.MFASecret != secret {
		t.Fatalf("enrollment not persisted: %+v", a)
	}
}

// Re-enrollment (admin already enrolled) on a session that is NOT
// MFA-verified and with no step-up code must be REJECTED with 403 —
// this is the #144 fix. A stolen short-lived access token must not be
// able to silently overwrite an enrolled admin's TOTP seed.
func TestMFAVerify_ReEnroll_NoStepUp_Rejected(t *testing.T) {
	h, repo := newTestHandler(t)

	// Existing enrollment.
	oldSecret, _ := GenerateTOTPSecret()
	if err := repo.SetMFAEnrollment(context.Background(), "adm_1", oldSecret, nil); err != nil {
		t.Fatalf("seed enrollment: %v", err)
	}

	// Attacker tries to plant a brand-new secret with a valid code for
	// THAT new secret, but no proof of the existing factor.
	newSecret, _ := GenerateTOTPSecret()
	newCode := generateAt(t, newSecret, time.Now())
	body := `{"secret":"` + newSecret + `","code":"` + newCode + `"}`

	rec := httptest.NewRecorder()
	h.MFAVerify(rec, mfaVerifyReq("adm_1", body, false /* not MFA-verified */))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("re-enroll without step-up must be 403; got %d body=%s", rec.Code, rec.Body)
	}
	if out := decodeVerify(rec); out["requireMfa"] != true {
		t.Errorf("expected requireMfa=true in 403 body, got %v", out)
	}
	// The original secret must be untouched.
	a, _ := repo.FindByID(context.Background(), "adm_1")
	if a == nil || a.MFASecret != oldSecret {
		t.Fatalf("existing secret was overwritten without step-up: %+v", a)
	}
}

// Re-enrollment on an MFA-verified session is allowed and rotates the
// secret.
func TestMFAVerify_ReEnroll_MFAVerifiedSession_Allowed(t *testing.T) {
	h, repo := newTestHandler(t)

	oldSecret, _ := GenerateTOTPSecret()
	if err := repo.SetMFAEnrollment(context.Background(), "adm_1", oldSecret, nil); err != nil {
		t.Fatalf("seed enrollment: %v", err)
	}

	newSecret, _ := GenerateTOTPSecret()
	newCode := generateAt(t, newSecret, time.Now())
	body := `{"secret":"` + newSecret + `","code":"` + newCode + `"}`

	rec := httptest.NewRecorder()
	h.MFAVerify(rec, mfaVerifyReq("adm_1", body, true /* MFA-verified session */))

	if rec.Code != http.StatusOK {
		t.Fatalf("re-enroll on MFA-verified session should succeed; got %d body=%s", rec.Code, rec.Body)
	}
	a, _ := repo.FindByID(context.Background(), "adm_1")
	if a == nil || a.MFASecret != newSecret {
		t.Fatalf("secret was not rotated to the new value: %+v", a)
	}
}

// Re-enrollment with a valid step-up TOTP code for the EXISTING secret
// is allowed even when the session token is not MFA-verified (e.g. the
// claim was dropped on refresh).
func TestMFAVerify_ReEnroll_WithStepUpTOTP_Allowed(t *testing.T) {
	h, repo := newTestHandler(t)

	oldSecret, _ := GenerateTOTPSecret()
	if err := repo.SetMFAEnrollment(context.Background(), "adm_1", oldSecret, nil); err != nil {
		t.Fatalf("seed enrollment: %v", err)
	}

	newSecret, _ := GenerateTOTPSecret()
	newCode := generateAt(t, newSecret, time.Now())
	stepUp := generateAt(t, oldSecret, time.Now()) // proof of the existing factor
	body := `{"secret":"` + newSecret + `","code":"` + newCode + `","stepUpCode":"` + stepUp + `"}`

	rec := httptest.NewRecorder()
	h.MFAVerify(rec, mfaVerifyReq("adm_1", body, false /* not MFA-verified, but stepUpCode supplied */))

	if rec.Code != http.StatusOK {
		t.Fatalf("re-enroll with valid step-up code should succeed; got %d body=%s", rec.Code, rec.Body)
	}
	a, _ := repo.FindByID(context.Background(), "adm_1")
	if a == nil || a.MFASecret != newSecret {
		t.Fatalf("secret was not rotated after valid step-up: %+v", a)
	}
}

// A step-up code that is NOT valid for the existing secret (here: a code
// for the *new* secret, a classic confused-deputy attempt) must not
// satisfy the gate.
func TestMFAVerify_ReEnroll_WrongStepUp_Rejected(t *testing.T) {
	h, repo := newTestHandler(t)

	oldSecret, _ := GenerateTOTPSecret()
	if err := repo.SetMFAEnrollment(context.Background(), "adm_1", oldSecret, nil); err != nil {
		t.Fatalf("seed enrollment: %v", err)
	}

	newSecret, _ := GenerateTOTPSecret()
	newCode := generateAt(t, newSecret, time.Now())
	// stepUpCode validates against the NEW secret, not the existing one.
	body := `{"secret":"` + newSecret + `","code":"` + newCode + `","stepUpCode":"` + newCode + `"}`

	rec := httptest.NewRecorder()
	h.MFAVerify(rec, mfaVerifyReq("adm_1", body, false))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("step-up code not matching the existing secret must be rejected; got %d body=%s", rec.Code, rec.Body)
	}
	a, _ := repo.FindByID(context.Background(), "adm_1")
	if a == nil || a.MFASecret != oldSecret {
		t.Fatalf("existing secret overwritten despite invalid step-up: %+v", a)
	}
}

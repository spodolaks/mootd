package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// MFA enrollment + management endpoints (P5-02 / mootd-admin#35).
//
// Three endpoints, all behind admin auth (no special permission —
// every authenticated admin must be able to enroll their own MFA
// without external help). The shape:
//
//   POST /admin/v1/auth/mfa/setup
//     Body: empty
//     Returns: { secret, otpauthUri }
//     Side effect: NONE — secret is generated server-side and
//     sent to the FE; the admin's record isn't updated until
//     they verify a code via /verify. Idempotent — calling
//     /setup twice gives a different secret each time.
//
//   POST /admin/v1/auth/mfa/verify
//     Body: { secret, code, stepUpCode? }
//     Returns: { recoveryCodes } (8 codes, plaintext, ONE TIME ONLY)
//     Side effect: persists secret + hashed recovery codes,
//     flips MFAEnforced=true.
//
// /setup intentionally returns the secret to the FE rather than
// stashing it in the DB. The FE then echoes it back on /verify
// alongside the proof code, so a network-blip-mid-enrollment
// can't strand a half-saved secret. Stateless enrollment.
//
// Re-enrollment step-up (#144). /verify validates a *client-supplied*
// secret, so without a guard a stolen short-lived admin access token
// could silently overwrite an already-enrolled admin's TOTP seed and
// recovery codes (lock-out + factor-planting). When the admin is
// already enrolled, re-enrollment therefore requires step-up: either
// an MFA-verified session (a token minted from a login that presented
// the existing second factor) OR a current valid TOTP/recovery code
// for the *existing* secret, supplied as `stepUpCode`. First-time
// enrollment (not yet enrolled) stays open — every admin must be able
// to enroll without external help.
// ────────────────────────────────────────────────────────────────────

// MFASetup handles POST /admin/v1/auth/mfa/setup. Returns a
// fresh TOTP secret + the otpauth:// URI the FE renders as a QR
// code. Doesn't touch the DB.
func (h *Handler) MFASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	adminID, ok := AdminIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	a, err := h.repo.FindByID(ctx, adminID)
	if err != nil || a == nil {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	secret, err := GenerateTOTPSecret()
	if err != nil {
		h.logger.Printf("admin /mfa/setup: generate: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{
		"secret":     secret,
		"otpauthUri": OTPAuthURI(secret, a.Email),
	})
}

// MFAVerify handles POST /admin/v1/auth/mfa/verify. Body:
// {secret, code, stepUpCode?}. On success: persists, flips
// MFAEnforced=true, returns 8 plaintext recovery codes the admin must
// save now. Re-enrollment (admin already enrolled) additionally
// requires step-up — an MFA-verified session or a valid stepUpCode for
// the existing factor (#144).
func (h *Handler) MFAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	adminID, ok := AdminIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
		// StepUpCode is a current TOTP or recovery code for the admin's
		// *existing* secret, used to authorise re-enrollment when the
		// session token isn't MFA-verified (#144). Ignored on first-time
		// enrollment.
		StepUpCode string `json:"stepUpCode"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	body.Secret = strings.TrimSpace(body.Secret)
	body.Code = strings.TrimSpace(body.Code)
	body.StepUpCode = strings.TrimSpace(body.StepUpCode)
	if body.Secret == "" || body.Code == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "secret and code are required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Load the admin so we can tell first-time enrollment from
	// re-enrollment. An admin is "already enrolled" when MFAEnforced is
	// set with a stored secret.
	a, err := h.repo.FindByID(ctx, adminID)
	if err != nil || a == nil {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Re-enrollment step-up gate (#144). Overwriting an already-enrolled
	// admin's TOTP seed / recovery codes is privileged: a thief of a
	// short-lived access token must not be able to do it silently. Allow
	// only when the session is MFA-verified, or the caller proves
	// possession of the *existing* second factor via stepUpCode. The
	// stepUpCode is matched against the CURRENT secret/recovery codes —
	// never the new client-supplied secret.
	alreadyEnrolled := a.MFAEnforced && a.MFASecret != ""
	if alreadyEnrolled && !AdminMFAVerifiedFromContext(r.Context()) {
		if !verifyExistingFactor(a, body.StepUpCode) {
			response.WriteJSON(w, http.StatusForbidden, map[string]any{
				"error":      "mfa re-verification required to re-enroll",
				"requireMfa": true,
			})
			return
		}
	}

	if !VerifyTOTP(body.Secret, body.Code, time.Now()) {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "code did not verify against the supplied secret"})
		return
	}

	plain, hashes, err := GenerateRecoveryCodes()
	if err != nil {
		h.logger.Printf("admin /mfa/verify: recovery codes: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if err := h.repo.SetMFAEnrollment(ctx, adminID, body.Secret, hashes); err != nil {
		h.logger.Printf("admin /mfa/verify: persist: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Audit. MFA enrollment is a privileged event — admins
	// reviewing the log should see it clearly. Re-enrollment (which
	// rotates an existing factor) is flagged so it stands out from a
	// first-time enroll. Reuse the admin loaded above for the email.
	action := "mfa.enroll"
	if alreadyEnrolled {
		action = "mfa.reenroll"
	}
	Audit(ctx, h.repo, h.logger, AuditEntry{
		ID:         generateAuditID(),
		AdminID:    adminID,
		AdminEmail: a.Email,
		Action:     action,
		At:         time.Now().UTC(),
		IP:         clientIP(r),
		UserAgent:  r.Header.Get("User-Agent"),
	})

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"recoveryCodes": plain,
	})
}

// verifyExistingFactor reports whether `code` is a valid current TOTP
// code or recovery code for the admin's EXISTING (already-stored) MFA
// secret. It is the proof-of-possession check that authorises
// re-enrollment when the session isn't MFA-verified (#144).
//
// It is intentionally read-only: it neither records the consumed TOTP
// step nor burns the matched recovery code. That's safe here because a
// successful re-enroll immediately rewrites BOTH the secret and the
// whole recovery-code set via SetMFAEnrollment, so any code accepted
// here is invalidated wholesale the moment the enrollment lands. The
// login path (handler.go) still does the replay-resistant
// MarkTOTPStepUsed / ConsumeRecoveryCode bookkeeping for the actual
// authentication event.
func verifyExistingFactor(a *Admin, code string) bool {
	code = strings.TrimSpace(code)
	if a == nil || code == "" {
		return false
	}
	if a.MFASecret != "" && VerifyTOTP(a.MFASecret, code, time.Now()) {
		return true
	}
	matched, _ := ConsumeRecoveryCode(a.MFARecoveryCodes, code)
	return matched
}

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
//     Body: { secret, code }
//     Returns: { recoveryCodes } (8 codes, plaintext, ONE TIME ONLY)
//     Side effect: persists secret + hashed recovery codes,
//     flips MFAEnforced=true.
//
// /setup intentionally returns the secret to the FE rather than
// stashing it in the DB. The FE then echoes it back on /verify
// alongside the proof code, so a network-blip-mid-enrollment
// can't strand a half-saved secret. Stateless enrollment.
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
		"secret":      secret,
		"otpauthUri":  OTPAuthURI(secret, a.Email),
	})
}

// MFAVerify handles POST /admin/v1/auth/mfa/verify. Body:
// {secret, code}. On success: persists, flips MFAEnforced=true,
// returns 8 plaintext recovery codes the admin must save now.
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
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	body.Secret = strings.TrimSpace(body.Secret)
	body.Code = strings.TrimSpace(body.Code)
	if body.Secret == "" || body.Code == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "secret and code are required"})
		return
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.SetMFAEnrollment(ctx, adminID, body.Secret, hashes); err != nil {
		h.logger.Printf("admin /mfa/verify: persist: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Audit. MFA enrollment is a privileged event — admins
	// reviewing the log should see it clearly.
	var adminEmail string
	if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
		adminEmail = a.Email
	}
	Audit(ctx, h.repo, h.logger, AuditEntry{
		ID:         generateAuditID(),
		AdminID:    adminID,
		AdminEmail: adminEmail,
		Action:     "mfa.enroll",
		At:         time.Now().UTC(),
		IP:         clientIP(r),
		UserAgent:  r.Header.Get("User-Agent"),
	})

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"recoveryCodes": plain,
	})
}

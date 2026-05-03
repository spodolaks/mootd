package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// UserPurger is the privacy.Service contract the admin handler
// depends on. Defined here so admin/ doesn't import privacy/
// and the dependency stays one-way.
type UserPurger interface {
	Purge(ctx context.Context, userID string) (*PurgeReport, error)
}

// PurgeReport mirrors privacy.PurgeReport for the wire. Defined
// in admin/ rather than imported so the response type lives next
// to the spec-driven generator.
type PurgeReport struct {
	UserID      string           `json:"userId"`
	PurgedAt    time.Time        `json:"purgedAt"`
	Collections map[string]int64 `json:"collections"`
	Total       int64            `json:"total"`
}

// ErrUserAlreadyPurged is the sentinel returned by a UserPurger
// when the target user has no record left.
var ErrUserAlreadyPurged = errors.New("admin: user already purged")

// WithUserPurger wires the privacy service so the admin
// /users/{id}/purge endpoint becomes available. Optional —
// when unset, the endpoint returns 503.
func (h *Handler) WithUserPurger(p UserPurger) *Handler {
	h.userPurger = p
	return h
}

// PurgeUser handles DELETE /admin/v1/users/{id}/purge.
//
// Gates:
//   - users:purge permission (catalog: permissions.go).
//   - MFA re-verification: the access token MUST carry
//     mfaVerified=true. Stale tokens (where mfaVerified was
//     dropped on refresh) are rejected with 403, forcing the
//     admin to log in fresh before performing the purge.
//
// Audit: on success an admin_audit row records who triggered
// the purge, the target user, and the per-collection counts
// the privacy service returned. On failure, no audit row is
// written — we only audit completed actions so the audit log
// stays a "what happened" record, not a "what was attempted."
//
// Body (required): {"notes": "ticket-link or rationale"}.
// Notes appears in the audit row's metadata to satisfy the
// "every privileged action has a written rationale" rule
// from the runbook (docs/RUNBOOKS/incident-admin-credential-
// compromise.md).
func (h *Handler) PurgeUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermUsersPurge) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermUsersPurge,
		})
		return
	}
	// MFA gate. Token-level claim — set on login when an admin
	// presented their TOTP, dropped on refresh so re-verification
	// is required for sensitive actions.
	if !AdminMFAVerifiedFromContext(r.Context()) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":      "mfa re-verification required",
			"requireMfa": true,
		})
		return
	}
	if h.userPurger == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user purge not wired"})
		return
	}

	id = strings.TrimSpace(id)
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	var body struct {
		Notes string `json:"notes"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Notes) == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "notes is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	report, err := h.userPurger.Purge(ctx, id)
	if err != nil {
		// Idempotency: a purger returning ErrUserAlreadyPurged
		// (or a sentinel that wraps "no such user") translates
		// to 404. The privacy package's ErrUserNotFound is
		// rewrapped into ErrUserAlreadyPurged at the
		// adapter — here we accept either via Is.
		if errors.Is(err, ErrUserAlreadyPurged) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user already purged"})
			return
		}
		h.logger.Printf("admin /users/%s/purge: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "purge failed"})
		return
	}

	// Audit row. Done after the purge so the audit log only
	// records completed actions (per the audit.go contract).
	adminID, _ := AdminIDFromContext(r.Context())
	var adminEmail string
	if h.repo != nil {
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
	}
	Audit(ctx, h.repo, h.logger, AuditEntry{
		ID:           generateAuditID(),
		AdminID:      adminID,
		AdminEmail:   adminEmail,
		Action:       "users.purge",
		TargetUserID: id,
		TargetEntity: "users/" + id,
		Metadata: map[string]any{
			"notes":       body.Notes,
			"collections": report.Collections,
			"totalDocs":   report.Total,
		},
		At:        time.Now().UTC(),
		IP:        clientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	})

	response.WriteJSON(w, http.StatusOK, report)
}

package privacy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// Handler serves the self-serve privacy endpoints.
type Handler struct {
	logger *log.Logger
	svc    *Service
}

// NewHandler constructs a privacy Handler.
func NewHandler(logger *log.Logger, svc *Service) *Handler {
	return &Handler{logger: logger, svc: svc}
}

// SelfPurge handles DELETE /v1/privacy/self.
//
// Wipes the authenticated user's data across every per-user
// collection AND the user record itself. After this call:
//   - the user can no longer log in (user record is gone, so
//     /v1/auth/refresh fails on FindByRefreshToken).
//   - re-running this call returns 404 (idempotent erasure
//     per acceptance criterion).
//
// 60-second timeout to absorb large purges (heavy wardrobe +
// outfit history). A partial purge from a timeout is harmless
// — the client retries and the next pass cleans up the rest
// before deleting the user record.
func (h *Handler) SelfPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	report, err := h.svc.Purge(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user already purged"})
			return
		}
		h.logger.Printf("privacy: self purge for %s failed: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "purge failed"})
		return
	}
	h.logger.Printf("privacy: user %s self-purged: %d docs across %d collections", userID, report.Total, len(report.Collections))
	response.WriteJSON(w, http.StatusOK, report)
}

// SelfExport handles GET /v1/privacy/export.
//
// Returns a ZIP attachment containing one JSON file per
// collection. The user can hand this to support, archive it,
// or feed it to a script — it's the canonical record of
// everything we hold about them.
//
// Single-file ZIP rather than a streamed multi-file dump:
// (a) the FE / curl downloads it as one artifact, (b) we don't
// have to deal with chunked-transfer-encoding edge cases, (c) a
// 50KB/MB JSON ZIP is small enough to materialize in memory
// without OOM. Heavy users with thousands of events still fit
// comfortably under a few MB.
//
// 30-second timeout. Pure-read; no idempotency concern.
func (h *Handler) SelfExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	data, err := h.svc.Export(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.logger.Printf("privacy: export for %s failed: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "export failed"})
		return
	}

	// Build the zip in-memory.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	if err := writeJSON(zw, "manifest.json", map[string]any{
		"userId":      data.UserID,
		"generatedAt": data.GeneratedAt,
		"format":      "mootd-privacy-export-v1",
	}); err != nil {
		h.logger.Printf("privacy: export zip manifest: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "export failed"})
		return
	}
	if data.User != nil {
		if err := writeJSON(zw, "user.json", data.User); err != nil {
			h.logger.Printf("privacy: export zip user.json: %v", err)
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "export failed"})
			return
		}
	}
	for _, ent := range []struct {
		name string
		val  any
	}{
		{"wardrobe_items.json", data.WardrobeItems},
		{"outfits.json", data.Outfits},
		{"outfit_jobs.json", data.OutfitJobs},
		{"moodboards.json", data.Moodboards},
		{"outfit_feedback.json", data.OutfitFeedback},
		{"events.json", data.Events},
		{"llm_calls.json", data.LLMCalls},
		{"detection_runs.json", data.DetectionRuns},
		{"user_budget.json", data.UserBudget},
	} {
		// Skip empty arrays / nil scalars to keep the ZIP tidy.
		if isEmpty(ent.val) {
			continue
		}
		if err := writeJSON(zw, ent.name, ent.val); err != nil {
			h.logger.Printf("privacy: export zip %s: %v", ent.name, err)
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "export failed"})
			return
		}
	}
	if err := zw.Close(); err != nil {
		h.logger.Printf("privacy: export zip close: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "export failed"})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mootd-export-%s-%s.zip"`, userID, data.GeneratedAt.Format("20060102-150405")))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf.Bytes()); err != nil {
		h.logger.Printf("privacy: export write to wire: %v", err)
	}
}

// writeJSON writes one JSON file into the zip.
func writeJSON(zw *zip.Writer, name string, v any) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// isEmpty returns true for nil, empty slice, and empty map.
// Used to skip empty sections in the zip.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"mootd/backend/internal/shared/response"
)

// ImageStore is the small surface admin/ needs to persist a default's
// image bytes alongside the wardrobe-side GridFS. Implemented in app/
// by an adapter over wardrobe's MongoRepository so admin/ stays free
// of the wardrobe import (one-way dependency convention).
type ImageStore interface {
	// Save stores the image under key (used as the GridFS filename
	// downstream). Idempotent — overwrites any existing entry under
	// the same key. contentType MUST be a real MIME (e.g. "image/png").
	Save(ctx context.Context, key string, data []byte, contentType string) error
}

// DetectionPrefill is the description-only subset of a single-item
// detection run, used to populate the new-default modal in the admin
// UI. Image stays the original upload — generation output is discarded
// so curated defaults look like real photos, not LLM-rendered ones.
type DetectionPrefill struct {
	Label                 string            `json:"label"`
	Category              string            `json:"category"`
	Confidence            float64           `json:"confidence,omitempty"`
	Traits                map[string]string `json:"traits,omitempty"`
	StructuredDescription map[string]any    `json:"structuredDescription,omitempty"`
}

// ItemDetector wraps the existing wardrobe-side detection backend so
// admin/ can run a fresh photo through the same pipeline used by the
// mobile wardrobe upload — without importing wardrobe directly.
//
// The implementation discards any generated/ghost-mannequin image the
// pipeline produces. Curated defaults always show the operator's
// upload, so users see a realistic photo on day-one rather than an
// AI-rendered placeholder.
type ItemDetector interface {
	DetectFromBytes(ctx context.Context, imageData []byte, filename string) (DetectionPrefill, error)
}

// WithImageStore + WithItemDetector wire the new "upload + autodetect"
// flow on /admin/v1/archetype-defaults/detect. Both are required for
// the endpoint to come online; either one missing → 503.
func (h *Handler) WithImageStore(s ImageStore) *Handler {
	h.imageStore = s
	return h
}

func (h *Handler) WithItemDetector(d ItemDetector) *Handler {
	h.itemDetector = d
	return h
}

// ArchetypeDefaultDetectionResult is the wire shape returned by
// POST /admin/v1/archetype-defaults/detect. The frontend prefills
// the new-default modal with category + label + traits + description
// and uses imageUrl as the image preview (and as the value to send
// back on the subsequent POST that creates the row).
type ArchetypeDefaultDetectionResult struct {
	// ID is the future archetype-default row's ID. Pre-minted on
	// detect so the upload bytes can be keyed against it; the FE
	// passes the same ID back on the create call.
	ID string `json:"id"`
	// ImageURL points at /v1/wardrobe/items/{id}/image (the
	// wardrobe ServeImage route works for any GridFS filename;
	// no ownership check). The seeder copies this URL string
	// straight onto seeded wardrobe items so the mobile app
	// can render the image without admin auth.
	ImageURL string `json:"imageUrl"`
	// Detection-derived fields. Curators can edit any of them
	// before saving.
	Category              string            `json:"category"`
	Label                 string            `json:"label"`
	Confidence            float64           `json:"confidence,omitempty"`
	Traits                map[string]string `json:"traits,omitempty"`
	StructuredDescription map[string]any    `json:"structuredDescription,omitempty"`
}

const (
	// maxArchetypeDefaultImageBytes caps a single upload — same
	// 10 MiB ceiling the orchestrator enforces, set client-side so
	// a too-big upload is rejected before we bill the pipeline.
	maxArchetypeDefaultImageBytes = 10 << 20

	// detectTimeout matches the 3-minute orchestrator ceiling.
	// Pipeline median is ~1-30s; the timeout is the upper-bound
	// safety net for a hung upstream.
	archetypeDefaultDetectTimeout = 3 * time.Minute
)

// detectArchetypeDefault handles POST /admin/v1/archetype-defaults/detect.
//
// Flow:
//  1. Multipart parse the `image` field.
//  2. Mint a fresh `ad_<hex>` ID.
//  3. Save the bytes to the image store (GridFS) under that key — so
//     the UI's preview URL works immediately AND the eventual saved
//     archetype_default_items row reuses the same key.
//  4. Run the detector; discard any generated image, keep description.
//  5. Return ArchetypeDefaultDetectionResult to the FE.
//
// Permission: prompts:write (curating content).
func (h *Handler) detectArchetypeDefault(w http.ResponseWriter, r *http.Request) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}
	if h.imageStore == nil || h.itemDetector == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "archetype-defaults detect not wired (image store or detector missing)",
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxArchetypeDefaultImageBytes+1<<20)
	if err := r.ParseMultipartForm(maxArchetypeDefaultImageBytes + 1<<20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			response.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error": fmt.Sprintf("image exceeds %d-byte limit", maxArchetypeDefaultImageBytes),
			})
			return
		}
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart parse: " + err.Error()})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'image' field: " + err.Error()})
		return
	}
	defer file.Close()

	imgData, err := io.ReadAll(file)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "read image: " + err.Error()})
		return
	}
	if len(imgData) == 0 {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image is empty"})
		return
	}

	// Sniff the MIME from magic bytes — Content-Type from the
	// browser is forgeable. The HTTP package's DetectContentType
	// covers all the wardrobe-supported formats (jpeg, png, gif,
	// webp).
	contentType := http.DetectContentType(imgData)

	id := "ad_" + randomHex(16)
	filename := id
	if header != nil && header.Filename != "" {
		filename = header.Filename
	}

	ctx, cancel := context.WithTimeout(r.Context(), archetypeDefaultDetectTimeout)
	defer cancel()

	if err := h.imageStore.Save(ctx, id, imgData, contentType); err != nil {
		h.logger.Printf("admin /archetype-defaults/detect: save image: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "save image"})
		return
	}

	prefill, err := h.itemDetector.DetectFromBytes(ctx, imgData, filename)
	if err != nil {
		h.logger.Printf("admin /archetype-defaults/detect: detect: %v", err)
		response.WriteJSON(w, http.StatusBadGateway, map[string]string{
			"error": "detection failed: " + err.Error(),
		})
		return
	}

	imageURL := "/v1/wardrobe/items/" + id + "/image"
	response.WriteJSON(w, http.StatusOK, ArchetypeDefaultDetectionResult{
		ID:                    id,
		ImageURL:              imageURL,
		Category:              prefill.Category,
		Label:                 prefill.Label,
		Confidence:            prefill.Confidence,
		Traits:                prefill.Traits,
		StructuredDescription: prefill.StructuredDescription,
	})
}


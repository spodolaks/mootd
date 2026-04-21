package wardrobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"mootd/backend/internal/shared/id"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/pagination"
	"mootd/backend/internal/shared/response"
)

const maxImageSize = 10 << 20 // 10 MB

// Handler handles wardrobe HTTP endpoints.
type Handler struct {
	logger       *log.Logger
	detector     *Detector
	searcher     *Searcher
	repo         Repository
	bgRemover    *BackgroundRemover
	workerCtx    context.Context // server-scoped context for background goroutines
	detectJobs   *DetectJobStore // optional — when nil, the async path is unavailable
}

// NewHandler creates a Handler with the given dependencies.
// workerCtx should be tied to server lifetime so background goroutines stop on shutdown.
// detectJobs may be nil (e.g. when Redis is unavailable); the async Detect path
// returns 503 in that case and clients fall back to the sync endpoint.
func NewHandler(logger *log.Logger, detector *Detector, searcher *Searcher, repo Repository, bgRemover *BackgroundRemover, workerCtx context.Context, detectJobs *DetectJobStore) *Handler {
	return &Handler{logger: logger, detector: detector, searcher: searcher, repo: repo, bgRemover: bgRemover, workerCtx: workerCtx, detectJobs: detectJobs}
}

// Detect handles POST /v1/wardrobe/detect.
// Accepts a multipart image, runs detection via the external service,
// persists each detected item, and returns them to the caller.
func (h *Handler) Detect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	if err := r.ParseMultipartForm(maxImageSize); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image too large or invalid form"})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing image field"})
		return
	}
	defer file.Close()

	imageData, err := io.ReadAll(io.LimitReader(file, maxImageSize))
	if err != nil {
		h.logger.Printf("wardrobe: read image: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read image"})
		return
	}

	detected, err := h.detector.Detect(r.Context(), imageData, header.Filename)
	if err != nil {
		h.logger.Printf("wardrobe: detect for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "clothing detection failed"})
		return
	}

	items := h.processDetected(r.Context(), userID, detected)
	response.WriteJSON(w, http.StatusOK, DetectResponse{Items: items})
}

// processDetected is the shared post-detection pipeline: filter out skipped
// items, then for each remaining item fetch or decode its image, save it to
// GridFS, fire-and-forget background removal, and persist the ClothingItem.
//
// Runs items in parallel because each one hits the detection service for an
// image download + the background-removal service. The parallel fan-out is
// bounded only by the goroutine stack; in practice detection returns 1-8
// items per photo so that's fine.
//
// Callable from both the sync Detect handler and the async SubmitDetect
// worker. The ctx the caller passes controls the lifetime: sync uses the
// request context (cancelled when the response closes); async uses the
// server-scoped workerCtx so processing outlives the HTTP exchange.
func (h *Handler) processDetected(ctx context.Context, userID string, detected []jobItem) []DetectedItem {
	if len(detected) == 0 {
		return []DetectedItem{}
	}

	now := time.Now().UTC()

	// Skipped items are noise from the detection service (low confidence,
	// duplicate class, etc.) — drop them before we fan out work.
	toProcess := make([]jobItem, 0, len(detected))
	for _, d := range detected {
		if !d.Skipped {
			toProcess = append(toProcess, d)
		}
	}
	if len(toProcess) == 0 {
		return []DetectedItem{}
	}

	responseItems := make([]DetectedItem, len(toProcess))
	var wg sync.WaitGroup
	for i, d := range toProcess {
		wg.Add(1)
		go func(idx int, d jobItem) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					h.logger.Printf("wardrobe: panic in detection goroutine for item %d: %v", idx, rec)
				}
			}()

			// Prefer the new family/type fields; fall back to legacy category/label.
			category := d.Family
			if category == "" {
				category = d.Category
			}
			label := d.Type
			if label == "" {
				label = d.Label
			}

			itemID := id.Generate()

			// Acquire image bytes: either inline from local API or downloaded from remote URL.
			stableImageURL := ""
			stablePngURL := ""
			var imgData []byte
			var imgCT string

			if len(d.ImageData) > 0 {
				// Local API provides image bytes inline (base64-decoded).
				imgData = d.ImageData
				imgCT = "image/png"
			} else if d.ImageURL != "" {
				var dlErr error
				imgData, imgCT, dlErr = downloadImage(ctx, d.ImageURL)
				if dlErr != nil {
					h.logger.Printf("wardrobe: download image for item %s: %v", itemID, dlErr)
				}
			}

			if len(imgData) > 0 {
				if saveErr := h.repo.SaveImage(ctx, itemID, imgData, imgCT); saveErr != nil {
					h.logger.Printf("wardrobe: store image for item %s: %v", itemID, saveErr)
				} else {
					stableImageURL = "/v1/wardrobe/items/" + itemID + "/image"

					if h.bgRemover != nil {
						pngData, bgErr := h.bgRemover.RemoveBackground(imgData, itemID+".jpg")
						if bgErr != nil {
							h.logger.Printf("wardrobe: bg removal for item %s: %v", itemID, bgErr)
							TriggerPNGRetry(ctx, h.repo, h.bgRemover, h.logger)
						} else if saveErr := h.repo.SaveImage(ctx, itemID+"-png", pngData, "image/png"); saveErr != nil {
							h.logger.Printf("wardrobe: store png for item %s: %v", itemID, saveErr)
						} else {
							stablePngURL = "/v1/wardrobe/items/" + itemID + "-png/image"
						}
					}
				}
			}

			traits := d.Traits
			if traits == nil {
				traits = map[string]string{}
			}

			item := ClothingItem{
				ID:          itemID,
				UserID:      userID,
				Category:    category,
				Label:       label,
				ImageURL:    stableImageURL,
				PngImageURL: stablePngURL,
				Traits:      traits,
				CreatedAt:   now,
			}
			if saveErr := h.repo.Save(ctx, item); saveErr != nil {
				h.logger.Printf("wardrobe: save item for user %s: %v", userID, saveErr)
				// Continue — return detected items even if a single save fails.
			}
			responseItems[idx] = DetectedItem{
				ID:          itemID,
				Category:    category,
				Label:       label,
				ImageURL:    stableImageURL,
				PngImageURL: stablePngURL,
				Confidence:  d.Confidence,
				Traits:      traits,
			}
		}(i, d)
	}
	wg.Wait()

	return responseItems
}

// SubmitDetect handles POST /v1/wardrobe/detect-jobs.
//
// Same input as Detect (multipart image), but returns immediately with
// 202 + { jobId }. The actual detection runs in a background goroutine
// using workerCtx (server-scoped) so it survives the HTTP exchange. The
// client polls /v1/wardrobe/detect-jobs/{id} for the result.
//
// This exists because Cloudflare's edge enforces a ~100s read timeout on
// the free plan; synchronous detection can take 30–180s for a full wardrobe
// photo and routinely trips that cap. The job runs in Redis with a 10-min
// TTL matching the outfit-generation async flow.
//
// Responses:
//
//	202 Accepted — { "jobId": "..." }
//	400 — missing/invalid image
//	401 — missing auth
//	503 — Redis unavailable (no async store)
func (h *Handler) SubmitDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.detectJobs == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "async detection unavailable (Redis not configured)"})
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	if err := r.ParseMultipartForm(maxImageSize); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "image too large or invalid form"})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing image field"})
		return
	}
	defer file.Close()

	imageData, err := io.ReadAll(io.LimitReader(file, maxImageSize))
	if err != nil {
		h.logger.Printf("wardrobe: read image: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read image"})
		return
	}
	filename := header.Filename

	jobID := id.Generate()
	job := &DetectJob{
		ID:        jobID,
		UserID:    userID,
		Status:    DetectJobPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.detectJobs.Save(r.Context(), job); err != nil {
		h.logger.Printf("wardrobe: save detect job %s: %v", jobID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create job"})
		return
	}

	go h.runDetectionJob(jobID, userID, imageData, filename)

	response.WriteJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// runDetectionJob performs the full detection pipeline in the background
// and writes the result back into the job store. Uses workerCtx (server-
// scoped) with a 3-minute budget — the external detection service's own
// timeout is 2 minutes, and this gives the subsequent image-download +
// GridFS-save work time to finish without abandoning the whole job.
func (h *Handler) runDetectionJob(jobID, userID string, imageData []byte, filename string) {
	ctx, cancel := context.WithTimeout(h.workerCtx, 3*time.Minute)
	defer cancel()

	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Printf("wardrobe: panic in detect job %s: %v", jobID, rec)
			failed := &DetectJob{ID: jobID, UserID: userID, Status: DetectJobFailed, Error: "detection failed", CreatedAt: time.Now().UTC()}
			if err := h.detectJobs.Save(ctx, failed); err != nil {
				h.logger.Printf("wardrobe: save panic-failed job %s: %v", jobID, err)
			}
		}
	}()

	processing := &DetectJob{ID: jobID, UserID: userID, Status: DetectJobProcessing, CreatedAt: time.Now().UTC()}
	if err := h.detectJobs.Save(ctx, processing); err != nil {
		h.logger.Printf("wardrobe: mark job %s processing: %v", jobID, err)
	}

	detected, err := h.detector.Detect(ctx, imageData, filename)
	if err != nil {
		h.logger.Printf("wardrobe: detect for job %s: %v", jobID, err)
		failed := &DetectJob{ID: jobID, UserID: userID, Status: DetectJobFailed, Error: "clothing detection failed", CreatedAt: time.Now().UTC()}
		if err := h.detectJobs.Save(ctx, failed); err != nil {
			h.logger.Printf("wardrobe: save failed job %s: %v", jobID, err)
		}
		return
	}

	items := h.processDetected(ctx, userID, detected)

	completed := &DetectJob{
		ID:        jobID,
		UserID:    userID,
		Status:    DetectJobCompleted,
		Items:     items,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.detectJobs.Save(ctx, completed); err != nil {
		h.logger.Printf("wardrobe: save completed job %s: %v", jobID, err)
	}
	h.logger.Printf("wardrobe: detect job %s completed — %d items", jobID, len(items))
}

// PollDetectJob handles GET /v1/wardrobe/detect-jobs/{id}.
//
// Ownership check mirrors outfit.PollJob: mismatches return 404 (not 403)
// so a malicious client can't enumerate other users' job IDs.
func (h *Handler) PollDetectJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.detectJobs == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "async detection unavailable (Redis not configured)"})
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	jobID := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/detect-jobs/")
	if jobID == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing job ID"})
		return
	}

	job, err := h.detectJobs.Get(r.Context(), jobID)
	if err != nil || job.UserID != userID {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	response.WriteJSON(w, http.StatusOK, job)
}

// itemsResponse is the paginated response for GET /v1/wardrobe/items.
type itemsResponse struct {
	Items      []ClothingItem `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

// Items handles GET /v1/wardrobe/items.
// Supports cursor-based pagination via ?limit=N&cursor=<token>.
func (h *Handler) Items(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	limit, cursor := pagination.ParseParams(r, 20, 200)

	items, err := h.repo.FindByUserPaginated(r.Context(), userID, limit, cursor)
	if err != nil {
		h.logger.Printf("wardrobe: list items for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch wardrobe"})
		return
	}

	var nextCursor *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		nc := pagination.EncodeCursor(last.CreatedAt, last.ID)
		nextCursor = &nc
	}

	// Return an empty array rather than null when there are no items.
	if items == nil {
		items = []ClothingItem{}
	}
	response.WriteJSON(w, http.StatusOK, itemsResponse{Items: items, NextCursor: nextCursor})
}

// Item dispatches PATCH and DELETE requests for /v1/wardrobe/items/{id}.
//
// PATCH  /v1/wardrobe/items/{id} — update item traits
// DELETE /v1/wardrobe/items/{id} — permanently remove the item
func (h *Handler) Item(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch:
		h.updateItem(w, r)
	case http.MethodDelete:
		h.deleteItem(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// updateItem handles PATCH /v1/wardrobe/items/{id}.
//
// Request body: { "traits": { "color": "black", ... } }
// Response: 200 OK — { "status": "ok" }
// Response: 400 — missing/invalid body
// Response: 401 — unauthorized
// Response: 404 — item not found or not owned by caller
func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	itemID := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/items/")
	if itemID == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing item id"})
		return
	}

	var body struct {
		Traits   map[string]string `json:"traits"`
		Label    string            `json:"label"`
		ImageURL string            `json:"imageUrl"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Traits == nil {
		body.Traits = map[string]string{}
	}

	// If imageUrl is an external product URL, download and store it locally
	// so the URL never expires, then run background removal asynchronously.
	stableImageURL := body.ImageURL
	if strings.HasPrefix(body.ImageURL, "http") && isAllowedImageURL(body.ImageURL) {
		imgData, imgCT, dlErr := downloadImage(r.Context(), body.ImageURL)
		if dlErr != nil {
			h.logger.Printf("wardrobe: update: download product image for item %s: %v", itemID, dlErr)
			stableImageURL = ""
		} else if saveErr := h.repo.SaveImage(r.Context(), itemID, imgData, imgCT); saveErr != nil {
			h.logger.Printf("wardrobe: update: store product image for item %s: %v", itemID, saveErr)
			stableImageURL = ""
		} else {
			stableImageURL = "/v1/wardrobe/items/" + itemID + "/image"
			go func() {
				defer func() {
					if r := recover(); r != nil {
						h.logger.Printf("wardrobe: update: bg removal panic for item %s: %v", itemID, r)
					}
				}()
				if h.bgRemover == nil {
					return
				}
				pngData, bgErr := h.bgRemover.RemoveBackground(imgData, itemID+".jpg")
				if bgErr != nil {
					h.logger.Printf("wardrobe: update: bg removal for item %s: %v", itemID, bgErr)
					TriggerPNGRetry(h.workerCtx, h.repo, h.bgRemover, h.logger)
					return
				}
				if saveErr := h.repo.SaveImage(h.workerCtx, itemID+"-png", pngData, "image/png"); saveErr != nil {
					h.logger.Printf("wardrobe: update: store png for item %s: %v", itemID, saveErr)
					return
				}
				pngURL := "/v1/wardrobe/items/" + itemID + "-png/image"
				if updateErr := h.repo.UpdatePngURL(h.workerCtx, itemID, pngURL); updateErr != nil {
					h.logger.Printf("wardrobe: update: set pngImageUrl for item %s: %v", itemID, updateErr)
				}
			}()
		}
	}

	if err := h.repo.UpdateItem(r.Context(), itemID, userID, body.Traits, body.Label, stableImageURL); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
			return
		}
		h.logger.Printf("wardrobe: update traits for item %s user %s: %v", itemID, userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update item"})
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deleteItem handles DELETE /v1/wardrobe/items/{id}.
//
// Response: 204 No Content — item deleted
// Response: 401 — unauthorized
// Response: 404 — item not found or not owned by caller
func (h *Handler) deleteItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	itemID := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/items/")
	if itemID == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing item id"})
		return
	}

	if err := h.repo.Delete(r.Context(), itemID, userID); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
			return
		}
		h.logger.Printf("wardrobe: delete item %s user %s: %v", itemID, userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete item"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ServeImage handles GET /v1/wardrobe/items/{id}/image.
// Returns the stored image for the item. No auth required — item IDs are UUIDs.
func (h *Handler) ServeImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path: /v1/wardrobe/items/{id}/image
	path := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/items/")
	itemID := strings.TrimSuffix(path, "/image")
	if itemID == "" || itemID == path {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	data, contentType, err := h.repo.GetImage(r.Context(), itemID)
	if err != nil {
		if errors.Is(err, mongo.ErrFileNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.logger.Printf("wardrobe: serve image for item %s: %v", itemID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := w.Write(data); err != nil {
		h.logger.Printf("wardrobe: serve image for item %s: write body failed: %v", itemID, err)
	}
}

// isAllowedImageURL validates that the URL points to a safe external host.
// Blocks requests to internal/private IPs to prevent SSRF attacks.
func isAllowedImageURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}

	// Block obviously internal hosts.
	blockedPrefixes := []string{"127.", "10.", "192.168.", "172.16.", "172.17.", "172.18.",
		"172.19.", "172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.", "0."}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(host, prefix) {
			return false
		}
	}

	blockedHosts := []string{"localhost", "metadata.google.internal", "::1", "[::1]"}
	for _, blocked := range blockedHosts {
		if host == blocked {
			return false
		}
	}

	// Block link-local / cloud metadata IPs.
	if strings.HasPrefix(host, "169.254.") || strings.HasPrefix(host, "fd") || strings.HasPrefix(host, "fc") {
		return false
	}

	return u.Scheme == "http" || u.Scheme == "https"
}

// imageDownloadClient is used for fetching external images with an explicit timeout.
var imageDownloadClient = &http.Client{Timeout: 30 * time.Second}

// downloadImage fetches image bytes from a URL. Returns data and content-type.
// Non-2xx responses are treated as errors.
func downloadImage(ctx context.Context, imageURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build image request: %w", err)
	}

	resp, err := imageDownloadClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image fetch returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize))
	if err != nil {
		return nil, "", fmt.Errorf("read image body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	return data, ct, nil
}

// Search handles POST /v1/wardrobe/items/{id}/search.
// Body: { "brand": "Nike" }
// Fetches the stored image for the item and searches the external catalog by brand.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Path: /v1/wardrobe/items/{id}/search
	path := strings.TrimPrefix(r.URL.Path, "/v1/wardrobe/items/")
	itemID := strings.TrimSuffix(path, "/search")
	if itemID == "" || itemID == path {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing item id"})
		return
	}

	var body struct {
		Brand string `json:"brand"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Brand == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "brand is required"})
		return
	}

	imageData, contentType, err := h.repo.GetImage(r.Context(), itemID)
	if err != nil {
		h.logger.Printf("wardrobe: search: get image for item %s: %v", itemID, err)
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "item image not found"})
		return
	}

	h.logger.Printf("wardrobe: search item %s brand %q", itemID, body.Brand)

	products, err := h.searcher.Search(r.Context(), imageData, contentType, body.Brand)
	if err != nil {
		h.logger.Printf("wardrobe: search item %s brand %q: %v", itemID, body.Brand, err)
		response.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "search failed"})
		return
	}

	if products == nil {
		products = []SearchProduct{}
	}
	h.logger.Printf("wardrobe: search item %s brand %q: %d result(s)", itemID, body.Brand, len(products))
	for i, p := range products {
		h.logger.Printf("  [%d] id=%s title=%q price=%s imageUrl=%s", i+1, p.ID, p.Title, p.Price, p.ImageURL)
	}
	response.WriteJSON(w, http.StatusOK, SearchResponse{Results: products})
}

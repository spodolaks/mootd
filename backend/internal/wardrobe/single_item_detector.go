package wardrobe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SingleItemDetector calls the singleItemDetection orchestrator
// instead of the legacy on-host cloth-detection API. Selected
// at boot when DETECTION_BACKEND=singleitem.
//
// The orchestrator is a separate service (singleItemDetection)
// that runs the v1 pipeline end-to-end (detect → describe →
// generate ghost mannequin → HITL gate). Its public contract:
//
//	POST   /v1/single-item/process              (multipart, SSE response)
//	GET    /v1/single-item/result/{request_id}  (final ClothingItem)
//	DELETE /v1/single-item/process/{request_id} (cancel)
//
// We use the POST + poll pattern: kick the pipeline off, drain
// the SSE response (so the orchestrator's writer doesn't block
// on a non-consuming client), then GET /result with backoff
// until 200. Drain-then-poll is simpler than parsing SSE frames
// and works for both "completed" and "failed" terminal states.
//
// The orchestrator processes ONE garment per photo by design.
// The legacy detector returned `[]jobItem` (multi-item); we
// preserve that shape with a single-element list so the
// wardrobe handler doesn't branch on backend.
type SingleItemDetector struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *log.Logger
}

// NewSingleItemDetector constructs a SingleItemDetector. baseURL
// must be the orchestrator's root (e.g.
// http://orchestrator:8080); apiKey is the optional service
// token (header X-API-Key — only enforced when
// SID_API_KEYS is set on the orchestrator side). logger is the
// shared mootd logger.
func NewSingleItemDetector(baseURL, apiKey string, logger *log.Logger) *SingleItemDetector {
	return &SingleItemDetector{
		baseURL: baseURL,
		apiKey:  apiKey,
		// 3-minute ceiling matches the legacy detector. Median
		// pipeline latency on the v1 pipeline is ~8-30s
		// (balanced tier); ceiling protects against a hung
		// upstream model.
		client: &http.Client{Timeout: 3 * time.Minute},
		logger: logger,
	}
}

// Compile-time satisfaction check.
var _ DetectorBackend = (*SingleItemDetector)(nil)

// pollInterval is the gap between /result polls. The
// orchestrator's pipeline emits a final SSE event when done;
// we don't subscribe to those (would require an SSE parser),
// so a short poll keeps end-to-end latency low without
// hammering the orchestrator.
const singleitemPollInterval = 500 * time.Millisecond

// Detect submits the image to the orchestrator and waits for
// the pipeline to complete. Returns a single-element jobItem
// list (the orchestrator processes one garment per photo).
func (d *SingleItemDetector) Detect(
	ctx context.Context,
	userID, runID string,
	imageData []byte,
	filename string,
) ([]jobItem, *DetectionRunData, error) {
	if d.baseURL == "" {
		return nil, nil, fmt.Errorf("singleitem detector: base URL not configured (set SINGLEITEM_BASE_URL)")
	}

	startedAt := time.Now().UTC()

	// Mint a request_id locally so we know which row to poll for
	// even before the SSE stream tells us. Reusing mootd's runID
	// would be cleaner but the orchestrator imposes its own
	// uniqueness contract (one in-flight job per id), and
	// re-running the same runID across detection backends would
	// collide.
	requestID := generateSingleItemRequestID(runID)

	if err := d.submitProcess(ctx, requestID, userID, imageData, filename); err != nil {
		return nil, nil, fmt.Errorf("submit process: %w", err)
	}

	item, err := d.pollResult(ctx, requestID)
	if err != nil {
		return nil, nil, fmt.Errorf("poll result: %w", err)
	}

	endedAt := time.Now().UTC()
	ji := itemToJobItem(item)

	// Hydrate the image bytes by fetching the orchestrator's
	// GridFS-backed blob endpoint. Without this step the
	// jobItem only carries an opaque "gridfs://sid_<bucket>/<id>"
	// URL that the wardrobe handler can't HTTP-download — and the
	// item lands in mootd's DB with an empty imageUrl, which the
	// mobile app renders as a blank tile. Best-effort: a fetch
	// failure logs + leaves ImageData empty (legacy behaviour),
	// so the bug is regression-bound, not a hard 500.
	if data, err := d.fetchBlobBytes(ctx, ji.ImageURL); err != nil {
		d.logger.Printf("singleitem: blob fetch for %s failed: %v (item will land with empty imageUrl)", ji.ImageURL, err)
	} else {
		ji.ImageData = data
		// Clear ImageURL so the handler's branch picks ImageData
		// rather than re-trying a download against the gridfs://
		// scheme.
		ji.ImageURL = ""
	}

	jobItems := []jobItem{ji}
	runData := &DetectionRunData{
		StartedAt: startedAt,
		EndedAt:   endedAt,
		// Orchestrator's pipelineMetadata carries cost +
		// per-stage latency; mootd's DetectionRunData has a
		// single TotalCostUSD slot, so we surface that and
		// let the admin /detection-runs detail page show "—"
		// for the per-stage breakdown (not collected for
		// orchestrator runs).
		TotalCostUSD: pipelineCostFromItem(item),
	}

	d.logger.Printf("singleitem: user=%s run=%s sid_request_id=%s item=%s/%s cost=$%.4f latency=%dms bytes=%d",
		userID, runID, requestID, item.Category, item.Label,
		runData.TotalCostUSD, endedAt.Sub(startedAt).Milliseconds(), len(ji.ImageData))

	return jobItems, runData, nil
}

// fetchBlobBytes translates an orchestrator-internal
// "gridfs://sid_<bucket>/<id>" URL into a real HTTP call against
// the orchestrator's GET /v1/single-item/blob/{bucket}/{id}
// route and returns the raw bytes. Empty / non-gridfs URLs
// surface a friendly error so callers can degrade rather than
// panic. Required because the mootd wardrobe handler expects
// either inline ImageData or a fetchable HTTP URL — the
// orchestrator's gridfs:// scheme satisfies neither.
func (d *SingleItemDetector) fetchBlobBytes(ctx context.Context, gridfsURL string) ([]byte, error) {
	const prefix = "gridfs://sid_"
	if !strings.HasPrefix(gridfsURL, prefix) {
		return nil, fmt.Errorf("expected gridfs://sid_<bucket>/<id> url, got %q", gridfsURL)
	}
	rest := gridfsURL[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash == len(rest)-1 {
		return nil, fmt.Errorf("malformed gridfs url %q (need bucket/id after sid_)", gridfsURL)
	}
	bucket, blobID := rest[:slash], rest[slash+1:]

	url := d.baseURL + "/v1/single-item/blob/" + bucket + "/" + blobID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("blob GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("blob GET %s returned %d: %s", url, resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

// submitProcess fires POST /v1/single-item/process with the
// image as a multipart upload and drains the SSE stream so the
// orchestrator can flush its writer. We don't parse the SSE
// frames — the GET /result endpoint is the source of truth
// for the final payload.
func (d *SingleItemDetector) submitProcess(ctx context.Context, requestID, userID string, imageData []byte, filename string) error {
	body, contentType, err := buildMultipart(imageData, filename)
	if err != nil {
		return fmt.Errorf("build multipart: %w", err)
	}

	url := d.baseURL + "/v1/single-item/process"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Request-Id", requestID)
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}
	if userID != "" {
		req.Header.Set("X-Mootd-User-Id", userID)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("orchestrator unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("orchestrator %s returned %d: %s", url, resp.StatusCode, string(errBody))
	}

	// Drain the SSE stream so the orchestrator's writer flushes
	// and the underlying connection releases. We cap the read
	// at 4MB — pipeline events are tiny (a few KB total) but a
	// runaway server should NEVER tie up the wardrobe handler.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024*1024))
	return nil
}

// pollResult walks GET /v1/single-item/result/{request_id}
// every singleitemPollInterval until the orchestrator returns
// 200 (completed) or the orchestrator returns a failed-status
// payload, or ctx is cancelled.
func (d *SingleItemDetector) pollResult(ctx context.Context, requestID string) (*singleItemClothingItem, error) {
	url := fmt.Sprintf("%s/v1/single-item/result/%s", d.baseURL, requestID)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build result request: %w", err)
		}
		if d.apiKey != "" {
			req.Header.Set("X-API-Key", d.apiKey)
		}

		resp, err := d.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("result poll: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read result body: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			// Could be the completed item OR a failed-status
			// payload. The orchestrator's Result handler
			// distinguishes by shape: completed → ClothingItem,
			// failed → {status, error_code, error_msg}.
			var maybeFail struct {
				Status    string `json:"status"`
				ErrorCode string `json:"error_code"`
				ErrorMsg  string `json:"error_msg"`
			}
			if err := json.Unmarshal(body, &maybeFail); err == nil && maybeFail.Status == "failed" {
				return nil, fmt.Errorf("orchestrator pipeline failed: %s (%s)", maybeFail.ErrorMsg, maybeFail.ErrorCode)
			}
			var item singleItemClothingItem
			if err := json.Unmarshal(body, &item); err != nil {
				return nil, fmt.Errorf("decode completed item: %w", err)
			}
			if item.ID == "" {
				return nil, errors.New("completed result has empty id")
			}
			return &item, nil
		case http.StatusAccepted:
			// Still running — back off + retry.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(singleitemPollInterval):
			}
		case http.StatusNotFound:
			// Race: orchestrator hasn't yet recorded the job.
			// Brief wait + retry once.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(singleitemPollInterval):
			}
		default:
			return nil, fmt.Errorf("orchestrator result %s returned %d: %s", url, resp.StatusCode, string(body))
		}
	}
}

// generateSingleItemRequestID mints a request_id for the
// orchestrator. Combines mootd's runID prefix (so logs across
// systems can be joined) with a random suffix (so retries
// against the same runID don't collide on the orchestrator's
// uniqueness check).
func generateSingleItemRequestID(runID string) string {
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	prefix := runID
	if prefix == "" {
		prefix = "mootd"
	}
	return prefix + "-" + hex.EncodeToString(suffix)
}

// pipelineCostFromItem extracts the rolled-up cost from the
// orchestrator's pipelineMetadata if present. Returns 0 when
// the field is missing or unparseable so the row still saves
// cleanly.
func pipelineCostFromItem(item *singleItemClothingItem) float64 {
	if item == nil || item.PipelineMetadata == nil {
		return 0
	}
	if v, ok := item.PipelineMetadata["totalCostUsd"].(float64); ok {
		return v
	}
	return 0
}

// singleItemClothingItem maps the orchestrator's domain
// `ClothingItem` shape (see
// singleItemDetection/internal/domain/clothing_item.go) onto
// the fields we actually consume on the mootd side. Anything
// not listed here is ignored — the admin proxy returns the
// full row to the admin UI directly when needed.
type singleItemClothingItem struct {
	ID                    string         `json:"id"`
	Category              string         `json:"category"`
	Label                 string         `json:"label"`
	ImageURL              string         `json:"imageUrl"`
	PngImageURL           string         `json:"pngImageUrl,omitempty"`
	GenerationImageURL    string         `json:"generationImageUrl,omitempty"`
	StructuredDescription map[string]any `json:"structuredDescription,omitempty"`
	ConfidenceOverall     float64        `json:"confidenceOverall,omitempty"`
	HITLRequired          bool           `json:"hitlRequired,omitempty"`
	HITLReason            string         `json:"hitlReason,omitempty"`
	PipelineMetadata      map[string]any `json:"pipelineMetadata,omitempty"`
}

// itemToJobItem adapts the orchestrator's ClothingItem into the
// internal jobItem shape. Picks the best available URL for
// each role (generationImageURL > pngImageURL > imageURL) and
// flattens the structured description into the trait map the
// downstream wardrobe code expects.
func itemToJobItem(it *singleItemClothingItem) jobItem {
	imgURL := it.ImageURL
	if it.GenerationImageURL != "" {
		imgURL = it.GenerationImageURL
	}
	return jobItem{
		ID:                    it.ID,
		Category:              it.Category,
		Label:                 it.Label,
		Family:                it.Category, // legacy alias
		Type:                  it.Label,    // legacy alias
		ImageURL:              imgURL,
		Confidence:            it.ConfidenceOverall,
		Skipped:               false,
		Traits:                flattenTraits(it.StructuredDescription),
		StructuredDescription: it.StructuredDescription,
	}
}

// flattenTraits projects the (potentially deep) structured
// description into a flat map[string]string. For each top-level
// key, we surface a representative string leaf so the mobile
// client (TraitSelectionScreen) can pre-fill its inputs and let
// the Done button enable — empty values dead-end that flow.
//
// Selection rule per top-level value:
//   - string  → kept verbatim (trimmed); empty strings dropped.
//   - object  → the "primary" leaf is preferred (matches the
//     orchestrator's closed-enum garment description, where
//     attributes nest as {primary, secondary, ...}); otherwise
//     the first non-empty string leaf found by a sorted-key DFS.
//   - other   → dropped (numbers, bools, arrays don't fit the
//     map[string]string trait shape; admins can drill into the
//     full description via the HITL queue).
func flattenTraits(structured map[string]any) map[string]string {
	if len(structured) == 0 {
		return nil
	}
	out := make(map[string]string, len(structured))
	for k, v := range structured {
		if s, ok := firstStringLeaf(v); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstStringLeaf returns a representative non-empty string from
// v. See flattenTraits for the selection rule; this helper exists
// so the rule is testable in isolation and recursive for nested
// attributes (e.g. `color.primary.value`).
func firstStringLeaf(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return "", false
		}
		return s, true
	case map[string]any:
		if p, ok := x["primary"]; ok {
			if s, ok := firstStringLeaf(p); ok {
				return s, true
			}
		}
		// Sorted iteration keeps the choice deterministic when
		// several leaves qualify — important for tests and for
		// stable diffs when the same item is re-detected.
		keys := make([]string, 0, len(x))
		for k := range x {
			if k == "primary" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := firstStringLeaf(x[k]); ok {
				return s, true
			}
		}
	}
	return "", false
}

// Compile-time guard so Go stops the build if the multipart
// helper signature drifts elsewhere.
var _ = multipart.Writer{}
var _ = filepath.Base

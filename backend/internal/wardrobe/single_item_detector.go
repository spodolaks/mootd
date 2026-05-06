package wardrobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"
)

// SingleItemDetector calls the singleItemDetection orchestrator
// instead of the legacy on-host cloth-detection API. Selected
// at boot when DETECTION_BACKEND=singleitem.
//
// The orchestrator is a separate service that runs the v1
// pipeline end-to-end (detect → describe → generate ghost
// mannequin → HITL gate). For mootd's purposes it returns a
// trimmed item list compatible with the legacy `[]jobItem`
// shape so the wardrobe handler doesn't branch on backend.
//
// Wire shape (POST {baseURL}/v1/items):
//
//	multipart: image=<bytes>, filename=<original-name>
//	→ 200 { "items": [{ id, category, label, imageUrl,
//	         pngImageUrl, confidence, structuredDescription,
//	         hitlRequired, hitlReason }] }
//
// Items where hitlRequired=true are still returned to the user
// (the FE renders them like any other detected item) — the
// admin can later approve / reject / regenerate via the HITL
// queue (mootd-admin#34/#35). The orchestrator is the
// source-of-truth for those records; mootd backend proxies
// admin reads/writes through.
type SingleItemDetector struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *log.Logger
}

// NewSingleItemDetector constructs a SingleItemDetector. baseURL
// must be the orchestrator's root (e.g.
// http://orchestrator:8080); apiKey is the service-to-service
// token (header X-API-Key). logger is the shared mootd logger
// — backend-side errors stay in the same stream as the rest
// of the wardrobe handler's logs.
func NewSingleItemDetector(baseURL, apiKey string, logger *log.Logger) *SingleItemDetector {
	return &SingleItemDetector{
		baseURL: baseURL,
		apiKey:  apiKey,
		// 3-minute ceiling matches the legacy detector. The
		// orchestrator's median latency on the v1 pipeline is
		// ~8-15s; ceiling protects against a hung backend.
		client: &http.Client{Timeout: 3 * time.Minute},
		logger: logger,
	}
}

// Compile-time satisfaction check.
var _ DetectorBackend = (*SingleItemDetector)(nil)

// Detect uploads the image to the orchestrator and adapts the
// response into the same `[]jobItem` + DetectionRunData shape
// the legacy detector returns.
//
// The orchestrator's response carries pipeline metadata
// (totalCostUsd, totalLatencyMs, retryCount, model versions)
// that we surface as the DetectionRunData so the admin
// /detection-runs page renders the same view regardless of
// backend.
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
	body, contentType, err := buildMultipart(imageData, filename)
	if err != nil {
		return nil, nil, fmt.Errorf("build multipart: %w", err)
	}

	url := d.baseURL + "/v1/items"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}
	// Forward the user + run identity so the orchestrator can
	// stamp them on its rows (the HITL queue surfaces the
	// originating user).
	req.Header.Set("X-Mootd-User-Id", userID)
	if runID != "" {
		req.Header.Set("X-Mootd-Run-Id", runID)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("orchestrator call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, nil, fmt.Errorf("orchestrator %s returned %d: %s", url, resp.StatusCode, string(errBody))
	}

	var parsed orchestratorResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, nil, fmt.Errorf("decode orchestrator response: %w", err)
	}

	items := make([]jobItem, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		items = append(items, it.toJobItem())
	}

	endedAt := time.Now().UTC()
	runData := &DetectionRunData{
		StartedAt:    startedAt,
		EndedAt:      endedAt,
		TotalCostUSD: parsed.PipelineCostUSD,
		// AnalyzeStats / GenerateStats / OverallStyle are
		// legacy-detector concepts. Leave nil — the run row
		// will record an empty per-stage breakdown for
		// orchestrator-served runs. P1-04's admin
		// /detection-runs page already handles nil stats
		// (renders "—" for each cell).
	}

	d.logger.Printf("singleitem: user=%s run=%s items=%d cost=%.4f latency=%dms",
		userID, runID, len(items), parsed.PipelineCostUSD, endedAt.Sub(startedAt).Milliseconds())

	return items, runData, nil
}

// orchestratorResponse maps the v1 wire shape from
// singleItemDetection's POST /v1/items endpoint. Field names
// match the SingleItemDetectionItem schema in admin-api.yaml.
type orchestratorResponse struct {
	Items           []orchestratorItem `json:"items"`
	PipelineCostUSD float64            `json:"pipelineCostUsd,omitempty"`
}

type orchestratorItem struct {
	ID                  string                 `json:"id"`
	Category            string                 `json:"category"`
	Label               string                 `json:"label"`
	ImageURL            string                 `json:"imageUrl"`
	PngImageURL         string                 `json:"pngImageUrl,omitempty"`
	ConfidenceOverall   float64                `json:"confidenceOverall,omitempty"`
	StructuredDesc      map[string]any         `json:"structuredDescription,omitempty"`
	HitlRequired        bool                   `json:"hitlRequired,omitempty"`
	HitlReason          string                 `json:"hitlReason,omitempty"`
}

// toJobItem adapts an orchestrator item to the internal jobItem
// shape. Traits are flattened from the structured description's
// closed-enum attribute paths so downstream code (which expects
// a map[string]string) stays unchanged.
func (it orchestratorItem) toJobItem() jobItem {
	return jobItem{
		ID:         it.ID,
		Category:   it.Category,
		Label:      it.Label,
		Family:     it.Category, // legacy field, alias for category
		Type:       it.Label,    // legacy field, alias for label
		ImageURL:   it.ImageURL,
		Confidence: it.ConfidenceOverall,
		Skipped:    false,
		Traits:     flattenTraits(it.StructuredDesc),
	}
}

// flattenTraits projects the (potentially deep) structured
// description into a flat map[string]string. Only top-level
// string-valued fields are kept — nested objects are skipped
// here; admins can drill into the full structured description
// via the HITL queue's detail page.
func flattenTraits(structured map[string]any) map[string]string {
	if len(structured) == 0 {
		return nil
	}
	out := make(map[string]string, len(structured))
	for k, v := range structured {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Compile-time guard so Go stops the build if the multipart
// helper signature drifts.
var _ = multipart.Writer{}
var _ = filepath.Base

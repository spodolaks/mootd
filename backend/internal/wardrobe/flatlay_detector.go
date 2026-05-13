package wardrobe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// FlatlayDetector calls a simpler one-shot detection service:
//
//	POST <baseURL>/flatlay/single
//	  multipart/form-data with `image` field
//	  X-API-Key: <apiKey>
//
// vs. the singleItemDetection orchestrator's async submit + poll
// dance, this service blocks the request until the full pipeline
// (detect → describe → generate flat-lay PNG → bg-remove) finishes
// (~19s median per the service's own timings_s). One request →
// one response → done.
//
// Selected at boot via DETECTION_BACKEND=flatlay. The service's
// inline base64-encoded PNG output maps onto jobItem.ImageData,
// which the wardrobe handler already saves to GridFS as the
// canonical item image — so downstream code (item display,
// moodboard collage, wardrobe-item routes) doesn't change.
type FlatlayDetector struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *log.Logger
}

// NewFlatlayDetector constructs a FlatlayDetector. apiKey is sent
// via X-API-Key (the service's convention). logger is the shared
// mootd logger. The 90-second HTTP ceiling is generous vs. the
// service's typical 19s round-trip but leaves headroom for slow
// cold-starts on the upstream gpt-4o-mini call.
func NewFlatlayDetector(baseURL, apiKey string, logger *log.Logger) *FlatlayDetector {
	return &FlatlayDetector{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 90 * time.Second},
		logger:  logger,
	}
}

// Compile-time satisfaction check — keep the new backend slotted
// into the DetectorBackend interface the rest of the codebase
// depends on.
var _ DetectorBackend = (*FlatlayDetector)(nil)

// flatlayResponse mirrors the JSON shape the service returns.
// Kept as a private wire type so the public detector surface
// stays narrow.
type flatlayResponse struct {
	Detection flatlayDetection `json:"detection"`
	Image     flatlayImage     `json:"image"`
	TimingsS  flatlayTimings   `json:"timings_s"`
	Tokens    flatlayTokens    `json:"tokens"`
	Config    flatlayConfig    `json:"config"`
}

type flatlayDetection struct {
	Label             string         `json:"label"`
	Category          string         `json:"category"`
	LayerHint         string         `json:"layer_hint"`
	Mode              string         `json:"mode"`
	Traits            map[string]any `json:"traits"`
	RenderDescription string         `json:"render_description"`
}

type flatlayImage struct {
	Format    string `json:"format"`
	Encoding  string `json:"encoding"`
	Data      string `json:"data"` // base64-encoded PNG
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SizeBytes int    `json:"size_bytes"`
}

type flatlayTimings struct {
	Resize        float64 `json:"resize"`
	Detect        float64 `json:"detect"`
	Generate      float64 `json:"generate"`
	BgRemove      float64 `json:"bg_remove"`
	TotalService  float64 `json:"total_service"`
}

type flatlayTokens struct {
	Detect flatlayDetectTokens `json:"detect"`
}

type flatlayDetectTokens struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type flatlayConfig struct {
	DetectModel     string `json:"detect_model"`
	DetectDetail    string `json:"detect_detail"`
	GeminiModel     string `json:"gemini_model"`
	RembgModel      string `json:"rembg_model"`
	MaxInputEdgePx  int    `json:"max_input_edge_px"`
}

// Detect submits the image to the flatlay service and translates
// the synchronous response into the standard (jobItem,
// DetectionRunData) tuple the wardrobe handler expects. The
// returned list always has exactly one element — flatlay/single
// is one-photo-one-garment by contract.
func (d *FlatlayDetector) Detect(
	ctx context.Context,
	userID, runID string,
	imageData []byte,
	filename string,
) ([]jobItem, *DetectionRunData, error) {
	if d.baseURL == "" {
		return nil, nil, fmt.Errorf("flatlay detector: base URL not configured (set FLATLAY_BASE_URL)")
	}
	if d.apiKey == "" {
		return nil, nil, fmt.Errorf("flatlay detector: API key not configured (set FLATLAY_API_KEY)")
	}

	startedAt := time.Now().UTC()

	respBody, err := d.postMultipart(ctx, "/flatlay/single", imageData, filename)
	if err != nil {
		return nil, nil, fmt.Errorf("flatlay detector: %w", err)
	}

	var parsed flatlayResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, nil, fmt.Errorf("flatlay detector: decode response: %w", err)
	}

	// Decode the inline base64 PNG. The wardrobe handler picks up
	// jobItem.ImageData via the same code path the legacy local
	// detector uses, writing it to GridFS as the item's image.
	var pngBytes []byte
	if parsed.Image.Data != "" {
		decoded, decErr := base64.StdEncoding.DecodeString(parsed.Image.Data)
		if decErr != nil {
			return nil, nil, fmt.Errorf("flatlay detector: decode image base64: %w", decErr)
		}
		pngBytes = decoded
	}

	endedAt := time.Now().UTC()

	// Map service traits → flat jobItem.Traits (string-only) so the
	// wardrobe item table picks them up. Array values (pockets,
	// hardware, secondary_colors) are joined with ", " so the trait
	// row stays a single string. Nested objects, if any future
	// service version adds them, are skipped — they live in the
	// preserved StructuredDescription below.
	traits := flattenFlatlayTraits(parsed.Detection.Traits)

	// StructuredDescription preserves the full attribute map plus
	// the prose render_description so admin tooling can surface
	// the rich version (e.g. the archetype-defaults autodetect
	// modal's prefill) without losing data to the flatten.
	structured := make(map[string]any, len(parsed.Detection.Traits)+1)
	for k, v := range parsed.Detection.Traits {
		structured[k] = v
	}
	if parsed.Detection.RenderDescription != "" {
		structured["render_description"] = parsed.Detection.RenderDescription
	}

	it := jobItem{
		ID:                    "flatlay_" + runID,
		Family:                parsed.Detection.Category, // legacy alias
		Type:                  parsed.Detection.Label,    // legacy alias
		Category:              parsed.Detection.Category,
		Label:                 parsed.Detection.Label,
		ImageData:             pngBytes,
		Confidence:            1.0, // service doesn't surface a confidence; treat single-garment success as confident
		Skipped:               false,
		Traits:                traits,
		StructuredDescription: structured,
	}

	runData := &DetectionRunData{
		StartedAt: startedAt,
		EndedAt:   endedAt,
		// Flatlay doesn't expose USD cost in the response. Leave
		// TotalCostUSD at zero; admin /detection-runs will show "—"
		// for cost, same as it does for any backend that doesn't
		// surface per-call dollars.
	}

	d.logger.Printf("flatlay: user=%s run=%s item=%s/%s img=%dx%d total=%.2fs (resize=%.2f detect=%.2f generate=%.2f bgremove=%.2f tokens=%d)",
		userID, runID, parsed.Detection.Category, parsed.Detection.Label,
		parsed.Image.Width, parsed.Image.Height,
		parsed.TimingsS.TotalService, parsed.TimingsS.Resize, parsed.TimingsS.Detect,
		parsed.TimingsS.Generate, parsed.TimingsS.BgRemove,
		parsed.Tokens.Detect.TotalTokens)

	return []jobItem{it}, runData, nil
}

// postMultipart sends the request. Mirrors the singleitem
// detector's helper rather than reusing the legacy Detector's
// pool/failover variant — flatlay is single-URL today; failover
// can be layered on when we run more than one host.
func (d *FlatlayDetector) postMultipart(ctx context.Context, path string, imageData []byte, filename string) ([]byte, error) {
	body, contentType, err := buildMultipart(imageData, filename)
	if err != nil {
		return nil, err
	}
	url := d.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-API-Key", d.apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flatlay %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read flatlay response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Truncate body to keep logs sane — the service returns
		// JSON error envelopes typically <1KB but a stray HTML
		// 502 page would otherwise spam the line.
		preview := string(respBody)
		if len(preview) > 500 {
			preview = preview[:500] + "…"
		}
		return nil, fmt.Errorf("flatlay %s returned %d: %s", path, resp.StatusCode, preview)
	}
	return respBody, nil
}

// flattenFlatlayTraits projects the (potentially mixed-type) trait
// map the service returns into a flat map[string]string. Strings
// pass through, arrays get joined with ", " (the wardrobe item
// table uses single-string trait values). Booleans + numbers are
// stringified — rare in the current service contract, but the
// fallback prevents data loss when a future schema version adds
// them.
//
// nil and empty-string values are dropped so we don't pollute the
// wardrobe row with empty trait columns.
func flattenFlatlayTraits(raw map[string]any) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			if val == "" {
				continue
			}
			out[k] = val
		case []any:
			parts := make([]string, 0, len(val))
			for _, e := range val {
				if s, ok := e.(string); ok && s != "" {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				out[k] = strings.Join(parts, ", ")
			}
		case bool:
			out[k] = fmt.Sprintf("%t", val)
		case float64: // JSON numbers decode as float64
			out[k] = fmt.Sprintf("%v", val)
		}
		// other types (nested maps, nil) intentionally dropped —
		// the full original is preserved in StructuredDescription.
	}
	return out
}

package wardrobe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"mootd/backend/internal/shared/endpoints"
)

// Detector submits images to the clothing-detection service and returns
// detected items. The local service (Cloth Detection API) responds
// synchronously — no polling required.
//
// recorder is an optional dependency that lets the detector emit
// observability rows for each upstream LLM call the detection service
// reports under `stats`. nil disables the emission (test setups, dev
// opt-out). Wired identically to the outfit service's recorder hook.
//
// pool (mootd#56) wraps a comma-separated DETECTION_API_BASE_URL into
// a round-robin upstream rotation. Single-URL configs degenerate to
// "always pick that one URL" — no behaviour change. Multi-URL configs
// rotate per request and skip the failed URL on 5xx retry.
type Detector struct {
	pool     *endpoints.Pool
	apiKey   string
	client   *http.Client
	logger   *log.Logger
	recorder LLMRecorder // optional
}

// NewDetector creates a Detector. baseURL accepts a single URL or a
// comma-separated list (mootd#56).
func NewDetector(baseURL, apiKey string, logger *log.Logger) *Detector {
	return &Detector{
		pool:   endpoints.NewPool(baseURL),
		apiKey: apiKey,
		client: &http.Client{Timeout: 3 * time.Minute},
		logger: logger,
	}
}

// WithRecorder returns the Detector wired with an LLMRecorder. Used
// by app.go to thread observability into detection calls without
// changing the constructor signature (which is shared with tests).
func (d *Detector) WithRecorder(r LLMRecorder) *Detector {
	d.recorder = r
	return d
}

// LLMRecorder is the narrow interface the detector needs from
// observability.LLMRecorder. Defined here so the wardrobe package
// doesn't import observability directly — the dependency is injected
// at app.go via a thin adapter.
type LLMRecorder interface {
	Record(ctx context.Context, cc DetectorRecorderContext, obs DetectorRecorderObservation)
}

// DetectorRecorderContext mirrors observability.CallContext for the
// fields the detection path supplies.
type DetectorRecorderContext struct {
	UserID         string
	Feature        string // "detection_analyze" | "detection_generate"
	DetectionRunID string // links the row to /admin/v1/detection-runs/{id}
}

// DetectorRecorderObservation mirrors observability.CallObservation.
type DetectorRecorderObservation struct {
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	StartedAt    time.Time
	EndedAt      time.Time
	Err          error
}

// --- Local Cloth Detection API response types (POST /api/v1/generate) ---

type generateResponse struct {
	GeneratedImages []generatedImage `json:"generated_images"`
	OverallStyle    string           `json:"overall_style"`
	Stats           *detectionStats  `json:"stats,omitempty"`
}

// DetectionStats captures the per-request cost / timing breakdown the
// detection service returns under `stats`. Populated as of the
// 2026-04 service update; absent on older deployments.
//
// Each modelStats block represents a single billable LLM call the
// service made on our behalf (Claude for trait analysis,
// gpt-image-1 for the per-item image generation). The wardrobe layer
// uses these to write llm_calls rows so detection costs surface in
// /admin/v1/traces alongside outfit-generation costs.
type detectionStats struct {
	Claude           *modelStats       `json:"claude,omitempty"`
	OpenAIImages     *modelStats       `json:"openai_images,omitempty"`
	LocalModels      []localModelStats `json:"local_models,omitempty"`
	TotalWallSeconds float64           `json:"total_wall_seconds,omitempty"`
	ModelsUsed       []string          `json:"models_used,omitempty"`
}

type modelStats struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	Model        string  `json:"model"`
	CostUSD      float64 `json:"cost_usd,omitempty"` // 2026-04 update on openai_images
}

type localModelStats struct {
	ModelName   string  `json:"model_name"`
	CPUSeconds  float64 `json:"cpu_seconds"`
	WallSeconds float64 `json:"wall_seconds"`
}

type generatedImage struct {
	ItemType      string   `json:"item_type"`
	Category      string   `json:"category"`
	PromptUsed    *string  `json:"prompt_used"`
	RevisedPrompt *string  `json:"revised_prompt"`
	ImageB64      *string  `json:"image_b64"`
	Error         *string  `json:"error"`
	CostUSD       *float64 `json:"cost_usd"` // per-image cost from gpt-image-1 (2026-04 update)
}

// --- Local Cloth Detection API response types (POST /api/v1/analyze) ---

type analyzeResponse struct {
	Items             []analyzeItem   `json:"items"`
	Accessories       []analyzeItem   `json:"accessories"`
	OverallStyle      string          `json:"overall_style"`
	SegmentationLabel []string        `json:"segmentation_labels"`
	Stats             *detectionStats `json:"stats,omitempty"`
}

type analyzeItem struct {
	ItemType            string       `json:"item_type"`
	Category            string       `json:"category"`
	Color               colorInfo    `json:"color"`
	Fabric              *string      `json:"fabric"`
	Style               *string      `json:"style"`
	Fit                 *string      `json:"fit"`
	Pattern             *patternInfo `json:"pattern"`
	Brand               brandInfo    `json:"brand"`
	Details             []string     `json:"details"`
	GraphicsDescription *string      `json:"graphics_description"`
	Occasion            []string     `json:"occasion"`
	VisibleArea         string       `json:"visible_area"`
}

type colorInfo struct {
	Primary   string  `json:"primary"`
	Secondary *string `json:"secondary"`
	Accent    *string `json:"accent"`
}

type patternInfo struct {
	Type        string  `json:"type"`
	Description *string `json:"description"`
}

type brandInfo struct {
	Detected   bool     `json:"detected"`
	Name       *string  `json:"name"`
	Confidence *float64 `json:"confidence"`
}

// jobItem is the internal representation used by the Detect handler.
// Both the local and remote detector produce this shape.
type jobItem struct {
	ID         string            `json:"id"`
	Family     string            `json:"family"`
	Type       string            `json:"type"`
	ImageURL   string            `json:"image_url"`
	ImageData  []byte            `json:"-"` // inline base64-decoded image (local API)
	Confidence float64           `json:"confidence"`
	Skipped    bool              `json:"skipped"`
	Traits     map[string]string `json:"traits"`
	Category   string            `json:"category"`
	Label      string            `json:"label"`
	// StructuredDescription is the rich (potentially nested)
	// per-attribute description produced by the orchestrator's
	// stage 2 — e.g. {"color": "indigo", "fit": "slim",
	// "material_top": "cotton"}. Lossy compared to the wardrobe
	// row's flat traits, but useful for admin tools that want to
	// surface the full LLM-derived description (e.g. the
	// archetype-defaults curator's auto-detect prefill). nil for
	// the legacy detector — only the singleitem backend populates it.
	StructuredDescription map[string]any `json:"-"`
	// PromptUsed + GenerateCostUSD bubble up the per-item generation
	// metadata so the wardrobe handler can stamp them onto the
	// detection_run archive (P1-04 / mootd-admin#16). Empty when the
	// item came from /analyze only (no generated image).
	PromptUsed      string  `json:"-"`
	GenerateCostUSD float64 `json:"-"`
}

// DetectionRunData is the per-call archive produced by Detect.
// Contains everything the wardrobe handler needs to write a
// detection_runs row: full stats from both upstream calls, per-item
// generation metadata, and the timing window. The handler owns
// persistence; the detector just produces the data.
type DetectionRunData struct {
	AnalyzeStats  *detectionStats
	GenerateStats *detectionStats
	OverallStyle  string
	StartedAt     time.Time
	EndedAt       time.Time
	TotalCostUSD  float64
}

// Detect submits an image to the local detection service.
// It calls /api/v1/analyze for traits and /api/v1/generate for per-item images,
// then merges the results into jobItems the handler can process.
//
// userID is required for the observability emission (so detection
// rows in /admin/v1/traces attribute to the right user). Pass "" to
// skip recorder emission entirely.
//
// runID stamps each emitted llm_calls row with the parent
// detection_run id so the admin UI can navigate from a /traces row
// to /admin/v1/detection-runs/{id}. Caller mints this upfront so
// it can persist the run row alongside the input image atomically.
func (d *Detector) Detect(ctx context.Context, userID, runID string, imageData []byte, filename string) ([]jobItem, *DetectionRunData, error) {
	overallStart := time.Now().UTC()
	// Run analyze and generate in parallel.
	type analyzeResult struct {
		resp      *analyzeResponse
		err       error
		startedAt time.Time
		endedAt   time.Time
	}
	type generateResult struct {
		resp      *generateResponse
		err       error
		startedAt time.Time
		endedAt   time.Time
	}

	analyzeCh := make(chan analyzeResult, 1)
	generateCh := make(chan generateResult, 1)

	go func() {
		startedAt := time.Now().UTC()
		defer func() {
			if r := recover(); r != nil {
				d.logger.Printf("wardrobe: detector: analyze panic: %v", r)
				analyzeCh <- analyzeResult{nil, fmt.Errorf("analyze panic: %v", r), startedAt, time.Now().UTC()}
			}
		}()
		resp, err := d.callAnalyze(ctx, imageData, filename)
		analyzeCh <- analyzeResult{resp, err, startedAt, time.Now().UTC()}
	}()
	go func() {
		startedAt := time.Now().UTC()
		defer func() {
			if r := recover(); r != nil {
				d.logger.Printf("wardrobe: detector: generate panic: %v", r)
				generateCh <- generateResult{nil, fmt.Errorf("generate panic: %v", r), startedAt, time.Now().UTC()}
			}
		}()
		resp, err := d.callGenerate(ctx, imageData, filename)
		generateCh <- generateResult{resp, err, startedAt, time.Now().UTC()}
	}()

	aResult := <-analyzeCh
	gResult := <-generateCh

	if aResult.err != nil {
		d.logger.Printf("wardrobe: detector: analyze failed: %v", aResult.err)
	}
	if gResult.err != nil {
		d.logger.Printf("wardrobe: detector: generate failed: %v", gResult.err)
	}

	// Emit observability rows for each upstream LLM the detection
	// service reported under `stats`. Best-effort — the recorder
	// itself swallows write failures, so a Mongo blip never fails
	// the user-facing detection.
	d.recordStats(ctx, userID, runID, "detection_analyze",
		statsOf(aResult.resp), aResult.err, aResult.startedAt, aResult.endedAt)
	d.recordStats(ctx, userID, runID, "detection_generate",
		statsOf(gResult.resp), gResult.err, gResult.startedAt, gResult.endedAt)

	if aResult.err != nil && gResult.err != nil {
		return nil, nil, fmt.Errorf("analyze: %w; generate: %w", aResult.err, gResult.err)
	}

	// Index generated images by category for fuzzy matching.
	type genEntry struct {
		g     generatedImage
		taken bool
	}
	var genByCat map[string][]*genEntry
	if gResult.resp != nil {
		genByCat = make(map[string][]*genEntry, len(gResult.resp.GeneratedImages))
		for _, g := range gResult.resp.GeneratedImages {
			if g.Error != nil {
				continue
			}
			cat := strings.ToLower(g.Category)
			genByCat[cat] = append(genByCat[cat], &genEntry{g: g})
		}
		d.logger.Printf("wardrobe: detector: generate returned %d images", len(gResult.resp.GeneratedImages))
	}

	// Build the item list from analyze results (has rich traits).
	items := []jobItem{}

	if aResult.resp != nil {
		allAnalyzed := append(aResult.resp.Items, aResult.resp.Accessories...)
		d.logger.Printf("wardrobe: detector: analyze returned %d items + %d accessories",
			len(aResult.resp.Items), len(aResult.resp.Accessories))

		for _, a := range allAnalyzed {
			traits := buildTraits(a, aResult.resp.OverallStyle)
			item := jobItem{
				Family:     a.Category,
				Type:       a.ItemType,
				Confidence: 1.0,
				Traits:     traits,
			}

			// Match generated image: first try exact item_type+category,
			// then fall back to same category (first untaken entry).
			if genByCat != nil {
				cat := strings.ToLower(a.Category)
				matched := false

				// Exact match
				for _, e := range genByCat[cat] {
					if !e.taken && strings.EqualFold(e.g.ItemType, a.ItemType) && e.g.ImageB64 != nil {
						decoded, err := base64.StdEncoding.DecodeString(*e.g.ImageB64)
						if err == nil {
							item.ImageData = decoded
						}
						copyGenerationMeta(&item, e.g)
						e.taken = true
						matched = true
						break
					}
				}
				// Fuzzy: same category, first available
				if !matched {
					for _, e := range genByCat[cat] {
						if !e.taken && e.g.ImageB64 != nil {
							decoded, err := base64.StdEncoding.DecodeString(*e.g.ImageB64)
							if err == nil {
								item.ImageData = decoded
							}
							copyGenerationMeta(&item, e.g)
							e.taken = true
							break
						}
					}
				}
			}

			items = append(items, item)
		}
	} else if gResult.resp != nil {
		// Analyze failed but generate succeeded — use generate data only.
		for _, g := range gResult.resp.GeneratedImages {
			if g.Error != nil {
				continue
			}
			item := jobItem{
				Family: g.Category,
				Type:   g.ItemType,
				Traits: map[string]string{},
			}
			if g.ImageB64 != nil {
				decoded, err := base64.StdEncoding.DecodeString(*g.ImageB64)
				if err == nil {
					item.ImageData = decoded
				}
			}
			copyGenerationMeta(&item, g)
			items = append(items, item)
		}
	}

	// Build the per-call run archive. Total cost rolls up Claude
	// (analyze + generate) + per-image generation; the API exposes
	// `cost_usd` on stats.openai_images, which already sums the
	// per-item costs.
	var totalCost float64
	// Claude cost is computed downstream via model_prices (at the
	// recorder), not summed here — today only gpt-image-1 self-reports
	// cost_usd.
	if gResult.resp != nil && gResult.resp.Stats != nil {
		if gResult.resp.Stats.OpenAIImages != nil {
			totalCost += gResult.resp.Stats.OpenAIImages.CostUSD
		}
	}
	overallStyle := ""
	if aResult.resp != nil {
		overallStyle = aResult.resp.OverallStyle
	} else if gResult.resp != nil {
		overallStyle = gResult.resp.OverallStyle
	}
	run := &DetectionRunData{
		AnalyzeStats:  statsOf(aResult.resp),
		GenerateStats: statsOf(gResult.resp),
		OverallStyle:  overallStyle,
		StartedAt:     overallStart,
		EndedAt:       time.Now().UTC(),
		TotalCostUSD:  totalCost,
	}

	return items, run, nil
}

// copyGenerationMeta lifts per-image fields from the detection
// service's `generated_images` entry onto the corresponding jobItem
// so the wardrobe handler can persist them on the detection_run row
// (P1-04). Cheap field copies; called once per matched item.
func copyGenerationMeta(item *jobItem, g generatedImage) {
	if g.PromptUsed != nil {
		item.PromptUsed = *g.PromptUsed
	}
	if g.CostUSD != nil {
		item.GenerateCostUSD = *g.CostUSD
	}
}

// callAnalyze calls POST /api/v1/analyze and returns the parsed response.
func (d *Detector) callAnalyze(ctx context.Context, imageData []byte, filename string) (*analyzeResponse, error) {
	body, err := d.postMultipart(ctx, "/api/v1/analyze", imageData, filename)
	if err != nil {
		return nil, err
	}
	var resp analyzeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode analyze response: %w", err)
	}
	return &resp, nil
}

// callGenerate calls POST /api/v1/generate and returns the parsed response.
func (d *Detector) callGenerate(ctx context.Context, imageData []byte, filename string) (*generateResponse, error) {
	body, err := d.postMultipart(ctx, "/api/v1/generate", imageData, filename)
	if err != nil {
		return nil, err
	}
	var resp generateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode generate response: %w", err)
	}
	return &resp, nil
}

// postMultipart sends a multipart POST with the image to the given path.
//
// Failover (mootd#56): when DETECTION_API_BASE_URL is a comma-separated
// list, the first attempt picks the next URL via round-robin. On
// network/5xx failure, one fallback attempt against a different URL
// fires before surfacing the error. 4xx responses (caller errors —
// missing API key, bad image format, etc.) skip the failover since
// they'd recur on every host.
func (d *Detector) postMultipart(ctx context.Context, path string, imageData []byte, filename string) ([]byte, error) {
	body, contentType, err := buildMultipart(imageData, filename)
	if err != nil {
		return nil, err
	}

	tryOnce := func(url string) (int, []byte, error) {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url+path, bytes.NewReader(body))
		if reqErr != nil {
			return 0, nil, reqErr
		}
		req.Header.Set("Content-Type", contentType)
		if d.apiKey != "" {
			req.Header.Set("X-API-Key", d.apiKey)
		}
		resp, doErr := d.client.Do(req)
		if doErr != nil {
			return 0, nil, doErr
		}
		defer resp.Body.Close()
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return resp.StatusCode, nil, fmt.Errorf("read response from %s: %w", path, readErr)
		}
		return resp.StatusCode, respBody, nil
	}

	url := d.pool.Next()
	status, respBody, err := tryOnce(url)
	// Failover gate: only retry against another host when this
	// host is broken (network error or 5xx). 4xx is caller-side
	// — the next host would 4xx the same way.
	if (err != nil || status >= 500) && d.pool.Size() > 1 {
		fb := d.pool.Fallback(url)
		if fb != url {
			d.logger.Printf("detector: %s failed (status=%d, err=%v); failing over to %s", url, status, err, fb)
			status, respBody, err = tryOnce(fb)
		}
	}
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("detection service %s returned %d: %s", path, status, string(respBody))
	}
	return respBody, nil
}

// buildMultipart prepares the request body once so retries
// against a fallback URL can replay the same bytes (a streamed
// multipart writer can't be rewound).
func buildMultipart(imageData []byte, filename string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := createFormFileWithMIME(mw, "image", filepath.Base(filename))
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, "", err
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// createFormFileWithMIME is like multipart.Writer.CreateFormFile but sets the
// Content-Type based on the file extension instead of application/octet-stream.
func createFormFileWithMIME(w *multipart.Writer, fieldname, filename string) (io.Writer, error) {
	ct := mime.TypeByExtension(filepath.Ext(filename))
	if ct == "" {
		ct = "image/jpeg" // safe default for photos
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldname, filename))
	h.Set("Content-Type", ct)
	return w.CreatePart(h)
}

// statsOf is a tiny helper that lets callers pull *detectionStats
// off either response shape uniformly. Returns nil when the response
// itself is nil OR the stats block is absent (older detection-service
// builds).
func statsOf(resp interface{}) *detectionStats {
	switch r := resp.(type) {
	case *analyzeResponse:
		if r == nil {
			return nil
		}
		return r.Stats
	case *generateResponse:
		if r == nil {
			return nil
		}
		return r.Stats
	default:
		return nil
	}
}

// recordStats emits one llm_calls row per paid LLM the detection
// service used. Local-model timing (yolos / segformer) is not in the
// LLM ledger — those models are CPU-billed, not token-billed; if we
// want them tracked separately a sibling `compute_costs` ledger is
// the right shape, not this one. Best-effort: skipped silently when
// no recorder is wired or stats is nil. The transport-level error
// rides on the *first* row we emit so admins can see "the call
// failed at this provider"; subsequent rows record success.
//
// userID == "" disables emission entirely (test setups).
func (d *Detector) recordStats(ctx context.Context, userID, runID, feature string, stats *detectionStats, err error, startedAt, endedAt time.Time) {
	if d.recorder == nil || userID == "" || stats == nil {
		return
	}
	cc := DetectorRecorderContext{UserID: userID, Feature: feature, DetectionRunID: runID}
	// Claude: trait analysis (always present when /analyze or
	// /generate succeeded — both call the same Claude pipeline).
	if stats.Claude != nil && stats.Claude.Model != "" {
		d.recorder.Record(ctx, cc, DetectorRecorderObservation{
			Provider:     "anthropic",
			Model:        stats.Claude.Model,
			InputTokens:  stats.Claude.InputTokens,
			OutputTokens: stats.Claude.OutputTokens,
			StartedAt:    startedAt,
			EndedAt:      endedAt,
			Err:          err,
		})
	}
	// gpt-image-1: per-item image generation (only on /generate).
	if stats.OpenAIImages != nil && stats.OpenAIImages.Model != "" {
		d.recorder.Record(ctx, cc, DetectorRecorderObservation{
			Provider:     "openai",
			Model:        stats.OpenAIImages.Model,
			InputTokens:  stats.OpenAIImages.InputTokens,
			OutputTokens: stats.OpenAIImages.OutputTokens,
			StartedAt:    startedAt,
			EndedAt:      endedAt,
			// Don't double-attribute the transport error to the
			// second row — the err parameter applies to the upstream
			// HTTP call, which already bubbled into the first emission.
			Err: nil,
		})
	}
}

// buildTraits converts the rich analyze response into a flat traits map,
// preserving all available information from the detection service.
func buildTraits(a analyzeItem, overallStyle string) map[string]string {
	traits := map[string]string{
		"macro_category": a.Category,
		"color":          a.Color.Primary,
	}
	if a.Color.Secondary != nil {
		traits["color_secondary"] = *a.Color.Secondary
	}
	if a.Color.Accent != nil {
		traits["color_accent"] = *a.Color.Accent
	}
	if a.Fabric != nil {
		traits["fabric"] = *a.Fabric
	}
	if a.Style != nil {
		traits["style"] = *a.Style
	}
	if a.Fit != nil {
		traits["fit"] = *a.Fit
	}
	if a.Pattern != nil {
		traits["pattern"] = a.Pattern.Type
		if a.Pattern.Description != nil {
			traits["pattern_description"] = *a.Pattern.Description
		}
	}
	if a.Brand.Detected && a.Brand.Name != nil {
		traits["brand"] = *a.Brand.Name
	}
	if a.GraphicsDescription != nil {
		traits["graphics_description"] = *a.GraphicsDescription
	}
	if a.VisibleArea != "" {
		traits["visible_area"] = a.VisibleArea
	}
	if len(a.Details) > 0 {
		traits["details"] = strings.Join(a.Details, "; ")
	}
	if len(a.Occasion) > 0 {
		traits["occasion"] = strings.Join(a.Occasion, ", ")
	}
	if overallStyle != "" {
		traits["overall_style"] = overallStyle
	}
	return traits
}

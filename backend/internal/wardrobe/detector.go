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
)

// Detector submits images to the clothing-detection service and returns
// detected items. The local service (Cloth Detection API) responds
// synchronously — no polling required.
type Detector struct {
	baseURL string
	apiKey  string
	client  *http.Client
	logger  *log.Logger
}

// NewDetector creates a Detector.
func NewDetector(baseURL, apiKey string, logger *log.Logger) *Detector {
	return &Detector{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 3 * time.Minute},
		logger:  logger,
	}
}

// --- Local Cloth Detection API response types (POST /api/v1/generate) ---

type generateResponse struct {
	GeneratedImages []generatedImage `json:"generated_images"`
	OverallStyle    string           `json:"overall_style"`
}

type generatedImage struct {
	ItemType      string  `json:"item_type"`
	Category      string  `json:"category"`
	PromptUsed    *string `json:"prompt_used"`
	RevisedPrompt *string `json:"revised_prompt"`
	ImageB64      *string `json:"image_b64"`
	Error         *string `json:"error"`
}

// --- Local Cloth Detection API response types (POST /api/v1/analyze) ---

type analyzeResponse struct {
	Items             []analyzeItem `json:"items"`
	Accessories       []analyzeItem `json:"accessories"`
	OverallStyle      string        `json:"overall_style"`
	SegmentationLabel []string      `json:"segmentation_labels"`
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
	Detected   bool    `json:"detected"`
	Name       *string `json:"name"`
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
}

// Detect submits an image to the local detection service.
// It calls /api/v1/analyze for traits and /api/v1/generate for per-item images,
// then merges the results into jobItems the handler can process.
func (d *Detector) Detect(ctx context.Context, imageData []byte, filename string) ([]jobItem, error) {
	// Run analyze and generate in parallel.
	type analyzeResult struct {
		resp *analyzeResponse
		err  error
	}
	type generateResult struct {
		resp *generateResponse
		err  error
	}

	analyzeCh := make(chan analyzeResult, 1)
	generateCh := make(chan generateResult, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				d.logger.Printf("wardrobe: detector: analyze panic: %v", r)
				analyzeCh <- analyzeResult{nil, fmt.Errorf("analyze panic: %v", r)}
			}
		}()
		resp, err := d.callAnalyze(ctx, imageData, filename)
		analyzeCh <- analyzeResult{resp, err}
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				d.logger.Printf("wardrobe: detector: generate panic: %v", r)
				generateCh <- generateResult{nil, fmt.Errorf("generate panic: %v", r)}
			}
		}()
		resp, err := d.callGenerate(ctx, imageData, filename)
		generateCh <- generateResult{resp, err}
	}()

	aResult := <-analyzeCh
	gResult := <-generateCh

	if aResult.err != nil {
		d.logger.Printf("wardrobe: detector: analyze failed: %v", aResult.err)
	}
	if gResult.err != nil {
		d.logger.Printf("wardrobe: detector: generate failed: %v", gResult.err)
	}
	if aResult.err != nil && gResult.err != nil {
		return nil, fmt.Errorf("analyze: %w; generate: %w", aResult.err, gResult.err)
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
			items = append(items, item)
		}
	}

	return items, nil
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
func (d *Detector) postMultipart(ctx context.Context, path string, imageData []byte, filename string) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, err := createFormFileWithMIME(mw, "image", filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, err
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if d.apiKey != "" {
		req.Header.Set("X-API-Key", d.apiKey)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("detection service %s returned %d: %s", path, resp.StatusCode, string(respBody))
	}
	return respBody, nil
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

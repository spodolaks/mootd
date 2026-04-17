package wardrobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

const searchMaxResults = 12

// Searcher calls the external clothing-search endpoint (POST /search).
type Searcher struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewSearcher creates a Searcher using the same base URL and API key as the Detector.
func NewSearcher(baseURL, apiKey string) *Searcher {
	return &Searcher{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// searchServiceResponse is the actual shape returned by POST /search.
// { "found": bool, "products": [ { "image_url", "title", "source", "price" } ], ... }
type searchServiceResponse struct {
	Products []struct {
		ImageURL string `json:"image_url"`
		Title    string `json:"title"`
		Source   string `json:"source"`
		Price    string `json:"price"`
	} `json:"products"`
}

// Search submits an image + brand to the external search service and returns matching products.
func (s *Searcher) Search(ctx context.Context, imageData []byte, contentType, brand string) ([]SearchProduct, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	ext := "jpg"
	if contentType == "image/png" {
		ext = "png"
	}
	part, err := mw.CreateFormFile("image", "item."+ext)
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, err
	}
	if err = mw.WriteField("brand", brand); err != nil {
		return nil, err
	}
	if err = mw.WriteField("mode", "programmatic"); err != nil {
		return nil, err
	}
	if err = mw.WriteField("max_results", strconv.Itoa(searchMaxResults)); err != nil {
		return nil, err
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/search", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-API-Key", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search service returned %d: %s", resp.StatusCode, string(body))
	}

	var result searchServiceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	products := make([]SearchProduct, len(result.Products))
	for i, p := range result.Products {
		products[i] = SearchProduct{
			ID:       strconv.Itoa(i),
			Title:    p.Title,
			Source:   p.Source,
			Price:    p.Price,
			ImageURL: p.ImageURL,
		}
	}
	return products, nil
}

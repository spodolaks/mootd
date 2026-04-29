package wardrobe

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// BackgroundRemover calls an external service to remove image backgrounds.
//
// http.Post (used by an earlier version) goes through http.DefaultClient
// which has no Timeout — a hung rembg service silently held goroutines
// for the full request duration. Bounded client guards the worker pool.
type BackgroundRemover struct {
	baseURL string
	client  *http.Client
}

// bgRemoverTimeout caps the round-trip to rembg. Real-world bg-remove
// of a single 1024×1024 photo takes ~3-8s on the local CPU service;
// 60s is generous headroom without leaving the worker hung on a
// silent failure.
const bgRemoverTimeout = 60 * time.Second

// NewBackgroundRemover creates a BackgroundRemover pointing at baseURL.
func NewBackgroundRemover(baseURL string) *BackgroundRemover {
	return &BackgroundRemover{
		baseURL: baseURL,
		client:  &http.Client{Timeout: bgRemoverTimeout},
	}
}

// RemoveBackground sends imageData to the service and returns the PNG result.
// filename is used as the multipart filename (e.g. "item.jpg").
func (b *BackgroundRemover) RemoveBackground(imageData []byte, filename string) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	part, err := mw.CreateFormFile("files", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(imageData); err != nil {
		return nil, fmt.Errorf("write image data: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	resp, err := b.client.Post(b.baseURL+"/remove-background", mw.FormDataContentType(), &body) //nolint:gosec // URL from trusted config
	if err != nil {
		return nil, fmt.Errorf("post to bg remover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("bg remover returned %d: %s", resp.StatusCode, string(errBody))
	}

	png, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize))
	if err != nil {
		return nil, fmt.Errorf("read bg remover response: %w", err)
	}
	return png, nil
}

package wardrobe

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// BackgroundRemover calls an external service to remove image backgrounds.
type BackgroundRemover struct {
	baseURL string
}

// NewBackgroundRemover creates a BackgroundRemover pointing at baseURL.
func NewBackgroundRemover(baseURL string) *BackgroundRemover {
	return &BackgroundRemover{baseURL: baseURL}
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

	resp, err := http.Post(b.baseURL+"/remove-background", mw.FormDataContentType(), &body) //nolint:gosec // URL from trusted config
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

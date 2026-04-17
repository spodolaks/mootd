// Package response provides shared HTTP response helpers used across all domain handlers.
package response

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// WriteJSON encodes payload as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.Printf("response: write JSON body failed (status=%d, %d bytes): %v", status, len(body), err)
	}
}

// DecodeJSONBody decodes a JSON request body into dst.
// It disallows unknown fields, rejects empty bodies, and requires exactly one JSON object.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	const maxBodyBytes = 1 << 20

	limitedBody := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer limitedBody.Close()

	dec := json.NewDecoder(limitedBody)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty request body")
		}
		return errors.New("invalid json body")
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}

	return nil
}

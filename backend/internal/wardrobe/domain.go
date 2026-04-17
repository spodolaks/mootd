// Package wardrobe owns clothing item detection, storage, and retrieval.
package wardrobe

import "time"

// ClothingItem is a persisted garment in a user's wardrobe.
type ClothingItem struct {
	ID          string            `bson:"_id"          json:"id"`
	UserID      string            `bson:"userId"       json:"userId"`
	Category    string            `bson:"category"     json:"category"`
	Label       string            `bson:"label"        json:"label"`
	ImageURL    string            `bson:"imageUrl"     json:"imageUrl"`
	PngImageURL string            `bson:"pngImageUrl"  json:"pngImageUrl,omitempty"`
	Traits      map[string]string `bson:"traits"       json:"traits"`
	CreatedAt   time.Time         `bson:"createdAt"    json:"createdAt"`
}

// DetectedItem is the client-facing representation of one detected garment
// returned immediately after detection (before the user reviews traits).
type DetectedItem struct {
	ID          string            `json:"id"`
	Category    string            `json:"category"`
	Label       string            `json:"label"`
	ImageURL    string            `json:"imageUrl,omitempty"`
	PngImageURL string            `json:"pngImageUrl,omitempty"`
	Confidence  float64           `json:"confidence,omitempty"`
	Traits      map[string]string `json:"traits,omitempty"`
}

// DetectResponse is returned to the client from POST /v1/wardrobe/detect.
type DetectResponse struct {
	Items []DetectedItem `json:"items"`
}

// SearchProduct is one result from the clothing search service.
// Fields match the actual service response: { image_url, title, source, price (formatted string) }.
type SearchProduct struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Source   string `json:"source"`
	Price    string `json:"price,omitempty"`
	ImageURL string `json:"imageUrl"`
}

// SearchResponse is returned to the client from POST /v1/wardrobe/items/{id}/search.
type SearchResponse struct {
	Results []SearchProduct `json:"results"`
}

// Package brands owns the global brand dictionary used for autocomplete.
// Brands are shared across all users — no ownership or user scoping.
package brands

import "time"

// Brand is a clothing brand stored in the global dictionary.
type Brand struct {
	// ID is the lowercase-normalised name — used for deduplication.
	ID          string    `bson:"_id"         json:"-"`
	DisplayName string    `bson:"displayName" json:"name"`
	CreatedAt   time.Time `bson:"createdAt"   json:"createdAt"`
}

// SaveBrandRequest is the body for POST /v1/brands.
type SaveBrandRequest struct {
	Name string `json:"name"`
}

// SearchResponse is returned from GET /v1/brands.
type SearchResponse struct {
	Brands []string `json:"brands"`
}

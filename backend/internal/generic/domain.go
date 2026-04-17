// Package generic manages shared AI-generated clothing items used to fill
// wardrobes that are too small for varied outfit generation.
package generic

import "time"

// GenericItem is a shared, AI-generated clothing item in the global pool.
// It is not owned by any user — items are matched by archetype affinity.
type GenericItem struct {
	ID               string             `bson:"_id"              json:"id"`
	Category         string             `bson:"category"         json:"category"`
	Label            string             `bson:"label"            json:"label"`
	Description      string             `bson:"description"      json:"description"`
	ImageURL         string             `bson:"imageUrl"         json:"imageUrl"`
	PngImageURL      string             `bson:"pngImageUrl"      json:"pngImageUrl,omitempty"`
	Traits           map[string]string  `bson:"traits"           json:"traits"`
	ArchetypeScores  map[string]float64 `bson:"archetypeScores"  json:"archetypeScores"`
	PrimaryArchetype string             `bson:"primaryArchetype" json:"primaryArchetype"`
	DedupKey         string             `bson:"dedupKey"         json:"dedupKey"`
	UsageCount       int                `bson:"usageCount"       json:"usageCount"`
	CreatedAt        time.Time          `bson:"createdAt"        json:"createdAt"`
}

// PredictedItem is a predicted wardrobe item the user likely needs.
type PredictedItem struct {
	Category        string            // "outerwear", "top", "bottom", "footwear", "accessory"
	Label           string            // e.g. "structured wool overcoat"
	SourceArchetype string            // which archetype suggested this
	Priority        int               // 1=critical slot missing, 2=variety needed, 3=nice-to-have
	Traits          map[string]string // pre-populated from archetype suggestion
}

// ListResponse is returned from GET /v1/generic/items.
type ListResponse struct {
	Items []GenericItem `json:"items"`
}

// MinWardrobeSize is the threshold below which generic items are injected.
const MinWardrobeSize = 8

// MaxGenericItems is the maximum number of generic items to generate per user prediction.
const MaxGenericItems = 6

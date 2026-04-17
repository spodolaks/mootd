// Package surface owns the panel and background textures the moodboard
// flat-lay is rendered on. Surfaces are stored as documents in MongoDB
// alongside GridFS-hosted image bytes, and chosen per-outfit by the LLM
// during outfit generation based on their descriptions and mood tags.
package surface

import "time"

// Kind discriminates the two surface roles on a moodboard card.
type Kind string

const (
	// KindPanel — the textured surface garments sit on (wood, marble, paper).
	KindPanel Kind = "panel"
	// KindBackground — the ambient environment the panel sits in (bokeh, gradient).
	KindBackground Kind = "background"
)

// Surface is a reusable visual asset that can back a moodboard card. The
// metadata fields drive LLM-based selection so the style of the surface
// matches the outfit's mood and archetype.
type Surface struct {
	ID                string             `bson:"_id"                json:"id"`
	Kind              Kind               `bson:"kind"               json:"kind"`
	Name              string             `bson:"name"               json:"name"`
	Description       string             `bson:"description"        json:"description,omitempty"`
	MoodTags          []string           `bson:"moodTags"           json:"moodTags,omitempty"`
	ColorPalette      []string           `bson:"colorPalette"       json:"colorPalette,omitempty"`      // auto-sampled hex strings
	ArchetypeAffinity map[string]float64 `bson:"archetypeAffinity"  json:"archetypeAffinity,omitempty"` // 0.0-1.0 weights per archetype
	CreatedAt         time.Time          `bson:"createdAt"          json:"createdAt"`
}

// ImageURL returns the path the frontend should use to fetch this surface's
// image bytes. Paired with the route registered by Handler.ServeImage.
func (s *Surface) ImageURL() string {
	return "/v1/surfaces/" + s.ID + "/image"
}

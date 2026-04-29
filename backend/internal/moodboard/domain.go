package moodboard

import "time"

// OutfitItem is a snapshot of a wardrobe item at the time the moodboard was saved.
// This ensures the moodboard remains fully displayable even if the original item
// is later deleted or its image is updated.
type OutfitItem struct {
	ID          string `bson:"id"          json:"id"`
	Category    string `bson:"category"    json:"category"`
	Label       string `bson:"label"       json:"label"`
	ImageURL    string `bson:"imageUrl"    json:"imageUrl"`
	PngImageURL string `bson:"pngImageUrl" json:"pngImageUrl,omitempty"`
}

// Weather captures the weather context for a saved outfit so the card can
// render an accurate chip when the moodboard is viewed later.
type Weather struct {
	Temperature string `bson:"temperature,omitempty" json:"temperature,omitempty"`
	Condition   string `bson:"condition,omitempty"   json:"condition,omitempty"`
	Unit        string `bson:"unit,omitempty"        json:"unit,omitempty"`
}

// Outfit is stored inline (not by reference) so the moodboard remains displayable
// even if the original wardrobe items are later deleted.
type Outfit struct {
	// ID is optional — the client assigns one when a generated batch is shown
	// so feedback events can distinguish which outfit in the batch was picked.
	// The server doesn't require it; the moodboard itself uses SavedMoodBoard.ID.
	ID              string             `bson:"id,omitempty"     json:"id,omitempty"`
	Name            string             `bson:"name"             json:"name"`
	Description     string             `bson:"description"      json:"description"`
	Items           []string           `bson:"items"            json:"items"`                       // wardrobe item IDs
	Rationale       string             `bson:"rationale"        json:"rationale,omitempty"`         // 1-line stylist explanation
	LayoutRoles     map[string]string  `bson:"layoutRoles"      json:"layoutRoles,omitempty"`       // itemID → hero|support|accent
	// VisualWeights tags the signature piece per outfit (P1-H, prompts.go v2+).
	// itemID → "statement" | "supporting" | "minor". Optional; older saves and
	// outfits where the LLM omitted it deserialise unchanged. Without this
	// field the strict JSON decoder 400s on every save the FE makes since
	// the v2 prompt landed.
	VisualWeights   map[string]string  `bson:"visualWeights,omitempty"  json:"visualWeights,omitempty"`
	Snapshots       []OutfitItem       `bson:"snapshots"        json:"snapshots,omitempty"`         // resolved item data at save time
	Suggestions     []string           `bson:"suggestions"      json:"suggestions,omitempty"`       // text hints for missing complementary items
	ArchetypeScores map[string]float64 `bson:"archetypeScores"  json:"archetypeScores,omitempty"`   // archetype alignment at save time
	SmartSuggestion string             `bson:"smartSuggestion"  json:"smartSuggestion,omitempty"`   // archetype-driven item suggestion
	Weather         *Weather           `bson:"weather,omitempty"       json:"weather,omitempty"`       // weather context at save time
	Palette         []string           `bson:"palette,omitempty"       json:"palette,omitempty"`       // dominant colors as #RRGGBB
	PanelID         string             `bson:"panelId,omitempty"       json:"panelId,omitempty"`       // surface id the LLM chose
	BackgroundID    string             `bson:"backgroundId,omitempty"  json:"backgroundId,omitempty"`  // surface id the LLM chose
	PanelURL        string             `bson:"panelUrl,omitempty"      json:"panelUrl,omitempty"`      // resolved panel image URL
	BackgroundURL   string             `bson:"backgroundUrl,omitempty" json:"backgroundUrl,omitempty"` // resolved background image URL
}

// SavedMoodBoard is a moodboard selected by the user for a specific date.
//
// ImageURL is the path to the rendered collage PNG captured client-side at
// save time and stored in GridFS under the filename "moodboard:{id}". It is
// populated only when the client supplied BoardImage on the save request;
// older rows render from Outfit.Snapshots alone (current calendar behaviour
// is preserved as a fallback).
type SavedMoodBoard struct {
	ID        string    `bson:"_id"                json:"id"`
	UserID    string    `bson:"userId"             json:"userId"`
	Outfit    Outfit    `bson:"outfit"             json:"outfit"`
	Date      string    `bson:"date"               json:"date"` // YYYY-MM-DD
	ImageURL  string    `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"`
	CreatedAt time.Time `bson:"createdAt"          json:"createdAt"`
}

// SaveRequest is the body for POST /v1/moodboards.
//
// GeneratedBatch is optional but strongly recommended — it lets the server
// record the *full set* of outfits the user was shown (saved + rejected) on
// the feedback event, which is the only way preference pairs can be
// reconstructed later for ranker / DPO training. Without it we know only
// what was picked, not what was passed over.
//
// JobID ties the save back to the POST /v1/outfits/generate job that
// produced the batch, so the training pipeline can cross-reference prompt
// inputs.
type SaveRequest struct {
	Outfit         Outfit   `json:"outfit"`
	Date           string   `json:"date"` // YYYY-MM-DD; if empty, today is used
	GeneratedBatch []Outfit `json:"generatedBatch,omitempty"`
	JobID          string   `json:"jobId,omitempty"`
	// BoardImage is the rendered collage captured on-device before save, as
	// base64-encoded PNG bytes (with or without a data: URL prefix). Optional
	// — when present, the server stores it in GridFS and populates
	// SavedMoodBoard.ImageURL on the response. When absent, the calendar
	// continues to render the collage from Outfit.Snapshots.
	BoardImage string `json:"boardImage,omitempty"`
}

// ListResponse is returned from GET /v1/moodboards.
type ListResponse struct {
	MoodBoards []SavedMoodBoard `json:"moodboards"`
	NextCursor *string          `json:"nextCursor"`
}

// Package feedback captures explicit and implicit user reactions to generated
// outfits. It is an append-only event log — every row is an observation, never
// an update — so the dataset can be exported as JSONL for offline ranker /
// preference-pair training without race-condition worries.
//
// Design notes:
//
//   - SchemaVersion is attached to every event so later shape changes don't
//     poison historical training data. Bump it when adding or renaming fields.
//   - GeneratedBatch is optional on the event itself: it can live on a
//     saved-moodboard row that the event references. Kept here as well for the
//     case where the moodboard is rejected (no save) but the batch + chosen
//     signal are still worth learning from.
//   - No PII beyond userId. Weather, DoW, hour, archetype, occasion — all
//     coarse-grained so the collection can later be exported to training infra
//     without GDPR friction.
package feedback

import "time"

// CurrentSchemaVersion is written to every new event so future shape changes
// can be filtered at export time.
const CurrentSchemaVersion = 1

// Action is the user-facing verb that produced the event.
type Action string

const (
	// ActionSaved — user committed an outfit to their calendar. Strong positive.
	ActionSaved Action = "saved"
	// ActionSkipped — user left the moodboard without picking any outfit.
	ActionSkipped Action = "skipped"
	// ActionRegenerated — user asked for a fresh batch without saving. Weak negative.
	ActionRegenerated Action = "regenerated"
	// ActionRated — user attached a thumbs / star rating to one outfit.
	ActionRated Action = "rated"
	// ActionItemSwapped — user replaced one item in a suggested outfit before saving.
	// chosenOutfitId references the original outfit; extras.swappedFrom / swappedTo
	// carry the item IDs.
	ActionItemSwapped Action = "item_swapped"
)

// Valid reports whether a is one of the known actions.
func (a Action) Valid() bool {
	switch a {
	case ActionSaved, ActionSkipped, ActionRegenerated, ActionRated, ActionItemSwapped:
		return true
	}
	return false
}

// Context holds the coarse, non-PII signals that shape outfit suggestions.
// All fields are optional — emit what the client has, leave the rest zero.
type Context struct {
	Weather   string `bson:"weather,omitempty"   json:"weather,omitempty"`
	DayOfWeek string `bson:"dayOfWeek,omitempty" json:"dayOfWeek,omitempty"`
	Hour      int    `bson:"hour,omitempty"      json:"hour,omitempty"`
	Archetype string `bson:"archetype,omitempty" json:"archetype,omitempty"`
	Occasion  string `bson:"occasion,omitempty"  json:"occasion,omitempty"`
}

// OutfitSnapshot is a trimmed-down shape of a generated outfit, preserved on
// the event so training jobs don't need to cross-reference a moodboard row that
// may later be deleted.
type OutfitSnapshot struct {
	ID              string             `bson:"id"                        json:"id"`
	Name            string             `bson:"name,omitempty"            json:"name,omitempty"`
	Items           []string           `bson:"items"                     json:"items"`
	Rationale       string             `bson:"rationale,omitempty"       json:"rationale,omitempty"`
	ArchetypeScores map[string]float64 `bson:"archetypeScores,omitempty" json:"archetypeScores,omitempty"`
}

// Event is a single user reaction to a generation batch. It is append-only.
type Event struct {
	ID             string `bson:"_id"                       json:"id"`
	UserID         string `bson:"userId"                    json:"userId"`
	JobID          string `bson:"jobId,omitempty"           json:"jobId,omitempty"`
	ChosenOutfitID string `bson:"chosenOutfitId,omitempty"  json:"chosenOutfitId,omitempty"`
	Action         Action `bson:"action"                    json:"action"`
	// Rating is a 1–5 scalar when Action == rated, nil otherwise.
	Rating *int `bson:"rating,omitempty" json:"rating,omitempty"`
	// GeneratedBatch is the full set of outfits the user was shown. This is
	// the single most important field for future preference-pair training:
	// without it a "saved" event tells us nothing about what was rejected.
	GeneratedBatch []OutfitSnapshot `bson:"generatedBatch,omitempty"  json:"generatedBatch,omitempty"`
	Context        Context          `bson:"context,omitempty"         json:"context,omitempty"`
	// PromptVersion and GeneratorVersion let the training pipeline filter out
	// events produced by prompts or providers that have since been retired.
	PromptVersion    string `bson:"promptVersion,omitempty"    json:"promptVersion,omitempty"`
	GeneratorVersion string `bson:"generatorVersion,omitempty" json:"generatorVersion,omitempty"`
	// SwappedFrom and SwappedTo apply to Action == item_swapped events. They
	// record the wardrobe item IDs involved in the swap, giving training an
	// explicit (rejected → accepted) pair within the same outfit without
	// needing to diff sequential GeneratedBatch snapshots.
	SwappedFrom   string    `bson:"swappedFrom,omitempty"      json:"swappedFrom,omitempty"`
	SwappedTo     string    `bson:"swappedTo,omitempty"        json:"swappedTo,omitempty"`
	SchemaVersion int       `bson:"schemaVersion"           json:"schemaVersion"`
	CreatedAt     time.Time `bson:"createdAt"               json:"createdAt"`
}

// SubmitRequest is the POST /v1/outfits/feedback body. UserID is taken from
// the JWT, never the body, so clients can't forge events for other users.
type SubmitRequest struct {
	JobID            string           `json:"jobId,omitempty"`
	ChosenOutfitID   string           `json:"chosenOutfitId,omitempty"`
	Action           Action           `json:"action"`
	Rating           *int             `json:"rating,omitempty"`
	GeneratedBatch   []OutfitSnapshot `json:"generatedBatch,omitempty"`
	Context          Context          `json:"context,omitempty"`
	PromptVersion    string           `json:"promptVersion,omitempty"`
	GeneratorVersion string           `json:"generatorVersion,omitempty"`
	SwappedFrom      string           `json:"swappedFrom,omitempty"`
	SwappedTo        string           `json:"swappedTo,omitempty"`
}

// SubmitResponse confirms the event was persisted.
type SubmitResponse struct {
	ID string `json:"id"`
}

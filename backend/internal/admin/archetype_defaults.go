package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/shared/gender"
)

// ────────────────────────────────────────────────────────────────────
// Archetype default wardrobe items.
//
// Admin-curated items keyed by archetype. On user signup (or when
// the user's archetype profile is established), the matching
// defaults are COPIED into the user's wardrobe — they become the
// user's own items, fully editable and deletable, so the cold-start
// problem ("empty closet, can't generate outfits") disappears.
//
// Two principles:
//
//   - Items are curated by admins, not generated. The admin uploads
//     image + metadata + structured description. This sidesteps the
//     LLM hallucinations that plagued the auto-generated generic
//     items pool.
//   - Items are copied (not referenced) on seeding. The user's
//     wardrobe row is independent — they can edit traits, delete,
//     re-categorize. The default item itself stays unchanged in the
//     pool.
//
// Distinct from `internal/generic/GenericItem`: that's an on-demand
// AI-generated filler shown only inside outfit-generation prompts
// when the wardrobe is too small. Defaults here become real
// wardrobe items.
// ────────────────────────────────────────────────────────────────────

// ArchetypeDefaultItem is one curated default. Mirrors the user-
// facing wardrobe.ClothingItem shape minus userId (defaults are
// pool entries — userId is stamped at copy time).
type ArchetypeDefaultItem struct {
	ID                    string            `bson:"_id"                              json:"id"`
	Archetype             string            `bson:"archetype"                        json:"archetype"`
	Category              string            `bson:"category"                         json:"category"`
	// Gender — "male", "female", or "unisex". Decides which users
	// this default is mixed into moodboards for. "unisex" (the
	// default when unset) participates for every user.
	Gender                string            `bson:"gender,omitempty"                 json:"gender,omitempty"`
	Label                 string            `bson:"label"                            json:"label"`
	Description           string            `bson:"description,omitempty"            json:"description,omitempty"`
	ImageURL              string            `bson:"imageUrl"                         json:"imageUrl"`
	PngImageURL           string            `bson:"pngImageUrl,omitempty"            json:"pngImageUrl,omitempty"`
	Traits                map[string]string `bson:"traits,omitempty"                 json:"traits,omitempty"`
	StructuredDescription map[string]any    `bson:"structuredDescription,omitempty"  json:"structuredDescription,omitempty"`
	// SeededCount tracks how many user wardrobes have received a
	// copy of this row. Pure observability — admins can see which
	// defaults are landing in the field vs sitting unused.
	SeededCount int       `bson:"seededCount,omitempty" json:"seededCount,omitempty"`
	CreatedBy   string    `bson:"createdBy,omitempty"   json:"createdBy,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"             json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt,omitempty"   json:"updatedAt,omitempty"`
}

// ArchetypeDefaultsRepository is the persistence contract.
type ArchetypeDefaultsRepository interface {
	List(ctx context.Context, archetype string) ([]ArchetypeDefaultItem, error)
	Get(ctx context.Context, id string) (*ArchetypeDefaultItem, error)
	Create(ctx context.Context, item ArchetypeDefaultItem) (*ArchetypeDefaultItem, error)
	Update(ctx context.Context, id string, patch ArchetypeDefaultItemPatch) (*ArchetypeDefaultItem, error)
	Delete(ctx context.Context, id string) error
	// IncrementSeeded bumps SeededCount by n. Best-effort — caller
	// logs and continues on error. Used by the seed hook so admins
	// can see which defaults are landing.
	IncrementSeeded(ctx context.Context, id string, n int) error
	// SampleForOutfitGen returns up to `sampleSize` random rows for
	// the given archetype, excluding ids in `excludeIDs`. Used by
	// the outfit-side filler loader: replaces the previous
	// "List(archetype) + Go-side filter + Go-side shuffle" with a
	// single Mongo aggregation ($match → $nin → $sample). Index-
	// backed via the (archetype, createdAt) compound index already
	// in place.
	SampleForOutfitGen(ctx context.Context, archetype, userGender string, excludeIDs []string, sampleSize int) ([]ArchetypeDefaultItem, error)
}

// ArchetypeDefaultItemPatch carries the optional update fields.
// Pointers so a deliberate clear (e.g. dropping the description)
// is distinguishable from "leave unchanged".
type ArchetypeDefaultItemPatch struct {
	Category              *string            `json:"category,omitempty"`
	Gender                *string            `json:"gender,omitempty"`
	Label                 *string            `json:"label,omitempty"`
	Description           *string            `json:"description,omitempty"`
	ImageURL              *string            `json:"imageUrl,omitempty"`
	PngImageURL           *string            `json:"pngImageUrl,omitempty"`
	Traits                *map[string]string `json:"traits,omitempty"`
	StructuredDescription *map[string]any    `json:"structuredDescription,omitempty"`
}

// ArchetypeDefaultsMongoRepository implements the interface
// against a Mongo collection `archetype_default_items`.
type ArchetypeDefaultsMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewArchetypeDefaultsMongoRepository constructs the repo and
// ensures indexes:
//   - (archetype, createdAt desc) for the per-archetype list
//   - (archetype, category, label) unique partial — prevents
//     accidental duplicate curation of the same item per
//     archetype. Sparse so absent fields don't bloat the index.
func NewArchetypeDefaultsMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*ArchetypeDefaultsMongoRepository, error) {
	r := &ArchetypeDefaultsMongoRepository{client: client, dbName: dbName}
	_, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "archetype", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("archetype_defaults_archetype_created_desc"),
		},
		{
			Keys: bson.D{{Key: "archetype", Value: 1}, {Key: "category", Value: 1}, {Key: "label", Value: 1}},
			Options: options.Index().
				SetName("archetype_defaults_uniq_per_archetype").
				SetUnique(true),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ensure archetype_default_items indexes: %w", err)
	}
	return r, nil
}

func (r *ArchetypeDefaultsMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("archetype_default_items")
}

// List returns every default for an archetype, newest first. Pass
// "" to get every default across every archetype.
func (r *ArchetypeDefaultsMongoRepository) List(ctx context.Context, arche string) ([]ArchetypeDefaultItem, error) {
	filter := bson.M{}
	if arche != "" {
		filter["archetype"] = arche
	}
	cur, err := r.col().Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []ArchetypeDefaultItem
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns one item by id. (nil, nil) when not found so the
// handler can surface 404 cleanly.
func (r *ArchetypeDefaultsMongoRepository) Get(ctx context.Context, id string) (*ArchetypeDefaultItem, error) {
	var doc ArchetypeDefaultItem
	err := r.col().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// Create persists a new default. Validates the archetype against
// the canonical Profiles map so a typo doesn't bury an item under
// "rebbel" or similar. ID is auto-generated when empty.
func (r *ArchetypeDefaultsMongoRepository) Create(ctx context.Context, item ArchetypeDefaultItem) (*ArchetypeDefaultItem, error) {
	if _, ok := archetype.Profiles[item.Archetype]; !ok {
		return nil, fmt.Errorf("admin: unknown archetype %q (must be one of %v)", item.Archetype, archetypeNames())
	}
	if strings.TrimSpace(item.Category) == "" || strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.ImageURL) == "" {
		return nil, errors.New("admin: archetype default requires category, label, imageUrl")
	}
	// Gender decides which users this default is mixed into
	// moodboards for. Unset → "unisex" (participates for everyone).
	switch item.Gender {
	case "":
		item.Gender = gender.Unisex
	case gender.Male, gender.Female, gender.Unisex:
		// valid
	default:
		return nil, fmt.Errorf("admin: invalid gender %q (must be male, female, or unisex)", item.Gender)
	}
	if item.ID == "" {
		item.ID = "ad_" + randomHex(16)
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	if _, err := r.col().InsertOne(ctx, item); err != nil {
		return nil, err
	}
	return &item, nil
}

// Update applies non-nil fields. Returns the updated row.
func (r *ArchetypeDefaultsMongoRepository) Update(ctx context.Context, id string, patch ArchetypeDefaultItemPatch) (*ArchetypeDefaultItem, error) {
	set := bson.M{"updatedAt": time.Now().UTC()}
	if patch.Category != nil {
		set["category"] = *patch.Category
	}
	if patch.Gender != nil {
		if !gender.ValidItem(*patch.Gender) {
			return nil, fmt.Errorf("admin: invalid gender %q (must be male, female, or unisex)", *patch.Gender)
		}
		set["gender"] = *patch.Gender
	}
	if patch.Label != nil {
		set["label"] = *patch.Label
	}
	if patch.Description != nil {
		set["description"] = *patch.Description
	}
	if patch.ImageURL != nil {
		set["imageUrl"] = *patch.ImageURL
	}
	if patch.PngImageURL != nil {
		set["pngImageUrl"] = *patch.PngImageURL
	}
	if patch.Traits != nil {
		set["traits"] = *patch.Traits
	}
	if patch.StructuredDescription != nil {
		set["structuredDescription"] = *patch.StructuredDescription
	}
	res, err := r.col().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return nil, err
	}
	if res.MatchedCount == 0 {
		return nil, mongo.ErrNoDocuments
	}
	return r.Get(ctx, id)
}

// Delete removes one default. Existing wardrobe items already
// seeded from this row stay intact (they were copies).
func (r *ArchetypeDefaultsMongoRepository) Delete(ctx context.Context, id string) error {
	res, err := r.col().DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// IncrementSeeded bumps the seededCount counter on a row.
func (r *ArchetypeDefaultsMongoRepository) IncrementSeeded(ctx context.Context, id string, n int) error {
	_, err := r.col().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": bson.M{"seededCount": n}})
	return err
}

// SampleForOutfitGen returns up to `sampleSize` random rows for
// `archetype`, excluding any ids in `excludeIDs`. Single Mongo round-
// trip: $match limits the scan to one archetype, $nin folds in the
// per-user rejection list, $sample asks the server to randomise +
// truncate. Replaces the previous List + Go-side filter + Go-side
// shuffle path (mootd#72).
//
// `sampleSize <= 0` returns nil with no error (caller should not
// query in that case); a non-existent archetype legitimately yields
// an empty slice — the caller logs "no fillers" and proceeds.
func (r *ArchetypeDefaultsMongoRepository) SampleForOutfitGen(ctx context.Context, arche, userGender string, excludeIDs []string, sampleSize int) ([]ArchetypeDefaultItem, error) {
	if sampleSize <= 0 {
		return nil, nil
	}
	match := bson.M{"archetype": arche}
	if len(excludeIDs) > 0 {
		match["_id"] = bson.M{"$nin": excludeIDs}
	}
	// Gender filter: a male/female user only gets defaults tagged
	// with their own gender or "unisex". Rows with no gender field —
	// or an empty one — pre-date the gender feature and count as
	// unisex so they keep participating. A "unisex" user ("as long
	// as it's stylish") or an unknown gender skips the filter
	// entirely — every filler is eligible.
	if userGender == gender.Male || userGender == gender.Female {
		match["$or"] = []bson.M{
			{"gender": bson.M{"$in": []string{userGender, gender.Unisex, ""}}},
			{"gender": bson.M{"$exists": false}},
		}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$sample", Value: bson.M{"size": sampleSize}}},
	}
	cursor, err := r.col().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []ArchetypeDefaultItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// archetypeNames returns the canonical archetype keys. Only used
// to surface a friendly error message; not on the hot path.
func archetypeNames() []string {
	out := make([]string, 0, len(archetype.Profiles))
	for k := range archetype.Profiles {
		out = append(out, k)
	}
	return out
}

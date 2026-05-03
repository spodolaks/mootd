package privacy

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ErrUserNotFound is returned by Purge when the user has no
// data left to delete. Idempotency: caller should map this to
// HTTP 404 on the second call.
var ErrUserNotFound = errors.New("privacy: user not found or already purged")

// Service orchestrates per-user data export + purge across
// every collection that holds user-scoped data.
//
// Holds the Mongo client + dbName directly (rather than a fan
// of repositories) because the operations are short and
// crossing each domain's repository would just duplicate "find
// docs where userId=X" 9 times. The collections are an
// allowlist baked into one place — easy to audit.
type Service struct {
	client *mongo.Client
	dbName string
}

// NewService constructs a Service.
func NewService(client *mongo.Client, dbName string) *Service {
	return &Service{client: client, dbName: dbName}
}

func (s *Service) col(name string) *mongo.Collection {
	return s.client.Database(s.dbName).Collection(name)
}

// userScopedCollections is the allowlist of collections holding
// per-user data. Each entry is (collection, fieldName-pointing-
// to-userId).
//
// Edits-with-thought policy: when adding a new collection that
// stores user-scoped data, also add it here. CI test in
// privacy_collections_test.go enforces that any new collection
// using "userId" as a field shows up here.
var userScopedCollections = []struct {
	Name  string
	Field string
}{
	{"wardrobe_items", "userId"},
	{"outfits", "userId"},
	{"outfit_jobs", "userId"},
	{"outfit_feedback", "userId"},
	{"moodboards", "userId"},
	{"events", "userId"},
	{"llm_calls", "userId"},
	{"detection_runs", "userId"},
	{"user_budgets", "_id"}, // user_budgets is keyed by userId
}

// Purge wipes every per-user document for userID and the user
// document itself. Idempotent: the second call returns
// ErrUserNotFound because the user record is gone.
//
// Order: delete the user record LAST. If anything between
// here and there fails, the user can retry — they'll still
// authenticate (their JWT is still valid) and the cleanup will
// continue. The user record being last means we don't leave
// the system in a "user exists but their data is half-gone"
// state without a way to retry.
func (s *Service) Purge(ctx context.Context, userID string) (*PurgeReport, error) {
	if userID == "" {
		return nil, errors.New("privacy: userID required")
	}
	// Existence check up front so we can return 404 cleanly
	// for idempotency.
	count, err := s.col("users").CountDocuments(ctx, bson.M{"_id": userID})
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrUserNotFound
	}

	report := &PurgeReport{
		UserID:      userID,
		PurgedAt:    time.Now().UTC(),
		Collections: map[string]int64{},
	}

	for _, c := range userScopedCollections {
		filter := bson.M{c.Field: userID}
		res, err := s.col(c.Name).DeleteMany(ctx, filter)
		if err != nil {
			return nil, err
		}
		if res.DeletedCount > 0 {
			report.Collections[c.Name] = res.DeletedCount
			report.Total += res.DeletedCount
		}
	}

	// Finally the user record. After this point the user can no
	// longer log in (refresh token hash is on the user document).
	res, err := s.col("users").DeleteOne(ctx, bson.M{"_id": userID})
	if err != nil {
		return nil, err
	}
	if res.DeletedCount > 0 {
		report.Collections["users"] = res.DeletedCount
		report.Total += res.DeletedCount
	}

	return report, nil
}

// Export collects every per-user document into an ExportData.
// Read-only; safe to call repeatedly.
//
// Returns ErrUserNotFound when the user record is gone — same
// shape as Purge so handlers can branch the same way.
func (s *Service) Export(ctx context.Context, userID string) (*ExportData, error) {
	if userID == "" {
		return nil, errors.New("privacy: userID required")
	}

	out := &ExportData{
		UserID:      userID,
		GeneratedAt: time.Now().UTC(),
	}

	// User doc.
	var userDoc bson.M
	err := s.col("users").FindOne(ctx, bson.M{"_id": userID}).Decode(&userDoc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	// Strip refresh token hash — exporting it back to the user
	// is harmless, but it's a credential and shouldn't appear
	// in a downloaded zip a user might forward to support.
	delete(userDoc, "refreshTokenHash")
	out.User = userDoc

	// Per-collection arrays.
	loaders := []struct {
		name   string
		field  string
		target *[]any
	}{
		{"wardrobe_items", "userId", &out.WardrobeItems},
		{"outfits", "userId", &out.Outfits},
		{"outfit_jobs", "userId", &out.OutfitJobs},
		{"moodboards", "userId", &out.Moodboards},
		{"outfit_feedback", "userId", &out.OutfitFeedback},
		{"events", "userId", &out.Events},
		{"llm_calls", "userId", &out.LLMCalls},
		{"detection_runs", "userId", &out.DetectionRuns},
	}
	for _, l := range loaders {
		cur, err := s.col(l.name).Find(ctx, bson.M{l.field: userID})
		if err != nil {
			return nil, err
		}
		var docs []bson.M
		if err := cur.All(ctx, &docs); err != nil {
			return nil, err
		}
		if len(docs) > 0 {
			converted := make([]any, len(docs))
			for i, d := range docs {
				converted[i] = d
			}
			*l.target = converted
		}
	}

	// user_budgets is keyed by _id (= userId).
	var budget bson.M
	if err := s.col("user_budgets").FindOne(ctx, bson.M{"_id": userID}).Decode(&budget); err == nil {
		out.UserBudget = budget
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	return out, nil
}

package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// anchorBounds returns the user ids in `anchors` plus the min and max
// anchor timestamps. A funnel step's qualifying event for any user
// falls in (anchor_u, anchor_u+window], so [min, max+window] is a safe
// global time bound for the step query (#110 E3). Returns zero times
// for an empty map.
func anchorBounds(anchors map[string]time.Time) (ids []string, minA, maxA time.Time) {
	ids = make([]string, 0, len(anchors))
	first := true
	for u, a := range anchors {
		ids = append(ids, u)
		if first || a.Before(minA) {
			minA = a
		}
		if first || a.After(maxA) {
			maxA = a
		}
		first = false
	}
	return ids, minA, maxA
}

// ────────────────────────────────────────────────────────────────────
// Funnels (P2-04 / mootd-admin#21).
//
// One `admin_funnels` collection holds the saved configurations.
// Stats are computed live on demand against the events
// collection — no precomputation, no materialized views. The
// per-funnel cardinality is small (admins look at ~5 funnels)
// and the analysis window is bounded (default 7 days), so
// "compute on each /stats hit" stays cheap.
//
// Algorithm: for an N-step funnel, run N+1 aggregations:
//   - Step 0: for each user with the first event in the
//     analysis window, record min(createdAt) as the anchor.
//   - Step k: for each user that passed step k-1 (anchor in
//     hand), check if event[k] exists with createdAt in
//     (anchor, anchor + windowDays]. The set of users that
//     passed step k becomes the input for step k+1's anchor.
//
// We don't strictly enforce order between non-consecutive
// steps — same convention as Amplitude / Mixpanel funnels.
// "Did the user reach step 3 within window of step 0" is the
// question; "did they hit them in exact sequence with no
// other event in between" is a strict-funnel concern that
// adds complexity for marginal value.
// ────────────────────────────────────────────────────────────────────

const adminFunnelsCollection = "admin_funnels"

// FunnelStep is one ordered event with optional property
// filters. `filters` is map[string]any so we can equality-match
// on any property the events catalog records — kept loose for
// admin flexibility.
type FunnelStep struct {
	EventName string                 `bson:"eventName" json:"eventName"`
	Filters   map[string]interface{} `bson:"filters,omitempty" json:"filters,omitempty"`
}

// Funnel is one saved row.
type Funnel struct {
	ID           string       `bson:"_id"          json:"id"`
	Name         string       `bson:"name"         json:"name"`
	Steps        []FunnelStep `bson:"steps"        json:"steps"`
	WindowDays   int          `bson:"windowDays"   json:"windowDays"`   // step-to-step window
	AnalysisDays int          `bson:"analysisDays" json:"analysisDays"` // how far back to anchor on step 0
	CreatedBy    string       `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	CreatedAt    time.Time    `bson:"createdAt"    json:"createdAt"`
}

// FunnelStepStat is one row in the stats response.
type FunnelStepStat struct {
	StepIndex   int     `json:"stepIndex"`
	EventName   string  `json:"eventName"`
	UserCount   int64   `json:"userCount"`
	DropOffRate float64 `json:"dropOffRate,omitempty"` // 0-1; vs prior step
	Cumulative  float64 `json:"cumulative,omitempty"`  // 0-1; vs step 0
}

// FunnelStats is the wire shape for GET /funnels/{id}/stats.
type FunnelStats struct {
	FunnelID     string           `json:"funnelId"`
	WindowDays   int              `json:"windowDays"`
	AnalysisDays int              `json:"analysisDays"`
	Steps        []FunnelStepStat `json:"steps"`
	GeneratedAt  time.Time        `json:"generatedAt"`
}

// FunnelsRepository is the persistence boundary.
type FunnelsRepository interface {
	List(ctx context.Context) ([]Funnel, error)
	Get(ctx context.Context, id string) (*Funnel, error)
	Create(ctx context.Context, f Funnel) (Funnel, error)
	Stats(ctx context.Context, id string) (*FunnelStats, error)
}

// FunnelsMongoRepository implements FunnelsRepository.
type FunnelsMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewFunnelsMongoRepository ensures the (createdAt desc)
// index for the list view + seeds the default funnel
// (signed_up → photo_uploaded → generated_outfit →
// saved_moodboard) on first boot.
func NewFunnelsMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*FunnelsMongoRepository, error) {
	r := &FunnelsMongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "createdAt", Value: -1}},
		Options: options.Index().SetName("admin_funnels_created_desc"),
	}); err != nil {
		return nil, fmt.Errorf("ensure admin_funnels indexes: %w", err)
	}
	// Seed default funnel idempotently (Issue's acceptance
	// criterion: "Default funnel: signed_up → photo_uploaded →
	// generated_outfit → saved_moodboard"). InsertOne with
	// duplicate-key swallowed.
	defaultID := "fn_default_v1"
	_, _ = r.col().UpdateOne(ctx,
		bson.M{"_id": defaultID},
		bson.M{"$setOnInsert": Funnel{
			ID:           defaultID,
			Name:         "Onboarding to first save",
			WindowDays:   7,
			AnalysisDays: 30,
			Steps: []FunnelStep{
				{EventName: "signed_up"},
				{EventName: "photo_uploaded"},
				{EventName: "generated_outfit"},
				{EventName: "saved_moodboard"},
			},
			CreatedAt: time.Now().UTC(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
	return r, nil
}

func (r *FunnelsMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection(adminFunnelsCollection)
}

func (r *FunnelsMongoRepository) eventsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("events")
}

func (r *FunnelsMongoRepository) List(ctx context.Context) ([]Funnel, error) {
	cur, err := r.col().Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []Funnel
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *FunnelsMongoRepository) Get(ctx context.Context, id string) (*Funnel, error) {
	var doc Funnel
	err := r.col().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// Create persists f and returns it with its generated ID populated, so
// the caller can echo the created row without a follow-up read (#111 F7).
func (r *FunnelsMongoRepository) Create(ctx context.Context, f Funnel) (Funnel, error) {
	if f.Name == "" || len(f.Steps) < 2 {
		return Funnel{}, errors.New("admin: funnel needs name + at least 2 steps")
	}
	if f.WindowDays <= 0 || f.WindowDays > 90 {
		return Funnel{}, errors.New("admin: windowDays must be 1-90")
	}
	if f.AnalysisDays <= 0 || f.AnalysisDays > 180 {
		return Funnel{}, errors.New("admin: analysisDays must be 1-180")
	}
	if f.ID == "" {
		f.ID = "fn_" + generateAuditID()[len("aud_"):]
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	if _, err := r.col().InsertOne(ctx, f); err != nil {
		return Funnel{}, err
	}
	return f, nil
}

// Stats walks the steps and computes per-user pass-through.
// Each step's user-set is the input for the next step's
// "did this user emit this event with createdAt > anchor
// AND createdAt <= anchor + windowDays" check.
func (r *FunnelsMongoRepository) Stats(ctx context.Context, id string) (*FunnelStats, error) {
	f, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}

	now := time.Now().UTC()
	windowMs := int64(f.WindowDays) * 24 * 60 * 60 * 1000
	analysisStart := now.Add(-time.Duration(f.AnalysisDays) * 24 * time.Hour)

	// Step 0: anchor = each user's earliest event of the first
	// step's name in [analysisStart, now].
	step0Match := bson.M{
		"name":      f.Steps[0].EventName,
		"createdAt": bson.M{"$gte": analysisStart},
	}
	for k, v := range f.Steps[0].Filters {
		step0Match["properties."+k] = v
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: step0Match}},
		{{Key: "$group", Value: bson.M{
			"_id":    "$userId",
			"anchor": bson.M{"$min": "$createdAt"},
		}}},
	}
	cur, err := r.eventsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	type anchorRow struct {
		UserID string    `bson:"_id"`
		Anchor time.Time `bson:"anchor"`
	}
	var anchors []anchorRow
	if err := cur.All(ctx, &anchors); err != nil {
		cur.Close(ctx)
		return nil, err
	}
	cur.Close(ctx)

	// Map for O(1) per-user anchor lookup at each subsequent
	// step.
	currentAnchors := map[string]time.Time{}
	for _, a := range anchors {
		currentAnchors[a.UserID] = a.Anchor
	}

	stats := &FunnelStats{
		FunnelID:     id,
		WindowDays:   f.WindowDays,
		AnalysisDays: f.AnalysisDays,
		GeneratedAt:  now,
	}
	step0Count := int64(len(currentAnchors))
	stats.Steps = append(stats.Steps, FunnelStepStat{
		StepIndex:  0,
		EventName:  f.Steps[0].EventName,
		UserCount:  step0Count,
		Cumulative: 1.0,
	})

	prevCount := step0Count
	for i := 1; i < len(f.Steps); i++ {
		step := f.Steps[i]
		// Pull this step's events for users still in play. Pre-filter
		// by the user-id list AND by time: a qualifying event for user
		// u must fall in (anchor_u, anchor_u+window], so globally it
		// lies in [min(anchor), max(anchor)+window]. Without this bound
		// the query loaded EVERY event of this name for these users
		// across all history into memory (#110 E3); the per-user
		// anchor/window check below still does the exact filtering.
		userIDs, minAnchor, maxAnchor := anchorBounds(currentAnchors)
		if len(userIDs) == 0 {
			stats.Steps = append(stats.Steps, FunnelStepStat{
				StepIndex: i, EventName: step.EventName,
			})
			continue
		}

		match := bson.M{
			"userId": bson.M{"$in": userIDs},
			"name":   step.EventName,
			"createdAt": bson.M{
				"$gt":  minAnchor,
				"$lte": maxAnchor.Add(time.Duration(windowMs) * time.Millisecond),
			},
		}
		for k, v := range step.Filters {
			match["properties."+k] = v
		}
		c2, err := r.eventsCol().Find(ctx, match,
			options.Find().SetSort(bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: 1}}),
		)
		if err != nil {
			return nil, err
		}
		var rows []struct {
			UserID    string    `bson:"userId"`
			CreatedAt time.Time `bson:"createdAt"`
		}
		if err := c2.All(ctx, &rows); err != nil {
			c2.Close(ctx)
			return nil, err
		}
		c2.Close(ctx)

		// For each user, take the earliest event AFTER their
		// anchor and within windowMs. That becomes the new
		// anchor (for the next step's window check).
		nextAnchors := map[string]time.Time{}
		for _, row := range rows {
			anchor, ok := currentAnchors[row.UserID]
			if !ok {
				continue // not in the carry-forward set
			}
			if row.CreatedAt.Before(anchor) || row.CreatedAt.Equal(anchor) {
				continue
			}
			if row.CreatedAt.Sub(anchor).Milliseconds() > windowMs {
				continue
			}
			// First match per user wins (rows sorted asc by
			// createdAt; the map insert is also a no-op on
			// duplicates).
			if _, exists := nextAnchors[row.UserID]; !exists {
				nextAnchors[row.UserID] = row.CreatedAt
			}
		}

		count := int64(len(nextAnchors))
		var dropOff, cumulative float64
		if prevCount > 0 {
			dropOff = 1 - float64(count)/float64(prevCount)
		}
		if step0Count > 0 {
			cumulative = float64(count) / float64(step0Count)
		}
		stats.Steps = append(stats.Steps, FunnelStepStat{
			StepIndex:   i,
			EventName:   step.EventName,
			UserCount:   count,
			DropOffRate: dropOff,
			Cumulative:  cumulative,
		})

		currentAnchors = nextAnchors
		prevCount = count
	}

	return stats, nil
}

package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ────────────────────────────────────────────────────────────────────
// Retention cohorts (P2-05 / mootd-admin#22).
//
// "Did yesterday's signups come back today?"
//
// One endpoint, computed live. Algorithm:
//
//   1. For each cohort bucket in the trailing 30 buckets
//      (days OR ISO weeks per `unit`), find users who
//      emitted `signed_up` in that bucket. That's the cohort
//      size, ordered ascending by bucket → row 0 is the
//      oldest cohort.
//
//   2. For each day-N (or week-N) offset in [0, N), count
//      members of the cohort who emitted *any* event in
//      bucket[X + N]. Day-0 retention is the cohort size
//      itself (everyone who signed up was "active" that
//      bucket by definition).
//
// Output is a heatmap: rows = cohorts, cols = N retention
// columns. Each cell carries both the user count and the
// retention pct (cell.users / row.cohortSize) so the FE can
// render text + color without re-deriving.
// ────────────────────────────────────────────────────────────────────

// CohortRetentionCell is one cell in the heatmap.
type CohortRetentionCell struct {
	OffsetN   int     `json:"offsetN"` // 0 = cohort week itself
	Users     int64   `json:"users"`
	Retention float64 `json:"retention"` // 0..1
}

// CohortRow is one cohort.
type CohortRow struct {
	BucketStart time.Time             `json:"bucketStart"`
	Label       string                `json:"label"` // "2026-04-01" or "2026-W17"
	CohortSize  int64                 `json:"cohortSize"`
	Cells       []CohortRetentionCell `json:"cells"`
}

// CohortRetentionResponse is the wire shape.
type CohortRetentionResponse struct {
	CohortUnit  string      `json:"cohortUnit"` // "day" or "week"
	N           int         `json:"n"`
	Cohorts     []CohortRow `json:"cohorts"`
	GeneratedAt time.Time   `json:"generatedAt"`
}

// RetentionRepository computes cohort retention live from the
// events collection. Read-only and self-contained — no indexes
// of its own, leans on the (userId, createdAt desc) and
// (name, createdAt desc) indexes the events package already
// maintains.
type RetentionRepository interface {
	Compute(ctx context.Context, unit string, n int) (*CohortRetentionResponse, error)
}

// RetentionMongoRepository implements RetentionRepository against
// the events collection.
type RetentionMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewRetentionMongoRepository constructs a RetentionMongoRepository.
// No index work — relies on the events package's own indexes.
func NewRetentionMongoRepository(client *mongo.Client, dbName string) *RetentionMongoRepository {
	return &RetentionMongoRepository{client: client, dbName: dbName}
}

func (r *RetentionMongoRepository) eventsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("events")
}

// Compute runs the aggregations against the events collection.
// `unit` ∈ {"day", "week"}; `n` is the number of retention
// columns (default 7).
func (r *RetentionMongoRepository) Compute(ctx context.Context, unit string, n int) (*CohortRetentionResponse, error) {
	if unit != "day" && unit != "week" {
		return nil, errors.New("admin: cohortUnit must be day or week")
	}
	if n <= 0 || n > 30 {
		n = 7
	}

	const cohortCount = 30
	now := time.Now().UTC()
	bucketDur := 24 * time.Hour
	if unit == "week" {
		bucketDur = 7 * 24 * time.Hour
	}

	// Compute the start of the oldest cohort: 30 buckets back,
	// truncated to the bucket boundary.
	endBucket := truncateToBucket(now, unit)
	startBucket := endBucket.Add(-time.Duration(cohortCount-1) * bucketDur)

	// Step 1: for each user with a signed_up event in
	// [startBucket, endBucket+bucketDur), record their
	// signup-bucket index (0 = oldest cohort).
	signupCur, err := r.eventsCol().Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"name":      "signed_up",
			"createdAt": bson.M{"$gte": startBucket, "$lt": endBucket.Add(bucketDur)},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":      "$userId",
			"signedAt": bson.M{"$min": "$createdAt"},
		}}},
	})
	if err != nil {
		return nil, err
	}
	defer signupCur.Close(ctx)

	type signupRow struct {
		UserID   string    `bson:"_id"`
		SignedAt time.Time `bson:"signedAt"`
	}
	var signups []signupRow
	if err := signupCur.All(ctx, &signups); err != nil {
		return nil, err
	}

	// Bucketize signups: cohortIdx → set of user IDs.
	cohortUsers := make(map[int]map[string]bool, cohortCount)
	cohortStarts := make(map[int]time.Time, cohortCount)
	userCohort := make(map[string]int, len(signups)) // userId → cohortIdx
	for _, s := range signups {
		idx := bucketIndex(s.SignedAt, startBucket, bucketDur)
		if idx < 0 || idx >= cohortCount {
			continue
		}
		if cohortUsers[idx] == nil {
			cohortUsers[idx] = map[string]bool{}
			cohortStarts[idx] = startBucket.Add(time.Duration(idx) * bucketDur)
		}
		cohortUsers[idx][s.UserID] = true
		userCohort[s.UserID] = idx
	}

	// Step 2: pull every event for users in any cohort,
	// bucketed by user. We need per-user "did this user
	// touch the app in bucket X?" booleans.
	if len(userCohort) == 0 {
		// No signups in window → empty cohorts list.
		return &CohortRetentionResponse{
			CohortUnit:  unit,
			N:           n,
			Cohorts:     []CohortRow{},
			GeneratedAt: now,
		}, nil
	}

	userIDs := make([]string, 0, len(userCohort))
	for u := range userCohort {
		userIDs = append(userIDs, u)
	}
	// Window: from start of oldest cohort to end of last
	// retention column (which can extend up to n buckets
	// past the most-recent cohort).
	windowEnd := endBucket.Add(time.Duration(n) * bucketDur)
	evCur, err := r.eventsCol().Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    bson.M{"$in": userIDs},
			"createdAt": bson.M{"$gte": startBucket, "$lt": windowEnd},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"userId": "$userId",
				"bucket": bson.M{"$dateToString": bson.M{
					"format":   "%Y-%m-%d",
					"date":     "$createdAt",
					"timezone": "UTC",
				}},
			},
		}}},
	})
	if err != nil {
		return nil, err
	}
	defer evCur.Close(ctx)
	type activityRow struct {
		ID struct {
			UserID string `bson:"userId"`
			Bucket string `bson:"bucket"`
		} `bson:"_id"`
	}
	var rows []activityRow
	if err := evCur.All(ctx, &rows); err != nil {
		return nil, err
	}

	// retentionCells[cohortIdx][offsetN] = unique-users count
	// (using a set since the day-string above is per-day, but
	// for week buckets we re-bucket below).
	cellUsers := make(map[int]map[int]map[string]bool, cohortCount)
	for _, row := range rows {
		cohortIdx, ok := userCohort[row.ID.UserID]
		if !ok {
			continue
		}
		t, err := time.Parse("2006-01-02", row.ID.Bucket)
		if err != nil {
			continue
		}
		activityIdx := bucketIndex(t, startBucket, bucketDur)
		offset := activityIdx - cohortIdx
		if offset < 0 || offset >= n {
			continue
		}
		if cellUsers[cohortIdx] == nil {
			cellUsers[cohortIdx] = map[int]map[string]bool{}
		}
		if cellUsers[cohortIdx][offset] == nil {
			cellUsers[cohortIdx][offset] = map[string]bool{}
		}
		cellUsers[cohortIdx][offset][row.ID.UserID] = true
	}

	// Build the response.
	out := &CohortRetentionResponse{
		CohortUnit:  unit,
		N:           n,
		GeneratedAt: now,
	}
	for i := 0; i < cohortCount; i++ {
		start, ok := cohortStarts[i]
		if !ok {
			start = startBucket.Add(time.Duration(i) * bucketDur)
		}
		cohortSize := int64(len(cohortUsers[i]))
		cells := make([]CohortRetentionCell, 0, n)
		for offset := 0; offset < n; offset++ {
			count := int64(len(cellUsers[i][offset]))
			var pct float64
			if cohortSize > 0 {
				pct = float64(count) / float64(cohortSize)
			}
			cells = append(cells, CohortRetentionCell{
				OffsetN:   offset,
				Users:     count,
				Retention: pct,
			})
		}
		out.Cohorts = append(out.Cohorts, CohortRow{
			BucketStart: start,
			Label:       formatBucketLabel(start, unit),
			CohortSize:  cohortSize,
			Cells:       cells,
		})
	}
	return out, nil
}

// truncateToBucket aligns `t` to the start of its bucket.
//   - day: midnight UTC of the day.
//   - week: Monday 00:00 UTC of the ISO week.
func truncateToBucket(t time.Time, unit string) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if unit == "day" {
		return day
	}
	// Monday = ISO week start. Weekday() returns Sunday=0.
	wd := int(day.Weekday())
	if wd == 0 {
		wd = 7 // Sunday → 7 so Monday is 1.
	}
	return day.AddDate(0, 0, -(wd - 1))
}

// bucketIndex returns the index (0-based) of `t` relative to
// `start`. Negative when `t` predates start; returns floor.
func bucketIndex(t, start time.Time, bucketDur time.Duration) int {
	delta := t.UTC().Sub(start)
	if delta < 0 {
		return -1
	}
	return int(delta / bucketDur)
}

// formatBucketLabel formats the wire-side label for a cohort.
//   - day:  "2026-04-01"
//   - week: "2026-W17"
func formatBucketLabel(t time.Time, unit string) string {
	if unit == "week" {
		year, week := t.ISOWeek()
		return formatISOWeek(year, week)
	}
	return t.Format("2006-01-02")
}

// formatISOWeek → "2026-W17"
func formatISOWeek(year, week int) string {
	w := strconv.Itoa(week)
	if week < 10 {
		w = "0" + w
	}
	return strconv.Itoa(year) + "-W" + w
}

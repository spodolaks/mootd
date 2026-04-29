package admin

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// OverviewPeriod is the headline-period selector. Headline scalars
// (spendUsd / callCount) reflect the chosen window; daily series and
// DAU are independent of it.
type OverviewPeriod string

const (
	PeriodToday OverviewPeriod = "today"
	Period7d    OverviewPeriod = "7d"
	Period30d   OverviewPeriod = "30d"
)

// resolvePeriod accepts a query-param value and returns a clamped
// OverviewPeriod. Empty / unknown values fall back to PeriodToday.
func resolvePeriod(s string) OverviewPeriod {
	switch OverviewPeriod(s) {
	case Period7d, Period30d:
		return OverviewPeriod(s)
	default:
		return PeriodToday
	}
}

// periodWindow returns the [start, end) interval for a given period
// anchored at `now`. UTC throughout.
func periodWindow(p OverviewPeriod, now time.Time) (start, end time.Time) {
	end = now.UTC()
	switch p {
	case Period7d:
		start = end.Add(-7 * 24 * time.Hour)
	case Period30d:
		start = end.Add(-30 * 24 * time.Hour)
	default: // today
		start = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	}
	return start, end
}

// DailyMetric is one cell in a sparkline series.
type DailyMetric struct {
	Date  string  `bson:"date" json:"date"`
	Value float64 `bson:"value" json:"value"`
}

// OverviewMetrics is the wire shape returned by GET /admin/v1/overview.
//
// Renamed from the previous spendUsdToday/callCountToday scalars to
// spendUsd/callCount because the value now reflects the chosen
// period — not always today. dauApprox stays today-only (it's the
// "who is here right now" signal, period-independent).
type OverviewMetrics struct {
	Period          OverviewPeriod    `json:"period"`
	SpendUSD        float64           `json:"spendUsd"`
	CallCount       int64             `json:"callCount"`
	DauApprox       int64             `json:"dauApprox"`
	SpendUSDPrior   float64           `json:"spendUsdPrior,omitempty"`
	CallCountPrior  int64             `json:"callCountPrior,omitempty"`
	DauPrior        int64             `json:"dauPrior,omitempty"`
	SpendSeries     []DailyMetric     `json:"spendSeries,omitempty"`
	CallCountSeries []DailyMetric     `json:"callCountSeries,omitempty"`
	DauSeries       []DailyMetric     `json:"dauSeries,omitempty"`
	LastCalls       []LLMCallSnapshot `json:"lastCalls"`
	CacheMetrics    *CacheMetrics     `json:"cacheMetrics,omitempty"`
	GeneratedAt     time.Time         `json:"generatedAt"`
}

// CacheMetrics summarises Anthropic prompt-cache effectiveness over
// the selected period. Aggregated from llm_calls.cacheReadTokens /
// cacheWriteTokens. All zero when no Anthropic calls hit the period
// (common in Ollama-only dev environments) — handler omits the field
// in that case via the *CacheMetrics indirection.
type CacheMetrics struct {
	HitRate     float64 `json:"hitRate"`
	ReadTokens  int64   `json:"readTokens"`
	WriteTokens int64   `json:"writeTokens"`
	SavingsUSD  float64 `json:"savingsUsd"`
}

// LLMCallSnapshot is the trimmed view of an llm_calls row used in the
// "recent activity" feed. Full row shape lives in observability/llmcalls.go;
// admin doesn't import observability to avoid a dependency loop, so we
// re-shape via a Mongo projection.
type LLMCallSnapshot struct {
	ID               string    `bson:"_id" json:"id"`
	UserID           string    `bson:"userId" json:"userId"`
	UserEmail        string    `bson:"-" json:"userEmail,omitempty"` // resolved server-side
	Provider         string    `bson:"provider" json:"provider"`
	Model            string    `bson:"model" json:"model"`
	Feature          string    `bson:"feature" json:"feature"`
	CostUSD          float64   `bson:"costUsd" json:"costUsd"`
	DurationMs       int64     `bson:"durationMs" json:"durationMs"`
	Status           string    `bson:"status" json:"status"`
	CacheReadTokens  int64     `bson:"cacheReadTokens" json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64     `bson:"cacheWriteTokens" json:"cacheWriteTokens,omitempty"`
	CreatedAt        time.Time `bson:"createdAt" json:"createdAt"`
}

// OverviewRepository reads aggregates from llm_calls + users.
type OverviewRepository interface {
	// PeriodMetrics returns headline + prior totals for the given
	// window. start is inclusive, end exclusive.
	PeriodMetrics(ctx context.Context, start, end time.Time) (spendUSD float64, callCount int64, err error)
	// DailySeries returns one zero-filled 30-day spark series for
	// each of {spend, callCount, distinctUsers}, oldest-first.
	DailySeries(ctx context.Context, now time.Time) (spend, callCount, dau []DailyMetric, err error)
	// RecentLLMCalls returns the last n calls regardless of user.
	// userEmail field is left empty here; the handler joins it.
	RecentLLMCalls(ctx context.Context, n int) ([]LLMCallSnapshot, error)
	// ApproxDAU — distinct users active since `since`.
	ApproxDAU(ctx context.Context, since time.Time) (int64, error)
	// ApproxDAUBetween — distinct users active in [from, to). Used
	// to compute prior-period DAU without the overlap bug that bit
	// the previous (ApproxDAU(48h) - ApproxDAU(24h)) approach.
	// See the implementation comment for the data-model caveat.
	ApproxDAUBetween(ctx context.Context, from, to time.Time) (int64, error)
	// EmailsForUserIDs resolves user IDs to email addresses. Used
	// to decorate LLMCallSnapshot rows in the recent-activity feed.
	// Returns map[userID]email — IDs not found are absent from the map.
	EmailsForUserIDs(ctx context.Context, ids []string) (map[string]string, error)
	// CacheMetricsFor aggregates Anthropic prompt-cache effectiveness
	// over [start, end). Returns nil when no Anthropic calls in the
	// window — the handler omits the field in OverviewMetrics rather
	// than emitting all-zero rows.
	CacheMetricsFor(ctx context.Context, start, end time.Time) (*CacheMetrics, error)
}

// OverviewMongoRepository implements OverviewRepository against the
// shared Mongo cluster. Reads only.
type OverviewMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewOverviewMongoRepository constructs the repo.
func NewOverviewMongoRepository(client *mongo.Client, dbName string) *OverviewMongoRepository {
	return &OverviewMongoRepository{client: client, dbName: dbName}
}

func (r *OverviewMongoRepository) llmCallsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("llm_calls")
}

func (r *OverviewMongoRepository) usersCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("users")
}

// PeriodMetrics aggregates llm_calls.costUsd + count over [start, end).
// One $group pipeline; cheaper than two separate calls.
func (r *OverviewMongoRepository) PeriodMetrics(ctx context.Context, start, end time.Time) (float64, int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": start, "$lt": end},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"spend": bson.M{"$sum": "$costUsd"},
			"count": bson.M{"$sum": 1},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return 0, 0, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		Spend float64 `bson:"spend"`
		Count int64   `bson:"count"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	return rows[0].Spend, rows[0].Count, nil
}

// DailySeries returns three 30-day spark arrays. Single aggregation
// pipeline grouping by date string ($dateToString) so we get one
// round-trip not 90.
func (r *OverviewMongoRepository) DailySeries(ctx context.Context, now time.Time) ([]DailyMetric, []DailyMetric, []DailyMetric, error) {
	startOfWindow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-29 * 24 * time.Hour)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": startOfWindow},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt", "timezone": "UTC"}},
			"spend": bson.M{"$sum": "$costUsd"},
			"count": bson.M{"$sum": 1},
			"users": bson.M{"$addToSet": "$userId"},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, nil, nil, err
	}
	defer cur.Close(ctx)

	type bucket struct {
		ID    string   `bson:"_id"`
		Spend float64  `bson:"spend"`
		Count int64    `bson:"count"`
		Users []string `bson:"users"`
	}
	var rows []bucket
	if err := cur.All(ctx, &rows); err != nil {
		return nil, nil, nil, err
	}

	// Index by date for zero-fill iteration.
	byDate := make(map[string]bucket, len(rows))
	for _, b := range rows {
		byDate[b.ID] = b
	}

	// Walk forward 30 days from start, zero-fill missing days.
	spend := make([]DailyMetric, 0, 30)
	count := make([]DailyMetric, 0, 30)
	dau := make([]DailyMetric, 0, 30)
	for i := 0; i < 30; i++ {
		d := startOfWindow.Add(time.Duration(i) * 24 * time.Hour)
		key := d.Format("2006-01-02")
		b := byDate[key] // zero-valued struct when missing — exactly what we want
		spend = append(spend, DailyMetric{Date: key, Value: b.Spend})
		count = append(count, DailyMetric{Date: key, Value: float64(b.Count)})
		dau = append(dau, DailyMetric{Date: key, Value: float64(len(b.Users))})
	}
	return spend, count, dau, nil
}

// RecentLLMCalls returns the last n calls. Email join happens in
// the handler — repo stays focused on the call data.
func (r *OverviewMongoRepository) RecentLLMCalls(ctx context.Context, n int) ([]LLMCallSnapshot, error) {
	if n <= 0 || n > 100 {
		n = 10
	}
	cur, err := r.llmCallsCol().Find(
		ctx,
		bson.M{},
		findOpts().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(n)),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []LLMCallSnapshot
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ApproxDAU returns the count of distinct user_ids whose users
// document was updated since `since`. Heuristic.
func (r *OverviewMongoRepository) ApproxDAU(ctx context.Context, since time.Time) (int64, error) {
	count, err := r.usersCol().CountDocuments(ctx, bson.M{
		"updatedAt": bson.M{"$gte": since},
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ApproxDAUBetween returns distinct user_ids whose latest updatedAt
// lies in the half-open interval [from, to). Used for prior-period
// DAU on the dashboard.
//
// Data-model caveat (worth understanding when reading the chart):
// the users collection only carries a single updatedAt — the
// *latest* activity. A user who was active both yesterday and
// today shows updatedAt=today and is therefore excluded from the
// yesterday window. The number this method returns is "users last
// seen in the window", not "users seen in the window."
//
// At our volume that systematically undercounts prior-period DAU
// by the daily returning-users population; the chart still
// communicates direction correctly, but the magnitude reads low.
// A real fix needs a per-day activity rollup (lands with the
// events pipeline in P2-02 / mootd-admin#19) — until then this is
// the cleanest approximation, and importantly it doesn't have the
// double-count overlap bug the previous
// ApproxDAU(48h) - ApproxDAU(24h) approach did.
func (r *OverviewMongoRepository) ApproxDAUBetween(ctx context.Context, from, to time.Time) (int64, error) {
	if !to.After(from) {
		return 0, nil
	}
	count, err := r.usersCol().CountDocuments(ctx, bson.M{
		"updatedAt": bson.M{"$gte": from, "$lt": to},
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// EmailsForUserIDs resolves a small list of user IDs to emails. One
// $in query per call; suitable for the recent-calls feed (≤10 ids)
// and the per-page traces decoration.
func (r *OverviewMongoRepository) EmailsForUserIDs(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	cur, err := r.usersCol().Find(
		ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		findOpts().SetProjection(bson.M{"_id": 1, "email": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		ID    string `bson:"_id"`
		Email string `bson:"email"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Email
	}
	return out, nil
}

// CacheMetricsFor aggregates Anthropic prompt-cache stats over
// [start, end). Single $group pipeline; rides on the existing
// (createdAt) index. Returns nil when no Anthropic calls in the
// window so the handler can omit the field entirely.
//
// Hit rate definition:
//
//	cacheRead / (cacheRead + cacheWrite + uncachedInput)
//
// uncachedInput = inputTokens - cacheReadTokens (cache reads are
// already counted in inputTokens by both Anthropic and our recorder,
// per the v3 schema). Healthy is roughly 0.6–0.8.
//
// Savings:
//
//	readTokens × (full_input_price - cache_read_price)
//
// We approximate full_input_price at $3/Mtok and cache_read_price at
// $0.30/Mtok (the Sonnet 4.5 numbers from model_prices). A future
// version can join model_prices for exact pricing per row, but the
// approximation is within ~5% of true at our model mix today.
func (r *OverviewMongoRepository) CacheMetricsFor(ctx context.Context, start, end time.Time) (*CacheMetrics, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt":        bson.M{"$gte": start, "$lt": end},
			"provider":         "anthropic",
			"cacheReadTokens":  bson.M{"$exists": true},
			"cacheWriteTokens": bson.M{"$exists": true},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":         nil,
			"readTokens":  bson.M{"$sum": "$cacheReadTokens"},
			"writeTokens": bson.M{"$sum": "$cacheWriteTokens"},
			"inputTokens": bson.M{"$sum": "$inputTokens"},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		ReadTokens  int64 `bson:"readTokens"`
		WriteTokens int64 `bson:"writeTokens"`
		InputTokens int64 `bson:"inputTokens"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	if row.ReadTokens == 0 && row.WriteTokens == 0 {
		// No cache activity; skip the row entirely so the dashboard
		// hides the tile rather than showing a sad zero.
		return nil, nil
	}

	// uncached portion of input. inputTokens already includes cache
	// reads in the recorder (see observability/recorder.go), so
	// subtract to isolate the un-cached input.
	uncached := row.InputTokens - row.ReadTokens
	if uncached < 0 {
		uncached = 0
	}
	denom := float64(row.ReadTokens + row.WriteTokens + uncached)
	hitRate := 0.0
	if denom > 0 {
		hitRate = float64(row.ReadTokens) / denom
	}

	// Approximate Sonnet 4.5 pricing. See model_prices for exact
	// numbers; we live with ~5% error for the dashboard tile.
	const fullInputPerMTok = 3.0
	const cacheReadPerMTok = 0.30
	const million = 1_000_000.0
	savings := float64(row.ReadTokens) / million * (fullInputPerMTok - cacheReadPerMTok)

	return &CacheMetrics{
		HitRate:     hitRate,
		ReadTokens:  row.ReadTokens,
		WriteTokens: row.WriteTokens,
		SavingsUSD:  savings,
	}, nil
}

// errInvalidOverview is returned by the handler's parser when a query
// parameter is malformed. Public so handler tests can expect it.
var errInvalidOverview = errors.New("admin: invalid overview query")

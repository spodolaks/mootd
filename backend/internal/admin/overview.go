package admin

import (
	"context"
	"sort"
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

// SpendByFeatureSeries is one 30-day spend spark for a single
// feature label (mootd-admin#94). Series is zero-filled to 30
// rows oldest-first; a feature with no calls in the window
// still gets 30 zeroes so the FE's stack ordering stays stable.
type SpendByFeatureSeries struct {
	Feature string        `json:"feature"`
	Series  []DailyMetric `json:"series"`
}

// SinceDelta is the "what changed since admin's last visit"
// summary (mootd-admin#97). Computed fresh on every overview
// request that carries `since=<RFC-3339>`. Numbers cover the
// half-open interval [since, now); SpendChangePct compares
// that window's spend to an equal-length window immediately
// before it.
type SinceDelta struct {
	Since           time.Time `json:"since"`
	DurationMinutes int64     `json:"durationMinutes"`
	NewErrors       int64     `json:"newErrors"`
	NewSignups      int64     `json:"newSignups"`
	SpendUSD        float64   `json:"spendUsd"`
	// SpendChangePct is (current / prior) - 1, expressed as a
	// fraction (0.18 = +18%). Omitted when the prior window's
	// spend is zero (avoids divide-by-zero + infinite values
	// the FE would have to special-case).
	SpendChangePct  *float64 `json:"spendChangePct,omitempty"`
	OverBudgetUsers int64    `json:"overBudgetUsers,omitempty"`
}

// OverviewMetrics is the wire shape returned by GET /admin/v1/overview.
//
// Renamed from the previous spendUsdToday/callCountToday scalars to
// spendUsd/callCount because the value now reflects the chosen
// period — not always today. dauApprox stays today-only (it's the
// "who is here right now" signal, period-independent).
type OverviewMetrics struct {
	Period          OverviewPeriod `json:"period"`
	SpendUSD        float64        `json:"spendUsd"`
	CallCount       int64          `json:"callCount"`
	DauApprox       int64          `json:"dauApprox"`
	SpendUSDPrior   float64        `json:"spendUsdPrior,omitempty"`
	CallCountPrior  int64          `json:"callCountPrior,omitempty"`
	DauPrior        int64          `json:"dauPrior,omitempty"`
	SpendSeries     []DailyMetric  `json:"spendSeries,omitempty"`
	CallCountSeries []DailyMetric  `json:"callCountSeries,omitempty"`
	DauSeries       []DailyMetric  `json:"dauSeries,omitempty"`
	// SpendSeriesByFeature is one zero-filled 30-day spark per
	// feature ordered by total spend desc (mootd-admin#94).
	// Sum across features equals SpendSeries day-for-day. Used
	// to render a stacked-area on the spend KPI so a price
	// change or feature regression is visible at a glance.
	SpendSeriesByFeature []SpendByFeatureSeries `json:"spendSeriesByFeature,omitempty"`
	// SinceLastVisit is the "what changed since I last looked"
	// summary (mootd-admin#97). Populated only when the request
	// carries a `since=<RFC-3339>` query param. When the
	// caller's lastActiveAt is too recent or too old (heuristic
	// gate inside the handler) the field is omitted.
	SinceLastVisit *SinceDelta       `json:"sinceLastVisit,omitempty"`
	LastCalls      []LLMCallSnapshot `json:"lastCalls"`
	CacheMetrics   *CacheMetrics     `json:"cacheMetrics,omitempty"`
	GeneratedAt    time.Time         `json:"generatedAt"`
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
	// DailySeriesByFeature returns per-feature spend sparks for
	// the trailing 30 days (mootd-admin#94). Ordered by total
	// period spend desc; capped at maxFeatures (caller hint).
	// Sum across the returned series equals DailySeries.spend
	// day-for-day.
	DailySeriesByFeature(ctx context.Context, now time.Time, maxFeatures int) ([]SpendByFeatureSeries, error)
	// SinceMetrics aggregates "what happened in [since, now)"
	// for the since-last-visit callout (mootd-admin#97). The
	// caller computes the prior window (immediately before
	// `since`) and supplies its endpoints separately so the
	// repo doesn't need to know about prior-window heuristics.
	SinceMetrics(ctx context.Context, since, now time.Time) (newErrors, newSignups int64, spendUSD float64, err error)
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

// DailySeriesByFeature aggregates per-feature spend across the
// trailing 30 days (mootd-admin#94). Single Mongo round-trip:
// $group by (feature, day-bucket) → in-Go pivot to one series
// per feature, zero-filled.
//
// Cardinality control: features past `maxFeatures` (after
// sorting by total spend desc) collapse into a synthetic
// "other" series so an explosion in distinct feature names
// doesn't blow up the response. Pass 0 / negative to use the
// default cap.
func (r *OverviewMongoRepository) DailySeriesByFeature(ctx context.Context, now time.Time, maxFeatures int) ([]SpendByFeatureSeries, error) {
	if maxFeatures <= 0 {
		maxFeatures = 8
	}
	startOfWindow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-29 * 24 * time.Hour)

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": startOfWindow},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"feature": "$feature",
				"day":     bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt", "timezone": "UTC"}},
			},
			"spend": bson.M{"$sum": "$costUsd"},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	type row struct {
		ID struct {
			Feature string `bson:"feature"`
			Day     string `bson:"day"`
		} `bson:"_id"`
		Spend float64 `bson:"spend"`
	}
	var rows []row
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}

	// Pivot: feature → day → spend.
	totals := make(map[string]float64)
	byFeature := make(map[string]map[string]float64)
	for _, x := range rows {
		feat := x.ID.Feature
		if feat == "" {
			feat = "unknown"
		}
		totals[feat] += x.Spend
		if byFeature[feat] == nil {
			byFeature[feat] = map[string]float64{}
		}
		byFeature[feat][x.ID.Day] += x.Spend
	}

	// Sort features by total desc, deterministic on ties via
	// alphabetical to keep the stack order stable across
	// requests (recharts re-orders arbitrarily otherwise).
	type rank struct {
		feat  string
		total float64
	}
	ranked := make([]rank, 0, len(totals))
	for f, t := range totals {
		ranked = append(ranked, rank{feat: f, total: t})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].total != ranked[j].total {
			return ranked[i].total > ranked[j].total
		}
		return ranked[i].feat < ranked[j].feat
	})

	// Walk forward 30 days, build zero-filled series for each
	// top-N feature. Anything past N folds into "other".
	keys := make([]string, 30)
	for i := 0; i < 30; i++ {
		keys[i] = startOfWindow.Add(time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
	}

	out := make([]SpendByFeatureSeries, 0, maxFeatures+1)
	for i, r := range ranked {
		if i >= maxFeatures {
			break
		}
		series := make([]DailyMetric, 30)
		for j, k := range keys {
			series[j] = DailyMetric{Date: k, Value: byFeature[r.feat][k]}
		}
		out = append(out, SpendByFeatureSeries{Feature: r.feat, Series: series})
	}
	if len(ranked) > maxFeatures {
		other := make([]DailyMetric, 30)
		for i, r := range ranked[maxFeatures:] {
			for j, k := range keys {
				other[j].Date = k
				other[j].Value += byFeature[r.feat][k]
			}
			_ = i
		}
		out = append(out, SpendByFeatureSeries{Feature: "other", Series: other})
	}
	return out, nil
}

// SinceMetrics aggregates the cheap "what happened recently"
// numbers for mootd-admin#97. Three independent counts so the
// caller can decide which to show. Two Mongo round-trips:
//
//   - llm_calls match `createdAt ∈ [since, now)` → spend total
//   - error count via $facet.
//   - events match `name=signed_up` in the same window → signup
//     count.
//
// `over_budget_users` lives elsewhere (budget package) and is
// joined inside the handler when the budget reader is wired.
func (r *OverviewMongoRepository) SinceMetrics(ctx context.Context, since, now time.Time) (int64, int64, float64, error) {
	if !since.Before(now) {
		return 0, 0, 0, nil
	}
	// Single $facet pipeline: counts + sum of cost in one round.
	llmPipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"createdAt": bson.M{"$gte": since, "$lt": now},
		}}},
		{{Key: "$facet", Value: bson.M{
			"errors": bson.A{
				bson.M{"$match": bson.M{"status": "error"}},
				bson.M{"$count": "n"},
			},
			"spend": bson.A{
				bson.M{"$group": bson.M{
					"_id":   nil,
					"total": bson.M{"$sum": "$costUsd"},
				}},
			},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, llmPipeline)
	if err != nil {
		return 0, 0, 0, err
	}
	defer cur.Close(ctx)

	type facetResult struct {
		Errors []struct {
			N int64 `bson:"n"`
		} `bson:"errors"`
		Spend []struct {
			Total float64 `bson:"total"`
		} `bson:"spend"`
	}
	var rows []facetResult
	if err := cur.All(ctx, &rows); err != nil {
		return 0, 0, 0, err
	}
	var newErrors int64
	var spendUSD float64
	if len(rows) > 0 {
		if len(rows[0].Errors) > 0 {
			newErrors = rows[0].Errors[0].N
		}
		if len(rows[0].Spend) > 0 {
			spendUSD = rows[0].Spend[0].Total
		}
	}

	// Signups in the window — counts events with name=signed_up.
	signupCount, err := r.client.Database(r.dbName).Collection("events").CountDocuments(ctx, bson.M{
		"name":      "signed_up",
		"createdAt": bson.M{"$gte": since, "$lt": now},
	})
	if err != nil {
		// Non-fatal: keep the other counts.
		signupCount = 0
	}
	return newErrors, signupCount, spendUSD, nil
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

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ────────────────────────────────────────────────────────────────────
// Eval suite (P3-04 / mootd-admin#27).
//
// Pieces, top-down:
//
//   types               — wire shapes echoed by the admin API
//   EvalSetSummary      — header for one golden set on disk
//   EvalRun / Case      — persisted state for a single run
//
//   EvalsRepository     — Mongo CRUD for the eval_runs collection
//   EvalSetLoader       — interface for "find golden tuples on disk".
//                          The default is FilesystemSetLoader; tests
//                          (and a future upload UI) can swap it.
//   EvalRunner          — the live-mode runner: loads tuples,
//                          spawns goroutines, calls the generator
//                          per case, calls the judge after, writes
//                          progress to Mongo, computes aggregate.
//
//   EvalJudge           — Anthropic Claude client used as LLM-as-judge.
//                          Separate from outfit.Generator because the
//                          judge prompt is unrelated to wardrobe
//                          generation and we want to keep the dep
//                          surface tiny.
//
// The runner is async — POST /admin/v1/evals/runs returns 202 with a
// runId immediately and a goroutine takes it from there. The runId
// is what the FE polls to render progress + results.
// ────────────────────────────────────────────────────────────────────

const (
	evalRunsCollection = "eval_runs"

	// Worker pool size for case fan-out. Each case is two LLM
	// round-trips (generator + judge); 5 concurrent works out to
	// roughly 10 LLM calls in flight at peak — comfortably inside
	// Anthropic's per-key rate limits without burning tokens
	// faster than Claude can stream them.
	evalRunnerParallelism = 5

	// Per-run hard timeout. The acceptance criterion is "30 cases
	// under 2 minutes"; 5 minutes leaves headroom for cold starts +
	// cases that hit the LLM tail-latency.
	evalRunHardTimeout = 5 * time.Minute
)

// EvalRunStatus is the lifecycle of a single run.
type EvalRunStatus string

const (
	EvalStatusPending    EvalRunStatus = "pending"
	EvalStatusProcessing EvalRunStatus = "processing"
	EvalStatusCompleted  EvalRunStatus = "completed"
	EvalStatusFailed     EvalRunStatus = "failed"
)

// EvalCaseStatus is the per-case outcome.
type EvalCaseStatus string

const (
	EvalCaseStatusPending EvalCaseStatus = "pending"
	EvalCaseStatusSuccess EvalCaseStatus = "success"
	EvalCaseStatusFailed  EvalCaseStatus = "failed"
)

// EvalCaseResult mirrors the spec shape. Unfortunately we can't
// just reuse the generated types from `gen/` because the gen layer
// emits string-typed enums as their own named string types, which
// would force every field assignment through a cast. Hand-written
// keeps the call sites readable.
type EvalCaseResult struct {
	CaseID                string         `bson:"caseId"          json:"caseId"`
	Status                EvalCaseStatus `bson:"status"          json:"status"`
	JudgeScore            int            `bson:"judgeScore,omitempty"            json:"judgeScore,omitempty"`
	JudgeRationale        string         `bson:"judgeRationale,omitempty"        json:"judgeRationale,omitempty"`
	AutomatedChecksPassed int            `bson:"automatedChecksPassed,omitempty" json:"automatedChecksPassed,omitempty"`
	AutomatedChecksTotal  int            `bson:"automatedChecksTotal,omitempty"  json:"automatedChecksTotal,omitempty"`
	OutfitName            string         `bson:"outfitName,omitempty"            json:"outfitName,omitempty"`
	OutfitJSON            string         `bson:"outfitJson,omitempty"            json:"outfitJSON,omitempty"`
	Error                 string         `bson:"error,omitempty"                 json:"error,omitempty"`
	DurationMs            int64          `bson:"durationMs,omitempty"            json:"durationMs,omitempty"`
	CostUSD               float64        `bson:"costUsd,omitempty"               json:"costUsd,omitempty"`
}

// EvalRunAggregate is the rollup the FE renders without re-summing.
type EvalRunAggregate struct {
	TotalCases     int     `bson:"totalCases"        json:"totalCases"`
	CompletedCases int     `bson:"completedCases"    json:"completedCases"`
	PassedCases    int     `bson:"passedCases,omitempty"    json:"passedCases,omitempty"`
	AvgJudgeScore  float64 `bson:"avgJudgeScore,omitempty"  json:"avgJudgeScore,omitempty"`
	TotalCostUSD   float64 `bson:"totalCostUsd,omitempty"   json:"totalCostUsd,omitempty"`
}

// EvalRun is the eval_runs collection row.
type EvalRun struct {
	ID            string           `bson:"_id"                       json:"id"`
	EvalSetID     string           `bson:"evalSetId"                 json:"evalSetId"`
	EvalSetName   string           `bson:"evalSetName,omitempty"     json:"evalSetName,omitempty"`
	Status        EvalRunStatus    `bson:"status"                    json:"status"`
	Provider      string           `bson:"provider,omitempty"        json:"provider,omitempty"`
	Model         string           `bson:"model,omitempty"           json:"model,omitempty"`
	PromptVersion string           `bson:"promptVersion,omitempty"   json:"promptVersion,omitempty"`
	CreatedBy     string           `bson:"createdBy,omitempty"       json:"createdBy,omitempty"`
	CreatedAt     time.Time        `bson:"createdAt"                 json:"createdAt"`
	StartedAt     *time.Time       `bson:"startedAt,omitempty"       json:"startedAt,omitempty"`
	CompletedAt   *time.Time       `bson:"completedAt,omitempty"     json:"completedAt,omitempty"`
	Cases         []EvalCaseResult `bson:"cases,omitempty"           json:"cases,omitempty"`
	Aggregate     EvalRunAggregate `bson:"aggregate"                 json:"aggregate"`
	Error         string           `bson:"error,omitempty"           json:"error,omitempty"`
}

// EvalsRepository wraps the eval_runs collection.
type EvalsRepository interface {
	Create(ctx context.Context, run EvalRun) error
	Get(ctx context.Context, id string) (*EvalRun, error)
	List(ctx context.Context, cursor string, limit int) ([]EvalRun, string, error)
	UpdateProgress(ctx context.Context, id string, status EvalRunStatus, completed int, aggregate EvalRunAggregate, startedAt *time.Time) error
	UpsertCase(ctx context.Context, runID string, c EvalCaseResult) error
	Finalize(ctx context.Context, id string, status EvalRunStatus, completedAt time.Time, aggregate EvalRunAggregate, errMsg string) error
}

// EvalsMongoRepository is the production implementation.
type EvalsMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewEvalsMongoRepository ensures indexes + returns a ready repo.
func NewEvalsMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*EvalsMongoRepository, error) {
	r := &EvalsMongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("eval_runs_created_desc"),
		},
		{
			Keys:    bson.D{{Key: "evalSetId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("eval_runs_set_created"),
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure eval_runs indexes: %w", err)
	}
	return r, nil
}

func (r *EvalsMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection(evalRunsCollection)
}

func (r *EvalsMongoRepository) Create(ctx context.Context, run EvalRun) error {
	if run.ID == "" {
		return errors.New("admin: eval run id required")
	}
	_, err := r.col().InsertOne(ctx, run)
	return err
}

func (r *EvalsMongoRepository) Get(ctx context.Context, id string) (*EvalRun, error) {
	var doc EvalRun
	err := r.col().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (r *EvalsMongoRepository) List(ctx context.Context, cursor string, limit int) ([]EvalRun, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	filter := bson.M{}
	if cursor != "" {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	cur, err := r.col().Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		// Strip the cases array from list-view rows; the detail
		// endpoint refetches the full doc. Keeps the list response
		// O(n) in number-of-runs, not O(n*m) in cases-per-run.
		SetProjection(bson.M{"cases": 0}).
		SetLimit(int64(limit+1)),
	)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)
	var rows []EvalRun
	if err := cur.All(ctx, &rows); err != nil {
		return nil, "", err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	next := ""
	if hasMore && len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	return rows, next, nil
}

func (r *EvalsMongoRepository) UpdateProgress(ctx context.Context, id string, status EvalRunStatus, completed int, agg EvalRunAggregate, startedAt *time.Time) error {
	set := bson.M{
		"status":    status,
		"aggregate": agg,
	}
	if startedAt != nil {
		set["startedAt"] = startedAt
	}
	_ = completed // currently embedded in aggregate.CompletedCases, kept as separate arg for future progress nuance
	_, err := r.col().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

func (r *EvalsMongoRepository) UpsertCase(ctx context.Context, runID string, c EvalCaseResult) error {
	// Atomic upsert-into-array (#111 F2). The previous $pull-then-$push
	// was two separate updates: between them the case was absent from
	// the array, so a concurrent UpsertCase for a DIFFERENT case (the
	// runner writes 5 at a time) could interleave and drop or duplicate
	// entries. Instead: update the case in place if present, else push
	// it with a $ne guard — each statement is atomic and they never
	// touch a sibling element.
	res, err := r.col().UpdateOne(ctx,
		bson.M{"_id": runID, "cases.caseId": c.CaseID},
		bson.M{"$set": bson.M{"cases.$": c}},
	)
	if err != nil {
		return fmt.Errorf("eval upsert case set: %w", err)
	}
	if res.MatchedCount > 0 {
		return nil // updated in place
	}
	// Not present yet — push, guarding against a concurrent push of the
	// same caseId (the $ne fails to match once the element exists, so
	// the second writer is a no-op rather than a duplicate).
	if _, err := r.col().UpdateOne(ctx,
		bson.M{"_id": runID, "cases.caseId": bson.M{"$ne": c.CaseID}},
		bson.M{"$push": bson.M{"cases": c}},
	); err != nil {
		return fmt.Errorf("eval upsert case push: %w", err)
	}
	return nil
}

func (r *EvalsMongoRepository) Finalize(ctx context.Context, id string, status EvalRunStatus, completedAt time.Time, agg EvalRunAggregate, errMsg string) error {
	set := bson.M{
		"status":      status,
		"completedAt": completedAt,
		"aggregate":   agg,
	}
	if errMsg != "" {
		set["error"] = errMsg
	}
	_, err := r.col().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

// ────────────────────────────────────────────────────────────────────
// Set discovery (filesystem-based for v1).
// ────────────────────────────────────────────────────────────────────

// EvalSetLoader is the source of truth for "what eval sets exist
// and what cases do they contain." Today it reads from disk
// (backend/eval/golden/<id>/*.json) — the same files mootd#09e2277
// laid down for the CLI harness. A future ticket adds an upload
// UI + a Mongo-backed loader; this interface keeps that swap easy.
type EvalSetLoader interface {
	List(ctx context.Context) ([]EvalSetSummary, error)
	LoadTuples(ctx context.Context, setID string) ([]EvalTuple, error)
}

// EvalSetSummary mirrors the wire shape.
type EvalSetSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CaseCount   int    `json:"caseCount"`
	Description string `json:"description,omitempty"`
}

// EvalTuple is a thin DTO between the loader and the runner. We
// don't import the flat eval/ package's Tuple here to keep the
// dependency direction one-way (eval/ may import shared things;
// admin/ shouldn't import eval/). The runner is responsible for
// translating EvalTuple into eval.Tuple at the boundary.
type EvalTuple struct {
	ID            string           `json:"id"`
	Description   string           `json:"description,omitempty"`
	UserID        string           `json:"userId,omitempty"`
	Items         []map[string]any `json:"items"`
	Weather       map[string]any   `json:"weather,omitempty"`
	TopArchetypes []map[string]any `json:"topArchetypes,omitempty"`
	Expectations  *map[string]any  `json:"expectations,omitempty"`
}

// FilesystemSetLoader is the production loader. Pointed at
// `backend/eval/golden/`; each immediate subdirectory is a set,
// and each `.json` file inside is a case. A `_meta.json` file at
// the set root, when present, supplies a description.
type FilesystemSetLoader struct {
	root string
}

// NewFilesystemSetLoader points the loader at `root`. Pass the
// absolute path to `backend/eval/golden/`; we resolve sets as
// immediate children.
func NewFilesystemSetLoader(root string) *FilesystemSetLoader {
	return &FilesystemSetLoader{root: root}
}

func (l *FilesystemSetLoader) List(_ context.Context) ([]EvalSetSummary, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		// Non-fatal — treat "no golden dir" as "no sets to run."
		// The /sets endpoint will return an empty list and the FE
		// shows a friendly "no sets configured" notice.
		if errors.Is(err, os.ErrNotExist) {
			return []EvalSetSummary{}, nil
		}
		return nil, fmt.Errorf("list golden root: %w", err)
	}
	out := []EvalSetSummary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		setID := e.Name()
		setDir := filepath.Join(l.root, setID)
		count, desc := countAndDescribeSet(setDir)
		out = append(out, EvalSetSummary{
			ID:          setID,
			Name:        setID,
			CaseCount:   count,
			Description: desc,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (l *FilesystemSetLoader) LoadTuples(_ context.Context, setID string) ([]EvalTuple, error) {
	if setID == "" || strings.ContainsAny(setID, "/\\.") {
		return nil, errors.New("admin: invalid eval set id")
	}
	dir := filepath.Join(l.root, setID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load tuples: %w", err)
	}
	tuples := []EvalTuple{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "_meta.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var t EvalTuple
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("decode %s: %w", e.Name(), err)
		}
		if t.ID == "" {
			t.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		tuples = append(tuples, t)
	}
	sort.Slice(tuples, func(i, j int) bool { return tuples[i].ID < tuples[j].ID })
	return tuples, nil
}

func countAndDescribeSet(dir string) (int, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, ""
	}
	count := 0
	desc := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "_meta.json" {
			if raw, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
				var meta struct {
					Description string `json:"description"`
				}
				_ = json.Unmarshal(raw, &meta)
				desc = meta.Description
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") {
			count++
		}
	}
	return count, desc
}

// ────────────────────────────────────────────────────────────────────
// Generator + Judge interfaces — kept tiny so admin/ doesn't import
// the outfit package directly.
// ────────────────────────────────────────────────────────────────────

// EvalGenerator is the slice of outfit.Generator the runner uses.
// app/ wires a real outfit.Generator to satisfy this. The interface
// is deliberately untyped (map[string]any tuple in, JSON-encoded
// outfits + cost out) so admin/ doesn't drag in the outfit package.
type EvalGenerator interface {
	// GenerateForEval runs the LLM with the same prompts the
	// production outfit service would build, and returns the
	// generated outfits as a JSON-encodable slice + the dollar cost
	// + a one-line outfit display name (the first outfit's `name`
	// field) for table-row use.
	GenerateForEval(ctx context.Context, tuple EvalTuple) (outfitsJSON string, primaryName string, automatedChecksPassed int, automatedChecksTotal int, costUSD float64, err error)

	// Provider + Model + PromptVersion echo what the wire reports.
	// Captured per-run so the run row records exactly which
	// generator config was tested.
	ProviderName() string
	ModelName() string
	PromptVersionName() string
}

// EvalJudge rates a generated outfit 1-5 against the case's
// expectations and returns a one-paragraph rationale. The judge is
// independent of the generator — same provider in production today
// (Claude), but we explicitly model the dependency boundary so a
// future ticket can swap it (e.g. to GPT-4 for an independent
// second opinion).
type EvalJudge interface {
	Score(ctx context.Context, tuple EvalTuple, outfitsJSON string) (score int, rationale string, costUSD float64, err error)
}

// ────────────────────────────────────────────────────────────────────
// The Anthropic-backed judge.
// ────────────────────────────────────────────────────────────────────

// AnthropicJudge talks to api.anthropic.com directly. Deliberately
// minimal — we don't need streaming or tool use, just one
// JSON-shaped completion per case.
type AnthropicJudge struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicJudge constructs the judge from env. Returns nil
// when ANTHROPIC_API_KEY isn't set so the runner can fall back to
// "no judge — automated checks only" instead of failing the run.
func NewAnthropicJudge() *AnthropicJudge {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil
	}
	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &AnthropicJudge{
		apiKey: key,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Score asks Claude to rate the outfit on a 1-5 scale. Prompt
// reads as a strict rubric so the model can't drift into vague
// praise — see promptJudgeSystem below for the rationale.
func (j *AnthropicJudge) Score(ctx context.Context, tuple EvalTuple, outfitsJSON string) (int, string, float64, error) {
	if j == nil {
		return 0, "", 0, errors.New("admin: judge not configured")
	}
	system := promptJudgeSystem
	user := buildJudgeUserPrompt(tuple, outfitsJSON)

	body, _ := json.Marshal(map[string]any{
		"model":      j.model,
		"max_tokens": 600,
		"system":     system,
		"messages": []map[string]any{
			{"role": "user", "content": user},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return 0, "", 0, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", j.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := j.client.Do(req)
	if err != nil {
		return 0, "", 0, fmt.Errorf("judge call: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, "", 0, fmt.Errorf("judge call: status=%d body=%s", resp.StatusCode, truncateForLog(raw, 200))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return 0, "", 0, fmt.Errorf("judge response decode: %w", err)
	}
	if len(parsed.Content) == 0 {
		return 0, "", 0, errors.New("judge response empty")
	}
	text := strings.TrimSpace(parsed.Content[0].Text)

	// Parse the model's JSON response. The system prompt nails the
	// shape, but Claude sometimes wraps JSON in prose ("Here's my
	// rating: { ... }") — we strip everything outside the first
	// {} block to be tolerant.
	score, rationale := parseJudgeResponse(text)
	cost := approxAnthropicCostUSD(j.model, parsed.Usage.InputTokens, parsed.Usage.OutputTokens)
	return score, rationale, cost, nil
}

// promptJudgeSystem is the rubric. Heavy on the constraints
// because what we want most is consistency across cases — a
// permissive "rate from 1-5 how good this outfit is" gives near-
// random scores. Anchoring each score to specific behaviours
// makes the rating reproducible.
const promptJudgeSystem = `You are an expert evaluator of fashion outfits. Your job is to rate one outfit on a 1-5 scale and explain your reasoning in two short sentences.

Use this exact rubric:
- 5 = Outfit fully respects the expected archetype, weather, and constraints. Items are coherent. No banned-style language or generic filler.
- 4 = Outfit respects the expectations and is coherent. One small issue (a slightly off color, a minor item-count drift).
- 3 = Outfit is on-brief but has a clear weakness (e.g. one item conflicts with weather, or the rationale is generic).
- 2 = Outfit drifts from the expected archetype OR breaks a hard constraint (weather, item count). Coherence is mediocre.
- 1 = Outfit is incoherent, ignores the brief, includes banned/generic style language, or contains injection-leak text.

Respond with strict JSON in this exact shape and nothing else:
{"score": <integer 1-5>, "rationale": "<one to two sentences>"}

Do not include any prose before or after the JSON.`

// buildJudgeUserPrompt produces the per-case prompt body. We
// embed the case + outfit verbatim — the judge needs to see the
// full output to rate it, and there's no need to redact anything
// here since the inputs are admin-curated golden cases.
func buildJudgeUserPrompt(t EvalTuple, outfitsJSON string) string {
	var b strings.Builder
	b.WriteString("CASE_ID: " + t.ID + "\n")
	if t.Description != "" {
		b.WriteString("DESCRIPTION: " + t.Description + "\n")
	}
	if len(t.TopArchetypes) > 0 {
		raw, _ := json.Marshal(t.TopArchetypes)
		b.WriteString("EXPECTED_ARCHETYPES: " + string(raw) + "\n")
	}
	if len(t.Weather) > 0 {
		raw, _ := json.Marshal(t.Weather)
		b.WriteString("WEATHER: " + string(raw) + "\n")
	}
	if t.Expectations != nil {
		raw, _ := json.Marshal(*t.Expectations)
		b.WriteString("CONSTRAINTS: " + string(raw) + "\n")
	}
	b.WriteString("ITEMS: " + asJSON(t.Items) + "\n\n")
	b.WriteString("LLM_GENERATED_OUTFIT:\n")
	b.WriteString(outfitsJSON)
	b.WriteString("\n\nRate this outfit. Respond with JSON only.")
	return b.String()
}

func asJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// parseJudgeResponse extracts {score, rationale} from a text blob.
// Tolerates JSON wrapped in prose ("Here's the rating: {...}").
func parseJudgeResponse(text string) (int, string) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < 0 || end <= start {
		return 0, "judge response unparseable: " + truncateString(text, 120)
	}
	chunk := text[start : end+1]
	var parsed struct {
		Score     int    `json:"score"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(chunk), &parsed); err != nil {
		return 0, "judge response decode failed: " + err.Error()
	}
	if parsed.Score < 1 || parsed.Score > 5 {
		return 0, "judge returned out-of-range score: " + truncateString(text, 120)
	}
	return parsed.Score, parsed.Rationale
}

// approxAnthropicCostUSD is a hard-coded ballpark for the judge's
// cost — close enough for "did this run cost $0.10 or $1.00?" but
// not the ledger of record. The real ledger lives on llm_calls;
// the judge calls bypass that ledger today (admin-triggered
// observability data), so this approximation is intentional.
func approxAnthropicCostUSD(model string, inputTokens, outputTokens int) float64 {
	// Sonnet 4 list price as of 2025: $3 / million input, $15 /
	// million output. Recompute from real prices when this matters.
	in := 3.0 / 1_000_000
	out := 15.0 / 1_000_000
	if strings.Contains(model, "haiku") {
		in = 0.8 / 1_000_000
		out = 4.0 / 1_000_000
	} else if strings.Contains(model, "opus") {
		in = 15.0 / 1_000_000
		out = 75.0 / 1_000_000
	}
	return float64(inputTokens)*in + float64(outputTokens)*out
}

// ────────────────────────────────────────────────────────────────────
// Runner: orchestrates one async run end-to-end.
// ────────────────────────────────────────────────────────────────────

// EvalRunner is the live-mode runner. NewEvalRunner wires the deps;
// Start kicks off a goroutine.
type EvalRunner struct {
	repo      EvalsRepository
	loader    EvalSetLoader
	generator EvalGenerator
	judge     EvalJudge
	logger    interface{ Printf(string, ...any) }
}

// NewEvalRunner constructs a runner. `judge` may be nil — when not
// configured, cases still complete and record automated-check
// counts; judgeScore is 0 and the FE renders "(no judge)" instead
// of a number.
func NewEvalRunner(
	repo EvalsRepository,
	loader EvalSetLoader,
	generator EvalGenerator,
	judge EvalJudge,
	logger interface{ Printf(string, ...any) },
) *EvalRunner {
	return &EvalRunner{repo: repo, loader: loader, generator: generator, judge: judge, logger: logger}
}

// Start creates an eval_runs row, returns the runId, and spawns a
// goroutine that drives the run. The goroutine uses a fresh
// background context (not the request context) so the run survives
// the HTTP exchange that kicked it off — same pattern as the outfit
// async generation.
func (r *EvalRunner) Start(ctx context.Context, evalSetID, promptVersion, createdBy string) (string, error) {
	if r == nil || r.repo == nil || r.loader == nil || r.generator == nil {
		return "", errors.New("admin: eval runner not wired")
	}

	// Pre-flight: load tuples now so we can return a sensible 4xx
	// if the set doesn't exist (instead of a "pending" run that
	// immediately fails).
	tuples, err := r.loader.LoadTuples(ctx, evalSetID)
	if err != nil {
		return "", fmt.Errorf("load tuples: %w", err)
	}
	if len(tuples) == 0 {
		return "", errors.New("admin: eval set has no cases")
	}

	runID := generateEvalRunID()
	now := time.Now().UTC()
	pending := make([]EvalCaseResult, len(tuples))
	for i, t := range tuples {
		pending[i] = EvalCaseResult{CaseID: t.ID, Status: EvalCaseStatusPending}
	}

	run := EvalRun{
		ID:            runID,
		EvalSetID:     evalSetID,
		EvalSetName:   evalSetID,
		Status:        EvalStatusPending,
		Provider:      r.generator.ProviderName(),
		Model:         r.generator.ModelName(),
		PromptVersion: r.generator.PromptVersionName(),
		CreatedBy:     createdBy,
		CreatedAt:     now,
		Cases:         pending,
		Aggregate: EvalRunAggregate{
			TotalCases: len(tuples),
		},
	}
	if promptVersion != "" {
		run.PromptVersion = promptVersion
	}

	if err := r.repo.Create(ctx, run); err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}

	// Detach from the request context — the run outlives the
	// caller's HTTP request.
	go r.execute(runID, tuples)
	return runID, nil
}

// execute is the goroutine body. Runs the cases concurrently with
// a worker-pool fan-out, writes per-case results as they land, and
// finalizes the aggregate at the end.
func (r *EvalRunner) execute(runID string, tuples []EvalTuple) {
	ctx, cancel := context.WithTimeout(context.Background(), evalRunHardTimeout)
	defer cancel()

	startedAt := time.Now().UTC()
	if err := r.repo.UpdateProgress(ctx, runID, EvalStatusProcessing, 0, EvalRunAggregate{TotalCases: len(tuples)}, &startedAt); err != nil {
		r.logger.Printf("eval %s: mark processing: %v", runID, err)
	}

	var (
		mu              sync.Mutex
		completed       int
		passed          int
		scoreSum        int
		scoreCount      int
		costAcc         float64
		caseResultsCopy = make([]EvalCaseResult, 0, len(tuples))
	)

	sem := make(chan struct{}, evalRunnerParallelism)
	var wg sync.WaitGroup

	for _, t := range tuples {
		wg.Add(1)
		sem <- struct{}{}
		go func(tuple EvalTuple) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if rec := recover(); rec != nil {
					r.logger.Printf("eval %s case %s: panic: %v", runID, tuple.ID, rec)
				}
			}()

			caseStart := time.Now()
			res := EvalCaseResult{CaseID: tuple.ID, Status: EvalCaseStatusFailed}

			outfitJSON, primaryName, checksPass, checksTotal, genCost, err := r.generator.GenerateForEval(ctx, tuple)
			res.AutomatedChecksPassed = checksPass
			res.AutomatedChecksTotal = checksTotal
			res.OutfitName = primaryName
			res.OutfitJSON = outfitJSON
			res.CostUSD += genCost

			if err != nil {
				res.Error = "generate: " + err.Error()
			} else if r.judge != nil && outfitJSON != "" {
				score, rationale, judgeCost, jerr := r.judge.Score(ctx, tuple, outfitJSON)
				res.CostUSD += judgeCost
				if jerr != nil {
					res.Error = "judge: " + jerr.Error()
				} else {
					res.JudgeScore = score
					res.JudgeRationale = rationale
				}
			}

			res.DurationMs = time.Since(caseStart).Milliseconds()
			if res.Error == "" {
				res.Status = EvalCaseStatusSuccess
			}

			mu.Lock()
			completed++
			caseResultsCopy = append(caseResultsCopy, res)
			if res.JudgeScore > 0 {
				scoreSum += res.JudgeScore
				scoreCount++
			}
			// "Passed" = automated checks all green AND judge ≥ 4.
			// When the judge is offline (judge == nil), fall back to
			// checks-only.
			if checksTotal > 0 && checksPass == checksTotal {
				if r.judge == nil || res.JudgeScore >= 4 {
					passed++
				}
			}
			costAcc += res.CostUSD
			snap := EvalRunAggregate{
				TotalCases:     len(tuples),
				CompletedCases: completed,
				PassedCases:    passed,
				TotalCostUSD:   costAcc,
			}
			if scoreCount > 0 {
				snap.AvgJudgeScore = float64(scoreSum) / float64(scoreCount)
			}
			mu.Unlock()

			if err := r.repo.UpsertCase(ctx, runID, res); err != nil {
				r.logger.Printf("eval %s case %s: upsert: %v", runID, tuple.ID, err)
			}
			// Pass snap.CompletedCases (captured under mu above) rather
			// than reading `completed` here outside the lock — that read
			// raced concurrent workers (#111 F3). Same value, no race.
			if err := r.repo.UpdateProgress(ctx, runID, EvalStatusProcessing, snap.CompletedCases, snap, nil); err != nil {
				r.logger.Printf("eval %s: progress update: %v", runID, err)
			}
		}(t)
	}
	wg.Wait()

	mu.Lock()
	finalAgg := EvalRunAggregate{
		TotalCases:     len(tuples),
		CompletedCases: completed,
		PassedCases:    passed,
		TotalCostUSD:   costAcc,
	}
	if scoreCount > 0 {
		finalAgg.AvgJudgeScore = float64(scoreSum) / float64(scoreCount)
	}
	mu.Unlock()

	if err := r.repo.Finalize(ctx, runID, EvalStatusCompleted, time.Now().UTC(), finalAgg, ""); err != nil {
		r.logger.Printf("eval %s: finalize: %v", runID, err)
	}
}

// generateEvalRunID emits a content-free run id. Reuses
// generateAuditID's underlying source — same readable hex format,
// same uniqueness guarantee.
func generateEvalRunID() string {
	return "eval_" + generateAuditID()[len("aud_"):]
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func truncateForLog(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// Package eval provides a regression-style harness for the LLM
// prompt + outfit-generation pipeline (mootd#59).
//
// The runner loads tuples from a golden set (testdata-style JSON
// files), feeds each through the prompt construction + parsing
// pipeline, and produces a comparison report against a saved
// baseline. Two run modes:
//
//   - mock (default): use a deterministic FakeGenerator that returns
//     hardcoded outfits. Becomes a smoke test that the prompt
//     construction + parsing layer didn't regress, without paying
//     any real LLM cost. Good for CI.
//
//   - live: call a real Generator (Anthropic / OpenAI / Ollama) and
//     measure quality. Used by humans iterating on the prompt.
//     Costs real money on every run; not for CI.
//
// The harness intentionally stays out of the main service binary —
// it lives under cmd/eval and imports outfit/prompts directly so it
// can probe the prompt without spinning up Mongo, Redis, or HTTP.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/outfit"
)

// Tuple is one entry in the golden set. Loaded from JSON.
type Tuple struct {
	ID            string                      `json:"id"`
	Description   string                      `json:"description"`
	UserID        string                      `json:"userID"`
	Items         []ItemSpec                  `json:"items"`
	TopArchetypes []archetype.ScoredArchetype `json:"topArchetypes"`
	Weather       outfit.Weather              `json:"weather"`
	Expectations  Expectations                `json:"expectations"`
}

// ItemSpec is a serializable wardrobe item for the golden set.
type ItemSpec struct {
	ID       string            `json:"id"`
	Category string            `json:"category"`
	Label    string            `json:"label"`
	Traits   map[string]string `json:"traits"`
}

// Expectations describes what a "good" outfit looks like for this
// tuple. The runner scores against these.
type Expectations struct {
	MinItems              int      `json:"minItems"`
	MaxItems              int      `json:"maxItems"`
	MustIncludeCategories []string `json:"mustIncludeCategories"`
	PreferTraits          []string `json:"preferTraits"`
	AvoidBannedWords      bool     `json:"avoidBannedWords"`
	// Adversarial expectations (injection-attempt tuple).
	MustNotContain        []string `json:"mustNotContain"`
	ExpectedRedactionMark string   `json:"expectedRedactionMarker"`
}

// LoadGoldenSet reads every *.json file in dir and returns the
// tuples sorted by id. Skips non-json files. Errors are wrapped with
// the offending filename for fast diagnostics.
func LoadGoldenSet(dir string) ([]Tuple, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := make([]Tuple, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var t Tuple
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Result is one tuple's run outcome.
type Result struct {
	TupleID       string          `json:"tupleId"`
	PromptVersion string          `json:"promptVersion"`
	SystemPrompt  string          `json:"systemPrompt"`
	UserMessage   string          `json:"userMessage"`
	SystemTokens  int             `json:"systemTokens"`
	UserTokens    int             `json:"userTokens"`
	DurationMs    int64           `json:"durationMs"`
	Outfits       []outfit.Outfit `json:"outfits,omitempty"`
	Error         string          `json:"error,omitempty"`
	Checks        []Check         `json:"checks"`
}

// Check is a single boolean expectation evaluated against a result.
type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// RunOptions controls a single run.
type RunOptions struct {
	Tuple     Tuple
	Generator outfit.Generator // injected; runner doesn't pick a provider
	Verbose   bool

	// PromptOverrides maps template name → body; when set, both the
	// prompt snapshot and the live generator call render with these
	// bodies instead of the production templates (used by the admin
	// eval runner to test a draft version before promotion). nil =
	// production templates.
	PromptOverrides map[string]string
}

// Run prompts construction + (optionally) the LLM, then runs checks.
// Never panics; all errors are encoded into Result.Error.
func Run(opts RunOptions) Result {
	r := Result{TupleID: opts.Tuple.ID, PromptVersion: outfit.PromptVersion}

	// Build the prompt the same way the production service would.
	items := make([]outfit.GenItem, 0, len(opts.Tuple.Items))
	for _, it := range opts.Tuple.Items {
		items = append(items, outfit.GenItem{
			ID:       it.ID,
			Category: it.Category,
			Label:    it.Label,
			Traits:   it.Traits,
		})
	}
	system := outfit.BuildSystemPromptForEvalWithOverrides(opts.PromptOverrides, opts.Tuple.Weather, nil, opts.Tuple.TopArchetypes, nil, nil)
	user := outfit.BuildUserMessageWithOverrides(opts.PromptOverrides, items)
	r.SystemPrompt = system
	r.UserMessage = user
	r.SystemTokens = approxTokens(system)
	r.UserTokens = approxTokens(user)

	// Baseline check — every tuple should produce a non-empty prompt.
	// Catches accidental nil-out / format-string regressions.
	r.Checks = append(r.Checks, Check{
		Name:   "prompt construction",
		Pass:   system != "" && user != "",
		Detail: fmt.Sprintf("system=%d chars, user=%d chars", len(system), len(user)),
	})

	// Adversarial checks: did sanitisation neutralise the payload?
	for _, banned := range opts.Tuple.Expectations.MustNotContain {
		check := Check{Name: "sanitisation/" + truncate(banned, 30), Pass: !strings.Contains(user, banned) && !strings.Contains(system, banned)}
		if !check.Pass {
			check.Detail = fmt.Sprintf("found %q in prompt", banned)
		}
		r.Checks = append(r.Checks, check)
	}
	if mark := opts.Tuple.Expectations.ExpectedRedactionMark; mark != "" {
		r.Checks = append(r.Checks, Check{
			Name: "sanitisation/expected redaction marker",
			Pass: strings.Contains(user, mark),
		})
	}

	// If no generator wired, this is a prompt-only run (CI smoke
	// test). Skip the LLM call.
	if opts.Generator == nil {
		return r
	}

	// Generator call.
	start := time.Now()
	req := outfit.GeneratorRequest{
		UserID:          opts.Tuple.UserID,
		Items:           items,
		TopArchetypes:   opts.Tuple.TopArchetypes,
		Weather:         opts.Tuple.Weather,
		PromptOverrides: opts.PromptOverrides,
	}
	outfits, _, err := opts.Generator.Generate(contextBg(), req)
	r.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Outfits = outfits

	// Outcome checks.
	r.Checks = append(r.Checks, checkOutfits(outfits, opts.Tuple.Expectations)...)

	return r
}

// checkOutfits runs the post-generation expectations. Each returns
// a Check; a tuple typically produces 5–10 checks.
func checkOutfits(outfits []outfit.Outfit, exp Expectations) []Check {
	var checks []Check
	if len(outfits) == 0 {
		checks = append(checks, Check{Name: "produced outfits", Pass: false, Detail: "0 outfits returned"})
		return checks
	}
	checks = append(checks, Check{Name: "produced outfits", Pass: true, Detail: fmt.Sprintf("%d outfits", len(outfits))})

	// Item count bounds (per outfit).
	for i, o := range outfits {
		count := len(o.Items)
		ok := (exp.MinItems == 0 || count >= exp.MinItems) && (exp.MaxItems == 0 || count <= exp.MaxItems)
		checks = append(checks, Check{
			Name:   fmt.Sprintf("outfit %d item count", i+1),
			Pass:   ok,
			Detail: fmt.Sprintf("%d items (min=%d max=%d)", count, exp.MinItems, exp.MaxItems),
		})
	}

	// Banned-words check on description + rationale, when requested.
	if exp.AvoidBannedWords {
		for i, o := range outfits {
			hits := bannedWordHits(o.Description) + bannedWordHits(o.Rationale)
			checks = append(checks, Check{
				Name:   fmt.Sprintf("outfit %d banned words", i+1),
				Pass:   hits == 0,
				Detail: fmt.Sprintf("%d hit(s)", hits),
			})
		}
	}

	return checks
}

// bannedWordsList intentionally duplicates a subset of the prompt's
// banned list — keeps this layer independent. Drift is fine; the
// eval just measures one slice of LLM-instruction-following.
var bannedWordsList = []string{
	"perfect", "perfect for", "versatile", "blends", "effortless",
	"timeless", "curated", "elevate", "elevated", "sophisticated",
	"classic", "essential", "staple", "go-to", "everyday",
	"vibe", "chic", "sleek", "polished", "refined",
}

func bannedWordHits(s string) int {
	if s == "" {
		return 0
	}
	lower := strings.ToLower(s)
	hits := 0
	for _, w := range bannedWordsList {
		if strings.Contains(lower, w) {
			hits++
		}
	}
	return hits
}

// approxTokens is a cheap token-count proxy: words / 0.75.
// Real tokenisation depends on the provider; this is good enough
// for sanity-checking prompt growth.
func approxTokens(s string) int {
	if s == "" {
		return 0
	}
	words := len(strings.Fields(s))
	return int(float64(words) / 0.75)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Avoid pulling in context here for the simple Background usage.
func contextBg() ctxBg { return ctxBg{} }

// Minimal context.Context implementation pinned to Background.
// The real Generators' tests inject their own contexts; eval only
// needs Background for the smoke pipeline.
type ctxBg struct{}

func (ctxBg) Deadline() (deadline time.Time, ok bool) { return }
func (ctxBg) Done() <-chan struct{}                   { return nil }
func (ctxBg) Err() error                              { return nil }
func (ctxBg) Value(key any) any                       { return nil }

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"mootd/backend/eval"
	"mootd/backend/internal/admin"
	"mootd/backend/internal/archetype"
	"mootd/backend/internal/outfit"
)

// evalGeneratorAdapter satisfies admin.EvalGenerator on top of the
// production outfit.Generator. Lives in app/ so admin/ can stay
// ignorant of the outfit package — same one-way dep we use for
// the wardrobe + detection-run adapters.
//
// The adapter is the bridge between two abstraction levels:
//
//	admin.EvalTuple (wire-shaped, untyped maps)
//	  ↓
//	eval.Tuple (fully typed; built by translateTuple below)
//	  ↓
//	outfit.GeneratorRequest + outfit.Generator.Generate(...)
//	  ↓
//	admin.EvalGenerator return shape (JSON-encoded outfits, count
//	of automated checks passed/total, dollar cost)
type evalGeneratorAdapter struct {
	gen           outfit.Generator
	promptVersion string
}

func newEvalGeneratorAdapter(gen outfit.Generator) *evalGeneratorAdapter {
	return &evalGeneratorAdapter{gen: gen, promptVersion: outfit.PromptVersion}
}

func (a *evalGeneratorAdapter) ProviderName() string {
	if a.gen == nil {
		return ""
	}
	return a.gen.Name()
}

func (a *evalGeneratorAdapter) ModelName() string {
	// outfit.Generator doesn't expose a model name today; the name
	// helper folds provider+model. Returning the same string for
	// both is fine for the v1 admin UI — we render them as a
	// "provider · model" label and a single value is easier to
	// read than two redundant ones.
	return ""
}

func (a *evalGeneratorAdapter) PromptVersionName() string {
	return a.promptVersion
}

// GenerateForEval is the workhorse. Translates the admin tuple →
// eval.Tuple, runs the existing harness (which yields prompts +
// automated checks), and on top calls the live generator with
// the same translated request.
func (a *evalGeneratorAdapter) GenerateForEval(ctx context.Context, t admin.EvalTuple) (string, string, int, int, float64, error) {
	if a.gen == nil {
		return "", "", 0, 0, 0, fmt.Errorf("eval generator not configured")
	}

	tuple, err := translateTuple(t)
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("translate tuple: %w", err)
	}

	// Build prompts + run automated checks via the shared harness.
	// We pass the live generator so eval.Run also calls the LLM
	// itself; the harness's checkOutfits() then runs against the
	// real output. That's exactly what we want for #27 — admin
	// runs are live-mode.
	res := eval.Run(eval.RunOptions{Tuple: tuple, Generator: a.gen})

	// If the generator failed, the harness records res.Error.
	// Surface it as the outer error so the per-case row reads
	// "generate: <message>" rather than swallowing it.
	if res.Error != "" {
		return "", "", 0, 0, 0, errors.New(res.Error)
	}

	// Count automated checks. Excludes the prompt-construction
	// noise check (always passes) when we report — but actually
	// keeping it gives us a clean fraction (pass/total) the FE
	// can render as "12/12 ✓" without per-check filtering.
	passed, total := countChecks(res.Checks)

	// JSON-encode outfits so the admin UI can render them in a
	// collapsible block + replay them from another tool. Encoding
	// here (not the runner) keeps the encoding isolated to the
	// boundary.
	encoded, err := json.Marshal(res.Outfits)
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("encode outfits: %w", err)
	}

	primaryName := ""
	if len(res.Outfits) > 0 {
		primaryName = res.Outfits[0].Name
	}

	// Cost: outfit.Generator doesn't expose Usage cleanly through
	// the current Generator interface (it returns *Usage that we
	// throw away in eval.Run). For v1 we accept "judge cost only"
	// and surface a TODO. Future: thread Usage through eval.Run
	// and sum here. The judge cost is the dominant component of
	// per-eval-case spend anyway; the outfit gen is amortized
	// across moodboards in production.
	return string(encoded), primaryName, passed, total, 0.0, nil
}

// translateTuple maps admin.EvalTuple's untyped JSON shape into
// the eval.Tuple the harness expects. The two diverge because
// admin/ doesn't import eval/ (one-way dependency), so we re-decode
// the JSON shape that lived on the admin wire.
func translateTuple(t admin.EvalTuple) (eval.Tuple, error) {
	out := eval.Tuple{
		ID:          t.ID,
		Description: t.Description,
		UserID:      t.UserID,
	}

	// Items
	for _, raw := range t.Items {
		spec := eval.ItemSpec{
			ID:       getString(raw, "id"),
			Category: getString(raw, "category"),
			Label:    getString(raw, "label"),
		}
		if traits, ok := raw["traits"].(map[string]any); ok {
			spec.Traits = make(map[string]string, len(traits))
			for k, v := range traits {
				if s, ok := v.(string); ok {
					spec.Traits[k] = s
				}
			}
		}
		out.Items = append(out.Items, spec)
	}

	// Top archetypes
	for _, raw := range t.TopArchetypes {
		score, _ := raw["score"].(float64)
		out.TopArchetypes = append(out.TopArchetypes, archetype.ScoredArchetype{
			Name:  getString(raw, "name"),
			Score: score,
		})
	}

	// Weather
	if t.Weather != nil {
		out.Weather = outfit.Weather{
			Temperature: getString(t.Weather, "temperature"),
			Unit:        getString(t.Weather, "unit"),
			Condition:   getString(t.Weather, "condition"),
		}
	}

	// Expectations — the harness uses zero-value defaults when no
	// expectations block is supplied, which is the right thing.
	if t.Expectations != nil {
		exp := *t.Expectations
		if v, ok := exp["minItems"].(float64); ok {
			out.Expectations.MinItems = int(v)
		}
		if v, ok := exp["maxItems"].(float64); ok {
			out.Expectations.MaxItems = int(v)
		}
		if arr, ok := exp["mustIncludeCategories"].([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					out.Expectations.MustIncludeCategories = append(out.Expectations.MustIncludeCategories, s)
				}
			}
		}
		if arr, ok := exp["preferTraits"].([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					out.Expectations.PreferTraits = append(out.Expectations.PreferTraits, s)
				}
			}
		}
		if v, ok := exp["avoidBannedWords"].(bool); ok {
			out.Expectations.AvoidBannedWords = v
		}
		if arr, ok := exp["mustNotContain"].([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					out.Expectations.MustNotContain = append(out.Expectations.MustNotContain, s)
				}
			}
		}
		if s, ok := exp["expectedRedactionMark"].(string); ok {
			out.Expectations.ExpectedRedactionMark = s
		}
	}

	return out, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func countChecks(checks []eval.Check) (passed, total int) {
	for _, c := range checks {
		total++
		if c.Pass {
			passed++
		}
	}
	return
}

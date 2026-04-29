package observability

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLLMCallRepo captures appended rows so we can assert on them.
// Goroutine-safe — Record runs synchronously today but the recorder
// contract doesn't promise that.
type fakeLLMCallRepo struct {
	mu   sync.Mutex
	rows []LLMCall
}

func (f *fakeLLMCallRepo) AppendLLMCall(_ context.Context, c LLMCall) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, c)
	return nil
}

func (f *fakeLLMCallRepo) snapshot() []LLMCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]LLMCall, len(f.rows))
	copy(out, f.rows)
	return out
}

func quietLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// stubPriceRepo serves a single in-memory price row so the recorder
// has a non-nil price table to consult. We don't assert on cost in
// these tests — that's pricing_test.go's job — but ComputeCost would
// nil-panic without a wired repo.
type stubPriceRepo struct{}

func (stubPriceRepo) ListEffective(_ context.Context, _ time.Time) ([]ModelPrice, error) {
	return []ModelPrice{
		{
			ID:                  "test-model",
			Model:               "test-model",
			Provider:            "test",
			InputUsdPerMTok:     1.0,
			OutputUsdPerMTok:    2.0,
			CacheReadUsdPerMTok: 0.1,
			EffectiveFrom:       time.Now().Add(-time.Hour),
			Notes:               "test fixture",
		},
	}, nil
}

func (stubPriceRepo) Upsert(_ context.Context, _ ModelPrice) error { return nil }

func newRecorderForTest(t *testing.T) (*LLMRecorder, *fakeLLMCallRepo) {
	t.Helper()
	repo := &fakeLLMCallRepo{}
	prices, err := NewPriceTable(context.Background(), stubPriceRepo{}, quietLogger())
	if err != nil {
		t.Fatalf("NewPriceTable: %v", err)
	}
	return NewLLMRecorder(repo, prices, quietLogger()), repo
}

// TestRecord_ArchivesPromptResponseAndItems is the main P1-11 Step B
// guarantee: when CallContext supplies SystemPrompt / UserMessage /
// WardrobeItemIDs and CallObservation supplies RawResponse, all four
// land on the persisted row.
func TestRecord_ArchivesPromptResponseAndItems(t *testing.T) {
	r, repo := newRecorderForTest(t)

	r.Record(context.Background(),
		CallContext{
			UserID:          "user_test",
			Feature:         "outfit_generate",
			SystemPrompt:    "You are a stylist.",
			UserMessage:     "Wardrobe: a, b, c.",
			WardrobeItemIDs: []string{"a", "b", "c"},
		},
		CallObservation{
			Provider:     "test",
			Model:        "test-model",
			InputTokens:  100,
			OutputTokens: 200,
			RawResponse:  `{"outfits":[{"items":["a","b","c"]}]}`,
			StartedAt:    time.Now().Add(-2 * time.Second),
			EndedAt:      time.Now(),
		})

	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.SystemPrompt != "You are a stylist." {
		t.Errorf("SystemPrompt = %q; want stored verbatim", row.SystemPrompt)
	}
	if row.UserMessage != "Wardrobe: a, b, c." {
		t.Errorf("UserMessage = %q; want stored verbatim", row.UserMessage)
	}
	if !strings.Contains(row.ResponseRaw, `"outfits"`) {
		t.Errorf("ResponseRaw missing outfit JSON: %q", row.ResponseRaw)
	}
	if got, want := len(row.WardrobeItemIDs), 3; got != want {
		t.Errorf("WardrobeItemIDs len = %d; want %d", got, want)
	}
	if row.PromptHash == "" {
		t.Error("PromptHash should be populated when SystemPrompt+UserMessage are present")
	}
}

func TestRecord_PromptHashFromPromptText(t *testing.T) {
	r, repo := newRecorderForTest(t)
	r.Record(context.Background(),
		CallContext{UserID: "u", Feature: "outfit_generate", PromptText: "explicit"},
		CallObservation{Provider: "test", Model: "test-model", StartedAt: time.Now(), EndedAt: time.Now()})
	rows := repo.snapshot()
	if rows[0].PromptHash != HashPrompt("explicit") {
		t.Errorf("PromptHash = %q; want hash of explicit PromptText", rows[0].PromptHash)
	}
}

func TestRecord_PromptHashFallbackFromArchivalFields(t *testing.T) {
	r, repo := newRecorderForTest(t)
	r.Record(context.Background(),
		CallContext{UserID: "u", Feature: "outfit_generate", SystemPrompt: "S", UserMessage: "U"},
		CallObservation{Provider: "test", Model: "test-model", StartedAt: time.Now(), EndedAt: time.Now()})
	rows := repo.snapshot()
	want := HashPrompt("S\nU")
	if rows[0].PromptHash != want {
		t.Errorf("PromptHash = %q; want %q (fallback to system\\nuser concat)", rows[0].PromptHash, want)
	}
}

func TestRecord_ArchivalFieldsTruncatedAtCap(t *testing.T) {
	r, repo := newRecorderForTest(t)
	huge := strings.Repeat("a", archivalFieldMaxBytes*2)
	r.Record(context.Background(),
		CallContext{UserID: "u", Feature: "f", SystemPrompt: huge, UserMessage: huge},
		CallObservation{Provider: "test", Model: "test-model", RawResponse: huge, StartedAt: time.Now(), EndedAt: time.Now()})
	row := repo.snapshot()[0]
	for name, field := range map[string]string{
		"SystemPrompt": row.SystemPrompt,
		"UserMessage":  row.UserMessage,
		"ResponseRaw":  row.ResponseRaw,
	} {
		if len(field) > archivalFieldMaxBytes+len("…(truncated)") {
			t.Errorf("%s exceeded cap: len=%d", name, len(field))
		}
		if !strings.HasSuffix(field, "…(truncated)") {
			t.Errorf("%s missing truncation sentinel", name)
		}
	}
}

func TestRecord_ErrorRowStillCarriesArchivalFields(t *testing.T) {
	// Even when the LLM call failed, we want the prompt + (partial)
	// response stored — that's how operators debug failed calls.
	r, repo := newRecorderForTest(t)
	r.Record(context.Background(),
		CallContext{UserID: "u", Feature: "f", SystemPrompt: "S", UserMessage: "U"},
		CallObservation{
			Provider: "test", Model: "test-model",
			RawResponse: "partial output before error",
			StartedAt:   time.Now(), EndedAt: time.Now(),
			Err: errors.New("upstream 503"),
		})
	row := repo.snapshot()[0]
	if row.Status != "error" {
		t.Errorf("Status = %q; want error", row.Status)
	}
	if row.SystemPrompt == "" || row.ResponseRaw == "" {
		t.Errorf("archival fields should survive error path; got system=%q raw=%q", row.SystemPrompt, row.ResponseRaw)
	}
	if row.ErrorMsg == "" {
		t.Errorf("ErrorMsg should be populated on error path")
	}
}

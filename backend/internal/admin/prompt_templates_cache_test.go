package admin

import (
	"context"
	"errors"
	"testing"
)

// fakeTemplatesRepo is an in-memory PromptTemplatesRepository for
// cache tests. Only the read paths the cache exercises matter;
// tests mutate the maps directly instead of going through writers.
type fakeTemplatesRepo struct {
	prod    map[string]string // name -> production body
	names   []string          // ListNames result; derived from prod when nil
	listErr error
}

func (f *fakeTemplatesRepo) GetProduction(_ context.Context, name string) (*PromptTemplate, error) {
	if body, ok := f.prod[name]; ok {
		return &PromptTemplate{Name: name, Version: 1, Body: body, IsProduction: true}, nil
	}
	return nil, nil
}

func (f *fakeTemplatesRepo) ListNames(_ context.Context) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.names != nil {
		return f.names, nil
	}
	out := make([]string, 0, len(f.prod))
	for n := range f.prod {
		out = append(out, n)
	}
	return out, nil
}

func (f *fakeTemplatesRepo) ListVersions(context.Context, string) ([]PromptTemplate, error) {
	return nil, nil
}
func (f *fakeTemplatesRepo) CreateVersion(context.Context, PromptTemplate) error { return nil }
func (f *fakeTemplatesRepo) Promote(context.Context, string, int) error          { return nil }
func (f *fakeTemplatesRepo) Get(context.Context, string) (*PromptTemplate, error) {
	return nil, nil
}

// The regression this guards: BodyOrFallback must serve names that
// exist only in Mongo (operator-created, e.g. the per-archetype
// variants "outfit_system_base.<archetype>" from mootd#65), not just
// names present in the seeded fallback map. The pre-fix refresh
// iterated fallback keys only, so an operator-created name was never
// fetched and silently fell back forever.
func TestCacheServesOperatorCreatedNames(t *testing.T) {
	repo := &fakeTemplatesRepo{prod: map[string]string{
		"outfit_system_base":         "universal body",
		"outfit_system_base.creator": "creator body",
	}}
	c := NewCachedPromptTemplates(repo, map[string]string{
		"outfit_system_base": "fallback body",
	}, nil)

	if got := c.BodyOrFallback(context.Background(), "outfit_system_base.creator"); got != "creator body" {
		t.Fatalf("operator-created name: got %q, want %q", got, "creator body")
	}
	if got := c.BodyOrFallback(context.Background(), "outfit_system_base"); got != "universal body" {
		t.Fatalf("seeded name: got %q, want %q", got, "universal body")
	}
}

func TestCacheFallsBackWhenNoProductionVersion(t *testing.T) {
	// Names exist in the collection (drafts were created) but have no
	// production version — GetProduction returns nil for both. Seeded
	// names serve their fallback; operator-created names serve ""
	// (the outfit caller then falls back to the universal template).
	repo := &fakeTemplatesRepo{
		prod:  map[string]string{},
		names: []string{"outfit_system_base", "outfit_system_base.creator"},
	}
	c := NewCachedPromptTemplates(repo, map[string]string{
		"outfit_system_base": "fallback body",
	}, nil)

	if got := c.BodyOrFallback(context.Background(), "outfit_system_base"); got != "fallback body" {
		t.Fatalf("seeded name without production row: got %q, want fallback", got)
	}
	if got := c.BodyOrFallback(context.Background(), "outfit_system_base.creator"); got != "" {
		t.Fatalf("unpromoted operator name: got %q, want empty", got)
	}
}

func TestCacheRefreshSurvivesListNamesError(t *testing.T) {
	repo := &fakeTemplatesRepo{
		prod:    map[string]string{"outfit_system_base": "universal body"},
		listErr: errors.New("mongo down"),
	}
	c := NewCachedPromptTemplates(repo, map[string]string{
		"outfit_system_base": "fallback body",
	}, nil)
	if got := c.BodyOrFallback(context.Background(), "outfit_system_base"); got != "universal body" {
		t.Fatalf("seeded key should still refresh when ListNames fails: got %q", got)
	}
}

func TestCacheInvalidatePicksUpNewName(t *testing.T) {
	repo := &fakeTemplatesRepo{prod: map[string]string{
		"outfit_system_base": "universal body",
	}}
	c := NewCachedPromptTemplates(repo, map[string]string{
		"outfit_system_base": "fallback body",
	}, nil)
	// Prime the cache.
	_ = c.BodyOrFallback(context.Background(), "outfit_system_base")
	// Operator creates + promotes a new archetype variant; the
	// promote handler then calls Invalidate.
	repo.prod["outfit_system_base.creator"] = "creator body"
	if got := c.BodyOrFallback(context.Background(), "outfit_system_base.creator"); got != "" {
		t.Fatalf("expected miss from still-fresh cache before invalidate, got %q", got)
	}
	c.Invalidate()
	if got := c.BodyOrFallback(context.Background(), "outfit_system_base.creator"); got != "creator body" {
		t.Fatalf("after invalidate: got %q, want %q", got, "creator body")
	}
}

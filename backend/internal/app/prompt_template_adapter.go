package app

import (
	"context"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/outfit"
)

// promptTemplateAdapter satisfies outfit.PromptTemplateProvider
// on top of admin.CachedPromptTemplates. Lives here so outfit/
// stays clean of the admin/ import — same one-way-dep
// convention we use for budget, detection-runs, and tier
// routing.
//
// The cached reader's BodyOrFallback takes a context; outfit's
// interface is context-free (the prompt-builder runs in user
// goroutines; threading a context through buildSystemPrompt
// would noise up every caller for a benefit only this gate
// would see). We use context.Background() — the cache's TTL is
// short (60s) and refresh is fast (3-5 indexed Mongo reads)
// so dropping cancellability is acceptable.
type promptTemplateAdapter struct {
	cache *admin.CachedPromptTemplates
}

func newPromptTemplateAdapter(cache *admin.CachedPromptTemplates) *promptTemplateAdapter {
	return &promptTemplateAdapter{cache: cache}
}

func (p *promptTemplateAdapter) BodyOrFallbackForName(name string) string {
	if p == nil || p.cache == nil {
		return ""
	}
	return p.cache.BodyOrFallback(context.Background(), name)
}

// Compile-time assertion the adapter satisfies the interface.
var _ outfit.PromptTemplateProvider = (*promptTemplateAdapter)(nil)

package app

import (
	"context"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/outfit"
)

// promptTemplateAdapter satisfies outfit.PromptTemplateProvider
// on top of admin.CachedPromptTemplates + admin.CachedABTests.
// Lives here so outfit/ stays clean of the admin/ import —
// same one-way-dep convention we use for budget,
// detection-runs, and tier routing.
//
// At call time:
//   1. If an A/B test is active for this template AND the
//      user falls in the candidate cohort, fetch + return the
//      candidate version's body (admin.repo.Get).
//   2. Otherwise return the production body via
//      CachedPromptTemplates.BodyOrFallback.
//
// The candidate-fetch hits Mongo per call (no caching of the
// candidate body today). Fine: A/B tests are admin-controlled,
// admin-paced, and the candidate body fetch is a single
// indexed lookup. If that becomes a bottleneck a follow-up can
// add a small per-version body cache.
type promptTemplateAdapter struct {
	cache       *admin.CachedPromptTemplates
	abCache     *admin.CachedABTests
	templates   admin.PromptTemplatesRepository
}

func newPromptTemplateAdapter(
	cache *admin.CachedPromptTemplates,
	abCache *admin.CachedABTests,
	templates admin.PromptTemplatesRepository,
) *promptTemplateAdapter {
	return &promptTemplateAdapter{cache: cache, abCache: abCache, templates: templates}
}

func (p *promptTemplateAdapter) BodyForUser(name, userID string) string {
	if p == nil || p.cache == nil {
		return ""
	}
	// A/B test gate (P3-05 / mootd-admin#28).
	if p.abCache != nil && p.templates != nil {
		test := p.abCache.ActiveForTemplate(context.Background(), name)
		if admin.IsCandidateUser(userID, test) {
			doc, err := p.templates.Get(context.Background(),
				"pt_"+name+"_v"+itoaShort(test.CandidateVersion))
			if err == nil && doc != nil && doc.Body != "" {
				return doc.Body
			}
			// Fallthrough on lookup failure — better to serve the
			// production version than nothing.
		}
	}
	return p.cache.BodyOrFallback(context.Background(), name)
}

// itoaShort is a tiny formatter; avoids fmt for the hot path.
func itoaShort(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Compile-time assertion the adapter satisfies the interface.
var _ outfit.PromptTemplateProvider = (*promptTemplateAdapter)(nil)

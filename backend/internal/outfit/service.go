package outfit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// wardrobeRepository is the subset of the wardrobe repo the outfit service needs.
// GetImage is required for vision-capable generators (Claude); generators that
// don't use it ignore the method.
type wardrobeRepository interface {
	FindByUser(ctx context.Context, userID string) ([]wardrobe.ClothingItem, error)
	GetImage(ctx context.Context, itemID string) ([]byte, string, error)
}

// userProfileProvider reads the user's archetype profile.
type userProfileProvider interface {
	GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error)
}

// UserProfileFunc adapts a function to satisfy userProfileProvider.
type UserProfileFunc func(ctx context.Context, userID string) (map[string]float64, error)

func (f UserProfileFunc) GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error) {
	return f(ctx, userID)
}

// recentOutfitProvider fetches recent moodboards to avoid repeating outfits.
type recentOutfitProvider interface {
	FindRecent(ctx context.Context, userID string, limit int) ([]recentBoard, error)
}

type recentBoard struct {
	OutfitName    string
	ItemIDs       []string
	Description   string // free-text description saved with the outfit
	Rationale     string // one-line stylist reasoning saved with the outfit
	TopArchetype  string // highest-scoring archetype at save time (may be empty)
	Palette       []string // dominant colors as #RRGGBB (may be empty)
}

// RecentOutfitFunc is a function type that satisfies recentOutfitProvider.
type RecentOutfitFunc func(ctx context.Context, userID string, limit int) ([]recentBoard, error)

func (f RecentOutfitFunc) FindRecent(ctx context.Context, userID string, limit int) ([]recentBoard, error) {
	return f(ctx, userID, limit)
}

// RecentBoard re-exports recentBoard so callers can construct the adapter.
type RecentBoard = recentBoard

// Cache is the optional outfit-suggestion cache. Implementations key results
// on (user, wardrobe-fingerprint, weather-bucket, archetype-fingerprint).
type Cache interface {
	Get(ctx context.Context, key string) ([]Outfit, bool)
	Set(ctx context.Context, key string, outfits []Outfit)
}

// surfaceProvider lists available panels/backgrounds and resolves picks by
// ID. Decoupled via interface so the outfit package doesn't import surface
// (kept unidirectional: app wires the implementation).
type surfaceProvider interface {
	ListPanels(ctx context.Context) ([]SurfaceOption, error)
	ListBackgrounds(ctx context.Context) ([]SurfaceOption, error)
	ResolveURL(id string) string
}

// SurfaceOption is the trimmed shape the outfit service + LLM need to see
// per available surface. Larger surface metadata stays in the surface
// package — this is just enough to feed the prompt.
type SurfaceOption struct {
	ID                string
	Name              string
	Description       string
	MoodTags          []string
	ArchetypeAffinity map[string]float64
}

// Service encapsulates the outfit generation business logic, separated from
// HTTP concerns which live in Handler.
type Service struct {
	generator   Generator
	wardrobe    wardrobeRepository
	recent      recentOutfitProvider
	userProfile userProfileProvider
	surfaces    surfaceProvider
	useVision   bool
	cache       Cache
	llmRecorder llmRecorder // optional — nil disables LLM call logging
	logger      *log.Logger
}

// llmRecorder is the narrow interface the outfit service needs from
// observability.LLMRecorder. Defining it here keeps the dependency
// direction one-way (outfit doesn't import observability), so future
// reuse of the recorder in detection / search doesn't ripple imports.
type llmRecorder interface {
	Record(ctx context.Context, cc LLMRecorderContext, obs LLMRecorderObservation)
}

// LLMRecorderContext mirrors observability.CallContext — defined in this
// package so the outfit service can build it without importing the
// observability package directly.
type LLMRecorderContext struct {
	UserID     string
	Feature    string
	TraceID    string
	PromptText string
}

// LLMRecorderObservation mirrors observability.CallObservation.
type LLMRecorderObservation struct {
	Provider         string
	Model            string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	PromptVersion    string
	StartedAt        time.Time
	EndedAt          time.Time
	Err              error
}

// ServiceConfig holds the dependencies needed to construct a Service.
type ServiceConfig struct {
	Generator   Generator
	Wardrobe    wardrobeRepository
	Recent      recentOutfitProvider
	UserProfile userProfileProvider
	Surfaces    surfaceProvider // optional — when nil, outfits ship without panel/background URLs
	UseVision   bool
	Cache       Cache
	LLMRecorder llmRecorder // optional — nil disables LLM call logging
}

// NewService creates an outfit Service.
func NewService(logger *log.Logger, cfg ServiceConfig) *Service {
	return &Service{
		generator:   cfg.Generator,
		wardrobe:    cfg.Wardrobe,
		recent:      cfg.Recent,
		userProfile: cfg.UserProfile,
		surfaces:    cfg.Surfaces,
		useVision:   cfg.UseVision,
		cache:       cfg.Cache,
		llmRecorder: cfg.LLMRecorder,
		logger:      logger,
	}
}

// GenerateOutfits runs the full outfit generation pipeline: fetch wardrobe
// items, score archetypes, check cache, call the LLM generator, validate,
// apply fallback if needed, and cache the result.
func (s *Service) GenerateOutfits(ctx context.Context, userID string, weather Weather) ([]Outfit, error) {
	items, err := s.wardrobe.FindByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetch wardrobe: %w", err)
	}

	if len(items) == 0 {
		return []Outfit{}, nil
	}

	// Archetype context.
	wardrobeScores := archetype.ScoreItems(itemsToTraits(items))

	var effectiveScores archetype.Scores
	if s.userProfile != nil {
		userScores, err := s.userProfile.GetArchetypeProfile(ctx, userID)
		if err != nil {
			s.logger.Printf("outfit: archetype profile fetch failed for user %s: %v (falling back to wardrobe-only scoring)", userID, err)
		}
		if len(userScores) > 0 {
			effectiveScores = archetype.Merge(userScores, wardrobeScores, 0.6)
		} else {
			effectiveScores = wardrobeScores
		}
	} else {
		effectiveScores = wardrobeScores
	}
	topArchetypes := archetype.TopN(effectiveScores, 2)

	// Recent outfits — feeds both the "avoid repeating" anti-list and the
	// positive few-shot examples built in buildSystemPrompt.
	var recentBoards []RecentBoard
	if s.recent != nil {
		recent, err := s.recent.FindRecent(ctx, userID, 7)
		if err != nil {
			s.logger.Printf("outfit: recent-outfit fetch failed for user %s: %v (proceeding without dedup)", userID, err)
		}
		recentBoards = recent
	}

	genItems := itemsToGenItems(items)

	// attachWeather stamps the current weather onto each outfit so the UI can
	// render a weather chip. Applied after cache resolution so cached outfits
	// reflect the current request's weather rather than the weather at cache time.
	attachWeather := func(outfits []Outfit) []Outfit {
		if weather.Temperature == "" && weather.Condition == "" {
			return outfits
		}
		w := weather
		for i := range outfits {
			outfits[i].Weather = &w
		}
		return outfits
	}

	// Cache lookup.
	cacheKey := buildCacheKey(userID, items, weather, topArchetypes)
	if s.cache != nil {
		if cached, ok := s.cache.Get(ctx, cacheKey); ok {
			// Self-heal: cache entries written before the palette feature lack
			// Palette. Detect that case, recompute once, and rewrite the cache
			// so subsequent hits are free.
			needsPalette := false
			for _, o := range cached {
				if len(o.Palette) == 0 {
					needsPalette = true
					break
				}
			}
			if needsPalette {
				cached = s.attachPalettes(ctx, cached)
				s.cache.Set(ctx, cacheKey, cached)
				s.logger.Printf("outfit: cache hit for user %s (%d outfits) — upgraded with palette", userID, len(cached))
			} else {
				s.logger.Printf("outfit: cache hit for user %s (%d outfits)", userID, len(cached))
			}
			// Re-resolve surface URLs on every cache hit — IDs are stable but the
			// URL template could change as routes evolve, and cached entries may
			// predate the surface feature entirely.
			cached = s.resolveSurfaceURLs(cached, nil, nil)
			return attachWeather(cached), nil
		}
	}

	// Load surface menus for the LLM. Failures aren't fatal — the model just
	// won't pick and we fall back to no panel/background on those outfits.
	var panels, backgrounds []SurfaceOption
	if s.surfaces != nil {
		var err error
		if panels, err = s.surfaces.ListPanels(ctx); err != nil {
			s.logger.Printf("outfit: surface: list panels failed: %v", err)
		}
		if backgrounds, err = s.surfaces.ListBackgrounds(ctx); err != nil {
			s.logger.Printf("outfit: surface: list backgrounds failed: %v", err)
		}
	}

	req := GeneratorRequest{
		UserID:        userID,
		Items:         genItems,
		TopArchetypes: topArchetypes,
		Weather:       weather,
		RecentBoards:  recentBoards,
		Panels:        panels,
		Backgrounds:   backgrounds,
		UseVision:     s.useVision,
	}

	s.logger.Printf("outfit: %s generator for user %s (%d items, weather=%s/%s, recent=%d, archetype=%s)",
		s.generator.Name(), userID, len(items), weather.Temperature, weather.Condition, len(recentBoards),
		formatTopArchetypes(topArchetypes))

	startedAt := time.Now().UTC()
	parsedOutfits, usage, err := s.generator.Generate(ctx, req)
	endedAt := time.Now().UTC()
	if err != nil {
		s.logger.Printf("outfit: %s generator failed for user %s: %v", s.generator.Name(), userID, err)
		// Fall through to deterministic fallback below.
	}

	// Record the LLM call to the observability ledger. Best-effort —
	// failures here are logged inside Record, never bubbled. Skipped
	// when no recorder is wired (test setups, dev opt-out) or no
	// usage came back (transport error before we hit the provider).
	if s.llmRecorder != nil && usage != nil {
		s.llmRecorder.Record(ctx, LLMRecorderContext{
			UserID:  userID,
			Feature: "outfit_generate",
			// PromptText left empty for now — adding the rendered
			// prompt is P1-11 (prompt snapshot archival). Keeping
			// the hash off avoids an early-stage performance
			// surprise (sha256 over a 5KB string is fine, but we
			// also need to control its growth in the response
			// shape).
		}, LLMRecorderObservation{
			Provider:         usage.Provider,
			Model:            usage.Model,
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			PromptVersion:    usage.PromptVersion,
			StartedAt:        startedAt,
			EndedAt:          endedAt,
			Err:              err,
		})
	}

	s.logger.Printf("outfit: parsed %d outfits from %s", len(parsedOutfits), s.generator.Name())

	// Validate, score, enrich, dedupe.
	filtered := s.ValidateOutfits(parsedOutfits, items, effectiveScores)

	// Deterministic fallback if the generator failed or under-delivered.
	if len(filtered) < 3 {
		fallback := buildFallbackOutfits(items, topArchetypes, 4-len(filtered))
		fbValidated := s.ValidateOutfits(fallback, items, effectiveScores)
		seenIDs := make(map[string]bool)
		for _, o := range filtered {
			seenIDs[outfitFingerprint(o)] = true
		}
		for _, o := range fbValidated {
			if !seenIDs[outfitFingerprint(o)] {
				filtered = append(filtered, o)
				seenIDs[outfitFingerprint(o)] = true
			}
			if len(filtered) >= 4 {
				break
			}
		}
		if len(filtered) > 0 {
			s.logger.Printf("outfit: fallback fired for user %s — %d outfits returned", userID, len(filtered))
		}
	}

	// Attach per-item color palettes before caching so cache hits serve the
	// same enriched payload on subsequent reads.
	filtered = s.attachPalettes(ctx, filtered)

	// Resolve LLM-picked panel/background IDs into URLs. Invalid picks fall
	// through (URL stays empty) so the frontend can pick a local fallback.
	filtered = s.resolveSurfaceURLs(filtered, panels, backgrounds)

	if s.cache != nil && len(filtered) >= 3 {
		s.cache.Set(ctx, cacheKey, filtered)
	}

	s.logger.Printf("outfit: returning %d outfits for user %s", len(filtered), userID)
	return attachWeather(filtered), nil
}

// resolveSurfaceURLs converts the raw PanelID/BackgroundID the LLM returned
// into fully-qualified URLs the frontend can fetch.
//
// validPanels/validBackgrounds are optional — when provided, IDs that aren't
// in those lists are treated as hallucinations and dropped. When both are
// nil (cache-hit path), the resolver trusts whatever IDs are stored and
// simply regenerates the URL via the surface provider.
func (s *Service) resolveSurfaceURLs(outfits []Outfit, validPanels, validBackgrounds []SurfaceOption) []Outfit {
	if s.surfaces == nil {
		return outfits
	}
	allow := func(list []SurfaceOption) map[string]struct{} {
		if list == nil {
			return nil
		}
		m := make(map[string]struct{}, len(list))
		for _, o := range list {
			m[o.ID] = struct{}{}
		}
		return m
	}
	panelSet := allow(validPanels)
	bgSet := allow(validBackgrounds)

	check := func(id string, allowed map[string]struct{}) bool {
		if id == "" {
			return false
		}
		if allowed == nil {
			return true
		}
		_, ok := allowed[id]
		return ok
	}

	for i := range outfits {
		if check(outfits[i].PanelID, panelSet) {
			outfits[i].PanelURL = s.surfaces.ResolveURL(outfits[i].PanelID)
		} else if outfits[i].PanelID != "" {
			s.logger.Printf("outfit: surface: unknown panelId %q — dropping", outfits[i].PanelID)
			outfits[i].PanelID = ""
			outfits[i].PanelURL = ""
		}
		if check(outfits[i].BackgroundID, bgSet) {
			outfits[i].BackgroundURL = s.surfaces.ResolveURL(outfits[i].BackgroundID)
		} else if outfits[i].BackgroundID != "" {
			s.logger.Printf("outfit: surface: unknown backgroundId %q — dropping", outfits[i].BackgroundID)
			outfits[i].BackgroundID = ""
			outfits[i].BackgroundURL = ""
		}
	}
	return outfits
}

// ValidateOutfits drops outfits with hallucinated IDs, dedupes, and re-scores
// each outfit against the archetypes. Items missing from the wardrobe are removed.
func (s *Service) ValidateOutfits(outfits []Outfit, items []wardrobe.ClothingItem, profileScores archetype.Scores) []Outfit {
	validIDs := make(map[string]bool, len(items))
	itemByID := make(map[string]wardrobe.ClothingItem, len(items))
	existingCategories := make(map[string]bool)
	for _, item := range items {
		validIDs[item.ID] = true
		itemByID[item.ID] = item
		existingCategories[strings.ToLower(item.Category)] = true
	}

	smallWardrobe := len(items) < 20

	seen := make(map[string]bool)
	filtered := make([]Outfit, 0, len(outfits))
	for _, o := range outfits {
		validItems := make([]string, 0, len(o.Items))
		for _, id := range o.Items {
			if validIDs[id] {
				validItems = append(validItems, id)
			}
		}
		if len(validItems) < 4 {
			s.logger.Printf("outfit: skipping %q — only %d/%d valid items", o.Name, len(validItems), len(o.Items))
			continue
		}
		o.Items = validItems

		// Verify required categories: at least one top, one bottom, one footwear.
		hasTop, hasBottom, hasFootwear := false, false, false
		for _, id := range validItems {
			if item, ok := itemByID[id]; ok {
				role := ClassifyRole(item.Category)
				switch role {
				case "TOPS":
					hasTop = true
				case "BOTTOMS":
					hasBottom = true
				case "FOOTWEAR":
					hasFootwear = true
				}
			}
		}
		if !hasTop || !hasBottom || !hasFootwear {
			s.logger.Printf("outfit: skipping %q — missing required category (top=%v, bottom=%v, footwear=%v)",
				o.Name, hasTop, hasBottom, hasFootwear)
			continue
		}

		key := outfitFingerprint(o)
		if seen[key] {
			continue
		}
		seen[key] = true

		// Score this outfit against archetypes.
		outfitItems := make([]wardrobe.ClothingItem, 0, len(validItems))
		for _, id := range validItems {
			if item, ok := itemByID[id]; ok {
				outfitItems = append(outfitItems, item)
			}
		}
		o.ArchetypeScores = archetype.ScoreItems(itemsToTraits(outfitItems))

		// Strip layoutRoles entries that reference dropped items.
		if len(o.LayoutRoles) > 0 {
			cleaned := make(map[string]string, len(validItems))
			for _, id := range validItems {
				if role, ok := o.LayoutRoles[id]; ok {
					cleaned[id] = role
				}
			}
			o.LayoutRoles = cleaned
		}

		// P1-H: mirror the cleanup for visualWeights. Dropped items must
		// not leak through — the frontend treats the map as authoritative.
		if len(o.VisualWeights) > 0 {
			cleaned := make(map[string]string, len(validItems))
			for _, id := range validItems {
				if w, ok := o.VisualWeights[id]; ok {
					cleaned[id] = w
				}
			}
			o.VisualWeights = cleaned
		}

		if smallWardrobe {
			outfitTop := archetype.TopN(o.ArchetypeScores, 3)
			if s := archetype.SuggestMissingCategory(outfitTop, existingCategories); s != "" {
				o.SmartSuggestion = s
			}
		}

		filtered = append(filtered, o)
	}

	_ = profileScores // reserved for future ranking against the user profile
	return filtered
}

// itemsToTraits converts wardrobe items to archetype scoring input.
func itemsToTraits(items []wardrobe.ClothingItem) []archetype.ItemTraits {
	traits := make([]archetype.ItemTraits, len(items))
	for i, item := range items {
		traits[i] = archetype.ItemTraits{
			Category:       item.Category,
			Color:          item.Traits["color"],
			ColorSecondary: item.Traits["color_secondary"],
			Fabric:         item.Traits["fabric"],
			Style:          item.Traits["style"],
			Occasion:       item.Traits["occasion"],
			OverallStyle:   item.Traits["overall_style"],
			Details:        item.Traits["details"],
		}
	}
	return traits
}

// itemsToGenItems trims wardrobe items down to the generator-facing GenItem shape.
func itemsToGenItems(items []wardrobe.ClothingItem) []GenItem {
	out := make([]GenItem, len(items))
	for i, item := range items {
		out[i] = GenItem{
			ID:       item.ID,
			Category: item.Category,
			Label:    item.Label,
			Traits:   item.Traits,
		}
	}
	return out
}

// outfitFingerprint produces a stable identity for an outfit based on its sorted item IDs.
func outfitFingerprint(o Outfit) string {
	sorted := make([]string, len(o.Items))
	copy(sorted, o.Items)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// formatTopArchetypes renders the top archetypes for log output.
func formatTopArchetypes(archs []archetype.ScoredArchetype) string {
	if len(archs) == 0 {
		return "none"
	}
	parts := make([]string, len(archs))
	for i, a := range archs {
		parts[i] = a.Name
	}
	return strings.Join(parts, "+")
}

// buildCacheKey produces a deterministic key for the outfit cache.
// The key changes when the wardrobe membership, weather bucket, or top
// archetypes change — so a fresh wardrobe item or different weather forces
// re-generation, but tapping "regenerate" twice in a row hits the cache.
func buildCacheKey(userID string, items []wardrobe.ClothingItem, weather Weather, top []archetype.ScoredArchetype) string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	sort.Strings(ids)

	tempBucket := bucketTemperature(weather.Temperature, weather.Unit)
	condBucket := strings.ToLower(weather.Condition)

	archParts := make([]string, len(top))
	for i, a := range top {
		archParts[i] = a.Name
	}

	raw := strings.Join([]string{
		userID,
		strings.Join(ids, ","),
		tempBucket,
		condBucket,
		strings.Join(archParts, "+"),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// bucketTemperature collapses raw temperatures into 5-degree buckets so cache
// hits don't depend on degree-perfect matches between weather refreshes.
func bucketTemperature(temp, unit string) string {
	if temp == "" {
		return ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(temp))
	if err != nil {
		return temp + unit
	}
	bucket := (n / 5) * 5
	return strconv.Itoa(bucket) + unit
}

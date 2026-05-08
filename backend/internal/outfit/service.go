package outfit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// sampleFillers returns up to `n` items drawn from `pool` in
// randomised order. Used to rotate which archetype defaults reach
// the LLM across successive generations so a small wardrobe doesn't
// keep producing the same 3-4 outfit permutations.
//
// Randomness is intentionally non-deterministic — the per-call
// freshness IS the feature. Cache hits will be rare when fillers are
// in play; that's fine, the cost of a regen is small (~$0.01) and
// the user explicitly traded the cache hit for variety.
func sampleFillers(pool []wardrobe.ClothingItem, n int) []wardrobe.ClothingItem {
	if len(pool) <= n {
		out := make([]wardrobe.ClothingItem, len(pool))
		copy(out, pool)
		rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	idx := rand.Perm(len(pool))[:n]
	out := make([]wardrobe.ClothingItem, n)
	for i, j := range idx {
		out[i] = pool[j]
	}
	return out
}

// wardrobeRepository is the subset of the wardrobe repo the outfit service needs.
// GetImage is required for vision-capable generators (Claude); generators that
// don't use it ignore the method.
type wardrobeRepository interface {
	FindByUser(ctx context.Context, userID string) ([]wardrobe.ClothingItem, error)
	GetImage(ctx context.Context, itemID string) ([]byte, string, error)
}

// userProfileProvider reads the user's archetype profile and
// (optionally, mootd#67) the user's creativity preference.
type userProfileProvider interface {
	GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error)
}

// archetypeDefaultsLoader is the small surface the outfit service
// needs from the admin-side `archetype_default_items` collection.
// Defined as an interface so outfit/ doesn't import admin/ — same
// one-way-dep pattern as the rest of the package's optional plug
// points. nil is fine; the wardrobe-only fallback runs unchanged.
//
// Implementations (app/) wrap admin.ArchetypeDefaultsRepository.List
// and convert the rows into the wardrobe-shaped items the outfit
// service already knows how to consume (so we don't need a parallel
// "filler item" type leaking through every layer).
//
// `userID` parameter lets the implementation filter out per-user
// rejections (defaults the user has already marked as "not in my
// wardrobe"); without that filter, the same dismissed item would
// keep coming back on every regeneration.
type archetypeDefaultsLoader interface {
	// LoadFor returns curated items for the given archetype, capped
	// to `cap` rows. The returned items are wardrobe-shaped but
	// their IDs use the "ad_<hex>" prefix the admin layer mints. The
	// caller is responsible for marking them non-Preferred when they
	// fold into the LLM-facing pool. Rejected defaults for `userID`
	// are excluded.
	LoadFor(ctx context.Context, userID, archetype string, cap int) ([]wardrobe.ClothingItem, error)
}

// fillerSeeder kept for now as an unexported abstraction even
// though outfit/ no longer auto-seeds fillers — the same primitive
// is what the wardrobe handler will call when the user explicitly
// claims a filler ("I have this IRL"). Defined here so the
// app/-side adapter can satisfy a single interface used from two
// callers (wardrobe handler today; outfit service if/when we
// re-enable auto-seed behind a flag).
type fillerSeeder interface {
	// SeedForUser copies the archetype default identified by
	// defaultID into the user's wardrobe (or returns the previous
	// seed when already present). Returns the resulting wardrobe
	// item id (wi_<hex>).
	SeedForUser(ctx context.Context, userID, defaultID string) (string, error)
}

// creativityProvider is an optional extension of userProfileProvider
// (mootd#67). When the wired implementation satisfies it, the
// outfit service reads the user's preference and threads it through
// to the generator's temperature. Implementations that don't satisfy
// this interface degrade to the historical "use the provider's
// default temperature" behaviour — no breakage for old wirings.
type creativityProvider interface {
	GetCreativity(ctx context.Context, userID string) (float64, error)
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
	generator         Generator
	wardrobe          wardrobeRepository
	recent            recentOutfitProvider
	userProfile       userProfileProvider
	surfaces          surfaceProvider
	useVision         bool
	cache             Cache
	llmRecorder       llmRecorder             // optional — nil disables LLM call logging
	budgetEnforcer    BudgetEnforcer          // optional — nil disables P4-02 budget gate
	archetypeDefaults archetypeDefaultsLoader // optional — when nil, only user wardrobe feeds the LLM
	fillerSeeder      fillerSeeder            // optional — pairs with archetypeDefaults; materialises picked fillers into user wardrobe
	logger            *log.Logger
}

// llmRecorder is the narrow interface the outfit service needs from
// observability.LLMRecorder. Defining it here keeps the dependency
// direction one-way (outfit doesn't import observability), so future
// reuse of the recorder in detection / search doesn't ripple imports.
type llmRecorder interface {
	Record(ctx context.Context, cc LLMRecorderContext, obs LLMRecorderObservation)
}

// BudgetEnforcer is the pre-call gate the service consults before
// invoking the LLM. Defined as an interface so:
//
//   - The budget package owns the type definitions for Decision +
//     Reason (which travel through this interface) and the outfit
//     package doesn't pull them in.
//   - Tests can substitute a fake that always Allows (or always
//     Denies) without spinning up Redis.
//
// A nil enforcer means "no enforcement" — every call goes through.
// That's the boot mode when Redis is down or the budget feature
// isn't wired yet (matches the rest of the package's
// optional-deps-via-nil convention).
type BudgetEnforcer interface {
	// Check is called immediately before generator.Generate.
	// Returns shouldAllow + a reason struct + error. Reason is
	// untyped (map[string]any) here so the budget package's types
	// can flow without a one-way dep needing reverse imports —
	// the handler decodes the map.
	Check(ctx context.Context, userID string, estimatedUSD float64) (allow bool, reason map[string]any, err error)
}

// BudgetError is returned by GenerateOutfits when the enforcer
// denies a call. The handler maps this to HTTP 429 with the
// Reason map in the body. The map carries the budget package's
// Reason fields (code, message, dailyCapUSD, todaySpendUSD,
// estimatedUSD, suspendedUntil) — the outfit package keeps it
// untyped to avoid pulling in the budget package's types.
type BudgetError struct {
	Reason map[string]any
}

func (e *BudgetError) Error() string {
	if e == nil || e.Reason == nil {
		return "outfit: budget exceeded"
	}
	if msg, ok := e.Reason["message"].(string); ok && msg != "" {
		return "outfit: " + msg
	}
	return "outfit: budget exceeded"
}

// LLMRecorderContext mirrors observability.CallContext — defined in this
// package so the outfit service can build it without importing the
// observability package directly.
type LLMRecorderContext struct {
	UserID          string
	Feature         string
	TraceID         string
	PromptText      string
	SystemPrompt    string   // P1-11 archival: rendered system prompt
	UserMessage     string   // P1-11 archival: rendered user message
	WardrobeItemIDs []string // P1-11 archival: items present at call time
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
	RawResponse      string // P1-11 archival: model's text/tool-use payload
	StartedAt        time.Time
	EndedAt          time.Time
	Err              error
}

// ServiceConfig holds the dependencies needed to construct a Service.
type ServiceConfig struct {
	Generator         Generator
	Wardrobe          wardrobeRepository
	Recent            recentOutfitProvider
	UserProfile       userProfileProvider
	Surfaces          surfaceProvider // optional — when nil, outfits ship without panel/background URLs
	UseVision         bool
	Cache             Cache
	LLMRecorder       llmRecorder             // optional — nil disables LLM call logging
	BudgetEnforcer    BudgetEnforcer          // optional — nil disables P4-02 budget gate
	ArchetypeDefaults archetypeDefaultsLoader // optional — when set, the LLM pool is widened with archetype-default fillers (lower preference)
	FillerSeeder      fillerSeeder            // optional — when set, fillers picked by the LLM materialise as wi_<hex> items in the user's wardrobe before the outfit response leaves the service
}

// NewService creates an outfit Service.
func NewService(logger *log.Logger, cfg ServiceConfig) *Service {
	return &Service{
		generator:         cfg.Generator,
		wardrobe:          cfg.Wardrobe,
		recent:            cfg.Recent,
		userProfile:       cfg.UserProfile,
		surfaces:          cfg.Surfaces,
		useVision:         cfg.UseVision,
		cache:             cfg.Cache,
		llmRecorder:       cfg.LLMRecorder,
		budgetEnforcer:    cfg.BudgetEnforcer,
		archetypeDefaults: cfg.ArchetypeDefaults,
		fillerSeeder:      cfg.FillerSeeder,
		logger:            logger,
	}
}

// GenerateOutfits runs the full outfit generation pipeline: fetch wardrobe
// items, score archetypes, check cache, call the LLM generator, validate,
// apply fallback if needed, and cache the result.
func (s *Service) GenerateOutfits(ctx context.Context, userID string, weather Weather) ([]Outfit, error) {
	return s.GenerateOutfitsWithProgress(ctx, userID, weather, nil)
}

// GenerateOutfitsWithProgress is GenerateOutfits with an
// optional progress callback (mootd#62). Today's callback fires
// stage milestones (connecting → streaming → done) around the
// existing buffered LLM call; per-token streaming inside the
// generators is a follow-up. nil callback degrades to the old
// behaviour exactly.
//
// The connecting stage fires synchronously before the LLM call
// so the client immediately sees the connection is alive.
// During the LLM call, a goroutine fires a `streaming` heartbeat
// every 2s (with a hint description that escalates over time)
// so the client knows we haven't stalled. On completion, the
// `done` stage carries the final outfits.
func (s *Service) GenerateOutfitsWithProgress(
	ctx context.Context,
	userID string,
	weather Weather,
	onProgress StreamCallback,
) ([]Outfit, error) {
	if onProgress != nil {
		// Fire-and-forget; if the wire-write fails we let the
		// generation proceed — the client sees the failure when
		// the SSE connection drops.
		_ = onProgress(GenerateProgress{Stage: StageConnecting})
	}

	// Heartbeat goroutine: keeps the SSE connection alive while
	// the LLM call is in-flight. Cancelled when the call returns
	// (success or failure).
	if onProgress != nil {
		hbCtx, cancelHb := context.WithCancel(ctx)
		defer cancelHb()
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			messages := []string{
				"Drafting outfits…",
				"Picking pieces from your wardrobe…",
				"Matching to today's weather…",
				"Almost there…",
			}
			i := 0
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-ticker.C:
					_ = onProgress(GenerateProgress{
						Stage:       StageStreaming,
						Description: messages[i%len(messages)],
					})
					i++
				}
			}
		}()
	}

	outfits, err := s.generateOutfitsImpl(ctx, userID, weather)
	if err != nil {
		if onProgress != nil {
			_ = onProgress(GenerateProgress{Stage: StageError, Description: err.Error()})
		}
		return nil, err
	}
	if onProgress != nil {
		_ = onProgress(GenerateProgress{Stage: StageDone, Outfits: outfits})
	}
	return outfits, nil
}

// generateOutfitsImpl is the original buffered pipeline. Kept
// private so GenerateOutfitsWithProgress can wrap it without
// callers seeing the signature shift.
func (s *Service) generateOutfitsImpl(ctx context.Context, userID string, weather Weather) ([]Outfit, error) {
	userItems, err := s.wardrobe.FindByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetch wardrobe: %w", err)
	}

	if len(userItems) == 0 {
		return []Outfit{}, nil
	}

	// Archetype context. Scoring is computed off the user's own
	// items only — fillers we add below shouldn't bias the user's
	// archetype profile, otherwise loading defaults for archetype X
	// would push their profile further toward X regardless of what
	// they've actually uploaded.
	wardrobeScores := archetype.ScoreItems(itemsToTraits(userItems))

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

	// Widen the LLM-facing pool with archetype-default fillers
	// curated in admin. Only the top-1 archetype contributes to keep
	// the prompt lean; cap matches the system prompt's "use sparingly"
	// guidance — pool size shouldn't dwarf the user's own items.
	//
	// Items are folded into the same `items` slice the rest of the
	// pipeline already understands, but their Preferred flag is set
	// to false (vs true for user-owned). The system prompt + the
	// star-marked user-message inventory tell the LLM to lean on
	// Preferred items first and reach for fillers only when needed
	// to complete an outfit.
	items := userItems
	preferredIDs := make(map[string]bool, len(userItems))
	for _, it := range userItems {
		preferredIDs[it.ID] = true
	}
	if s.archetypeDefaults != nil && len(topArchetypes) > 0 {
		// Load a generous pool, then sample a smaller subset so that
		// successive generations see different fillers. Without this,
		// even a user with 24 fillers + 4 own items would lock onto
		// the same combinations once the LLM picked a "favourite"
		// stylistic mix; rotating the visible subset forces it to
		// reach for fresh items each call.
		const (
			fillerLoadCap   = 60 // upper bound we read from Mongo
			fillerVisibleCap = 18 // bound that actually reaches the LLM
		)
		topArche := topArchetypes[0].Name
		fillers, err := s.archetypeDefaults.LoadFor(ctx, userID, topArche, fillerLoadCap)
		if err != nil {
			s.logger.Printf("outfit: archetype-defaults load for %s/%s failed: %v (proceeding with user wardrobe only)", userID, topArche, err)
		} else if len(fillers) > 0 {
			fillers = sampleFillers(fillers, fillerVisibleCap)
			items = make([]wardrobe.ClothingItem, 0, len(userItems)+len(fillers))
			items = append(items, userItems...)
			items = append(items, fillers...)
			s.logger.Printf("outfit: pool widened for user %s — own=%d, fillers=%d (archetype=%s, sampled from a larger pool for variety)",
				userID, len(userItems), len(fillers), topArche)
		}
	}

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

	genItems := itemsToGenItemsWithPreference(items, preferredIDs)

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

	// mootd#67 — read creativity preference if the wired
	// userProfile satisfies the optional creativityProvider
	// interface. Failure / missing interface → 0 → generator
	// keeps its compiled-in default.
	var creativity float64
	if cp, ok := s.userProfile.(creativityProvider); ok {
		if c, err := cp.GetCreativity(ctx, userID); err != nil {
			s.logger.Printf("outfit: creativity fetch failed for user %s: %v (using provider default)", userID, err)
		} else {
			creativity = c
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
		Creativity:    creativity,
	}

	s.logger.Printf("outfit: %s generator for user %s (%d items, weather=%s/%s, recent=%d, archetype=%s)",
		s.generator.Name(), userID, len(items), weather.Temperature, weather.Condition, len(recentBoards),
		formatTopArchetypes(topArchetypes))

	// Budget gate (P4-02 / mootd-admin#30). The estimate is an
	// upper-bound for one outfit-generation call; chosen to be
	// conservative without hard-coding to a specific provider's
	// pricing. ~$0.20 covers the 90th-percentile Sonnet 4 call
	// (4-5K input tokens × $3/M + 1.5K output × $15/M ≈ $0.04;
	// vision adds ~$0.10). For Ollama / OpenAI mini this is
	// over-estimating, but the gate's job is to refuse calls that
	// *might* breach the cap; over-estimation just makes us
	// stricter, not unsafe.
	const estimatedCallUSD = 0.20
	if s.budgetEnforcer != nil {
		allow, reason, berr := s.budgetEnforcer.Check(ctx, userID, estimatedCallUSD)
		if berr != nil {
			// Treat enforcement-side errors as Allow so a Redis
			// blip doesn't deny service. Log + continue.
			s.logger.Printf("outfit: budget check for user %s: %v (allowing through)", userID, berr)
		} else if !allow {
			s.logger.Printf("outfit: budget gate denied user %s: %v", userID, reason)
			return nil, &BudgetError{Reason: reason}
		}
	}

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
	//
	// P1-11 archival (Step B / mootd-admin#16): re-render the system
	// + user prompt for storage. Each generator builds the prompt
	// internally; rebuilding it here means we hash the *exact* string
	// the generator saw, not a placeholder. The two builders
	// (BuildSystemPromptForEval + BuildUserMessage) are pure of side
	// effects and run in single-digit milliseconds at our wardrobe
	// sizes — cheap enough to do unconditionally.
	if s.llmRecorder != nil && usage != nil {
		systemPrompt := BuildSystemPromptForEval(weather, recentBoards, topArchetypes, panels, backgrounds)
		userMessage := BuildUserMessage(genItems)
		itemIDs := make([]string, 0, len(genItems))
		for _, it := range genItems {
			itemIDs = append(itemIDs, it.ID)
		}
		s.llmRecorder.Record(ctx, LLMRecorderContext{
			UserID:          userID,
			Feature:         "outfit_generate",
			SystemPrompt:    systemPrompt,
			UserMessage:     userMessage,
			WardrobeItemIDs: itemIDs,
			// PromptText drives the dedupe hash. Recorder will also
			// hash from SystemPrompt+UserMessage when this is empty;
			// we set it explicitly here so the format stays stable
			// even if the recorder's fallback changes.
			PromptText: systemPrompt + "\n" + userMessage,
		}, LLMRecorderObservation{
			Provider:         usage.Provider,
			Model:            usage.Model,
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			PromptVersion:    usage.PromptVersion,
			RawResponse:      usage.RawResponse,
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

	// Fillers are intentionally NOT auto-seeded into the user's
	// wardrobe. Picked ad_<hex> ids stay in the outfit response so
	// the FE can render them as "stylist suggestions" with a tap-
	// to-resolve affordance: the user explicitly chooses whether
	// each filler is something they own IRL (→ POST
	// /v1/wardrobe/items/from-archetype-default seeds it), or not
	// (→ POST /v1/wardrobe/archetype-rejections, then the loader
	// excludes it from this user's pool forever).
	//
	// This keeps the user's closet honest — only items they've
	// claimed appear there — and the rejection list closes the
	// loop so the same suggestion doesn't keep coming back.

	// Resolve per-item snapshots inline so the FE renders without a
	// second roundtrip. Built from the combined `items` slice
	// (user wardrobe + archetype-default fillers loaded above) so
	// virtual ad_<hex> ids resolve to label + imageUrl right here —
	// the FE doesn't need to reconcile them against /v1/wardrobe.
	// Source ("owned" | "filler") tells the FE which UX to show.
	filtered = attachItemSnapshots(filtered, items, preferredIDs)

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

// attachItemSnapshots fills outfit.ItemSnapshots from the combined
// items slice. The FE renders moodboard tiles straight from these
// snapshots (no /v1/wardrobe lookup needed), so virtual ad_<hex>
// fillers resolve correctly even though they live outside the user's
// wardrobe collection. Source = "owned" when the item is in
// preferredIDs (the user uploaded it), "filler" otherwise.
func attachItemSnapshots(outfits []Outfit, allItems []wardrobe.ClothingItem, preferredIDs map[string]bool) []Outfit {
	if len(outfits) == 0 || len(allItems) == 0 {
		return outfits
	}
	byID := make(map[string]wardrobe.ClothingItem, len(allItems))
	for _, it := range allItems {
		byID[it.ID] = it
	}
	for i := range outfits {
		o := &outfits[i]
		if len(o.Items) == 0 {
			continue
		}
		snaps := make([]OutfitItemSnapshot, 0, len(o.Items))
		for _, id := range o.Items {
			it, ok := byID[id]
			if !ok {
				// Should never happen post-ValidateOutfits, but
				// failing soft preserves the existing item id list
				// so the FE can degrade.
				continue
			}
			source := "filler"
			if preferredIDs[id] {
				source = "owned"
			}
			snaps = append(snaps, OutfitItemSnapshot{
				ID:          it.ID,
				Category:    it.Category,
				Label:       it.Label,
				ImageURL:    it.ImageURL,
				PngImageURL: it.PngImageURL,
				Source:      source,
			})
		}
		o.ItemSnapshots = snaps
	}
	return outfits
}

// itemsToGenItems trims wardrobe items down to the generator-facing
// GenItem shape. Every item is marked Preferred (the "wardrobe is the
// only source" path); kept for tests + callers that don't fold in
// archetype-default fillers. The runtime pipeline goes through
// itemsToGenItemsWithPreference instead.
func itemsToGenItems(items []wardrobe.ClothingItem) []GenItem {
	out := make([]GenItem, len(items))
	for i, item := range items {
		out[i] = GenItem{
			ID:        item.ID,
			Category:  item.Category,
			Label:     item.Label,
			Traits:    item.Traits,
			Preferred: true,
			Weight:    1.0,
		}
	}
	return out
}

// itemsToGenItemsWithPreference is itemsToGenItems with explicit
// preference control. preferredIDs is the set of IDs that should be
// flagged Preferred=true (typically the user's own wardrobe). Items
// whose ID isn't in the set come through with Preferred=false +
// Weight=FillerWeight (archetype-default fillers); the prompt + system
// message then tell the LLM to balance the numeric weights when
// composing each outfit, with a target filler count per outfit that
// scales inversely with wardrobe size.
func itemsToGenItemsWithPreference(items []wardrobe.ClothingItem, preferredIDs map[string]bool) []GenItem {
	out := make([]GenItem, len(items))
	for i, item := range items {
		preferred := preferredIDs[item.ID]
		weight := FillerWeight
		if preferred {
			weight = 1.0
		}
		out[i] = GenItem{
			ID:        item.ID,
			Category:  item.Category,
			Label:     item.Label,
			Traits:    item.Traits,
			Preferred: preferred,
			Weight:    weight,
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

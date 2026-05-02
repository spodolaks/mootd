// Package app wires together all domain handlers and builds the root HTTP handler.
package app

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/auth"
	"mootd/backend/internal/brands"
	"mootd/backend/internal/feedback"
	"mootd/backend/internal/generic"
	"mootd/backend/internal/health"
	"mootd/backend/internal/moodboard"
	"mootd/backend/internal/observability"
	"mootd/backend/internal/outfit"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/surface"
	"mootd/backend/internal/user"
	"mootd/backend/internal/wardrobe"
)

// App holds the dependencies shared across all domain handlers.
type App struct {
	Logger              *log.Logger
	MongoClient         *mongo.Client
	MongoDB             string
	JWTSecret           string
	AdminJWTSecret      string
	CORSAllowedOrigins  []string
	DetectionAPIBaseURL string
	DetectionAPIKey     string
	OllamaBaseURL       string
	OllamaModel         string
	BgRemoverBaseURL    string
	Environment         string

	// Outfit generator selection.
	OutfitProvider   string
	AnthropicBaseURL string
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicVision  bool

	// OpenAI DALL-E for moodboard image generation.
	OpenAIBaseURL string
	OpenAIAPIKey  string

	// Redis URL for caching, rate limiting, and async jobs.
	RedisURL string

	// EnableMockLogin gates the /v1/auth/mock-login development endpoint.
	// Fail-closed: must be explicitly true in config *and* Environment must not
	// be "production".
	EnableMockLogin bool
}

// NewHTTPHandler wires up all domain routes and wraps the mux with shared middleware.
func (a *App) NewHTTPHandler(workerCtx context.Context) (http.Handler, wardrobe.Repository, moodboard.Repository, func()) {
	// ── Redis connection (best-effort — falls back gracefully) ────────
	redisOpts, err := redis.ParseURL(a.RedisURL)
	var redisClient *redis.Client
	if err == nil {
		redisClient = redis.NewClient(redisOpts)
		if pingErr := redisClient.Ping(context.Background()).Err(); pingErr != nil {
			a.Logger.Printf("WARNING: Redis unavailable (%v) — falling back to MongoDB cache", pingErr)
			redisClient = nil
		} else {
			a.Logger.Printf("Redis connected: %s", a.RedisURL)
		}
	} else {
		a.Logger.Printf("WARNING: invalid REDIS_URL — falling back to MongoDB cache")
	}

	mux := http.NewServeMux()
	authMiddleware := middleware.Auth(a.JWTSecret)

	// Per-route rate limiters. Only wired when Redis is available — the global
	// in-memory fallback already covers the "service under flood" case at a
	// coarser granularity, and we don't want two independent counters drifting
	// across instances in a multi-pod deploy.
	var authLimit middleware.Middleware
	var outfitBurstLimit middleware.Middleware
	var outfitDailyLimit middleware.Middleware
	var feedbackLimit middleware.Middleware
	if redisClient != nil {
		// 20/min per IP on unauth'd auth endpoints blunts refresh-token spraying
		// and credential enumeration without annoying a real user.
		authLimit = middleware.RedisRateLimitScoped(redisClient, "auth", 20, 1*time.Minute)
		// 5/min per user contains accidental spam from a retry loop; 50/day is
		// the real cost ceiling against a determined caller hitting the LLM
		// provider.
		outfitBurstLimit = middleware.RedisRateLimitScoped(redisClient, "outfit:generate:burst", 5, 1*time.Minute)
		outfitDailyLimit = middleware.RedisRateLimitScoped(redisClient, "outfit:generate:daily", 50, 24*time.Hour)
		// Feedback is cheap to store but easy to spam from a broken client loop;
		// 120/min per user is well above normal usage and prevents log-flood.
		feedbackLimit = middleware.RedisRateLimitScoped(redisClient, "feedback", 120, 1*time.Minute)
	}

	// Mock-login is only registered when explicitly opted-in via config. Any
	// production deploy that forgets to set ENVIRONMENT stays safe — the default
	// is off.
	enableMockLogin := a.EnableMockLogin && a.Environment != "production"
	authRepo := auth.NewMongoRepository(a.MongoClient, a.MongoDB)
	auth.NewHandler(a.Logger, authRepo, a.JWTSecret).RegisterRoutes(mux, enableMockLogin, authLimit)

	// Admin auth (P0-03) + first protected endpoints (P0-04 audit log
	// foundation, P1-05 users list). Separate JWT issuer, separate
	// signing secret, separate persistence. Fails loudly in production
	// if the admin secret is unset or matches JWTSecret — see config.Load.
	adminRepo, err := admin.NewMongoRepository(context.Background(), a.MongoClient, a.MongoDB)
	if err != nil {
		a.Logger.Fatalf("admin repo init: %v", err)
	}
	adminUsersRepo := admin.NewUsersMongoRepository(a.MongoClient, a.MongoDB)
	adminOverviewRepo := admin.NewOverviewMongoRepository(a.MongoClient, a.MongoDB)
	adminTracesRepo := admin.NewTracesMongoRepository(a.MongoClient, a.MongoDB)
	requireAdmin := middleware.RequireAdminAuth(a.AdminJWTSecret)
	adminHandler := admin.NewHandler(a.Logger, adminRepo, adminUsersRepo, adminOverviewRepo, adminTracesRepo, a.AdminJWTSecret)

	// Per-user budget caps (P4-01 / mootd-admin#29). Best-effort wiring:
	// startup logs but doesn't gate if the index ensure fails — the
	// /budget endpoint then serves the static defaults read-only.
	if budgetsRepo, err := admin.NewUserBudgetsMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		adminHandler.WithUserBudgets(budgetsRepo)
	} else {
		a.Logger.Printf("admin: user_budgets repo init failed: %v (continuing read-only on /budget)", err)
	}

	adminHandler.RegisterRoutes(mux, authLimit, requireAdmin)

	userRepo := user.NewMongoRepository(a.MongoClient, a.MongoDB)
	health.NewHandler(a.Logger, a.MongoClient, a.MongoDB).RegisterRoutes(mux)

	bgRemover := wardrobe.NewBackgroundRemover(a.BgRemoverBaseURL)
	detector := wardrobe.NewDetector(a.DetectionAPIBaseURL, a.DetectionAPIKey, a.Logger)
	searcher := wardrobe.NewSearcher(a.DetectionAPIBaseURL, a.DetectionAPIKey)
	wardrobeRepo := wardrobe.NewMongoRepository(a.MongoClient, a.MongoDB)
	// Async detection uses Redis for job state. Same fallback pattern as
	// outfit generation: when Redis is down, the async endpoints return 503
	// and clients can still use the sync /v1/wardrobe/detect path.
	var detectJobs *wardrobe.DetectJobStore
	if redisClient != nil {
		detectJobs = wardrobe.NewDetectJobStore(redisClient)
	}
	// Detection-run archive (P1-04 / mootd-admin#16). Best-effort:
	// init failure logs but doesn't gate startup. The wardrobe
	// handler keeps working (detection, item save), it just stops
	// archiving the input photo + per-image cost.
	detectionRunRepo, err := wardrobe.NewDetectionRunMongoRepository(context.Background(), a.MongoClient, a.MongoDB)
	if err != nil {
		a.Logger.Printf("wardrobe: detection_runs repo init failed: %v (continuing without archive)", err)
	}
	wardrobeHandler := wardrobe.NewHandler(a.Logger, detector, searcher, wardrobeRepo, bgRemover, workerCtx, detectJobs)
	if detectionRunRepo != nil {
		wardrobeHandler.WithDetectionRuns(detectionRunRepo)
		// Same archive readable from the admin side via the wardrobe→admin adapter.
		adminHandler.WithDetectionRuns(newDetectionRunAdapter(detectionRunRepo, detector))
	}
	wardrobeHandler.RegisterRoutes(mux, authMiddleware)

	brands.NewHandler(a.Logger, brands.NewMongoRepository(a.MongoClient, a.MongoDB)).RegisterRoutes(mux, authMiddleware)

	// Shared adapters for archetype profile read/write via user repo.
	profileProvider := outfit.UserProfileFunc(func(ctx context.Context, userID string) (map[string]float64, error) {
		doc, err := userRepo.FindByID(ctx, userID)
		if err != nil {
			return nil, err
		}
		return doc.ArchetypeProfile, nil
	})

	profileUpdater := moodboard.ProfileUpdaterFunc{
		GetFn: func(ctx context.Context, userID string) (map[string]float64, error) {
			doc, err := userRepo.FindByID(ctx, userID)
			if err != nil {
				return nil, err
			}
			return doc.ArchetypeProfile, nil
		},
		UpdateFn: func(ctx context.Context, userID string, profile map[string]float64) error {
			return userRepo.UpdateArchetypeProfile(ctx, userID, profile)
		},
	}

	moodboardRepo := moodboard.NewMongoRepository(a.MongoClient, a.MongoDB)

	// Feedback collection is registered alongside moodboard — the save hook
	// wired below is currently the only server-side emitter.
	feedbackRepo := feedback.NewMongoRepository(a.MongoClient, a.MongoDB)
	feedback.NewHandler(a.Logger, feedbackRepo).RegisterRoutes(mux, authMiddleware, feedbackLimit)

	// Forward-declared so the moodboard onSave closure can stamp the name of
	// the currently-active generator on every feedback event. The generator
	// is actually constructed further down (it depends on OutfitProvider +
	// Anthropic config); the closure reads this variable at emit time — long
	// after wiring is done — so late assignment is safe.
	var generatorName string

	// Emit a "saved" feedback event every time a moodboard is saved. We keep
	// this wiring here so the moodboard package doesn't import feedback
	// directly; the hook translates moodboard types to feedback types at the
	// boundary. Errors are logged and swallowed — feedback is best-effort and
	// must never block a user save.
	moodboardOnSave := moodboard.SaveEventFn(func(ctx context.Context, userID string, req moodboard.SaveRequest, saved moodboard.SavedMoodBoard) {
		event := feedback.Event{
			ID:               saved.ID + ":saved",
			UserID:           userID,
			JobID:            req.JobID,
			ChosenOutfitID:   req.Outfit.ID,
			Action:           feedback.ActionSaved,
			GeneratedBatch:   toFeedbackSnapshots(req.GeneratedBatch, req.Outfit),
			Context:          toFeedbackContext(req.Outfit),
			PromptVersion:    outfit.PromptVersion,
			GeneratorVersion: generatorName,
			SchemaVersion:    feedback.CurrentSchemaVersion,
			CreatedAt:        saved.CreatedAt,
		}
		if err := feedbackRepo.Insert(ctx, event); err != nil {
			a.Logger.Printf("moodboard: emit saved-feedback for user %s board %s: %v", userID, saved.ID, err)
		}
	})
	moodboard.NewHandler(a.Logger, moodboardRepo, wardrobeRepo, profileUpdater, moodboardOnSave).RegisterRoutes(mux, authMiddleware)

	// Wire the user-deletion cascade now that we have all the owning repos.
	// Order: images + items → moodboards → feedback → user doc. The user doc
	// is deleted last so that if any earlier step fails, the user can still
	// re-authenticate and retry; deleting the user first would strand
	// orphaned data.
	userCascade := user.CascadeFn(func(ctx context.Context, userID string) error {
		if n, err := wardrobeRepo.DeleteAllByUser(ctx, userID); err != nil {
			return err
		} else if n > 0 {
			a.Logger.Printf("delete account: removed %d wardrobe items for user %s", n, userID)
		}
		if n, err := moodboardRepo.DeleteAllByUser(ctx, userID); err != nil {
			return err
		} else if n > 0 {
			a.Logger.Printf("delete account: removed %d moodboards for user %s", n, userID)
		}
		if n, err := feedbackRepo.DeleteAllByUser(ctx, userID); err != nil {
			return err
		} else if n > 0 {
			a.Logger.Printf("delete account: removed %d feedback events for user %s", n, userID)
		}
		return userRepo.DeleteByID(ctx, userID)
	})
	user.NewHandler(a.Logger, userRepo, userCascade).RegisterRoutes(mux, authMiddleware)

	// Surfaces (panel + background textures) — public image endpoint, no auth.
	surfaceRepo := surface.NewMongoRepository(a.MongoClient, a.MongoDB)
	surface.NewHandler(a.Logger, surfaceRepo).RegisterRoutes(mux)

	recentProvider := outfit.RecentOutfitFunc(func(ctx context.Context, userID string, limit int) ([]outfit.RecentBoard, error) {
		boards, err := moodboardRepo.FindRecent(ctx, userID, limit)
		if err != nil {
			return nil, err
		}
		result := make([]outfit.RecentBoard, len(boards))
		for i, b := range boards {
			// Pull the top archetype by score so the prompt can reference the
			// user's dominant taste at the time of the save — useful because
			// the archetype profile evolves and this pins the example to the
			// register it was authored under.
			var topArch string
			var topScore float64
			for name, score := range b.Outfit.ArchetypeScores {
				if score > topScore {
					topScore = score
					topArch = name
				}
			}
			result[i] = outfit.RecentBoard{
				OutfitName:   b.Outfit.Name,
				ItemIDs:      b.Outfit.Items,
				Description:  b.Outfit.Description,
				Rationale:    b.Outfit.Rationale,
				TopArchetype: topArch,
				Palette:      b.Outfit.Palette,
			}
		}
		return result, nil
	})

	// Pick the active outfit generator. The OUTFIT_PROVIDER env var
	// accepts:
	//   - a single name (e.g. "claude", "openai", "ollama") — single
	//     provider, original behaviour
	//   - a comma-separated chain (e.g. "claude,openai,ollama") —
	//     wraps in a CascadeGenerator that falls through on transient
	//     failures. Health-tracked per provider; unhealthy providers
	//     skipped during a 60s cooldown after 3 consecutive failures.
	//
	// Recommended production setup: "claude,openai,ollama". Single-
	// provider deploys keep the original simpler path.
	useVision := false
	buildOne := func(name string) outfit.Generator {
		switch name {
		case "claude":
			useVision = useVision || a.AnthropicVision
			return outfit.NewClaudeGenerator(outfit.ClaudeConfig{
				BaseURL: a.AnthropicBaseURL,
				APIKey:  a.AnthropicAPIKey,
				Model:   a.AnthropicModel,
				Vision:  a.AnthropicVision,
			}, a.Logger, wardrobeRepo)
		case "openai":
			return outfit.NewOpenAIGenerator(outfit.OpenAIConfig{
				BaseURL: a.OpenAIBaseURL,
				APIKey:  a.OpenAIAPIKey,
			}, a.Logger)
		default:
			return outfit.NewOllamaGenerator(outfit.OllamaConfig{
				BaseURL: a.OllamaBaseURL,
				Model:   a.OllamaModel,
			})
		}
	}

	chainNames := strings.Split(a.OutfitProvider, ",")
	for i := range chainNames {
		chainNames[i] = strings.TrimSpace(chainNames[i])
	}
	// Filter out empty entries (handles trailing commas / "" input).
	chain := make([]outfit.Generator, 0, len(chainNames))
	cleaned := make([]string, 0, len(chainNames))
	for _, n := range chainNames {
		if n == "" {
			continue
		}
		chain = append(chain, buildOne(n))
		cleaned = append(cleaned, n)
	}
	if len(chain) == 0 {
		// Empty / all-whitespace OUTFIT_PROVIDER → Ollama default.
		chain = []outfit.Generator{buildOne("ollama")}
		cleaned = []string{"ollama"}
	}

	var generator outfit.Generator
	if len(chain) == 1 {
		generator = chain[0]
		a.Logger.Printf("outfit: single provider %s (no cascade)", cleaned[0])
	} else {
		generator = outfit.NewCascadeGenerator(a.Logger, chain...)
		a.Logger.Printf("outfit: cascade chain %v", cleaned)
	}
	// Populate the name variable captured by moodboardOnSave. At this point
	// no request has been served yet, so the closure will always read a
	// populated value by the time it fires.
	generatorName = generator.Name()

	var outfitCache outfit.Cache
	if redisClient != nil {
		outfitCache = outfit.NewRedisCache(redisClient, 24*time.Hour)
	} else {
		outfitCache = outfit.NewMongoCache(a.MongoClient, a.MongoDB, 24*time.Hour, a.Logger)
	}

	// Outfit job store now writes Mongo (durable) + Redis (cache).
	// Mongo is required so jobs survive backend restart; Redis is
	// optional speed. Init also runs a stale-job recovery sweep —
	// any `processing` job older than 10min from a previous boot
	// gets marked failed (its goroutine died with the old process).
	jobStore, err := outfit.NewJobStore(context.Background(), a.MongoClient, a.MongoDB, redisClient, a.Logger)
	if err != nil {
		a.Logger.Fatalf("outfit: job store init: %v", err)
	}

	// Observability ledger (P1-01). Every LLM call writes one row to
	// llm_calls with model + tokens + cost (computed from the price
	// table at write time so historical rows stay accurate). The
	// recorder is best-effort — Mongo blips never fail the user's
	// outfit generation.
	llmCallRepo, err := observability.NewMongoLLMCallRepository(context.Background(), a.MongoClient, a.MongoDB)
	if err != nil {
		a.Logger.Fatalf("observability: llm_calls repo init: %v", err)
	}
	priceRepo, err := observability.NewMongoPriceRepository(context.Background(), a.MongoClient, a.MongoDB)
	if err != nil {
		a.Logger.Fatalf("observability: model_prices repo init: %v", err)
	}
	if err := observability.SeedDefaults(context.Background(), priceRepo, a.Logger); err != nil {
		a.Logger.Fatalf("observability: seed model prices: %v", err)
	}
	priceTable, err := observability.NewPriceTable(context.Background(), priceRepo, a.Logger)
	if err != nil {
		a.Logger.Fatalf("observability: price table init: %v", err)
	}
	priceTable.StartAutoRefresh(workerCtx, 5*time.Minute)
	// One core LLMRecorder, two domain adapters. Both detection and
	// outfit generation flow into the same llm_calls ledger so admins
	// see total spend across features in /admin/v1/overview.
	coreLLMRec := observability.NewLLMRecorder(llmCallRepo, priceTable, a.Logger)
	llmRecorder := observability.NewOutfitRecorderAdapter(coreLLMRec)
	detector.WithRecorder(observability.NewWardrobeRecorderAdapter(coreLLMRec))

	outfitService := outfit.NewService(a.Logger, outfit.ServiceConfig{
		Generator:   generator,
		Wardrobe:    wardrobeRepo,
		Recent:      recentProvider,
		UserProfile: profileProvider,
		Surfaces:    newSurfaceAdapter(surfaceRepo),
		UseVision:   useVision,
		Cache:       outfitCache,
		LLMRecorder: llmRecorder,
	})

	outfit.NewHandler(a.Logger, outfit.HandlerConfig{
		Service:   outfitService,
		JobStore:  jobStore,
		WorkerCtx: workerCtx,
	}).RegisterRoutes(mux, authMiddleware, outfitBurstLimit, outfitDailyLimit)

	genericRepo := generic.NewMongoRepository(a.MongoClient, a.MongoDB)
	generic.NewHandler(a.Logger, genericRepo, wardrobeRepo, profileProvider).RegisterRoutes(mux, authMiddleware)

	var rateLimiter func(http.Handler) http.Handler
	var rateLimitCloser func()
	if redisClient != nil {
		rateLimiter = middleware.RedisRateLimit(redisClient, 300, 1*time.Minute)
		rateLimitCloser = func() {} // Redis handles cleanup
	} else {
		rateLimiter, rateLimitCloser = middleware.RateLimit(100, 1*time.Minute)
	}

	// Middleware chain (outermost → innermost):
	//   RequestID → Recover → Logging → CORS → global rate limit → mux
	//
	// RequestID is outermost so panics, log lines, and the response header all
	// carry the same correlation ID. Recover sits just inside so it can log
	// with that ID before any handler state can be touched. Logging is inside
	// Recover so it sees the real status code (including 500s written by the
	// recovery handler).
	handler := middleware.RequestID(
		middleware.Recover(a.Logger)(
			middleware.Logging(a.Logger)(
				middleware.CORS(a.CORSAllowedOrigins)(
					rateLimiter(mux),
				),
			),
		),
	)
	return handler, wardrobeRepo, moodboardRepo, rateLimitCloser
}

// surfaceAdapter bridges surface.Repository to outfit's surfaceProvider
// interface. Keeping the adapter here means the outfit package never imports
// surface — the dependency direction stays one-way (app → outfit, app → surface).
type surfaceAdapter struct {
	repo surface.Repository
}

func newSurfaceAdapter(repo surface.Repository) *surfaceAdapter {
	return &surfaceAdapter{repo: repo}
}

func (a *surfaceAdapter) list(ctx context.Context, kind surface.Kind) ([]outfit.SurfaceOption, error) {
	items, err := a.repo.ListByKind(ctx, kind)
	if err != nil {
		return nil, err
	}
	opts := make([]outfit.SurfaceOption, len(items))
	for i, s := range items {
		opts[i] = outfit.SurfaceOption{
			ID:                s.ID,
			Name:              s.Name,
			Description:       s.Description,
			MoodTags:          s.MoodTags,
			ArchetypeAffinity: s.ArchetypeAffinity,
		}
	}
	return opts, nil
}

func (a *surfaceAdapter) ListPanels(ctx context.Context) ([]outfit.SurfaceOption, error) {
	return a.list(ctx, surface.KindPanel)
}

func (a *surfaceAdapter) ListBackgrounds(ctx context.Context) ([]outfit.SurfaceOption, error) {
	return a.list(ctx, surface.KindBackground)
}

func (a *surfaceAdapter) ResolveURL(id string) string {
	return "/v1/surfaces/" + id + "/image"
}

// StartWorkers launches background workers that run until ctx is cancelled.
func (a *App) StartWorkers(ctx context.Context, wardrobeRepo wardrobe.Repository) {
	bgRemover := wardrobe.NewBackgroundRemover(a.BgRemoverBaseURL)
	wardrobe.StartPNGRetryWorker(ctx, wardrobeRepo, bgRemover, a.Logger)
}

// toFeedbackSnapshots converts a moodboard batch (+ chosen outfit, which may or
// may not already be in the batch) into the trimmed shape the feedback log
// stores. Including the chosen outfit when it's missing from GeneratedBatch
// means even clients that only send the pick — not the rejects — still leave
// a usable trail.
func toFeedbackSnapshots(batch []moodboard.Outfit, chosen moodboard.Outfit) []feedback.OutfitSnapshot {
	if len(batch) == 0 && chosen.ID == "" && chosen.Name == "" {
		return nil
	}
	seen := make(map[string]bool, len(batch)+1)
	snapshots := make([]feedback.OutfitSnapshot, 0, len(batch)+1)
	for _, o := range batch {
		if o.ID != "" {
			if seen[o.ID] {
				continue
			}
			seen[o.ID] = true
		}
		snapshots = append(snapshots, feedback.OutfitSnapshot{
			ID:              o.ID,
			Name:            o.Name,
			Items:           append([]string(nil), o.Items...),
			Rationale:       o.Rationale,
			ArchetypeScores: o.ArchetypeScores,
		})
	}
	if chosen.ID != "" && !seen[chosen.ID] {
		snapshots = append(snapshots, feedback.OutfitSnapshot{
			ID:              chosen.ID,
			Name:            chosen.Name,
			Items:           append([]string(nil), chosen.Items...),
			Rationale:       chosen.Rationale,
			ArchetypeScores: chosen.ArchetypeScores,
		})
	}
	return snapshots
}

// toFeedbackContext extracts the coarse, non-PII signals we're comfortable
// shipping to the training pipeline. Weather is condensed to a short token
// (e.g. "sunny 18C") so downstream schemas don't need to parse a compound.
func toFeedbackContext(chosen moodboard.Outfit) feedback.Context {
	ctx := feedback.Context{}
	if chosen.Weather != nil {
		parts := make([]string, 0, 3)
		if chosen.Weather.Condition != "" {
			parts = append(parts, chosen.Weather.Condition)
		}
		if chosen.Weather.Temperature != "" {
			unit := chosen.Weather.Unit
			if unit == "" {
				unit = "C"
			}
			parts = append(parts, chosen.Weather.Temperature+unit)
		}
		if len(parts) > 0 {
			ctx.Weather = strings.Join(parts, " ")
		}
	}
	// DayOfWeek / Hour are filled in from createdAt by the hook's caller if
	// available. Archetype and Occasion come from the outfit when the client
	// supplies them; keep empty strings rather than guessing.
	if len(chosen.ArchetypeScores) > 0 {
		// Record the top archetype name so the ranker can segment by dominant taste.
		var topName string
		var topScore float64
		for name, score := range chosen.ArchetypeScores {
			if score > topScore {
				topScore = score
				topName = name
			}
		}
		ctx.Archetype = topName
	}
	return ctx
}

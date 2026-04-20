// Package app wires together all domain handlers and builds the root HTTP handler.
package app

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mootd/backend/internal/auth"
	"mootd/backend/internal/brands"
	"mootd/backend/internal/feedback"
	"mootd/backend/internal/generic"
	"mootd/backend/internal/health"
	"mootd/backend/internal/moodboard"
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

	userRepo := user.NewMongoRepository(a.MongoClient, a.MongoDB)
	health.NewHandler(a.Logger, a.MongoClient, a.MongoDB).RegisterRoutes(mux)

	bgRemover := wardrobe.NewBackgroundRemover(a.BgRemoverBaseURL)
	detector := wardrobe.NewDetector(a.DetectionAPIBaseURL, a.DetectionAPIKey, a.Logger)
	searcher := wardrobe.NewSearcher(a.DetectionAPIBaseURL, a.DetectionAPIKey)
	wardrobeRepo := wardrobe.NewMongoRepository(a.MongoClient, a.MongoDB)
	wardrobe.NewHandler(a.Logger, detector, searcher, wardrobeRepo, bgRemover, workerCtx).RegisterRoutes(mux, authMiddleware)

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
	moodboard.NewHandler(a.Logger, moodboardRepo, wardrobeRepo, profileUpdater).RegisterRoutes(mux, authMiddleware)

	// Feedback collection is registered alongside moodboard because that's
	// where save/skip events will originate from in a later PR. The endpoint
	// itself is callable now; nothing yet emits to it.
	feedbackRepo := feedback.NewMongoRepository(a.MongoClient, a.MongoDB)
	feedback.NewHandler(a.Logger, feedbackRepo).RegisterRoutes(mux, authMiddleware, feedbackLimit)

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
			result[i] = outfit.RecentBoard{OutfitName: b.Outfit.Name, ItemIDs: b.Outfit.Items}
		}
		return result, nil
	})

	// Pick the active outfit generator. "claude" uses Anthropic's Messages API
	// with tool use (and optionally vision); "ollama" falls back to the local
	// Qwen-style model. The choice is driven by OUTFIT_PROVIDER + ANTHROPIC_*
	// environment variables.
	var generator outfit.Generator
	useVision := false
	switch a.OutfitProvider {
	case "claude":
		claudeCfg := outfit.ClaudeConfig{
			BaseURL: a.AnthropicBaseURL,
			APIKey:  a.AnthropicAPIKey,
			Model:   a.AnthropicModel,
			Vision:  a.AnthropicVision,
		}
		generator = outfit.NewClaudeGenerator(claudeCfg, a.Logger, wardrobeRepo)
		useVision = a.AnthropicVision
		a.Logger.Printf("outfit: using Claude generator (model=%s, vision=%v)", a.AnthropicModel, a.AnthropicVision)
	case "openai":
		openaiCfg := outfit.OpenAIConfig{
			BaseURL: a.OpenAIBaseURL,
			APIKey:  a.OpenAIAPIKey,
		}
		generator = outfit.NewOpenAIGenerator(openaiCfg, a.Logger)
		a.Logger.Printf("outfit: using OpenAI generator (model=gpt-4o)")
	default:
		generator = outfit.NewOllamaGenerator(outfit.OllamaConfig{BaseURL: a.OllamaBaseURL, Model: a.OllamaModel})
		a.Logger.Printf("outfit: using Ollama generator (model=%s)", a.OllamaModel)
	}

	var outfitCache outfit.Cache
	if redisClient != nil {
		outfitCache = outfit.NewRedisCache(redisClient, 24*time.Hour)
	} else {
		outfitCache = outfit.NewMongoCache(a.MongoClient, a.MongoDB, 24*time.Hour, a.Logger)
	}

	var jobStore *outfit.JobStore
	if redisClient != nil {
		jobStore = outfit.NewJobStore(redisClient)
	}

	outfitService := outfit.NewService(a.Logger, outfit.ServiceConfig{
		Generator:   generator,
		Wardrobe:    wardrobeRepo,
		Recent:      recentProvider,
		UserProfile: profileProvider,
		Surfaces:    newSurfaceAdapter(surfaceRepo),
		UseVision:   useVision,
		Cache:       outfitCache,
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

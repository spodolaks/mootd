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

	enableMockLogin := a.Environment != "production"
	authRepo := auth.NewMongoRepository(a.MongoClient, a.MongoDB)
	auth.NewHandler(a.Logger, authRepo, a.JWTSecret).RegisterRoutes(mux, enableMockLogin)

	userRepo := user.NewMongoRepository(a.MongoClient, a.MongoDB)
	user.NewHandler(a.Logger, userRepo).RegisterRoutes(mux, authMiddleware)
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
	}).RegisterRoutes(mux, authMiddleware)

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

	handler := middleware.Logging(a.Logger)(
		middleware.CORS(a.CORSAllowedOrigins)(
			rateLimiter(mux),
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

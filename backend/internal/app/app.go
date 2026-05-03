// Package app wires together all domain handlers and builds the root HTTP handler.
package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/auth"
	"mootd/backend/internal/brands"
	"mootd/backend/internal/budget"
	"mootd/backend/internal/events"
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
	var eventsLimit middleware.Middleware
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
		// P2-02 / mootd-admin#19: analytics ingest. 600/min per
		// user. The batched SDK flushes every 10s + on queue
		// depth 25, so a normal session lands ~6 calls/min;
		// 600 leaves comfortable headroom for retry storms +
		// shorter batch intervals if we tune them later.
		eventsLimit = middleware.RedisRateLimitScoped(redisClient, "events", 600, 1*time.Minute)
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

	// Weekly cost report (P4-04 / mootd-admin#32). The repo runs
	// aggregations against llm_calls + users; SMTP config is
	// optional. When SMTP_HOST is unset, /reports/weekly still
	// works (admin previews on demand) but /send returns 503.
	reportsRepo := admin.NewReportsMongoRepository(a.MongoClient, a.MongoDB)
	var smtpCfg *admin.SMTPConfig
	if host := os.Getenv("SMTP_HOST"); host != "" {
		smtpCfg = &admin.SMTPConfig{
			Host:     host,
			Port:     os.Getenv("SMTP_PORT"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			FromAddr: os.Getenv("SMTP_FROM"),
			ToAddr:   os.Getenv("SMTP_TO"),
		}
		if smtpCfg.FromAddr == "" || smtpCfg.ToAddr == "" {
			a.Logger.Print("admin: SMTP_HOST set but SMTP_FROM / SMTP_TO missing; weekly report send disabled")
			smtpCfg = nil
		}
	}
	adminHandler.WithReports(reportsRepo, smtpCfg)
	if smtpCfg != nil {
		// Cron only fires when SMTP is configured. Without SMTP
		// the cron's send call would always 503; better to not
		// schedule it at all than to log a "send failed" every
		// Monday at 08:00.
		admin.StartWeeklyReportCron(workerCtx, a.Logger, nil, func(ctx context.Context) error {
			start, end := admin.LastCompletedISOWeek(time.Now().UTC())
			report, err := reportsRepo.Build(ctx, start, end)
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
			return admin.SendWeeklyReport(*smtpCfg, report)
		})
		a.Logger.Printf("admin: weekly report cron armed (SMTP %s → %s)", smtpCfg.Host, smtpCfg.ToAddr)
	}

	// Prompt templates (P3-01 / mootd-admin#24). Pulls the
	// constant parts of the outfit prompt out of Go strings and
	// into a Mongo collection. Best-effort wiring — when init or
	// seeding fails the system falls back to the hardcoded
	// constants. SetPromptTemplateProvider must run before any
	// outfit-gen call; the buildSystemPrompt path checks the
	// global at request time so late-binding here is safe.
	if templatesRepo, err := admin.NewPromptTemplatesMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		fallbacks := map[string]string{
			"outfit_system_base": outfit.DefaultSystemBaseTemplate(),
			"outfit_safety":      outfit.DefaultSafetyTemplate(),
		}
		if err := admin.SeedPromptTemplates(context.Background(), templatesRepo, fallbacks, a.Logger); err != nil {
			a.Logger.Printf("admin: prompt template seed failed: %v (falling back to hardcoded constants)", err)
		} else {
			cache := admin.NewCachedPromptTemplates(templatesRepo, fallbacks, a.Logger)
			// A/B testing (P3-05 / mootd-admin#28). Best-effort:
			// if the AB-test repo init fails the templates still
			// work, just without traffic splitting.
			var abCache *admin.CachedABTests
			if abRepo, abErr := admin.NewABTestMongoRepository(context.Background(), a.MongoClient, a.MongoDB); abErr == nil {
				abCache = admin.NewCachedABTests(abRepo, 0)
				adminHandler.WithABTests(abRepo, abCache)
			} else {
				a.Logger.Printf("admin: prompt_ab_tests repo init failed: %v (continuing without A/B testing)", abErr)
			}
			outfit.SetPromptTemplateProvider(newPromptTemplateAdapter(cache, abCache, templatesRepo))
			adminHandler.WithPromptTemplates(templatesRepo, cache)
			a.Logger.Print("admin: prompt templates wired (outfit_system_base + outfit_safety seeded)")
		}
	} else {
		a.Logger.Printf("admin: prompt_templates repo init failed: %v (continuing with hardcoded constants)", err)
	}

	// Funnels (P2-04 / mootd-admin#21). Best-effort wiring;
	// when the repo init fails (index ensure error) the
	// /funnels endpoints return 503.
	if funnelsRepo, err := admin.NewFunnelsMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		adminHandler.WithFunnels(funnelsRepo)
	} else {
		a.Logger.Printf("admin: admin_funnels repo init failed: %v (continuing without /funnels)", err)
	}

	// Session replay (P5-05 / mootd-admin#38). Best-effort:
	// init failure (e.g. TTL index ensure failed) just means the
	// FE silently no-ops on /events and the read endpoints
	// return 503. The admin still works; only audit replay is
	// affected.
	if sessionsRepo, err := admin.NewSessionsMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		adminHandler.WithSessions(sessionsRepo)
	} else {
		a.Logger.Printf("admin: session_events repo init failed: %v (continuing without session replay)", err)
	}

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

	// Analytics events (P2-02 / mootd-admin#19). Best-effort
	// init: index ensure failure logs but doesn't block startup
	// — without the indexes the writes still land, the read
	// surfaces in P2-04/05 just slow down.
	if eventsRepo, err := events.NewMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		events.NewHandler(a.Logger, eventsRepo).RegisterRoutes(mux, authMiddleware, eventsLimit)
	} else {
		a.Logger.Printf("events: repo init failed: %v (events ingest disabled)", err)
	}

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

	// P4-05 / mootd-admin#33: Wrap with the tier-routing layer so
	// admins can pick a provider per user tier without redeploy.
	//
	//   - Build the name→generator map from the chain.
	//   - When more than one provider is configured, wrap the
	//     existing cascade with TierRoutingGenerator. The cascade
	//     is the fallback so any misconfigured tier mapping
	//     degrades to "previous behaviour."
	//   - When only one provider is configured, skip the wrapper —
	//     the routing decision is moot but we *still* expose the
	//     admin endpoint so the surface is present and the UI
	//     reflects what's currently happening (free tier → that
	//     one provider).
	//   - Routing repo init is best-effort; failure logs and the
	//     admin endpoint returns 503.
	//
	// Tier resolver is FreeTierResolver in v1 — see the close
	// comment on mootd-admin#33 for the deferral rationale.
	byProvider := make(map[string]outfit.Generator, len(chain))
	for i, name := range cleaned {
		switch name {
		case "claude":
			byProvider["anthropic"] = chain[i]
		default:
			byProvider[name] = chain[i]
		}
	}
	if routingRepo, err := admin.NewRoutingMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		routingCache := admin.NewCachedRoutingReader(routingRepo, 0)
		if len(chain) > 1 {
			generator = outfit.NewTierRoutingGenerator(a.Logger, byProvider, routingCache, outfit.FreeTierResolver{}, generator)
			a.Logger.Printf("outfit: tier routing wired with providers %v", providerNamesFor(byProvider))
		} else {
			a.Logger.Printf("outfit: tier routing config exposed but not enforced (single provider %v)", cleaned)
		}
		adminHandler.WithRouting(routingRepo, routingCache, providerNamesFor(byProvider))
	} else {
		a.Logger.Printf("admin: model_routing repo init failed: %v (continuing without /model-routing)", err)
	}

	// Populate the name variable captured by moodboardOnSave. At this point
	// no request has been served yet, so the closure will always read a
	// populated value by the time it fires.
	generatorName = generator.Name()

	// P3-04 / mootd-admin#27: Eval suite. Wires the repo + loader
	// + runner into the already-registered admin handler; the
	// EvalsRouter handler reads these fields at request time, so
	// late-binding here (after RegisterRoutes) is safe — by the
	// time a request comes in, the fields are populated.
	//
	// All-or-nothing: if any of the three fail to init the
	// /evals/* endpoints return 503 with a clear message.
	if evalsRepo, err := admin.NewEvalsMongoRepository(context.Background(), a.MongoClient, a.MongoDB); err == nil {
		// EVAL_GOLDEN_DIR overrides the default for deployments
		// where the binary lives somewhere other than /app
		// (development off-Docker, custom runtime images, etc.).
		// Default matches the Dockerfile's COPY destination.
		goldenDir := os.Getenv("EVAL_GOLDEN_DIR")
		if goldenDir == "" {
			goldenDir = "eval/golden"
		}
		evalsLoader := admin.NewFilesystemSetLoader(goldenDir)
		evalGenerator := newEvalGeneratorAdapter(generator)
		// Avoid the typed-nil-in-interface gotcha: explicitly leave
		// the EvalJudge interface nil when no API key is set, so
		// `runner.judge != nil` checks behave the way the runner's
		// author expects.
		var evalJudge admin.EvalJudge
		if j := admin.NewAnthropicJudge(); j != nil {
			evalJudge = j
		}
		evalRunner := admin.NewEvalRunner(evalsRepo, evalsLoader, evalGenerator, evalJudge, a.Logger)
		adminHandler.WithEvalSuite(evalsRepo, evalsLoader, evalRunner)
		if evalJudge == nil {
			a.Logger.Print("admin: eval suite wired without LLM judge (ANTHROPIC_API_KEY unset). Cases run + record automated checks; judgeScore is 0.")
		} else {
			a.Logger.Print("admin: eval suite wired with Anthropic judge.")
		}
	} else {
		a.Logger.Printf("admin: eval_runs repo init failed: %v (continuing without /evals)", err)
	}

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

	// Budget tracker + enforcer (P4-02 / mootd-admin#30). Wire
	// before the outfit service so the service config can pin the
	// enforcer at construction time.
	//
	//   - SpendTracker is Redis-backed when Redis is up; falls
	//     back to NoopSpendTracker (zero spend, never suspends)
	//     so missing Redis doesn't block outfit generation.
	//   - BudgetReader is the user_budgets repo from #29 wrapped
	//     in a tiny adapter (newBudgetReaderAdapter, below).
	//   - Enforcer combines both. Wired into outfit.Service for
	//     pre-call gating + into the LLMRecorder for post-call
	//     spend bookkeeping.
	var spendTracker budget.SpendTracker = budget.NoopSpendTracker{}
	if redisClient != nil {
		spendTracker = budget.NewRedisSpendTracker(redisClient, "user")
	} else {
		a.Logger.Print("budget: Redis unavailable — spend tracker disabled (NoopSpendTracker). Per-user budget enforcement is OFF.")
	}
	var budgetEnforcer outfit.BudgetEnforcer
	if userBudgetsRepo, _ := admin.NewUserBudgetsMongoRepository(context.Background(), a.MongoClient, a.MongoDB); userBudgetsRepo != nil {
		budgetReader := newBudgetReaderAdapter(userBudgetsRepo)
		budgetEnforcer = newOutfitBudgetEnforcerAdapter(budget.NewEnforcer(budgetReader, spendTracker))
		// Bump the recorder so every successful llm_calls write
		// also increments today's spend in Redis.
		coreLLMRec.WithSpendTracker(spendTracker)
		// Surface today's spend on the admin Budget tab. Late-bind
		// onto the already-registered handler — RegisterRoutes
		// captured the method receiver, which reads h.budgetState
		// at request time, so this is safe.
		adminHandler.WithBudgetState(spendTracker)
	}

	llmRecorder := observability.NewOutfitRecorderAdapter(coreLLMRec)
	detector.WithRecorder(observability.NewWardrobeRecorderAdapter(coreLLMRec))

	outfitService := outfit.NewService(a.Logger, outfit.ServiceConfig{
		Generator:      generator,
		Wardrobe:       wardrobeRepo,
		Recent:         recentProvider,
		UserProfile:    profileProvider,
		Surfaces:       newSurfaceAdapter(surfaceRepo),
		UseVision:      useVision,
		Cache:          outfitCache,
		LLMRecorder:    llmRecorder,
		BudgetEnforcer: budgetEnforcer,
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

	// Admin IP allowlist (P5-03 / mootd-admin#36). Wraps only
	// /admin/v1/* paths so the user-facing API stays open. Empty
	// ADMIN_ALLOWED_IPS = no-op (development default).
	adminCIDRs := strings.Split(os.Getenv("ADMIN_ALLOWED_IPS"), ",")
	adminAllowlist := middleware.AdminIPAllowlist(adminCIDRs, a.Logger)
	gatedMux := http.NewServeMux()
	gatedMux.Handle("/admin/v1/", adminAllowlist(mux))
	gatedMux.Handle("/", mux)

	// Middleware chain (outermost → innermost):
	//   RequestID → Recover → Logging → CORS → global rate limit → admin allowlist → mux
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
					rateLimiter(gatedMux),
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


// providerNamesFor returns sorted provider names from a generator
// map. Used for the routing UI dropdown + the validator on PUT.
// Sorting makes the order deterministic across boots.
func providerNamesFor(byProvider map[string]outfit.Generator) []string {
	names := make([]string, 0, len(byProvider))
	for k := range byProvider {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

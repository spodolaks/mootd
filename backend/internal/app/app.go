// Package app wires together all domain handlers and builds the root HTTP handler.
package app

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
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
	"mootd/backend/internal/privacy"
	"mootd/backend/internal/shared/metrics"
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

	// Daily founder summary email (mootd-admin#99). Gated on
	// FOUNDER_EMAILS being set + SMTP configured. Different
	// cadence + content than the weekly report — daily numbers,
	// dashboard scope, multi-recipient. DAILY_SUMMARY_HOUR_UTC
	// overrides the default 07:00 UTC fire time.
	founderEmails := admin.ParseFounderEmails(os.Getenv("FOUNDER_EMAILS"))
	if smtpCfg != nil && len(founderEmails) > 0 {
		hourUTC := 7
		if raw := os.Getenv("DAILY_SUMMARY_HOUR_UTC"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 && v <= 23 {
				hourUTC = v
			} else {
				a.Logger.Printf("admin: bad DAILY_SUMMARY_HOUR_UTC=%q (using 7)", raw)
			}
		}
		summaryBuilder := admin.NewDailySummaryBuilder(adminOverviewRepo, a.MongoClient, a.MongoDB)
		admin.StartDailySummaryCron(workerCtx, a.Logger, nil, func(ctx context.Context) error {
			summary, err := summaryBuilder.Build(ctx, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
			return admin.SendDailySummary(*smtpCfg, summary, founderEmails)
		}, hourUTC)
		a.Logger.Printf("admin: daily summary cron armed (%02d:00 UTC → %d recipient(s))", hourUTC, len(founderEmails))
	} else if len(founderEmails) > 0 {
		a.Logger.Printf("admin: FOUNDER_EMAILS set (%d) but SMTP not configured; daily summary disabled", len(founderEmails))
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

	// Retention cohorts (P2-05 / mootd-admin#22). No init work
	// required — the repo is a thin client wrapper; we just
	// hand it the same MongoClient + dbName. No 503 path
	// outside of the wiring itself.
	adminHandler.WithRetention(admin.NewRetentionMongoRepository(a.MongoClient, a.MongoDB))

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

	// Privacy compliance (P2-06 / mootd-admin#23). One Service
	// powers both /v1/privacy/* (self-serve) and the admin
	// /admin/v1/users/{id}/purge surface. Adapter wraps the
	// privacy.ErrUserNotFound sentinel into the admin-side
	// ErrUserAlreadyPurged so admin's handler stays free of a
	// privacy import.
	privacySvc := privacy.NewService(a.MongoClient, a.MongoDB)
	adminHandler.WithUserPurger(adminPurgerAdapter{svc: privacySvc})

	// HITL queue proxy (singleItemDetection #34/#35). Reuses the
	// SINGLEITEM_BASE_URL + SINGLEITEM_API_KEY env vars set up
	// for the detector switch — the orchestrator serves both the
	// per-image submit endpoint and the admin reads/writes. nil
	// proxy when the env is empty → admin /hitl-queue + /items/{id}/*
	// endpoints return 503.
	if hitlProxy := admin.NewHitlProxy(os.Getenv("SINGLEITEM_BASE_URL"), os.Getenv("SINGLEITEM_API_KEY")); hitlProxy != nil {
		adminHandler.WithHitlProxy(hitlProxy)
		a.Logger.Printf("admin: HITL queue proxy → %s", hitlProxy.BaseURL)
	}

	adminHandler.RegisterRoutes(mux, authLimit, requireAdmin)

	userRepo := user.NewMongoRepository(a.MongoClient, a.MongoDB)
	// Health endpoints (mootd#40 / #55).
	//
	// Redis is required in production because every fallback
	// degrades behaviour silently — rate limit drops to
	// in-memory (per-instance), outfit cache vanishes, async
	// job state becomes local-only. Outside production we
	// honour the boot-time wiring decision but don't fail
	// /readyz for a missing Redis.
	redisRequired := strings.EqualFold(os.Getenv("ENVIRONMENT"), "production")
	healthHandler := health.NewHandler(a.Logger, a.MongoClient, a.MongoDB).
		WithRedis(redisClient, redisRequired)
	healthHandler.RegisterRoutes(mux)

	// Background heartbeat for metrics.RedisStatus (mootd#39 +
	// #55). Polls every 15s — same cadence as a typical
	// Prometheus scrape, so the gauge is rarely older than the
	// most recent scrape.
	if redisClient != nil {
		go redisHeartbeat(workerCtx, redisClient, a.Logger)
	}

	bgRemover := wardrobe.NewBackgroundRemover(a.BgRemoverBaseURL)

	// Detection backend selector. DETECTION_BACKEND switches the
	// implementation the wardrobe handler talks to:
	//   - "singleitem" (default): the singleItemDetection
	//     orchestrator at SINGLEITEM_BASE_URL. Runs the v1
	//     pipeline (detect → describe → ghost-mannequin → HITL)
	//     and is the source of truth for the HITL queue rows
	//     the admin proxy reads back.
	//   - "legacy": the on-host cloth-detection service at
	//     DETECTION_API_BASE_URL. Kept for parity testing +
	//     dev-without-orchestrator setups.
	// When the singleitem path can't be configured (empty
	// SINGLEITEM_BASE_URL) we fall back to legacy with a
	// warning so dev boots don't crash. Production should set
	// both vars explicitly.
	var detector wardrobe.DetectorBackend
	chosen := strings.ToLower(strings.TrimSpace(os.Getenv("DETECTION_BACKEND")))
	if chosen == "" {
		chosen = "singleitem"
	}
	switch chosen {
	case "singleitem":
		base := os.Getenv("SINGLEITEM_BASE_URL")
		if base == "" {
			a.Logger.Print("WARNING: DETECTION_BACKEND=singleitem but SINGLEITEM_BASE_URL is empty — falling back to legacy")
			detector = wardrobe.NewDetector(a.DetectionAPIBaseURL, a.DetectionAPIKey, a.Logger)
		} else {
			a.Logger.Printf("detection backend: singleitem orchestrator at %s", base)
			detector = wardrobe.NewSingleItemDetector(base, os.Getenv("SINGLEITEM_API_KEY"), a.Logger)
		}
	case "legacy":
		a.Logger.Printf("detection backend: legacy (%s)", a.DetectionAPIBaseURL)
		detector = wardrobe.NewDetector(a.DetectionAPIBaseURL, a.DetectionAPIKey, a.Logger)
	default:
		// Unknown value → fall back to the singleitem default
		// path (which itself falls back to legacy if URL is
		// empty). The double-fallback is intentional:
		// "make boot succeed" is more important than "honour
		// the typo".
		a.Logger.Printf("WARNING: unknown DETECTION_BACKEND=%q — falling back to singleitem default", os.Getenv("DETECTION_BACKEND"))
		base := os.Getenv("SINGLEITEM_BASE_URL")
		if base == "" {
			detector = wardrobe.NewDetector(a.DetectionAPIBaseURL, a.DetectionAPIKey, a.Logger)
		} else {
			detector = wardrobe.NewSingleItemDetector(base, os.Getenv("SINGLEITEM_API_KEY"), a.Logger)
		}
	}
	searcher := wardrobe.NewSearcher(a.DetectionAPIBaseURL, a.DetectionAPIKey)
	wardrobeRepo := wardrobe.NewMongoRepository(a.MongoClient, a.MongoDB)
	// Best-effort: create the (userId, traits.seededFromDefaultId)
	// compound index that backs FindBySeededDefault. Skipped silently
	// when Mongo isn't reachable yet — the production startup checks
	// already gate that — but logged so operators can spot a stale
	// connection at boot.
	if err := wardrobeRepo.EnsureWardrobeIndexes(context.Background()); err != nil {
		a.Logger.Printf("wardrobe: ensure indexes: %v (continuing — claim flow falls back to slower path if the index is missing)", err)
	}

	// Archetype defaults (cold-start fix). Best-effort wiring;
	// when the index ensure fails the /admin/v1/archetype-defaults
	// endpoints return 503. The seeder reaches into both
	// userRepo + wardrobeRepo via the wardrobeSeederAdapter.
	archetypeRepo, archetypeRepoErr := admin.NewArchetypeDefaultsMongoRepository(context.Background(), a.MongoClient, a.MongoDB)
	if archetypeRepoErr == nil {
		adminHandler.WithArchetypeDefaults(archetypeRepo)
		adminHandler.WithWardrobeSeeder(&wardrobeSeederAdapter{
			defaults:     archetypeRepo,
			wardrobeRepo: wardrobeRepo,
			userRepo:     userRepo,
			logger:       a.Logger,
		})
		// Upload + autodetect: lets curators drop a real photo
		// into /archetype-defaults and have label/category/traits
		// prefilled. Generation output is intentionally discarded —
		// curated defaults always show the operator's upload.
		//
		// Detector preference order:
		//   1. Claude (ANTHROPIC_API_KEY set) — direct vision call,
		//      ~$0.005, full structured attributes.
		//   2. Configured wardrobe detector (legacy or singleitem
		//      orchestrator) — same pipeline the mobile app uses.
		//      Returns mock data when the orchestrator is in default
		//      USE_REAL_STAGE1=false mode, so we put Claude first.
		adminHandler.WithImageStore(&wardrobeImageStoreAdapter{repo: wardrobeRepo})
		switch {
		case a.AnthropicAPIKey != "":
			adminHandler.WithItemDetector(admin.NewClaudeItemDetector(
				a.AnthropicBaseURL, a.AnthropicAPIKey, a.AnthropicModel, a.Logger,
			))
			a.Logger.Printf("admin: archetype-defaults autodetect using Claude (model=%s)", a.AnthropicModel)
		case detector != nil:
			adminHandler.WithItemDetector(&wardrobeItemDetectorAdapter{
				backend: detector,
				logger:  a.Logger,
			})
			a.Logger.Printf("admin: archetype-defaults autodetect using configured wardrobe detector")
		default:
			a.Logger.Printf("admin: archetype-defaults autodetect disabled (no Claude key or wardrobe detector wired)")
		}
	} else {
		a.Logger.Printf("admin: archetype_default_items repo init failed: %v (continuing without /archetype-defaults)", archetypeRepoErr)
	}
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

	// Per-user archetype-default rejections: backs the
	// /v1/wardrobe/archetype-rejections endpoint and is read by the
	// outfit-side filler loader to skip dismissed defaults.
	rejectionsRepo, rejErr := wardrobe.NewArchetypeRejectionsMongoRepository(context.Background(), a.MongoClient, a.MongoDB)
	if rejErr != nil {
		a.Logger.Printf("wardrobe: archetype_rejections repo init failed: %v (continuing — rejections endpoint will 503)", rejErr)
	}
	if archetypeRepoErr == nil {
		// Wire the wardrobe-side endpoints whenever the admin
		// defaults repo is up. Seeder is reused as the wardrobe
		// handler's "I have this IRL" engine.
		wardrobeHandler.WithArchetypeEndpoints(wardrobe.ArchetypeEndpointsConfig{
			Seeder: &fillerSeederAdapter{
				defaults:     archetypeRepo,
				wardrobeRepo: wardrobeRepo,
				logger:       a.Logger,
			},
			Rejections: rejectionsRepo,
		})
	}
	wardrobeHandler.RegisterRoutes(mux, authMiddleware)

	brands.NewHandler(a.Logger, brands.NewMongoRepository(a.MongoClient, a.MongoDB)).RegisterRoutes(mux, authMiddleware)

	// Shared adapter for the outfit service. Implements both
	// outfit.userProfileProvider (archetype scores) and
	// outfit.creativityProvider (mootd#67) so the service can
	// read both without juggling two wirings.
	profileProvider := &userOutfitAdapter{repo: userRepo}

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
	moodboardHandler := moodboard.NewHandler(a.Logger, moodboardRepo, wardrobeRepo, profileUpdater, moodboardOnSave)
	if archetypeRepoErr == nil {
		// When a saved moodboard references a virtual filler
		// (ad_<hex>), resolveSnapshots falls through to this
		// adapter so the calendar still renders a real tile.
		moodboardHandler.WithArchetypeDefaultsLookup(&moodboardArchetypeLookupAdapter{repo: archetypeRepo})
	}
	moodboardHandler.RegisterRoutes(mux, authMiddleware)

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

	// Privacy compliance — self-serve purge + export
	// (P2-06 / mootd-admin#23). Reuses the privacySvc instance
	// already wired into the admin handler so both flows share
	// the same allowlist of collections.
	privacy.NewHandler(a.Logger, privacySvc).RegisterRoutes(mux, authMiddleware)

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
	// Recorder wiring only applies to the legacy detector — its
	// per-stage analyze/generate calls each emit an llm_calls
	// row. The singleitem orchestrator emits its own llm_calls
	// (separate process) so we don't double-record.
	if legacy, ok := detector.(*wardrobe.Detector); ok {
		legacy.WithRecorder(observability.NewWardrobeRecorderAdapter(coreLLMRec))
	}

	// Wire archetype-default fillers into the outfit pool when the
	// admin-side repo came up cleanly. The two adapters share the
	// same archetypeRepo handle so the ad_<hex> id minted on the
	// admin side flows through into wardrobe seeding without going
	// via the wire format. nil-safe: when archetypeRepo failed to
	// init, both fields stay nil and outfit gen falls back to the
	// pre-fillers behaviour.
	outfitCfg := outfit.ServiceConfig{
		Generator:      generator,
		Wardrobe:       wardrobeRepo,
		Recent:         recentProvider,
		UserProfile:    profileProvider,
		Surfaces:       newSurfaceAdapter(surfaceRepo),
		UseVision:      useVision,
		Cache:          outfitCache,
		LLMRecorder:    llmRecorder,
		BudgetEnforcer: budgetEnforcer,
	}
	if archetypeRepoErr == nil {
		outfitCfg.ArchetypeDefaults = &archetypeDefaultsLoaderAdapter{
			repo:       archetypeRepo,
			rejections: rejectionsRepo, // nil-safe; loader degrades to no-filter
			logger:     a.Logger,
		}
		// Note: outfit gen used to also wire a FillerSeeder for the
		// auto-seed step removed in mootd@2dad4df. Fillers now stay
		// virtual until the user explicitly claims one via the
		// /v1/wardrobe/items/from-archetype-default endpoint, so the
		// outfit service no longer needs a seeder. The same
		// fillerSeederAdapter is still wired into the wardrobe handler
		// above for that user-driven path. mootd#74.
	}

	outfitService := outfit.NewService(a.Logger, outfitCfg)

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
	// /metrics is exempt from the admin IP allowlist + global
	// rate limiter — Prometheus scrapes every 15s and shouldn't
	// fight for token bucket against real users. Bind it on the
	// gated mux so the operator gates exposure at the front
	// (Caddy basic-auth or firewall) rather than a JWT middleware
	// that Prometheus doesn't speak.
	gatedMux.Handle("/metrics", metrics.Handler())
	gatedMux.Handle("/", mux)

	// Middleware chain (outermost → innermost):
	//   RequestID → Recover → Logging → CORS → global rate limit → admin allowlist → mux
	//
	// RequestID is outermost so panics, log lines, and the response header all
	// carry the same correlation ID. Recover sits just inside so it can log
	// with that ID before any handler state can be touched. Logging is inside
	// Recover so it sees the real status code (including 500s written by the
	// recovery handler). Metrics.Instrument wraps the entire chain so the
	// in-flight gauge + duration histogram observe the post-middleware reality.
	handler := metrics.Instrument(
		middleware.RequestID(
			middleware.Recover(a.Logger)(
				middleware.Logging(a.Logger)(
					middleware.CORS(a.CORSAllowedOrigins)(
						rateLimiter(gatedMux),
					),
				),
			),
		),
		routeLabel,
	)
	return handler, wardrobeRepo, moodboardRepo, rateLimitCloser
}

// surfaceAdapter bridges surface.Repository to outfit's surfaceProvider
// interface. Keeping the adapter here means the outfit package never imports
// surface — the dependency direction stays one-way (app → outfit, app → surface).
// userOutfitAdapter satisfies outfit.userProfileProvider AND
// the optional outfit.creativityProvider extension (mootd#67)
// from a user.Repository. Single struct with two methods so
// wiring stays in one place + the outfit service's runtime
// type-assertion finds creativityProvider without ceremony.
type userOutfitAdapter struct {
	repo *user.MongoRepository
}

func (a *userOutfitAdapter) GetArchetypeProfile(ctx context.Context, userID string) (map[string]float64, error) {
	doc, err := a.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return doc.ArchetypeProfile, nil
}

// GetCreativity returns the user's preference (0..1). Missing
// field → 0.5 (the dial's middle, equivalent to "no preference"
// since CreativityToTemperature for c=0.5 returns the historical
// 0.9 default).
func (a *userOutfitAdapter) GetCreativity(ctx context.Context, userID string) (float64, error) {
	doc, err := a.repo.FindByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	if doc.Creativity == nil {
		return 0.5, nil
	}
	return *doc.Creativity, nil
}

// adminPurgerAdapter satisfies admin.UserPurger by delegating
// to the privacy.Service while translating the privacy
// sentinel + report types into the admin-facing equivalents.
// Lives in app.go (the wiring layer) so admin/ stays free of a
// privacy/ import — same one-way-dep pattern used elsewhere.
type adminPurgerAdapter struct {
	svc *privacy.Service
}

func (a adminPurgerAdapter) Purge(ctx context.Context, userID string) (*admin.PurgeReport, error) {
	rep, err := a.svc.Purge(ctx, userID)
	if err != nil {
		if err == privacy.ErrUserNotFound {
			return nil, admin.ErrUserAlreadyPurged
		}
		return nil, err
	}
	return &admin.PurgeReport{
		UserID:      rep.UserID,
		PurgedAt:    rep.PurgedAt,
		Collections: rep.Collections,
		Total:       rep.Total,
	}, nil
}

// wardrobeSeederAdapter satisfies admin.WardrobeSeeder by
// reading curated defaults from the admin-side repo + writing
// fresh wardrobe rows via the wardrobe repo. The user must
// exist; absent → admin.ErrUserNotFoundForSeed.
//
// Each seeded item is a deep copy: fresh _id, fresh
// createdAt, the calling user's id stamped on. Future edits +
// deletes by the user don't touch the original default row.
type wardrobeSeederAdapter struct {
	defaults     admin.ArchetypeDefaultsRepository
	wardrobeRepo *wardrobe.MongoRepository
	userRepo     *user.MongoRepository
	logger       *log.Logger
}

func (a *wardrobeSeederAdapter) Seed(ctx context.Context, userID, archetypeName string) (int, error) {
	if u, err := a.userRepo.FindByID(ctx, userID); err != nil || u == nil {
		return 0, admin.ErrUserNotFoundForSeed
	}
	defaults, err := a.defaults.List(ctx, archetypeName)
	if err != nil {
		return 0, err
	}
	if len(defaults) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	count := 0
	for _, d := range defaults {
		traits := map[string]string{}
		for k, v := range d.Traits {
			traits[k] = v
		}
		// Stamp a "seededFromArchetype" trait so the user can
		// distinguish defaults from items they uploaded; also
		// useful for analytics ("which defaults survive vs get
		// deleted").
		traits["seededFromArchetype"] = d.Archetype
		traits["seededFromDefaultId"] = d.ID

		item := wardrobe.ClothingItem{
			ID:          "wi_" + randomHex(16),
			UserID:      userID,
			Category:    d.Category,
			Label:       d.Label,
			ImageURL:    d.ImageURL,
			PngImageURL: d.PngImageURL,
			Traits:      traits,
			CreatedAt:   now,
		}
		if err := a.wardrobeRepo.Save(ctx, item); err != nil {
			a.logger.Printf("seed wardrobe: save item %s for user %s failed: %v (continuing)", d.ID, userID, err)
			continue
		}
		count++
		// Best-effort observability bump on the default row.
		_ = a.defaults.IncrementSeeded(ctx, d.ID, 1)
	}
	return count, nil
}

// randomHex is a small helper duplicated from admin/random.go
// so app/ doesn't need to import that file just for the
// generator. 8 bytes → 16 hex chars; collision probability
// is fine for wardrobe-item ids that already include a userId.
func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = cryptoRand.Read(buf)
	return hex.EncodeToString(buf)
}

// wardrobeImageStoreAdapter satisfies admin.ImageStore by writing
// to the same GridFS bucket that wardrobe.ServeImage already serves
// on /v1/wardrobe/items/{id}/image. The serve route is unauthed and
// looks up by GridFS filename, so saving under "ad_<hex>" is enough
// to make the file publicly fetchable from the mobile app — exactly
// what we want for seeded items rendered on a user's moodboard.
type wardrobeImageStoreAdapter struct {
	repo *wardrobe.MongoRepository
}

func (a *wardrobeImageStoreAdapter) Save(ctx context.Context, key string, data []byte, contentType string) error {
	return a.repo.SaveImage(ctx, key, data, contentType)
}

// wardrobeItemDetectorAdapter satisfies admin.ItemDetector by calling
// the configured detection backend (legacy or singleitem). The
// backend's Detect signature is keyed on userID + runID so it can
// archive a detection_runs row; for admin curating we synthesise a
// stable user id ("admin_default_curator") and a fresh run id per
// call so the archive trail still works without leaking the
// orchestrator's image lifecycle into the default.
//
// The backend returns a flattened jobItem; we map its category,
// label, confidence, and string-valued traits into the
// admin.DetectionPrefill. The orchestrator's full
// `structuredDescription` is lost in the flatten step (only top-level
// string fields survive) — acceptable for now since the curator can
// edit the prefill before saving.
type wardrobeItemDetectorAdapter struct {
	backend wardrobe.DetectorBackend
	logger  *log.Logger
}

const adminDetectCuratorUserID = "admin_default_curator"

func (a *wardrobeItemDetectorAdapter) DetectFromBytes(ctx context.Context, imageData []byte, filename string) (admin.DetectionPrefill, error) {
	runID := "drun_admin_" + randomHex(8)
	items, _, err := a.backend.Detect(ctx, adminDetectCuratorUserID, runID, imageData, filename)
	if err != nil {
		return admin.DetectionPrefill{}, err
	}
	if len(items) == 0 {
		return admin.DetectionPrefill{}, fmt.Errorf("detector returned no items")
	}
	it := items[0]
	traits := map[string]string{}
	for k, v := range it.Traits {
		traits[k] = v
	}
	// Defensive copy of structuredDescription so admin/ can't
	// accidentally mutate the detector's internal map.
	var structured map[string]any
	if len(it.StructuredDescription) > 0 {
		structured = make(map[string]any, len(it.StructuredDescription))
		for k, v := range it.StructuredDescription {
			structured[k] = v
		}
	}
	return admin.DetectionPrefill{
		Label:                 it.Label,
		Category:              it.Category,
		Confidence:            it.Confidence,
		Traits:                traits,
		StructuredDescription: structured,
	}, nil
}

// archetypeDefaultsLoaderAdapter satisfies outfit's archetypeDefaultsLoader
// by wrapping the admin defaults repo + the per-user rejections
// repo. Converts each ArchetypeDefaultItem into a wardrobe.ClothingItem
// so the outfit service can fold them into the same items slice it
// already reasons about (validation, archetype scoring, layout). The
// id keeps its "ad_<hex>" prefix and stays virtual through the entire
// outfit pipeline — the user explicitly claims a filler ("I have this
// IRL") via POST /v1/wardrobe/items/from-archetype-default before
// anything materialises in their wardrobe.
//
// rejections may be nil; when nil the filter is a no-op (all defaults
// for the archetype come through). In production it's always wired so
// "not in my wardrobe" sticks across regenerations.
type archetypeDefaultsLoaderAdapter struct {
	repo       admin.ArchetypeDefaultsRepository
	rejections wardrobe.ArchetypeRejectionsRepository
	logger     *log.Logger
}

func (a *archetypeDefaultsLoaderAdapter) LoadFor(ctx context.Context, userID, archetypeName string, cap int) ([]wardrobe.ClothingItem, error) {
	// Per-user rejection list — fed straight into the aggregation's
	// $nin so rejected ids never reach the application. Best-effort:
	// a Mongo error here degrades to "no rejections" rather than
	// failing the whole outfit generation — a stale rejection re-
	// appearing once is kinder than the user seeing zero suggestions
	// (logged so operators see the regression instead of silently
	// degrading forever, mootd#72).
	var excludeIDs []string
	if a.rejections != nil && userID != "" {
		ids, rerr := a.rejections.ListIDs(ctx, userID)
		if rerr != nil {
			a.logger.Printf("outfit-fillers: load rejections for user %s failed: %v (proceeding with no rejection filter)", userID, rerr)
		} else {
			excludeIDs = ids
		}
	}
	rows, err := a.repo.SampleForOutfitGen(ctx, archetypeName, excludeIDs, cap)
	if err != nil {
		return nil, err
	}
	out := make([]wardrobe.ClothingItem, 0, len(rows))
	for _, d := range rows {
		traits := map[string]string{}
		for k, v := range d.Traits {
			traits[k] = v
		}
		// Stamp a marker trait so prompts + downstream debugging
		// can tell the source apart even before the Preferred flag
		// gets stripped by serialisation.
		traits["seededFromArchetype"] = d.Archetype
		traits["seededFromDefaultId"] = d.ID
		out = append(out, wardrobe.ClothingItem{
			ID:          d.ID, // ad_<hex> — kept virtual; never auto-materialised
			UserID:      "",   // intentionally blank: not yet owned
			Category:    d.Category,
			Label:       d.Label,
			ImageURL:    d.ImageURL,
			PngImageURL: d.PngImageURL,
			Traits:      traits,
			CreatedAt:   d.CreatedAt,
		})
	}
	return out, nil
}

// moodboardArchetypeLookupAdapter satisfies the moodboard handler's
// archetypeDefaultsLookup so saved boards with virtual filler ids
// (ad_<hex>) still resolve to displayable snapshots.
type moodboardArchetypeLookupAdapter struct {
	repo admin.ArchetypeDefaultsRepository
}

func (a *moodboardArchetypeLookupAdapter) GetByID(ctx context.Context, id string) (*moodboard.ArchetypeDefaultSnapshot, error) {
	def, err := a.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, nil
	}
	return &moodboard.ArchetypeDefaultSnapshot{
		ID:          def.ID,
		Category:    def.Category,
		Label:       def.Label,
		ImageURL:    def.ImageURL,
		PngImageURL: def.PngImageURL,
	}, nil
}

// fillerSeederAdapter satisfies outfit's fillerSeeder. Idempotent:
// looks up the user's existing wardrobe for an item already seeded
// from the same default (matched on traits.seededFromDefaultId)
// before minting a new wi_<hex>. Re-running outfit gen for the same
// user+archetype therefore reuses the same wardrobe row instead of
// piling up duplicates.
type fillerSeederAdapter struct {
	defaults     admin.ArchetypeDefaultsRepository
	wardrobeRepo *wardrobe.MongoRepository
	logger       *log.Logger
}

func (a *fillerSeederAdapter) SeedForUser(ctx context.Context, userID, defaultID string) (string, error) {
	if userID == "" || defaultID == "" {
		return "", fmt.Errorf("filler seeder: userID and defaultID required (got %q / %q)", userID, defaultID)
	}
	// Idempotency: a previous claim of the same default returns the
	// existing wardrobe row. Backed by the (userId,
	// traits.seededFromDefaultId) compound index ensured at boot
	// (mootd#71) — constant-time vs the previous FindByUser+linear
	// scan that grew with the user's wardrobe size.
	if existing, err := a.wardrobeRepo.FindBySeededDefault(ctx, userID, defaultID); err != nil {
		return "", fmt.Errorf("filler seeder: lookup existing seed: %w", err)
	} else if existing != nil {
		return existing.ID, nil
	}
	// First time seeing this default for this user — copy it.
	def, err := a.defaults.Get(ctx, defaultID)
	if err != nil {
		return "", fmt.Errorf("filler seeder: load default %s: %w", defaultID, err)
	}
	if def == nil {
		return "", fmt.Errorf("filler seeder: default %s not found", defaultID)
	}
	traits := map[string]string{}
	for k, v := range def.Traits {
		traits[k] = v
	}
	traits["seededFromArchetype"] = def.Archetype
	traits["seededFromDefaultId"] = def.ID
	newID := "wi_" + randomHex(16)
	item := wardrobe.ClothingItem{
		ID:          newID,
		UserID:      userID,
		Category:    def.Category,
		Label:       def.Label,
		ImageURL:    def.ImageURL,
		PngImageURL: def.PngImageURL,
		Traits:      traits,
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.wardrobeRepo.Save(ctx, item); err != nil {
		return "", fmt.Errorf("filler seeder: save wardrobe item: %w", err)
	}
	// Best-effort observability bump on the default row so admins
	// can see which fillers are landing in real outfits.
	_ = a.defaults.IncrementSeeded(ctx, def.ID, 1)
	a.logger.Printf("outfit-filler: seeded default %s as %s for user %s (archetype=%s, label=%q)",
		def.ID, newID, userID, def.Archetype, def.Label)
	return newID, nil
}

// redisHeartbeat polls Redis every 15s and updates
// metrics.RedisStatus. Runs until ctx is cancelled.
func redisHeartbeat(ctx context.Context, client *redis.Client, logger *log.Logger) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	// Prime the gauge immediately so the first scrape after
	// boot doesn't see "0 = down" by default.
	pingAndRecord(ctx, client, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			pingAndRecord(ctx, client, logger)
		}
	}
}

func pingAndRecord(ctx context.Context, client *redis.Client, logger *log.Logger) {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		metrics.RedisStatus.Set(0)
		logger.Printf("redis heartbeat: down (%v)", err)
		return
	}
	metrics.RedisStatus.Set(1)
}

// routeLabel collapses an HTTP path to a low-cardinality label
// for the Prometheus duration histogram (mootd#39). Without
// this every per-id endpoint would emit a separate timeseries
// (`/v1/wardrobe/items/abc`, `…/def`, …) and exhaust scrape
// memory.
//
// Prefix-trees would scale better but the route surface here is
// small enough that a switch is the simplest correct thing.
// New routes need a new case here OR the path collapses to its
// /vN prefix automatically (the trailing case below).
func routeLabel(r *http.Request) string {
	p := r.URL.Path
	switch {
	case p == "/healthz", p == "/readyz", p == "/v1/health":
		return p
	case p == "/metrics":
		return "/metrics"
	case strings.HasPrefix(p, "/v1/wardrobe/items/"):
		return "/v1/wardrobe/items/{id}"
	case strings.HasPrefix(p, "/v1/outfits/jobs/"):
		return "/v1/outfits/jobs/{id}"
	case strings.HasPrefix(p, "/v1/moodboards/"):
		return "/v1/moodboards/{id}"
	case strings.HasPrefix(p, "/admin/v1/users/"):
		return "/admin/v1/users/{id}"
	case strings.HasPrefix(p, "/admin/v1/traces/"):
		return "/admin/v1/traces/{id}"
	case strings.HasPrefix(p, "/admin/v1/sessions/"):
		return "/admin/v1/sessions/{id}"
	case strings.HasPrefix(p, "/admin/v1/detection-runs/"):
		return "/admin/v1/detection-runs/{id}"
	case strings.HasPrefix(p, "/admin/v1/funnels/"):
		return "/admin/v1/funnels/{id}"
	case strings.HasPrefix(p, "/admin/v1/evals/"):
		return "/admin/v1/evals/{id}"
	case strings.HasPrefix(p, "/admin/v1/prompts/"):
		return "/admin/v1/prompts/{name}"
	case strings.HasPrefix(p, "/v1/surfaces/"):
		return "/v1/surfaces/{id}"
	}
	// Catch-all: collapse anything past the third segment so a
	// random new route doesn't blow up cardinality.
	parts := strings.SplitN(p, "/", 5)
	if len(parts) >= 5 {
		return "/" + parts[1] + "/" + parts[2] + "/" + parts[3] + "/*"
	}
	return p
}

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

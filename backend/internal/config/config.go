// Package config handles application configuration and environment variables.
package config

import (
	"errors"
	"log"
	"os"
	"strings"
	"time"
)

// ErrCORSWildcardInProduction is returned when ENVIRONMENT=production is set
// but CORS_ALLOWED_ORIGINS is left as the wildcard default (or resolves to an
// empty list after trimming). Refusing to start forces operators to supply an
// explicit origin allowlist instead of accidentally shipping an open policy.
var ErrCORSWildcardInProduction = errors.New("CORS_ALLOWED_ORIGINS must be an explicit comma-separated list in production; wildcard '*' is not permitted")

// ErrAdminAllowlistEmptyInProduction is returned when ENVIRONMENT=production is
// set but ADMIN_ALLOWED_IPS is empty. An empty allowlist makes
// middleware.AdminIPAllowlist fail OPEN (allow-all), which also re-exposes the
// unauthenticated /metrics endpoint. Refusing to start forces operators to
// supply an explicit source-IP allowlist instead of silently dropping it.
var ErrAdminAllowlistEmptyInProduction = errors.New("ADMIN_ALLOWED_IPS must be an explicit comma-separated CIDR/IP list in production; an empty allowlist fails open and exposes /metrics")

const (
	defaultHTTPAddr          = ":8080"
	defaultMongoURI          = "mongodb://mootd:mootd_dev@mongo:27017/?authSource=admin"
	defaultMongoDB           = "mootd"
	defaultConnectTimeout    = 10 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 4 * time.Minute // detection polling can take up to 3 min
	defaultIdleTimeout       = 60 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second

	// DefaultJWTExpiry is how long a mootd JWT remains valid.
	DefaultJWTExpiry = 15 * time.Minute
	// defaultJWTSecret is used only in development when JWT_SECRET is not set.
	defaultJWTSecret = "dev-secret-change-in-production-min-32-chars!!"

	// DefaultAdminJWTExpiry is how long a mootd-admin JWT remains valid. Shorter
	// than the user-facing DefaultJWTExpiry because admin sessions have a
	// bigger blast radius (they can read user data + trigger re-runs) so we
	// want forced re-authentication more often.
	DefaultAdminJWTExpiry = 1 * time.Hour
	// DefaultAdminRefreshExpiry caps the admin refresh lifetime — 7 days, much
	// tighter than the 30-day user refresh. Admin refresh tokens are single-
	// use (rotated on every /admin/v1/auth/refresh call).
	DefaultAdminRefreshExpiry = 7 * 24 * time.Hour
	// defaultAdminJWTSecret is used only in development when ADMIN_JWT_SECRET
	// is not set. Distinct from the user-side default so a bug that mixes up
	// the secrets can't silently sign admin tokens with the user secret or
	// vice versa — the resulting token fails validation on both sides.
	defaultAdminJWTSecret = "admin-dev-secret-change-in-production-min-32-chars!!"
	// defaultCORSOrigins allows all origins in development.
	defaultCORSOrigins = "*"
	// defaultGoogleClientID is mootd's own Google OAuth web client ID — the
	// same value the frontend signs in with (app/src/hooks/useGoogleAuth.ts).
	// It is the secure default for the GOOGLE_CLIENT_IDS audience allowlist so
	// that the /v1/auth/google audience check (which rejects tokens minted for
	// any OTHER OAuth client) works out of the box without breaking real
	// sign-in. A client ID is public, not a secret. Deployments that use a
	// different/additional client (e.g. a dedicated iOS/Android client) must
	// list every accepted client ID in GOOGLE_CLIENT_IDS.
	defaultGoogleClientID = "991290253393-eompo9m0q8up56n7iabg30tn62lkd5h2.apps.googleusercontent.com"
	// defaultDetectionBaseURL is the local clothing-detection service.
	// Override via DETECTION_API_BASE_URL environment variable.
	defaultDetectionBaseURL = "http://localhost:8000"

	// defaultOllamaBaseURL points to local Ollama. When running inside Docker on
	// macOS/Windows, host.docker.internal resolves to the host machine.
	// Override via OLLAMA_BASE_URL environment variable.
	defaultOllamaBaseURL = "http://host.docker.internal:11434"
	// defaultOllamaModel is the LLM used for outfit generation.
	// Override via OLLAMA_MODEL environment variable.
	defaultOllamaModel = "qwen3:14b"

	// defaultOutfitProvider selects the outfit generator backend.
	// "claude" uses the Anthropic API; "ollama" uses local Ollama.
	// Override via OUTFIT_PROVIDER environment variable.
	defaultOutfitProvider = "ollama"
	// defaultAnthropicBaseURL is the Anthropic Messages API endpoint.
	// Override via ANTHROPIC_BASE_URL environment variable.
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	// defaultAnthropicModel is the Claude model used for outfit generation.
	// Override via ANTHROPIC_MODEL environment variable.
	defaultAnthropicModel = "claude-sonnet-4-5"
	// defaultAnthropicVision controls whether item PNGs are sent to Claude for
	// visual reasoning. Override via ANTHROPIC_VISION environment variable
	// ("true"/"false"). Defaults to true when the Claude provider is selected.
	defaultAnthropicVision = "true"

	// defaultBgRemoverBaseURL points to the background removal service running
	// on the host machine. Override via BG_REMOVER_BASE_URL environment variable.
	defaultBgRemoverBaseURL = "http://host.docker.internal:8010"

	// defaultOpenAIBaseURL is the OpenAI API endpoint for DALL-E image generation.
	defaultOpenAIBaseURL = "https://api.openai.com"

	// defaultRedisURL is the default Redis connection string for caching and rate limiting.
	defaultRedisURL = "redis://localhost:6379"

	// defaultEnvironment is the deployment environment. Set ENVIRONMENT=production
	// to disable development-only features like mock-login.
	defaultEnvironment = "development"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	HTTPAddr            string
	MongoURI            string
	MongoDB             string
	ConnectTimeout      time.Duration
	ShutdownTimeout     time.Duration
	JWTSecret           string
	AdminJWTSecret      string
	CORSAllowedOrigins  []string
	DetectionAPIBaseURL string
	DetectionAPIKey     string
	OllamaBaseURL       string
	OllamaModel         string
	BgRemoverBaseURL    string
	Environment         string
	// EnableMockLogin gates the /v1/auth/mock-login dev endpoint. Fail-closed:
	// must be explicitly set to "true" via ENABLE_MOCK_LOGIN, and is refused in
	// production regardless.
	EnableMockLogin bool

	// Outfit generation provider selection.
	OutfitProvider   string // single provider ("claude") or a cascade chain ("claude,openai,ollama")
	AnthropicBaseURL string
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicVision  bool

	// OpenAI (DALL-E) for generating moodboard backgrounds/textures.
	OpenAIBaseURL string
	OpenAIAPIKey  string

	// Redis for caching, rate limiting, and async jobs.
	RedisURL string

	// GoogleClientIDs is the allowlist of Google OAuth client IDs whose tokens
	// /v1/auth/google will accept. The audience of the presented token must
	// match one of these, preventing token-substitution (confused-deputy)
	// account takeover. Loaded from GOOGLE_CLIENT_IDS (comma-separated);
	// defaults to mootd's own web client ID when unset.
	GoogleClientIDs []string
}

// DefaultReadTimeout returns the default read timeout for HTTP servers.
func DefaultReadTimeout() time.Duration {
	return defaultReadTimeout
}

// DefaultWriteTimeout returns the default write timeout for HTTP servers.
func DefaultWriteTimeout() time.Duration {
	return defaultWriteTimeout
}

// DefaultIdleTimeout returns the default idle timeout for HTTP servers.
func DefaultIdleTimeout() time.Duration {
	return defaultIdleTimeout
}

// DefaultReadHeaderTimeout returns the default read header timeout for HTTP servers.
func DefaultReadHeaderTimeout() time.Duration {
	return defaultReadHeaderTimeout
}

// Load loads and returns the application configuration from environment variables.
func Load(logger *log.Logger) Config {
	env := GetEnv("ENVIRONMENT", defaultEnvironment)

	jwtSecret := GetEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		if env == "production" {
			logger.Fatalf("FATAL: JWT_SECRET must be set in production. Refusing to start with the default secret.")
		}
		logger.Printf("WARNING: JWT_SECRET not set — using insecure development secret. Set JWT_SECRET in production.")
		jwtSecret = defaultJWTSecret
	}

	adminJWTSecret := GetEnv("ADMIN_JWT_SECRET", "")
	if adminJWTSecret == "" {
		if env == "production" {
			logger.Fatalf("FATAL: ADMIN_JWT_SECRET must be set in production. Refusing to start with the default admin secret.")
		}
		logger.Printf("WARNING: ADMIN_JWT_SECRET not set — using insecure development secret. Set ADMIN_JWT_SECRET in production.")
		adminJWTSecret = defaultAdminJWTSecret
	}
	// Defensive: even if an operator set both secrets to the same value, refuse
	// to start. Sharing the secret defeats the issuer-separation guarantee
	// (a stolen user token would validate against admin middleware).
	if adminJWTSecret == jwtSecret {
		logger.Fatalf("FATAL: ADMIN_JWT_SECRET must differ from JWT_SECRET. Sharing the secret defeats the issuer separation — any user token could be replayed as an admin token.")
	}

	rawOrigins := GetEnv("CORS_ALLOWED_ORIGINS", defaultCORSOrigins)
	var origins []string
	for _, o := range strings.Split(rawOrigins, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	if err := validateCORSOrigins(origins, env); err != nil {
		logger.Fatalf("FATAL: %v. Set CORS_ALLOWED_ORIGINS to a comma-separated list of trusted origins (e.g. \"https://app.example.com,https://admin.example.com\").", err)
	}
	if containsWildcard(origins) {
		logger.Printf("WARNING: CORS_ALLOWED_ORIGINS contains '*' — acceptable for development, but must be an explicit origin list in production.")
	}

	// Admin IP allowlist must be explicit in production. Empty → the allowlist
	// middleware fails open (allow-all), re-exposing the unauthenticated
	// /metrics endpoint that #108 moved behind it (#143).
	if err := validateAdminAllowlist(GetEnv("ADMIN_ALLOWED_IPS", ""), env); err != nil {
		logger.Fatalf("FATAL: %v. Set ADMIN_ALLOWED_IPS to the trusted admin source IPs/CIDRs (e.g. \"203.0.113.4/32,10.0.0.0/8\") — it gates /admin/v1/* and /metrics.", err)
	}

	detectionAPIKey := GetEnv("DETECTION_API_KEY", "")
	if detectionAPIKey == "" {
		logger.Printf("WARNING: DETECTION_API_KEY not set — clothing detection will fail in production.")
	}

	// OUTFIT_PROVIDER may be a single provider ("claude") or a comma-separated
	// cascade chain ("claude,openai,ollama"). Validate each entry — the old
	// whole-string whitelist rejected every chain and silently downgraded the
	// documented production setup to single-provider Ollama, making the cascade
	// and tier-routing layers unreachable.
	rawOutfitProvider := GetEnv("OUTFIT_PROVIDER", defaultOutfitProvider)
	providers, dropped := parseOutfitProviders(rawOutfitProvider)
	for _, d := range dropped {
		logger.Printf("WARNING: ignoring unknown OUTFIT_PROVIDER entry %q (valid: claude, openai, ollama)", d)
	}
	if len(providers) == 0 {
		logger.Printf("WARNING: no valid OUTFIT_PROVIDER in %q, falling back to %q", rawOutfitProvider, defaultOutfitProvider)
		providers = []string{defaultOutfitProvider}
	}
	outfitProvider := strings.Join(providers, ",")

	anthropicAPIKey := GetEnv("ANTHROPIC_API_KEY", "")
	hasClaude := false
	for _, p := range providers {
		if p == "claude" {
			hasClaude = true
			break
		}
	}
	if hasClaude && anthropicAPIKey == "" {
		logger.Printf("WARNING: OUTFIT_PROVIDER includes claude but ANTHROPIC_API_KEY is not set — outfit generation will fail.")
	}

	anthropicVision := strings.EqualFold(GetEnv("ANTHROPIC_VISION", defaultAnthropicVision), "true")

	// Mock-login is fail-closed: it is only enabled when ENABLE_MOCK_LOGIN=true
	// *and* we are not in production. A missing or misspelled env var in a prod
	// deploy must never silently expose the dev login endpoint.
	enableMockLogin := strings.EqualFold(GetEnv("ENABLE_MOCK_LOGIN", "false"), "true") && env != "production"
	if enableMockLogin {
		logger.Printf("WARNING: mock-login endpoint is enabled (ENABLE_MOCK_LOGIN=true, ENVIRONMENT=%s). Do NOT enable this in production.", env)
	}

	// Google OAuth audience allowlist. The token presented to /v1/auth/google
	// must have been minted for one of these client IDs; otherwise any valid
	// Google token (e.g. one issued to an unrelated attacker app) could be
	// replayed to take over the matching mootd account. Defaults to mootd's own
	// web client ID so real sign-in keeps working without extra config.
	var googleClientIDs []string
	for _, id := range strings.Split(GetEnv("GOOGLE_CLIENT_IDS", ""), ",") {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			googleClientIDs = append(googleClientIDs, trimmed)
		}
	}
	if len(googleClientIDs) == 0 {
		googleClientIDs = []string{defaultGoogleClientID}
		logger.Printf("INFO: GOOGLE_CLIENT_IDS not set — defaulting Google sign-in audience allowlist to the built-in web client ID. Set GOOGLE_CLIENT_IDS if you use a different/additional OAuth client (e.g. a dedicated iOS/Android client).")
	}

	cfg := Config{
		HTTPAddr:            GetEnv("HTTP_ADDR", defaultHTTPAddr),
		MongoURI:            GetEnv("MONGO_URI", defaultMongoURI),
		MongoDB:             GetEnv("MONGO_DB", defaultMongoDB),
		ConnectTimeout:      ParseDurationEnv("MONGO_CONNECT_TIMEOUT", defaultConnectTimeout, logger),
		ShutdownTimeout:     ParseDurationEnv("SHUTDOWN_TIMEOUT", defaultShutdownTimeout, logger),
		JWTSecret:           jwtSecret,
		AdminJWTSecret:      adminJWTSecret,
		CORSAllowedOrigins:  origins,
		DetectionAPIBaseURL: GetEnv("DETECTION_API_BASE_URL", defaultDetectionBaseURL),
		DetectionAPIKey:     detectionAPIKey,
		OllamaBaseURL:       GetEnv("OLLAMA_BASE_URL", defaultOllamaBaseURL),
		OllamaModel:         GetEnv("OLLAMA_MODEL", defaultOllamaModel),
		BgRemoverBaseURL:    GetEnv("BG_REMOVER_BASE_URL", defaultBgRemoverBaseURL),
		Environment:         GetEnv("ENVIRONMENT", defaultEnvironment),
		OutfitProvider:      outfitProvider,
		AnthropicBaseURL:    GetEnv("ANTHROPIC_BASE_URL", defaultAnthropicBaseURL),
		AnthropicAPIKey:     anthropicAPIKey,
		AnthropicModel:      GetEnv("ANTHROPIC_MODEL", defaultAnthropicModel),
		AnthropicVision:     anthropicVision,
		OpenAIBaseURL:       GetEnv("OPENAI_BASE_URL", defaultOpenAIBaseURL),
		OpenAIAPIKey:        GetEnv("OPENAI_API_KEY", ""),
		RedisURL:            GetEnv("REDIS_URL", defaultRedisURL),
		EnableMockLogin:     enableMockLogin,
		GoogleClientIDs:     googleClientIDs,
	}

	return cfg
}

// validateCORSOrigins returns an error if the origin list is unsafe for the
// given environment. In production, a wildcard entry or an empty list is
// rejected so operators cannot accidentally ship an open CORS policy.
func validateCORSOrigins(origins []string, env string) error {
	if env != "production" {
		return nil
	}
	if len(origins) == 0 || containsWildcard(origins) {
		return ErrCORSWildcardInProduction
	}
	return nil
}

// containsWildcard reports whether the origin list contains the '*' wildcard.
func containsWildcard(origins []string) bool {
	for _, o := range origins {
		if o == "*" {
			return true
		}
	}
	return false
}

// validateAdminAllowlist returns an error when the admin IP allowlist is unsafe
// for the given environment. In production an empty list (after trimming) is
// rejected, because middleware.AdminIPAllowlist treats an empty list as
// allow-all — which would leave /metrics reachable unauthenticated.
func validateAdminAllowlist(raw, env string) error {
	if env != "production" {
		return nil
	}
	hasEntry := false
	for _, c := range strings.Split(raw, ",") {
		if strings.TrimSpace(c) != "" {
			hasEntry = true
			break
		}
	}
	if !hasEntry {
		return ErrAdminAllowlistEmptyInProduction
	}
	return nil
}

// validOutfitProviders is the set of provider names the generator layer
// (app.go's buildOne) knows how to construct. Anything else is dropped from the
// OUTFIT_PROVIDER chain.
var validOutfitProviders = map[string]bool{"claude": true, "ollama": true, "openai": true}

// parseOutfitProviders normalises the OUTFIT_PROVIDER value, which may be a
// single provider ("claude") or a comma-separated cascade chain
// ("claude,openai,ollama"). Each entry is lower-cased and trimmed; empties and
// unknown names are dropped (the latter returned in `dropped` for warning
// logs); duplicates are removed while preserving order. Returns an empty
// `providers` when nothing valid remains, so the caller can fall back to the
// default.
func parseOutfitProviders(raw string) (providers []string, dropped []string) {
	seen := make(map[string]bool)
	for _, p := range strings.Split(raw, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if !validOutfitProviders[p] {
			dropped = append(dropped, p)
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		providers = append(providers, p)
	}
	return providers, dropped
}

// GetEnv retrieves an environment variable with a fallback value.
func GetEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// ParseDurationEnv parses a duration from an environment variable with a fallback value.
// If parsing fails, it logs a warning and returns the fallback duration.
func ParseDurationEnv(key string, fallback time.Duration, logger *log.Logger) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		logger.Printf("invalid duration in %s=%q, using fallback %s", key, value, fallback.String())
		return fallback
	}
	return parsed
}

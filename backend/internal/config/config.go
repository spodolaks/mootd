// Package config handles application configuration and environment variables.
package config

import (
	"log"
	"os"
	"strings"
	"time"
)

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
	// defaultCORSOrigins allows all origins in development.
	defaultCORSOrigins = "*"
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
	CORSAllowedOrigins  []string
	DetectionAPIBaseURL string
	DetectionAPIKey     string
	OllamaBaseURL       string
	OllamaModel         string
	BgRemoverBaseURL    string
	Environment         string

	// Outfit generation provider selection.
	OutfitProvider   string // "claude" or "ollama"
	AnthropicBaseURL string
	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicVision  bool

	// OpenAI (DALL-E) for generating moodboard backgrounds/textures.
	OpenAIBaseURL string
	OpenAIAPIKey  string

	// Redis for caching, rate limiting, and async jobs.
	RedisURL string
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

	rawOrigins := GetEnv("CORS_ALLOWED_ORIGINS", defaultCORSOrigins)
	var origins []string
	for _, o := range strings.Split(rawOrigins, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	detectionAPIKey := GetEnv("DETECTION_API_KEY", "")
	if detectionAPIKey == "" {
		logger.Printf("WARNING: DETECTION_API_KEY not set — clothing detection will fail in production.")
	}

	outfitProvider := strings.ToLower(GetEnv("OUTFIT_PROVIDER", defaultOutfitProvider))
	if outfitProvider != "claude" && outfitProvider != "ollama" && outfitProvider != "openai" {
		logger.Printf("WARNING: invalid OUTFIT_PROVIDER=%q, falling back to %q", outfitProvider, defaultOutfitProvider)
		outfitProvider = defaultOutfitProvider
	}

	anthropicAPIKey := GetEnv("ANTHROPIC_API_KEY", "")
	if outfitProvider == "claude" && anthropicAPIKey == "" {
		logger.Printf("WARNING: OUTFIT_PROVIDER=claude but ANTHROPIC_API_KEY is not set — outfit generation will fail.")
	}

	anthropicVision := strings.EqualFold(GetEnv("ANTHROPIC_VISION", defaultAnthropicVision), "true")

	cfg := Config{
		HTTPAddr:            GetEnv("HTTP_ADDR", defaultHTTPAddr),
		MongoURI:            GetEnv("MONGO_URI", defaultMongoURI),
		MongoDB:             GetEnv("MONGO_DB", defaultMongoDB),
		ConnectTimeout:      ParseDurationEnv("MONGO_CONNECT_TIMEOUT", defaultConnectTimeout, logger),
		ShutdownTimeout:     ParseDurationEnv("SHUTDOWN_TIMEOUT", defaultShutdownTimeout, logger),
		JWTSecret:           jwtSecret,
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
	}

	return cfg
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

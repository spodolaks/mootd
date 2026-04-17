package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"mootd/backend/internal/app"
	"mootd/backend/internal/config"
	"mootd/backend/internal/db"
	"mootd/backend/internal/shared/logging"
)

// main initializes and runs the mootd backend server.
// It:
//   - Loads configuration from environment variables
//   - Establishes a connection to MongoDB
//   - Creates an HTTP server with all routes and middleware
//   - Listens for incoming requests and gracefully shuts down on SIGINT/SIGTERM
func main() {
	// Use a basic logger for config loading (which may log warnings/fatals),
	// then switch to the structured slog-backed logger for the application.
	bootLogger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	cfg := config.Load(bootLogger)
	_, logger := logging.NewLogger(cfg.Environment)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	mongoClient, err := db.ConnectMongo(ctx, cfg.MongoURI)
	if err != nil {
		logger.Fatalf("mongo connect failed: %v", err)
	}
	defer func() {
		disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer disconnectCancel()
		if disconnectErr := mongoClient.Disconnect(disconnectCtx); disconnectErr != nil {
			logger.Printf("mongo disconnect error: %v", disconnectErr)
		}
	}()

	db.EnsureIndexes(ctx, mongoClient, cfg.MongoDB, logger)

	a := &app.App{
		Logger:              logger,
		MongoClient:         mongoClient,
		MongoDB:             cfg.MongoDB,
		JWTSecret:           cfg.JWTSecret,
		CORSAllowedOrigins:  cfg.CORSAllowedOrigins,
		DetectionAPIBaseURL: cfg.DetectionAPIBaseURL,
		DetectionAPIKey:     cfg.DetectionAPIKey,
		OllamaBaseURL:       cfg.OllamaBaseURL,
		OllamaModel:         cfg.OllamaModel,
		BgRemoverBaseURL:    cfg.BgRemoverBaseURL,
		Environment:         cfg.Environment,
		OutfitProvider:      cfg.OutfitProvider,
		AnthropicBaseURL:    cfg.AnthropicBaseURL,
		AnthropicAPIKey:     cfg.AnthropicAPIKey,
		AnthropicModel:      cfg.AnthropicModel,
		AnthropicVision:     cfg.AnthropicVision,
		OpenAIBaseURL:       cfg.OpenAIBaseURL,
		OpenAIAPIKey:        cfg.OpenAIAPIKey,
		RedisURL:            cfg.RedisURL,
	}

	// workerCtx is tied to server lifetime — cancelled on shutdown so background
	// goroutines (PNG retry, fire-and-forget bg removal) stop cleanly.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	handler, wardrobeRepo, _, rateLimitCloser := a.NewHTTPHandler(workerCtx)
	defer rateLimitCloser()

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: config.DefaultReadHeaderTimeout(),
		ReadTimeout:       config.DefaultReadTimeout(),
		WriteTimeout:      config.DefaultWriteTimeout(),
		IdleTimeout:       config.DefaultIdleTimeout(),
		MaxHeaderBytes:    1 << 20,
	}

	a.StartWorkers(workerCtx, wardrobeRepo)

	serverErr := make(chan error, 1)
	go func() {
		logger.Printf("api listening on %s", cfg.HTTPAddr)
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErr <- serveErr
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Printf("shutdown signal received: %s", sig.String())
	case serveErr := <-serverErr:
		logger.Fatalf("server failed: %v", serveErr)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Fatalf("server shutdown failed: %v", err)
	}
}

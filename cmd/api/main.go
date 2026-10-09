package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httphandler "github.com/Dragonmad10/leaderboard-api/internal/handler/http"
	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/Dragonmad10/leaderboard-api/internal/repository/postgres"
	"github.com/Dragonmad10/leaderboard-api/internal/repository/redis_repo"
	"github.com/Dragonmad10/leaderboard-api/internal/service"
	"github.com/redis/go-redis/v9"
)

// getEnv retrieves an environment variable or returns a default value if not set
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	// Initialize structured JSON logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// =========================================================================
	// REDIS CONNECTION SETUP
	// =========================================================================
	// Redis is used for:
	//   - Real-time leaderboard caching (Sorted Sets / ZSET)
	//   - Rate limiting (Sliding Window with Lua script)
	// Connection supports REDIS_ADDR, REDIS_URL, or defaults to localhost:6379
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = os.Getenv("REDIS_URL")
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	slog.Info("Connecting to Redis", slog.String("address", redisAddr))

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// Verify Redis connectivity with 5-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("Failed to connect to Redis", slog.Any("error", err))
		os.Exit(1)
	}

	// Initialize Redis repositories
	// LeaderboardRepository: handles ZSET operations for scores/ranks
	// RateLimiterRepository: handles sliding window rate limiting
	redisRepo := redis_repo.NewLeaderboardRepository(rdb)
	rateLimiterRepo := redis_repo.NewRateLimiterRepository(rdb)

	// =========================================================================
	// POSTGRESQL CONNECTION SETUP
	// =========================================================================
	// PostgreSQL is the "Source of Truth" for all persistent data:
	//   - Users, Leaderboards, Scores tables
	//   - ACID transactions for data integrity
	// Supports two configuration formats:
	//   1. Full connection URL (POSTGRES_URL)
	//   2. Individual variables (DB_HOST, DB_PORT, DB_USER, etc.) - Docker Compose style
	pgConnStr := os.Getenv("POSTGRES_URL")
	if pgConnStr == "" {
		// Build connection string from individual environment variables
		host := getEnv("DB_HOST", "localhost")
		port := getEnv("DB_PORT", "5432")
		user := getEnv("DB_USER", "postgres")
		password := getEnv("DB_PASSWORD", "postgres")
		dbname := getEnv("DB_NAME", "leaderboard")
		sslmode := getEnv("DB_SSLMODE", "disable")

		pgConnStr = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			user, password, host, port, dbname, sslmode)
		slog.Info("Built PostgreSQL connection string from individual env vars",
			slog.String("host", host), slog.String("port", port), slog.String("db", dbname))
	}

	slog.Info("Connecting to PostgreSQL")
	pgPool, err := postgres.NewPostgresPool(ctx, pgConnStr)
	if err != nil {
		slog.Error("Failed to connect to PostgreSQL", slog.Any("error", err))
		os.Exit(1)
	}
	defer pgPool.Close()

	// Initialize PostgreSQL repository (implements LeaderboardPersistentRepository interface)
	pgRepo := postgres.NewPostgresRepository(pgPool)

	// =========================================================================
	// SERVICE LAYER INITIALIZATION
	// =========================================================================
	// LeaderboardService coordinates between PostgreSQL (persistence) and Redis (cache)
	// RateLimiterService handles API rate limiting
	svc := service.NewLeaderboardService(pgRepo, redisRepo)
	rateLimiterService := service.NewRateLimiterService(rateLimiterRepo)

	// =========================================================================
	// HTTP HANDLER & ROUTER SETUP
	// =========================================================================
	handler := httphandler.NewLeaderboardHandler(svc)

	// Configure router with middleware chain:
	// 1. Gin Logger - request logging
	// 2. Recovery - panic recovery with structured logging
	// 3. RateLimiter - sliding window rate limiting (100 req/min per IP)
	rateLimiterMiddleware := middleware.RateLimiter(rateLimiterService, 100, 1*time.Minute)
	router := httphandler.SetupRouter(handler, rateLimiterMiddleware)

	// Start HTTP server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Run server in background goroutine
	go func() {
		slog.Info("Server started", slog.String("port", port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed to listen", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// =========================================================================
	// GRACEFUL SHUTDOWN HANDLING
	// =========================================================================
	// Wait for interrupt signal (SIGINT/SIGTERM)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")

	// Allow 5 seconds for in-flight requests to complete
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", slog.Any("error", err))
	}

	slog.Info("Server stopped cleanly")
}

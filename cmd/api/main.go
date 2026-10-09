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

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// --- Redis Connection ---
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("Failed to connect to Redis", slog.Any("error", err))
		os.Exit(1)
	}

	redisRepo := redis_repo.NewLeaderboardRepository(rdb)
	rateLimiterRepo := redis_repo.NewRateLimiterRepository(rdb)

	// --- PostgreSQL Connection ---
	// Soporta ambos formatos: POSTGRES_URL completo O vars individuales DB_*
	pgConnStr := os.Getenv("POSTGRES_URL")
	if pgConnStr == "" {
		// Construir desde vars individuales (formato docker-compose)
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

	pgRepo := postgres.NewPostgresRepository(pgPool)

	// --- Services ---
	// Pasar AMBOS repositorios: persistent (Postgres) y cache (Redis)
	svc := service.NewLeaderboardService(pgRepo, redisRepo)
	rateLimiterService := service.NewRateLimiterService(rateLimiterRepo)

	// --- HTTP Handler ---
	handler := httphandler.NewLeaderboardHandler(svc)

	// --- Router ---
	rateLimiterMiddleware := middleware.RateLimiter(rateLimiterService, 100, 1*time.Minute)
	router := httphandler.SetupRouter(handler, rateLimiterMiddleware)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	go func() {
		slog.Info("Server started", slog.String("port", port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed to listen", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", slog.Any("error", err))
	}

	slog.Info("Server stopped cleanly")
}

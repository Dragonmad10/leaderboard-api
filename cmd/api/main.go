package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httphandler "github.com/Dragonmad10/leaderboard-api/internal/handler/http"
	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/Dragonmad10/leaderboard-api/internal/repository/redis_repo"
	"github.com/Dragonmad10/leaderboard-api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

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

	repo := redis_repo.NewLeaderboardRepository(rdb)
	rateLimiterRepo := redis_repo.NewRateLimiterRepository(rdb)

	svc := service.NewLeaderboardService(repo)
	rateLimiterService := service.NewRateLimiterService(rateLimiterRepo)

	// Instanciado usando httphandler
	handler := httphandler.NewLeaderboardHandler(svc)

	router := gin.Default()
	router.Use(middleware.RateLimiter(rateLimiterService, 100, 1*time.Minute))

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "home"})
	})
	router.GET("/api/v1/leaderboards/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Leaderboard API is running"})
	})
	router.POST("/api/v1/leaderboards/:id/scores", handler.SubmitScore)
	router.GET("/api/v1/leaderboards/:id/scores", handler.GetTopScores)
	router.GET("/api/v1/leaderboards/:id/rank", handler.GetRank)
	router.DELETE("/api/v1/leaderboards/:id/scores/:user_id", handler.RemoveUserScore)

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

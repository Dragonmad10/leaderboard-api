package http

import (
	"log/slog"

	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/gin-gonic/gin"
)

// SetupRouter configures the HTTP router with all routes and middleware.
// log is where request logs go (main.go passes the JSON logger; tests pass one that discards).
func SetupRouter(handler *LeaderboardHandler, log *slog.Logger, rateLimiterMiddleware gin.HandlerFunc) *gin.Engine {
	router := gin.New()

	// Global middleware chain (applied to all routes). The ORDER matters:
	// each one wraps the ones after it, like layers of an onion.
	router.Use(middleware.Logger(log)) // 1. outermost: sees every request, even 429s and panics
	router.Use(middleware.Recovery())  // 2. turns a panic into a 500 that the logger can record
	router.Use(rateLimiterMiddleware)  // 3. rate limiting (sliding window)

	// Health and info endpoints (no authentication required)
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Welcome to Leaderboard API"})
	})
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	router.GET("/api/v1/leaderboards/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Leaderboard API is running"})
	})

	// User & Leaderboard management endpoints
	router.POST("/api/v1/users", handler.CreateUser)
	router.POST("/api/v1/leaderboards", handler.CreateLeaderboard)

	// Leaderboard score operations
	router.POST("/api/v1/leaderboards/:id/scores", handler.SubmitScore)                // Submit absolute score
	router.POST("/api/v1/leaderboards/:id/scores/increment", handler.IncrementScore)   // Increment score by delta
	router.GET("/api/v1/leaderboards/:id/scores", handler.GetTopScores)                // Get paginated top scores
	router.GET("/api/v1/leaderboards/:id/scores/:user_id", handler.GetUserScore)       // Get one user's score
	router.GET("/api/v1/leaderboards/:id/rank", handler.GetRank)                       // Get user rank
	router.GET("/api/v1/leaderboards/:id/stats", handler.GetStats)                     // Leaderboard stats (player count)
	router.DELETE("/api/v1/leaderboards/:id/scores/:user_id", handler.RemoveUserScore) // Remove user score

	return router
}

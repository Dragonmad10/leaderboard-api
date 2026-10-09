package http

import (
	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/gin-gonic/gin"
)

// SetupRouter configures the HTTP router with all routes and middleware
func SetupRouter(handler *LeaderboardHandler, rateLimiterMiddleware gin.HandlerFunc) *gin.Engine {
	router := gin.New()

	// Global middleware chain (applied to all routes)
	router.Use(gin.Logger())                 // Request/response logging
	router.Use(middleware.Recovery())        // Panic recovery with structured error response
	router.Use(rateLimiterMiddleware)        // Rate limiting (sliding window)

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
	router.POST("/api/v1/leaderboards/:id/scores", handler.SubmitScore)           // Submit absolute score
	router.POST("/api/v1/leaderboards/:id/scores/increment", handler.IncrementScore) // Increment score by delta
	router.GET("/api/v1/leaderboards/:id/scores", handler.GetTopScores)           // Get paginated top scores
	router.GET("/api/v1/leaderboards/:id/rank", handler.GetRank)                  // Get user rank
	router.DELETE("/api/v1/leaderboards/:id/scores/:user_id", handler.RemoveUserScore) // Remove user score

	return router
}
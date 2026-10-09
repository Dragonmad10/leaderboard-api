package http

import (
	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/gin-gonic/gin"
)

func SetupRouter(handler *LeaderboardHandler, rateLimiterMiddleware gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(middleware.Recovery())
	router.Use(rateLimiterMiddleware)

	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "home"})
	})
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	router.GET("/api/v1/leaderboards/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Leaderboard API is running"})
	})

	// User & Leaderboard management
	router.POST("/api/v1/users", handler.CreateUser)
	router.POST("/api/v1/leaderboards", handler.CreateLeaderboard)

	// Leaderboard endpoints
	router.POST("/api/v1/leaderboards/:id/scores", handler.SubmitScore)
	router.POST("/api/v1/leaderboards/:id/scores/increment", handler.IncrementScore)
	router.GET("/api/v1/leaderboards/:id/scores", handler.GetTopScores)
	router.GET("/api/v1/leaderboards/:id/rank", handler.GetRank)
	router.DELETE("/api/v1/leaderboards/:id/scores/:user_id", handler.RemoveUserScore)

	return router
}
package main

import (
	"github.com/Dragonmad10/leaderboard-api/internal/handler/http"
	"github.com/Dragonmad10/leaderboard-api/internal/repository/redis_repo"
	"github.com/Dragonmad10/leaderboard-api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func main() {
	// 1. Inicializar la conexión a Redis (La infraestructura)
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// 2. Crear el Repositorio inyectándole el cliente de Redis
	// Aquí repo es de tipo domain.LeaderboardRepository
	repo := redis_repo.NewLeaderboardRepository(rdb)

	// 3. Crear el Servicio inyectándole el Repositorio
	// Aquí svc es de tipo domain.LeaderboardService
	svc := service.NewLeaderboardService(repo)

	// 4. Crear el Handler inyectándole el Servicio
	handler := http.NewLeaderboardHandler(svc)

	// 5. Configurar Gin y las rutas (El servidor web)
	router := gin.Default()
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Home page"})
	})
	router.GET("/api/v1/leaderboards/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "Leaderboard API is running"})
	})
	router.POST("/api/v1/leaderboards/:id/scores", handler.SubmitScore)
	router.GET("/api/v1/leaderboards/:id/scores", handler.GetTopScores)
	router.GET("/api/v1/leaderboards/:id/rank", handler.GetRank)
	router.DELETE("/api/v1/leaderboards/:id/scores/:user_id", handler.RemoveUserScore)

	// 6. Levantar el servidor
	router.Run(":8080")
}

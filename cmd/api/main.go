package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// --- BASE DE DATOS FAKE (En memoria) ---
// Usamos un mapa para simular Redis. Clave: userID, Valor: Puntuación
var fakeDB = make(map[string]int)

// --- ESTRUCTURAS DE DATOS ---
// Así le decimos a Go qué formato tiene el JSON que nos envía el usuario
type ScoreRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Score  int    `json:"score" binding:"required"`
}

func main() {
	// 1. Inicializamos el router (el motor que gestiona las URLs)
	r := gin.Default()

	// 2. Creamos un grupo de rutas para la v1 de tu API
	v1 := r.Group("/api/v1")
	{
		// Endpoint de prueba para saber si el servidor está vivo
		v1.GET("/ping", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"mensaje": "¡El servidor funciona!"})
		})

		// Endpoint para añadir una puntuación (POST)
		v1.POST("/leaderboards/:id/scores", addScore)

		// Endpoint para ver el ranking (GET)
		v1.GET("/leaderboards/:id/top", getTopScores)
	}

	// 3. Arrancamos el servidor en el puerto 8080
	r.Run(":8080")
}

// --- CONTROLADORES (Endpoints) ---

// addScore recibe el JSON y guarda la puntuación
func addScore(c *gin.Context) {
	// Obtenemos el ID del leaderboard de la URL (ej: /leaderboards/global/scores)
	leaderboardID := c.Param("id")

	var req ScoreRequest
	// c.ShouldBindJSON lee el JSON del body y lo mete en la variable 'req'
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido o faltan datos"})
		return
	}

	// Guardamos en nuestra base de datos "fake"
	// En el futuro, aquí irá tu código de Redis: redisClient.ZAdd(...)
	fakeDB[req.UserID] = req.Score

	// Respondemos al cliente que todo ha ido bien
	c.JSON(http.StatusOK, gin.H{
		"mensaje":        "Puntuación guardada",
		"leaderboard_id": leaderboardID,
		"user_id":        req.UserID,
		"score":          req.Score,
	})
}

// getTopScores devuelve todas las puntuaciones guardadas
func getTopScores(c *gin.Context) {
	leaderboardID := c.Param("id")

	// En el futuro, aquí harás un ZREVRANGE en Redis.
	// Por ahora, simplemente devolvemos todo el mapa.
	c.JSON(http.StatusOK, gin.H{
		"leaderboard_id": leaderboardID,
		"ranking":        fakeDB,
	})
}
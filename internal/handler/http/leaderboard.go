package http

import (
	"fmt"
	"net/http"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/gin-gonic/gin"
)

// LeaderboardHandler es el equivalente a un [ApiController] en C#
type LeaderboardHandler struct {
	svc domain.LeaderboardService
}

// Constructor del Handler
func NewLeaderboardHandler(svc domain.LeaderboardService) *LeaderboardHandler {
	return &LeaderboardHandler{svc: svc}
}

// SubmitScore maneja el POST /api/v1/leaderboards/:id/scores
func (h *LeaderboardHandler) SubmitScore(c *gin.Context) {
	// 1. Extraer el ID de la URL
	leaderboardID := c.Param("id")

	// 2. Definir la estructura esperada del JSON (DTO en C#)
	var req struct {
		UserID string  `json:"user_id" binding:"required"`
		Score  float64 `json:"score" binding:"required"`
	}

	// 3. Parsear el JSON. Si falla (ej. falta el score), retorna Error 400 automático
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	// 4. Llamar a la capa de negocio (Servicio).
	// Pasamos c.Request.Context() para la propagación de cancelaciones.
	err := h.svc.SubmitScore(c.Request.Context(), leaderboardID, req.UserID, req.Score)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save score"})
		return
	}

	// 5. Retornar éxito (201 Created)
	c.JSON(http.StatusCreated, gin.H{"message": "score updated successfully"})
}

func (h *LeaderboardHandler) GetTopScores(c *gin.Context) {
	leaderboardID := c.Param("id")

	// Parse query parameters for limit and offset
	limit := 10 // default limit
	offset := 0 // default offset

	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if o := c.Query("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}

	scores, err := h.svc.GetTopScores(c.Request.Context(), leaderboardID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve scores"})
		return
	}

	c.JSON(http.StatusOK, scores)
}

func (h *LeaderboardHandler) GetRank(c *gin.Context) {
	leaderboardID := c.Param("id")
	userID := c.Query("user_id")

	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id query parameter is required"})
		return
	}

	rank, err := h.svc.GetUserRank(c.Request.Context(), leaderboardID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve rank"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_id": userID, "rank": rank})
}

func (h *LeaderboardHandler) RemoveUserScore(c *gin.Context) {
	leaderboardID := c.Param("id")
	userID := c.Param("user_id")

	err := h.svc.RemoveUserScore(c.Request.Context(), leaderboardID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove user score"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user score removed successfully"})
}

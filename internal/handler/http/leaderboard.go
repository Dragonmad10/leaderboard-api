package http

import (
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

// handleError convierte errores de dominio a respuestas HTTP apropiadas
func handleError(c *gin.Context, err error, defaultMsg string) {
	code := domain.GetErrorCode(err)
	msg := domain.GetErrorMessage(err)
	if msg == "Internal server error" || msg == "" {
		msg = defaultMsg
	}
	c.JSON(code, gin.H{"error": msg})
}

// SubmitScore maneja el POST /api/v1/leaderboards/:id/scores
func (h *LeaderboardHandler) SubmitScore(c *gin.Context) {
	leaderboardID := c.Param("id")

	var req struct {
		UserID string  `json:"user_id" binding:"required"`
		Score  float64 `json:"score" binding:"required,gte=0"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format: " + err.Error()})
		return
	}

	err := h.svc.SubmitScore(c.Request.Context(), leaderboardID, req.UserID, req.Score)
	if err != nil {
		handleError(c, err, "Failed to save score")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "score updated successfully"})
}

// IncrementScore maneja el POST /api/v1/leaderboards/:id/scores/increment
func (h *LeaderboardHandler) IncrementScore(c *gin.Context) {
	leaderboardID := c.Param("id")

	var req struct {
		UserID string `json:"user_id" binding:"required"`
		Delta  int64  `json:"delta" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format: " + err.Error()})
		return
	}

	newScore, err := h.svc.IncrementScore(c.Request.Context(), leaderboardID, req.UserID, req.Delta)
	if err != nil {
		handleError(c, err, "Failed to increment score")
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_id": req.UserID, "new_score": newScore})
}

// parseIntQuery parsea un parámetro de query string a int, retorna 0 si inválido
func parseIntQuery(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	var parsed int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		parsed = parsed*10 + int(r-'0')
	}
	return parsed, true
}

func (h *LeaderboardHandler) GetTopScores(c *gin.Context) {
	leaderboardID := c.Param("id")

	limit := 10
	offset := 0

	if l := c.Query("limit"); l != "" {
		if parsed, ok := parseIntQuery(l); ok {
			limit = parsed
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter"})
			return
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, ok := parseIntQuery(o); ok {
			offset = parsed
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid offset parameter"})
			return
		}
	}

	scores, err := h.svc.GetTopScores(c.Request.Context(), leaderboardID, limit, offset)
	if err != nil {
		handleError(c, err, "Failed to retrieve scores")
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
		handleError(c, err, "Failed to retrieve rank")
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_id": userID, "rank": rank})
}

func (h *LeaderboardHandler) RemoveUserScore(c *gin.Context) {
	leaderboardID := c.Param("id")
	userID := c.Param("user_id")

	err := h.svc.RemoveUserScore(c.Request.Context(), leaderboardID, userID)
	if err != nil {
		handleError(c, err, "Failed to remove user score")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user score removed successfully"})
}

// CreateUser maneja POST /api/v1/users
func (h *LeaderboardHandler) CreateUser(c *gin.Context) {
	var req struct {
		UserID   string `json:"user_id" binding:"required"`
		Username string `json:"username" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format: " + err.Error()})
		return
	}

	err := h.svc.CreateUser(c.Request.Context(), req.UserID, req.Username)
	if err != nil {
		handleError(c, err, "Failed to create user")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "user created successfully", "user_id": req.UserID})
}

// CreateLeaderboard maneja POST /api/v1/leaderboards
func (h *LeaderboardHandler) CreateLeaderboard(c *gin.Context) {
	var req struct {
		LeaderboardID string `json:"leaderboard_id" binding:"required"`
		Name          string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format: " + err.Error()})
		return
	}

	err := h.svc.CreateLeaderboard(c.Request.Context(), req.LeaderboardID, req.Name)
	if err != nil {
		handleError(c, err, "Failed to create leaderboard")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "leaderboard created successfully", "leaderboard_id": req.LeaderboardID})
}
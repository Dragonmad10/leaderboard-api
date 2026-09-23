package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/gin-gonic/gin"
)

func RateLimiter(service domain.RateLimiterService, limit int64, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Identificador único por IP del cliente (ampliable a API Key o Bearer Token)
		clientIP := c.ClientIP()

		result, err := service.CheckLimit(c.Request.Context(), clientIP, limit, window)
		if err != nil {
			// En producción Tier 1, si Redis falla se adopta una política "fail-open" para no bloquear usuarios
			c.Next()
			return
		}

		// Inyección de cabeceras RFC 6585 / IETF Draft
		c.Header("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(int64(result.ResetIn.Seconds()), 10))

		if !result.Allowed {
			c.Header("Retry-After", strconv.FormatInt(int64(result.ResetIn.Seconds()), 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":    "Too Many Requests",
				"message":  "Ha excedido el límite de peticiones permitido. Intente de nuevo más tarde.",
				"retry_in": result.ResetIn.String(),
			})
			return
		}

		c.Next()
	}
}

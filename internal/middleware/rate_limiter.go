package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/gin-gonic/gin"
)

// RateLimiter returns a Gin middleware that enforces rate limits per client IP.
// Uses sliding window algorithm via Redis (fail-open: allows requests if Redis is down).
// Injects standard rate limit headers (RFC 6585 / IETF Draft).
func RateLimiter(service domain.RateLimiterService, limit int64, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Identify client by IP (can be extended to API Key or Bearer Token)
		clientIP := c.ClientIP()

		result, err := service.CheckLimit(c.Request.Context(), clientIP, limit, window)
		if err != nil {
			// Fail-open: if Redis fails, allow request to not block legitimate users
			c.Next()
			return
		}

		// Inject standard rate limit headers
		c.Header("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(int64(result.ResetIn.Seconds()), 10))

		if !result.Allowed {
			c.Header("Retry-After", strconv.FormatInt(int64(result.ResetIn.Seconds()), 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":    "Too Many Requests",
				"message":  "Rate limit exceeded. Please try again later.",
				"retry_in": result.ResetIn.String(),
			})
			return
		}

		c.Next()
	}
}

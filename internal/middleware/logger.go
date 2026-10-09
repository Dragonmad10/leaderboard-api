package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"log/slog"
)

// Logger returns a Gin middleware that logs HTTP requests with structured logging.
// Uses slog for JSON-structured logs with request details.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Log after request completes
		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()
		errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String()

		if raw != "" {
			path = path + "?" + raw
		}

		slog.Info("HTTP Request",
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", statusCode),
			slog.String("latency", latency.String()),
			slog.String("client_ip", clientIP),
			slog.String("user_agent", c.Request.UserAgent()),
		)

		if errorMessage != "" {
			slog.Error("Request error", slog.String("error", errorMessage))
		}
	}
}

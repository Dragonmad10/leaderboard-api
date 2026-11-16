package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger returns a Gin middleware that writes ONE structured log line per request,
// after the request has finished.
//
// The logger is passed in (dependency injection) instead of using the global
// slog.Default(): main.go decides where logs go, and tests can pass a logger that
// writes into a buffer and check what was logged.
//
// Register it FIRST (outermost) so it also sees requests that other middleware
// stopped (429 from the rate limiter) or that panicked (500 from Recovery).
func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		if raw := c.Request.URL.RawQuery; raw != "" {
			path += "?" + raw
		}

		// Run the rest of the chain: other middleware and the handler.
		c.Next()

		status := c.Writer.Status()

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			// A number, not a string like "1.2ms", so log tools can average it and
			// compute percentiles. Float, because most requests take less than 1ms.
			slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("client_ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
		}

		// Errors that handlers attached with c.Error(err) go into this same line,
		// so one request = one log line with all its context.
		if errs := c.Errors.ByType(gin.ErrorTypePrivate).Errors(); len(errs) > 0 {
			attrs = append(attrs, slog.String("error", strings.Join(errs, "; ")))
		}

		log.LogAttrs(c.Request.Context(), levelForStatus(status), "http request", attrs...)
	}
}

// levelForStatus picks how urgent a request's log line is:
// 5xx is our fault (ERROR: someone should look), 4xx is the client's fault
// (WARN: worth seeing, not urgent), everything else is INFO.
func levelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

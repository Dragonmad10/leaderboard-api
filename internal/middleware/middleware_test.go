package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// fakeLimiter pretends to be the rate limiter service: it answers whatever we
// configure and remembers what it was asked.
type fakeLimiter struct {
	result *domain.RateLimitResult
	err    error

	gotIdentifier string
	gotLimit      int64
	gotWindow     time.Duration
}

func (f *fakeLimiter) CheckLimit(_ context.Context, identifier string, limit int64, window time.Duration) (*domain.RateLimitResult, error) {
	f.gotIdentifier, f.gotLimit, f.gotWindow = identifier, limit, window
	return f.result, f.err
}

// call sends one request through the given middleware. It returns the response
// and whether the request reached the real handler behind the middleware.
func call(mw gin.HandlerFunc, handler gin.HandlerFunc) (rec *httptest.ResponseRecorder, reachedHandler bool) {
	router := gin.New()
	router.Use(mw)
	router.GET("/test", func(c *gin.Context) {
		reachedHandler = true
		handler(c)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, reachedHandler
}

func ok(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) }

// =============================================================================
// RateLimiter
// =============================================================================

func TestRateLimiter_UnderLimit_LetsRequestThrough(t *testing.T) {
	limiter := &fakeLimiter{result: &domain.RateLimitResult{
		Allowed: true, Limit: 100, Remaining: 99, ResetIn: 60 * time.Second,
	}}

	rec, reached := call(RateLimiter(limiter, 100, time.Minute), ok)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("status = %d, reached handler = %v; want 200 and true", rec.Code, reached)
	}

	// The README promises these headers on every response.
	wantHeaders := map[string]string{
		"X-RateLimit-Limit":     "100",
		"X-RateLimit-Remaining": "99",
		"X-RateLimit-Reset":     "60",
	}
	for name, want := range wantHeaders {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("header %s = %q; want %q", name, got, want)
		}
	}
}

func TestRateLimiter_PassesIPLimitAndWindowToService(t *testing.T) {
	limiter := &fakeLimiter{result: &domain.RateLimitResult{Allowed: true}}

	call(RateLimiter(limiter, 100, time.Minute), ok)

	if limiter.gotIdentifier != "203.0.113.7" {
		t.Errorf("identifier = %q; want the client IP 203.0.113.7", limiter.gotIdentifier)
	}
	if limiter.gotLimit != 100 || limiter.gotWindow != time.Minute {
		t.Errorf("limit = %d, window = %v; want 100 and 1m", limiter.gotLimit, limiter.gotWindow)
	}
}

func TestRateLimiter_OverLimit_Returns429(t *testing.T) {
	limiter := &fakeLimiter{result: &domain.RateLimitResult{
		Allowed: false, Limit: 100, Remaining: 0, ResetIn: 42 * time.Second,
	}}

	rec, reached := call(RateLimiter(limiter, 100, time.Minute), ok)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d; want 429", rec.Code)
	}
	if reached {
		t.Error("a blocked request still reached the handler")
	}
	if got := rec.Header().Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q; want %q", got, "42")
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("X-RateLimit-Remaining = %q; want %q", got, "0")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("429 response is not JSON: %v", err)
	}
	if body["error"] == nil {
		t.Error(`429 response has no "error" field`)
	}
}

// README: "The rate limiter fails open. If Redis is down, requests are let
// through instead of blocking everyone."
func TestRateLimiter_RedisDown_FailsOpen(t *testing.T) {
	limiter := &fakeLimiter{err: errors.New("redis: connection refused")}

	rec, reached := call(RateLimiter(limiter, 100, time.Minute), ok)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("status = %d, reached handler = %v; want 200 and true", rec.Code, reached)
	}
}

// =============================================================================
// Recovery
// =============================================================================

// A panic inside a handler must not crash the server. The client gets a clean
// 500 JSON response that does not reveal what went wrong inside.
func TestRecovery_PanicBecomesA500(t *testing.T) {
	// Recovery logs the panic with a long stack trace. Hide it during the test.
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	rec, _ := call(Recovery(), func(c *gin.Context) {
		panic("secret internal detail")
	})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("500 response is not JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if body["error"] == nil {
		t.Error(`500 response has no "error" field`)
	}
	if got := rec.Body.String(); strings.Contains(got, "secret internal detail") {
		t.Errorf("response leaks the panic message: %s", got)
	}
}

func TestRecovery_NoPanic_DoesNothing(t *testing.T) {
	rec, reached := call(Recovery(), ok)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("status = %d, reached handler = %v; want 200 and true", rec.Code, reached)
	}
}

// =============================================================================
// Logger
// =============================================================================

// The logger receives its *slog.Logger as a parameter, so a test can give it
// one that writes into a buffer and then read back exactly what was logged.

// newCapturingLogger returns a JSON logger that writes into buf.
// Level Debug so nothing is filtered out by the logger itself.
func newCapturingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// logLines parses every line in buf as one JSON object.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %v\nline: %s", err, line)
		}
		lines = append(lines, entry)
	}
	return lines
}

// onlyLogLine checks that exactly one line was logged and returns it.
func onlyLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines; want exactly 1 per request\nlog:\n%s", len(lines), buf.String())
	}
	return lines[0]
}

// The logger must never change the response; it only observes it.
func TestLogger_DoesNotChangeTheResponse(t *testing.T) {
	var buf bytes.Buffer

	rec, reached := call(Logger(newCapturingLogger(&buf)), ok)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("status = %d, reached handler = %v; want 200 and true", rec.Code, reached)
	}
}

func TestLogger_WritesOneJSONLineWithTheRequestDetails(t *testing.T) {
	var buf bytes.Buffer

	call(Logger(newCapturingLogger(&buf)), ok)

	entry := onlyLogLine(t, &buf)
	if entry["method"] != "GET" {
		t.Errorf("method = %v; want GET", entry["method"])
	}
	if entry["path"] != "/test" {
		t.Errorf("path = %v; want /test", entry["path"])
	}
	// JSON numbers come back as float64 in Go. Status must be a number, not "200".
	if entry["status"] != float64(200) {
		t.Errorf("status = %v (%T); want the number 200", entry["status"], entry["status"])
	}
	if entry["client_ip"] != "203.0.113.7" {
		t.Errorf("client_ip = %v; want 203.0.113.7", entry["client_ip"])
	}
}

// Latency must be a number so log tools can average it and compute p95.
// A string like "1.2ms" cannot be sorted or summed.
func TestLogger_LatencyIsANumberInMilliseconds(t *testing.T) {
	var buf bytes.Buffer

	call(Logger(newCapturingLogger(&buf)), func(c *gin.Context) {
		time.Sleep(5 * time.Millisecond)
		ok(c)
	})

	entry := onlyLogLine(t, &buf)
	latency, isNumber := entry["latency_ms"].(float64)
	if !isNumber {
		t.Fatalf("latency_ms = %v (%T); want a number", entry["latency_ms"], entry["latency_ms"])
	}
	if latency < 5 {
		t.Errorf("latency_ms = %v; want at least 5 (the handler slept 5ms)", latency)
	}
}

// The level tells you how urgent a line is, so alerts can watch for ERROR.
// 5xx is our fault (ERROR), 4xx is the client's fault (WARN), the rest is INFO.
func TestLogger_LevelFollowsTheStatus(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{http.StatusOK, "INFO"},
		{http.StatusCreated, "INFO"},
		{http.StatusBadRequest, "WARN"},
		{http.StatusNotFound, "WARN"},
		{http.StatusTooManyRequests, "WARN"},
		{http.StatusInternalServerError, "ERROR"},
		{http.StatusServiceUnavailable, "ERROR"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer

		call(Logger(newCapturingLogger(&buf)), func(c *gin.Context) { c.Status(tt.status) })

		if got := onlyLogLine(t, &buf)["level"]; got != tt.want {
			t.Errorf("status %d logged at level %v; want %s", tt.status, got, tt.want)
		}
	}
}

// Errors a handler attaches with c.Error(err) go into the SAME log line as the
// request, so one request = one line with all its context.
func TestLogger_AttachedErrorsAreInTheSameLine(t *testing.T) {
	var buf bytes.Buffer

	call(Logger(newCapturingLogger(&buf)), func(c *gin.Context) {
		_ = c.Error(errors.New("connection refused"))
		c.Status(http.StatusInternalServerError)
	})

	entry := onlyLogLine(t, &buf)
	if got, _ := entry["error"].(string); !strings.Contains(got, "connection refused") {
		t.Errorf("error = %q; want it to contain the cause %q", got, "connection refused")
	}
}

func TestLogger_NoErrorFieldWhenThereIsNoError(t *testing.T) {
	var buf bytes.Buffer

	call(Logger(newCapturingLogger(&buf)), ok)

	if got, exists := onlyLogLine(t, &buf)["error"]; exists {
		t.Errorf("error = %v; want no error field for a successful request", got)
	}
}

// With Logger outside Recovery (the order used in SetupRouter), a request that
// panicked is still logged, with the 500 that Recovery wrote.
func TestLogger_OutsideRecovery_LogsPanicsAs500(t *testing.T) {
	var buf bytes.Buffer
	// Recovery still uses the global slog logger for the stack trace. Hide it.
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	router := gin.New()
	router.Use(Logger(newCapturingLogger(&buf)), Recovery())
	router.GET("/test", func(c *gin.Context) { panic("boom") })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/test", nil))

	entry := onlyLogLine(t, &buf)
	if entry["status"] != float64(500) || entry["level"] != "ERROR" {
		t.Errorf("status = %v, level = %v; want 500 and ERROR", entry["status"], entry["level"])
	}
}

package middleware

import (
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

// The logger must never change the response; it only observes it.
func TestLogger_DoesNotChangeTheResponse(t *testing.T) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	rec, reached := call(Logger(), ok)

	if rec.Code != http.StatusOK || !reached {
		t.Errorf("status = %d, reached handler = %v; want 200 and true", rec.Code, reached)
	}
}

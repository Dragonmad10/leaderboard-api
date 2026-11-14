//go:build integration

// Package integration tests the whole API against a REAL PostgreSQL and a REAL
// Redis: router -> handler -> service -> repositories -> databases.
//
// The unit tests use fakes, so they cannot tell if the SQL or the Redis
// commands are wrong. This test can.
//
// The first line of this file (the "build tag") means these tests are skipped
// by a normal `go test ./...`. Run them with:
//
//	docker compose up -d postgres redis
//	make test-integration
//
// By default it connects to the databases from docker-compose.yml. Set
// POSTGRES_URL and REDIS_ADDR to use different ones.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	httphandler "github.com/Dragonmad10/leaderboard-api/internal/handler/http"
	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/Dragonmad10/leaderboard-api/internal/repository/postgres"
	"github.com/Dragonmad10/leaderboard-api/internal/repository/redis_repo"
	"github.com/Dragonmad10/leaderboard-api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// app is the real application, wired the same way as cmd/api/main.go.
type app struct {
	router *gin.Engine
	pg     *pgxpool.Pool
	redis  *redis.Client
}

// newApp connects to the real databases and builds the real router.
// rateLimit is the number of requests allowed per minute per IP.
func newApp(t *testing.T, rateLimit int64) *app {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pgURL := envOr("POSTGRES_URL", "postgres://leaderboard_user:leaderboard_password@localhost:5432/leaderboard_db?sslmode=disable")
	pool, err := postgres.NewPostgresPool(ctx, pgURL)
	if err != nil {
		t.Fatalf("cannot connect to PostgreSQL (is `docker compose up -d postgres redis` running?): %v", err)
	}
	t.Cleanup(pool.Close)

	rdb := redis.NewClient(&redis.Options{Addr: envOr("REDIS_ADDR", "localhost:6379")})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("cannot connect to Redis (is `docker compose up -d postgres redis` running?): %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	svc := service.NewLeaderboardService(
		postgres.NewPostgresRepository(pool),
		redis_repo.NewLeaderboardRepository(rdb),
	)
	limiter := service.NewRateLimiterService(redis_repo.NewRateLimiterRepository(rdb))
	router := httphandler.SetupRouter(
		httphandler.NewLeaderboardHandler(svc),
		middleware.RateLimiter(limiter, rateLimit, time.Minute),
	)

	return &app{router: router, pg: pool, redis: rdb}
}

// do sends one HTTP request to the app, pretending to come from clientIP.
func (a *app) do(clientIP, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = clientIP + ":40000"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

// uniqueID makes names that no other test run (or real data) is using, so the
// test never touches data it did not create.
func uniqueID(prefix string) string {
	return fmt.Sprintf("it-%s-%d", prefix, time.Now().UnixNano())
}

var ipCounter int64

// uniqueIP returns a made-up client IP so the rate limit counter of this test
// is separate from everything else.
func uniqueIP() string {
	ipCounter++
	n := time.Now().UnixNano() + ipCounter
	return fmt.Sprintf("10.%d.%d.%d", (n>>16)%256, (n>>8)%256, n%256)
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, step string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: status = %d; want %d\nbody: %s", step, rec.Code, want, rec.Body.String())
	}
}

// topScores asks the API for the top 10 of a leaderboard.
func (a *app) topScores(t *testing.T, ip, leaderboardID string) []domain.UserScore {
	t.Helper()
	rec := a.do(ip, "GET", "/api/v1/leaderboards/"+leaderboardID+"/scores?limit=10", "")
	mustStatus(t, rec, http.StatusOK, "get top scores")

	var scores []domain.UserScore
	if err := json.Unmarshal(rec.Body.Bytes(), &scores); err != nil {
		t.Fatalf("top scores response is not a JSON list: %v\nbody: %s", err, rec.Body.String())
	}
	return scores
}

func (a *app) rank(t *testing.T, ip, leaderboardID, userID string) int64 {
	t.Helper()
	rec := a.do(ip, "GET", "/api/v1/leaderboards/"+leaderboardID+"/rank?user_id="+userID, "")
	mustStatus(t, rec, http.StatusOK, "get rank of "+userID)

	var body struct {
		Rank int64 `json:"rank"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("rank response is not JSON: %v", err)
	}
	return body.Rank
}

func wantOrder(t *testing.T, step string, scores []domain.UserScore, want ...string) {
	t.Helper()
	got := make([]string, len(scores))
	for i, s := range scores {
		got[i] = s.UserID
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: order = %v; want %v", step, got, want)
	}
}

// TestLeaderboardFlow plays a small but complete story against the real
// databases: create things, submit scores, read rankings, lose the cache,
// delete a player.
func TestLeaderboardFlow(t *testing.T) {
	a := newApp(t, 1000) // high limit: this test is not about rate limiting
	ctx := context.Background()
	ip := uniqueIP()

	board := uniqueID("board")
	alice, bob, carol := uniqueID("alice"), uniqueID("bob"), uniqueID("carol")
	cacheKey := "leaderboard:" + board

	// Remove everything this test created, even if it fails halfway.
	t.Cleanup(func() {
		_, _ = a.pg.Exec(ctx, `DELETE FROM leaderboards WHERE id = $1`, board) // scores go with it (ON DELETE CASCADE)
		_, _ = a.pg.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []string{alice, bob, carol})
		_ = a.redis.Del(ctx, cacheKey, "ratelimit:"+ip).Err()
	})

	// 1. Create the leaderboard and the users.
	rec := a.do(ip, "POST", "/api/v1/leaderboards", fmt.Sprintf(`{"leaderboard_id": %q, "name": "Integration test"}`, board))
	mustStatus(t, rec, http.StatusCreated, "create leaderboard")
	for _, user := range []string{alice, bob, carol} {
		rec = a.do(ip, "POST", "/api/v1/users", fmt.Sprintf(`{"user_id": %q, "username": "Test user"}`, user))
		mustStatus(t, rec, http.StatusCreated, "create user")
	}

	// Creating the same user twice must not fail (the README says it is idempotent).
	rec = a.do(ip, "POST", "/api/v1/users", fmt.Sprintf(`{"user_id": %q, "username": "Test user"}`, alice))
	mustStatus(t, rec, http.StatusCreated, "create the same user again")

	// 2. Submit scores.
	for user, score := range map[string]int{alice: 300, bob: 200, carol: 100} {
		rec = a.do(ip, "POST", "/api/v1/leaderboards/"+board+"/scores", fmt.Sprintf(`{"user_id": %q, "score": %d}`, user, score))
		mustStatus(t, rec, http.StatusCreated, "submit score")
	}

	// 3. Top scores come back highest first, with scores and ranks.
	top := a.topScores(t, ip, board)
	wantOrder(t, "after submitting", top, alice, bob, carol)
	if len(top) == 3 {
		if top[0].Score != 300 || top[2].Score != 100 {
			t.Errorf("scores = %d ... %d; want 300 ... 100", top[0].Score, top[2].Score)
		}
		for i, s := range top {
			if s.Rank != int64(i+1) {
				t.Errorf("position %d has rank %d; want %d", i+1, s.Rank, i+1)
			}
		}
	}

	// 4. Rank of one user.
	if got := a.rank(t, ip, board, bob); got != 2 {
		t.Errorf("bob's rank = %d; want 2", got)
	}

	// 5. Increment: carol gains 250 points (100 -> 350) and takes first place.
	rec = a.do(ip, "POST", "/api/v1/leaderboards/"+board+"/scores/increment", fmt.Sprintf(`{"user_id": %q, "delta": 250}`, carol))
	mustStatus(t, rec, http.StatusOK, "increment score")
	var inc struct {
		NewScore int64 `json:"new_score"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &inc)
	if inc.NewScore != 350 {
		t.Errorf("new_score = %d; want 350", inc.NewScore)
	}
	if got := a.rank(t, ip, board, carol); got != 1 {
		t.Errorf("carol's rank after the increment = %d; want 1", got)
	}

	// 6. PostgreSQL is the source of truth: the new score must really be there.
	var stored int64
	if err := a.pg.QueryRow(ctx, `SELECT score FROM scores WHERE leaderboard_id = $1 AND user_id = $2`, board, carol).Scan(&stored); err != nil {
		t.Fatalf("reading carol's score from PostgreSQL: %v", err)
	}
	if stored != 350 {
		t.Errorf("score stored in PostgreSQL = %d; want 350", stored)
	}

	// 7. Lose the cache (as if Redis had restarted). The API must still answer
	//    correctly from PostgreSQL, and then refill Redis in the background.
	if err := a.redis.Del(ctx, cacheKey).Err(); err != nil {
		t.Fatalf("deleting the cache key: %v", err)
	}
	wantOrder(t, "after losing the cache", a.topScores(t, ip, board), carol, alice, bob)

	refilled := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if n, _ := a.redis.ZCard(ctx, cacheKey).Result(); n == 3 {
			refilled = true
			break
		}
	}
	if !refilled {
		t.Error("Redis was not refilled after the cache miss")
	}

	// 8. Delete alice. She disappears from the list and her rank is a 404.
	rec = a.do(ip, "DELETE", "/api/v1/leaderboards/"+board+"/scores/"+alice, "")
	mustStatus(t, rec, http.StatusOK, "delete score")
	wantOrder(t, "after deleting alice", a.topScores(t, ip, board), carol, bob)

	rec = a.do(ip, "GET", "/api/v1/leaderboards/"+board+"/rank?user_id="+alice, "")
	mustStatus(t, rec, http.StatusNotFound, "rank of a deleted user")
}

// TestRateLimit checks the real Lua script in Redis: with a limit of 3
// requests per minute, the 4th request from the same IP gets a 429.
func TestRateLimit(t *testing.T) {
	a := newApp(t, 3)
	ip := uniqueIP()
	t.Cleanup(func() { _ = a.redis.Del(context.Background(), "ratelimit:"+ip).Err() })

	for i := 1; i <= 3; i++ {
		rec := a.do(ip, "GET", "/health", "")
		mustStatus(t, rec, http.StatusOK, fmt.Sprintf("request %d", i))

		wantRemaining := fmt.Sprint(3 - i)
		if got := rec.Header().Get("X-RateLimit-Remaining"); got != wantRemaining {
			t.Errorf("request %d: X-RateLimit-Remaining = %q; want %q", i, got, wantRemaining)
		}
	}

	rec := a.do(ip, "GET", "/health", "")
	mustStatus(t, rec, http.StatusTooManyRequests, "request 4")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("the 429 response has no Retry-After header")
	}

	// A different client is not affected.
	otherIP := uniqueIP()
	t.Cleanup(func() { _ = a.redis.Del(context.Background(), "ratelimit:"+otherIP).Err() })
	rec = a.do(otherIP, "GET", "/health", "")
	mustStatus(t, rec, http.StatusOK, "request from another client")
}

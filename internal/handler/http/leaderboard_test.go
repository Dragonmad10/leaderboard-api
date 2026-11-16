package http

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
	"github.com/Dragonmad10/leaderboard-api/internal/middleware"
	"github.com/gin-gonic/gin"
)

// init runs once before the tests in this package. It turns off Gin's debug
// output so the test results are easy to read.
func init() {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
}

// =============================================================================
// FAKE SERVICE
// =============================================================================
// The handlers only need "something that implements domain.LeaderboardService".
// This fake returns whatever we configure and remembers what it was asked,
// so we can test the HTTP layer without any database.

type fakeService struct {
	// What the fake should answer.
	err         error
	scores      []domain.UserScore
	rank        int64
	newScore    int64
	userScore   int64
	playerCount int64

	// What the handler sent to the service.
	called        bool
	leaderboardID string
	userID        string
	name          string
	score         float64
	delta         int64
	limit         int
	offset        int
}

func (f *fakeService) SubmitScore(_ context.Context, leaderboardID, userID string, score float64) error {
	f.called, f.leaderboardID, f.userID, f.score = true, leaderboardID, userID, score
	return f.err
}

func (f *fakeService) IncrementScore(_ context.Context, leaderboardID, userID string, delta int64) (int64, error) {
	f.called, f.leaderboardID, f.userID, f.delta = true, leaderboardID, userID, delta
	return f.newScore, f.err
}

func (f *fakeService) GetTopScores(_ context.Context, leaderboardID string, limit, offset int) ([]domain.UserScore, error) {
	f.called, f.leaderboardID, f.limit, f.offset = true, leaderboardID, limit, offset
	return f.scores, f.err
}

func (f *fakeService) GetUserRank(_ context.Context, leaderboardID, userID string) (int64, error) {
	f.called, f.leaderboardID, f.userID = true, leaderboardID, userID
	return f.rank, f.err
}

func (f *fakeService) GetUserScore(_ context.Context, leaderboardID, userID string) (int64, error) {
	f.called, f.leaderboardID, f.userID = true, leaderboardID, userID
	return f.userScore, f.err
}

func (f *fakeService) CountPlayers(_ context.Context, leaderboardID string) (int64, error) {
	f.called, f.leaderboardID = true, leaderboardID
	return f.playerCount, f.err
}

func (f *fakeService) RemoveUserScore(_ context.Context, leaderboardID, userID string) error {
	f.called, f.leaderboardID, f.userID = true, leaderboardID, userID
	return f.err
}

func (f *fakeService) CreateUser(_ context.Context, userID, username string) error {
	f.called, f.userID, f.name = true, userID, username
	return f.err
}

func (f *fakeService) CreateLeaderboard(_ context.Context, leaderboardID, name string) error {
	f.called, f.leaderboardID, f.name = true, leaderboardID, name
	return f.err
}

// =============================================================================
// TEST HELPERS
// =============================================================================

// noRateLimit is a middleware that lets everything through.
func noRateLimit(c *gin.Context) { c.Next() }

// discardLogger throws request logs away, so test output stays readable.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// send builds the real router (same routes as production), sends one request
// to it and returns the recorded response. No real server or port is needed.
func send(svc domain.LeaderboardService, method, path, body string) *httptest.ResponseRecorder {
	router := SetupRouter(NewLeaderboardHandler(svc), discardLogger, noRateLimit)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// jsonBody reads the response as a JSON object.
func jsonBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\nbody: %s", err, rec.Body.String())
	}
	return body
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d; want %d\nbody: %s", rec.Code, want, rec.Body.String())
	}
}

// =============================================================================
// parseIntQuery
// =============================================================================

func TestParseIntQuery(t *testing.T) {
	tests := []struct {
		input  string
		want   int
		wantOK bool
	}{
		{"10", 10, true},
		{"0", 0, true},
		{"007", 7, true},
		{"", 0, false},
		{"abc", 0, false},
		{"-5", 0, false},
		{"1.5", 0, false},
		{"10 ", 0, false},
		// A number far too big to fit in an int is not a valid limit/offset.
		// It must be rejected, not silently turned into some other number.
		{"99999999999999999999999999", 0, false},
	}

	for _, tc := range tests {
		t.Run("input="+tc.input, func(t *testing.T) {
			got, ok := parseIntQuery(tc.input)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("parseIntQuery(%q) = %d, %v; want %d, %v", tc.input, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// =============================================================================
// Health
// =============================================================================

func TestHealth(t *testing.T) {
	rec := send(&fakeService{}, "GET", "/health", "")

	wantStatus(t, rec, http.StatusOK)
	if got := jsonBody(t, rec)["status"]; got != "ok" {
		t.Errorf(`status field = %v; want "ok"`, got)
	}
}

// =============================================================================
// POST /api/v1/leaderboards/:id/scores
// =============================================================================

func TestSubmitScore_Valid(t *testing.T) {
	svc := &fakeService{}

	rec := send(svc, "POST", "/api/v1/leaderboards/game-1/scores", `{"user_id": "alice", "score": 1500}`)

	wantStatus(t, rec, http.StatusCreated)
	if svc.leaderboardID != "game-1" || svc.userID != "alice" || svc.score != 1500 {
		t.Errorf("service received leaderboard=%q user=%q score=%v; want game-1, alice, 1500",
			svc.leaderboardID, svc.userID, svc.score)
	}
}

// Zero is a perfectly normal score (a player who just started, or a reset).
func TestSubmitScore_ZeroIsAValidScore(t *testing.T) {
	svc := &fakeService{}

	rec := send(svc, "POST", "/api/v1/leaderboards/game-1/scores", `{"user_id": "alice", "score": 0}`)

	wantStatus(t, rec, http.StatusCreated)
	if !svc.called {
		t.Error("the score was never passed to the service")
	}
}

func TestSubmitScore_BadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"broken JSON", `{"user_id": "alice", "score": `},
		{"missing user_id", `{"score": 100}`},
		{"empty user_id", `{"user_id": "", "score": 100}`},
		{"missing score", `{"user_id": "alice"}`},
		{"negative score", `{"user_id": "alice", "score": -1}`},
		{"score is text", `{"user_id": "alice", "score": "a lot"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{}

			rec := send(svc, "POST", "/api/v1/leaderboards/game-1/scores", tc.body)

			wantStatus(t, rec, http.StatusBadRequest)
			if svc.called {
				t.Error("the service was called with invalid input")
			}
			if _, ok := jsonBody(t, rec)["error"]; !ok {
				t.Error(`response has no "error" field`)
			}
		})
	}
}

// =============================================================================
// POST /api/v1/leaderboards/:id/scores/increment
// =============================================================================

func TestIncrementScore_Valid(t *testing.T) {
	svc := &fakeService{newScore: 2000}

	rec := send(svc, "POST", "/api/v1/leaderboards/game-1/scores/increment", `{"user_id": "alice", "delta": 500}`)

	wantStatus(t, rec, http.StatusOK)
	if svc.leaderboardID != "game-1" || svc.userID != "alice" || svc.delta != 500 {
		t.Errorf("service received leaderboard=%q user=%q delta=%d; want game-1, alice, 500",
			svc.leaderboardID, svc.userID, svc.delta)
	}
	body := jsonBody(t, rec)
	if body["user_id"] != "alice" || body["new_score"] != float64(2000) {
		t.Errorf("body = %v; want user_id=alice new_score=2000", body)
	}
}

func TestIncrementScore_NegativeDeltaIsAllowed(t *testing.T) {
	svc := &fakeService{newScore: 1000}

	rec := send(svc, "POST", "/api/v1/leaderboards/game-1/scores/increment", `{"user_id": "alice", "delta": -500}`)

	wantStatus(t, rec, http.StatusOK)
	if svc.delta != -500 {
		t.Errorf("service received delta=%d; want -500", svc.delta)
	}
}

func TestIncrementScore_BadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"missing user_id", `{"delta": 5}`},
		{"missing delta", `{"user_id": "alice"}`},
		{"delta with decimals", `{"user_id": "alice", "delta": 1.5}`},
		{"delta is text", `{"user_id": "alice", "delta": "five"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{}

			rec := send(svc, "POST", "/api/v1/leaderboards/game-1/scores/increment", tc.body)

			wantStatus(t, rec, http.StatusBadRequest)
			if svc.called {
				t.Error("the service was called with invalid input")
			}
		})
	}
}

// =============================================================================
// GET /api/v1/leaderboards/:id/scores
// =============================================================================

func TestGetTopScores_ReturnsTheList(t *testing.T) {
	svc := &fakeService{scores: []domain.UserScore{
		{UserID: "alice", LeaderboardID: "game-1", Score: 300, Rank: 1},
		{UserID: "bob", LeaderboardID: "game-1", Score: 200, Rank: 2},
	}}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/scores", "")

	wantStatus(t, rec, http.StatusOK)
	var got []domain.UserScore
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a JSON list: %v\nbody: %s", err, rec.Body.String())
	}
	if len(got) != 2 || got[0].UserID != "alice" || got[0].Score != 300 || got[0].Rank != 1 || got[1].UserID != "bob" {
		t.Errorf("list = %+v; want alice (300, rank 1) then bob", got)
	}
}

func TestGetTopScores_DefaultsToLimit10Offset0(t *testing.T) {
	svc := &fakeService{}

	send(svc, "GET", "/api/v1/leaderboards/game-1/scores", "")

	if svc.limit != 10 || svc.offset != 0 {
		t.Errorf("service received limit=%d offset=%d; want 10 and 0", svc.limit, svc.offset)
	}
}

func TestGetTopScores_PassesLimitAndOffset(t *testing.T) {
	svc := &fakeService{}

	send(svc, "GET", "/api/v1/leaderboards/game-1/scores?limit=25&offset=50", "")

	if svc.leaderboardID != "game-1" || svc.limit != 25 || svc.offset != 50 {
		t.Errorf("service received leaderboard=%q limit=%d offset=%d; want game-1, 25, 50",
			svc.leaderboardID, svc.limit, svc.offset)
	}
}

func TestGetTopScores_InvalidQueryParams(t *testing.T) {
	queries := []string{"limit=abc", "limit=-1", "limit=1.5", "offset=abc", "offset=-1"}

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			svc := &fakeService{}

			rec := send(svc, "GET", "/api/v1/leaderboards/game-1/scores?"+query, "")

			wantStatus(t, rec, http.StatusBadRequest)
			if svc.called {
				t.Error("the service was called with invalid input")
			}
		})
	}
}

// An empty leaderboard must answer with an empty list "[]".
// Answering "null" breaks clients that do things like scores.length or
// scores.map(...) in JavaScript.
func TestGetTopScores_EmptyLeaderboardReturnsEmptyList(t *testing.T) {
	svc := &fakeService{scores: nil} // this is what the service returns when there are no rows

	rec := send(svc, "GET", "/api/v1/leaderboards/empty-game/scores", "")

	wantStatus(t, rec, http.StatusOK)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s; want []", got)
	}
}

// =============================================================================
// GET /api/v1/leaderboards/:id/rank
// =============================================================================

func TestGetRank_Valid(t *testing.T) {
	svc := &fakeService{rank: 3}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/rank?user_id=alice", "")

	wantStatus(t, rec, http.StatusOK)
	if svc.leaderboardID != "game-1" || svc.userID != "alice" {
		t.Errorf("service received leaderboard=%q user=%q; want game-1, alice", svc.leaderboardID, svc.userID)
	}
	body := jsonBody(t, rec)
	if body["user_id"] != "alice" || body["rank"] != float64(3) {
		t.Errorf("body = %v; want user_id=alice rank=3", body)
	}
}

func TestGetRank_MissingUserID(t *testing.T) {
	svc := &fakeService{}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/rank", "")

	wantStatus(t, rec, http.StatusBadRequest)
	if svc.called {
		t.Error("the service was called without a user_id")
	}
}

func TestGetRank_UnknownUserIs404(t *testing.T) {
	svc := &fakeService{err: domain.NewNotFoundError("user not found in leaderboard", nil)}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/rank?user_id=ghost", "")

	wantStatus(t, rec, http.StatusNotFound)
	if got := jsonBody(t, rec)["error"]; got != "user not found in leaderboard" {
		t.Errorf("error message = %v; want the not-found message", got)
	}
}

// =============================================================================
// GET /api/v1/leaderboards/:id/scores/:user_id
// =============================================================================

func TestGetUserScore_Valid(t *testing.T) {
	svc := &fakeService{userScore: 1500}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/scores/alice", "")

	wantStatus(t, rec, http.StatusOK)
	if svc.leaderboardID != "game-1" || svc.userID != "alice" {
		t.Errorf("service received leaderboard=%q user=%q; want game-1, alice", svc.leaderboardID, svc.userID)
	}
	body := jsonBody(t, rec)
	if body["user_id"] != "alice" || body["leaderboard_id"] != "game-1" || body["score"] != float64(1500) {
		t.Errorf("body = %v; want user_id=alice leaderboard_id=game-1 score=1500", body)
	}
}

func TestGetUserScore_UnknownUserIs404(t *testing.T) {
	svc := &fakeService{err: domain.NewNotFoundError("score not found", nil)}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/scores/ghost", "")

	wantStatus(t, rec, http.StatusNotFound)
	if got := jsonBody(t, rec)["error"]; got != "score not found" {
		t.Errorf("error message = %v; want the not-found message", got)
	}
}

// =============================================================================
// GET /api/v1/leaderboards/:id/stats
// =============================================================================

func TestGetStats_Valid(t *testing.T) {
	svc := &fakeService{playerCount: 42}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/stats", "")

	wantStatus(t, rec, http.StatusOK)
	if svc.leaderboardID != "game-1" {
		t.Errorf("service received leaderboard=%q; want game-1", svc.leaderboardID)
	}
	body := jsonBody(t, rec)
	if body["leaderboard_id"] != "game-1" || body["player_count"] != float64(42) {
		t.Errorf("body = %v; want leaderboard_id=game-1 player_count=42", body)
	}
}

func TestGetStats_ServiceErrorIs500(t *testing.T) {
	svc := &fakeService{err: errors.New("connection refused")}

	rec := send(svc, "GET", "/api/v1/leaderboards/game-1/stats", "")

	wantStatus(t, rec, http.StatusInternalServerError)
}

// =============================================================================
// DELETE /api/v1/leaderboards/:id/scores/:user_id
// =============================================================================

func TestRemoveUserScore_Valid(t *testing.T) {
	svc := &fakeService{}

	rec := send(svc, "DELETE", "/api/v1/leaderboards/game-1/scores/alice", "")

	wantStatus(t, rec, http.StatusOK)
	if svc.leaderboardID != "game-1" || svc.userID != "alice" {
		t.Errorf("service received leaderboard=%q user=%q; want game-1, alice", svc.leaderboardID, svc.userID)
	}
}

func TestRemoveUserScore_UnknownUserIs404(t *testing.T) {
	svc := &fakeService{err: domain.NewNotFoundError("score not found", nil)}

	rec := send(svc, "DELETE", "/api/v1/leaderboards/game-1/scores/ghost", "")

	wantStatus(t, rec, http.StatusNotFound)
}

// =============================================================================
// POST /api/v1/users and POST /api/v1/leaderboards
// =============================================================================

func TestCreateUser_Valid(t *testing.T) {
	svc := &fakeService{}

	rec := send(svc, "POST", "/api/v1/users", `{"user_id": "alice", "username": "Alice"}`)

	wantStatus(t, rec, http.StatusCreated)
	if svc.userID != "alice" || svc.name != "Alice" {
		t.Errorf("service received user=%q username=%q; want alice, Alice", svc.userID, svc.name)
	}
	if got := jsonBody(t, rec)["user_id"]; got != "alice" {
		t.Errorf("user_id in response = %v; want alice", got)
	}
}

func TestCreateUser_BadRequests(t *testing.T) {
	bodies := []string{``, `{"username": "Alice"}`, `{"user_id": "alice"}`, `not json`}

	for _, body := range bodies {
		t.Run("body="+body, func(t *testing.T) {
			svc := &fakeService{}

			rec := send(svc, "POST", "/api/v1/users", body)

			wantStatus(t, rec, http.StatusBadRequest)
			if svc.called {
				t.Error("the service was called with invalid input")
			}
		})
	}
}

func TestCreateUser_ConflictIs409(t *testing.T) {
	svc := &fakeService{err: domain.NewConflictError("resource already exists", nil)}

	rec := send(svc, "POST", "/api/v1/users", `{"user_id": "alice", "username": "Alice"}`)

	wantStatus(t, rec, http.StatusConflict)
}

func TestCreateLeaderboard_Valid(t *testing.T) {
	svc := &fakeService{}

	rec := send(svc, "POST", "/api/v1/leaderboards", `{"leaderboard_id": "game-1", "name": "High Scores"}`)

	wantStatus(t, rec, http.StatusCreated)
	if svc.leaderboardID != "game-1" || svc.name != "High Scores" {
		t.Errorf("service received leaderboard=%q name=%q; want game-1, High Scores", svc.leaderboardID, svc.name)
	}
}

func TestCreateLeaderboard_BadRequests(t *testing.T) {
	bodies := []string{``, `{"name": "High Scores"}`, `{"leaderboard_id": "game-1"}`}

	for _, body := range bodies {
		t.Run("body="+body, func(t *testing.T) {
			svc := &fakeService{}

			rec := send(svc, "POST", "/api/v1/leaderboards", body)

			wantStatus(t, rec, http.StatusBadRequest)
			if svc.called {
				t.Error("the service was called with invalid input")
			}
		})
	}
}

// =============================================================================
// Errors coming from the service
// =============================================================================

// When something unexpected breaks (database down, bug...), every endpoint must
// answer 500 with a friendly message, and must NOT show internal details like
// hostnames or SQL to the client.
func TestUnexpectedServiceError_Is500WithoutInternalDetails(t *testing.T) {
	secret := errors.New("dial tcp db-internal-host:5432: password authentication failed")

	requests := []struct {
		name, method, path, body string
	}{
		{"submit score", "POST", "/api/v1/leaderboards/game-1/scores", `{"user_id": "alice", "score": 10}`},
		{"increment score", "POST", "/api/v1/leaderboards/game-1/scores/increment", `{"user_id": "alice", "delta": 5}`},
		{"top scores", "GET", "/api/v1/leaderboards/game-1/scores", ""},
		{"rank", "GET", "/api/v1/leaderboards/game-1/rank?user_id=alice", ""},
		{"remove score", "DELETE", "/api/v1/leaderboards/game-1/scores/alice", ""},
		{"create user", "POST", "/api/v1/users", `{"user_id": "alice", "username": "Alice"}`},
		{"create leaderboard", "POST", "/api/v1/leaderboards", `{"leaderboard_id": "game-1", "name": "High Scores"}`},
	}

	for _, r := range requests {
		t.Run(r.name, func(t *testing.T) {
			rec := send(&fakeService{err: secret}, r.method, r.path, r.body)

			wantStatus(t, rec, http.StatusInternalServerError)
			if msg, _ := jsonBody(t, rec)["error"].(string); msg == "" {
				t.Error(`response has no "error" message`)
			}
			if strings.Contains(rec.Body.String(), "db-internal-host") {
				t.Errorf("response leaks internal details: %s", rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Rate limiting through the real router
// =============================================================================

// recordingLimiter remembers which identifier each request was counted under.
type recordingLimiter struct {
	identifiers []string
}

func (r *recordingLimiter) CheckLimit(_ context.Context, identifier string, limit int64, _ time.Duration) (*domain.RateLimitResult, error) {
	r.identifiers = append(r.identifiers, identifier)
	return &domain.RateLimitResult{Allowed: true, Limit: limit, Remaining: limit - 1}, nil
}

// The rate limiter counts requests per client IP. Anyone can write whatever
// they want in the "X-Forwarded-For" header, so if the API believes that
// header, a client can pretend to be a new IP on every request and never be
// limited. Two requests from the same connection must count as the same client.
func TestRateLimit_CannotBeBypassedWithForwardedForHeader(t *testing.T) {
	limiter := &recordingLimiter{}
	router := SetupRouter(NewLeaderboardHandler(&fakeService{}), discardLogger, middleware.RateLimiter(limiter, 100, time.Minute))

	for _, fakeIP := range []string{"1.1.1.1", "2.2.2.2"} {
		req := httptest.NewRequest("GET", "/health", nil)
		req.RemoteAddr = "203.0.113.7:54321" // the real address of the connection
		req.Header.Set("X-Forwarded-For", fakeIP)
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	if len(limiter.identifiers) != 2 {
		t.Fatalf("rate limiter was called %d time(s); want 2", len(limiter.identifiers))
	}
	if limiter.identifiers[0] != limiter.identifiers[1] {
		t.Errorf("same client was counted as two different ones: %q and %q",
			limiter.identifiers[0], limiter.identifiers[1])
	}
}

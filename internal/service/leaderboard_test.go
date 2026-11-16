package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
)

// =============================================================================
// FAKES
// =============================================================================
// The service needs PostgreSQL and Redis. In unit tests we replace them with
// "fakes": small in-memory versions that behave like the real thing (as far as
// the service can tell) but keep the data in a Go map.
//
// Both fakes can be told to fail (set .err) so we can test what the service
// does when a database is down.

// fakePostgres pretends to be the PostgreSQL repository.
type fakePostgres struct {
	mu     sync.Mutex
	scores map[string]map[string]int64 // leaderboardID -> userID -> score
	err    error                       // if set, every method fails with it
	calls  int                         // how many times the service called us
}

// fakeRedis pretends to be the Redis repository.
type fakeRedis struct {
	mu        sync.Mutex
	scores    map[string]map[string]int64
	err       error
	calls     int
	gotLimit  int // last limit/offset the service asked GetTop for
	gotOffset int
}

func newFakePostgres() *fakePostgres {
	return &fakePostgres{scores: map[string]map[string]int64{}}
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{scores: map[string]map[string]int64{}}
}

// --- helpers shared by both fakes ---

func setScore(scores map[string]map[string]int64, leaderboardID, userID string, score int64) {
	if scores[leaderboardID] == nil {
		scores[leaderboardID] = map[string]int64{}
	}
	scores[leaderboardID][userID] = score
}

// sortedPage returns one page of a board, highest score first.
func sortedPage(board map[string]int64, leaderboardID string, limit, offset int) []domain.UserScore {
	all := make([]domain.UserScore, 0, len(board))
	for userID, score := range board {
		all = append(all, domain.UserScore{UserID: userID, LeaderboardID: leaderboardID, Score: score})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].UserID < all[j].UserID
	})
	if offset >= len(all) {
		return nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end]
}

// rankOf returns the 1-based position of a user, or 0 if they are not there.
func rankOf(board map[string]int64, userID string) int64 {
	mine, ok := board[userID]
	if !ok {
		return 0
	}
	rank := int64(1)
	for _, score := range board {
		if score > mine {
			rank++
		}
	}
	return rank
}

// --- fakePostgres methods (same behaviour as internal/repository/postgres) ---

func (f *fakePostgres) seed(leaderboardID, userID string, score int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	setScore(f.scores, leaderboardID, userID, score)
}

func (f *fakePostgres) score(leaderboardID, userID string) (int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.scores[leaderboardID][userID]
	return s, ok
}

func (f *fakePostgres) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakePostgres) UpsertScore(_ context.Context, leaderboardID, userID string, score int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	setScore(f.scores, leaderboardID, userID, score)
	return nil
}

func (f *fakePostgres) IncrementScore(_ context.Context, leaderboardID, userID string, delta int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	setScore(f.scores, leaderboardID, userID, f.scores[leaderboardID][userID]+delta)
	return f.scores[leaderboardID][userID], nil
}

func (f *fakePostgres) GetScore(_ context.Context, leaderboardID, userID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	s, ok := f.scores[leaderboardID][userID]
	if !ok {
		return 0, domain.NewNotFoundError("score not found", nil)
	}
	return s, nil
}

// GetTopScores does NOT fill the Rank field, exactly like the real SQL query.
func (f *fakePostgres) GetTopScores(_ context.Context, leaderboardID string, limit, offset int) ([]domain.UserScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return sortedPage(f.scores[leaderboardID], leaderboardID, limit, offset), nil
}

func (f *fakePostgres) GetUserRank(_ context.Context, leaderboardID, userID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	rank := rankOf(f.scores[leaderboardID], userID)
	if rank == 0 {
		return 0, domain.NewNotFoundError("user not found in leaderboard", nil)
	}
	return rank, nil
}

func (f *fakePostgres) DeleteUserScore(_ context.Context, leaderboardID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	delete(f.scores[leaderboardID], userID)
	return nil
}

func (f *fakePostgres) CountPlayers(_ context.Context, leaderboardID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	return int64(len(f.scores[leaderboardID])), nil
}

func (f *fakePostgres) CreateUser(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakePostgres) CreateLeaderboard(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakePostgres) EnsureUserAndLeaderboard(context.Context, string, string, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

// --- fakeRedis methods (same behaviour as internal/repository/redis_repo) ---

func (f *fakeRedis) seed(leaderboardID, userID string, score int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	setScore(f.scores, leaderboardID, userID, score)
}

func (f *fakeRedis) score(leaderboardID, userID string) (int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.scores[leaderboardID][userID]
	return s, ok
}

func (f *fakeRedis) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeRedis) UpdateScore(_ context.Context, leaderboardID, userID string, score int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	setScore(f.scores, leaderboardID, userID, score)
	return nil
}

// IncrementScore behaves like ZINCRBY: if the user is not in the set yet,
// Redis creates them starting from 0.
func (f *fakeRedis) IncrementScore(_ context.Context, leaderboardID, userID string, delta int64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	setScore(f.scores, leaderboardID, userID, f.scores[leaderboardID][userID]+delta)
	return float64(f.scores[leaderboardID][userID]), nil
}

// GetTop fills the Rank field, exactly like the real Redis repository.
func (f *fakeRedis) GetTop(_ context.Context, leaderboardID string, limit, offset int) ([]domain.UserScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotLimit, f.gotOffset = limit, offset
	if f.err != nil {
		return nil, f.err
	}
	page := sortedPage(f.scores[leaderboardID], leaderboardID, limit, offset)
	for i := range page {
		page[i].Rank = int64(offset + i + 1)
	}
	return page, nil
}

// GetUserRank returns rank 0 and no error when the user is not cached,
// exactly like the real Redis repository.
func (f *fakeRedis) GetUserRank(_ context.Context, leaderboardID, userID string) (int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, 0, f.err
	}
	return rankOf(f.scores[leaderboardID], userID), f.scores[leaderboardID][userID], nil
}

func (f *fakeRedis) RemoveUser(_ context.Context, leaderboardID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	delete(f.scores[leaderboardID], userID)
	return nil
}

func (f *fakeRedis) PopulateCache(_ context.Context, leaderboardID string, scores []domain.UserScore) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	for _, s := range scores {
		setScore(f.scores, leaderboardID, s.UserID, s.Score)
	}
	return nil
}

// =============================================================================
// TEST HELPERS
// =============================================================================

var (
	ctx       = context.Background()
	errDBDown = errors.New("connection refused")
	leaderID  = "game-1"
)

// newTestService builds the real service on top of the two fakes.
func newTestService(t *testing.T) (domain.LeaderboardService, *fakePostgres, *fakeRedis) {
	t.Helper()

	// The service logs a warning when Redis fails. Hide it during tests.
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	pg := newFakePostgres()
	redis := newFakeRedis()
	return NewLeaderboardService(pg, redis), pg, redis
}

// userIDs turns a list of scores into just the user IDs, to compare easily.
func userIDs(scores []domain.UserScore) []string {
	ids := make([]string, len(scores))
	for i, s := range scores {
		ids[i] = s.UserID
	}
	return ids
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// =============================================================================
// SubmitScore
// =============================================================================

func TestSubmitScore_SavesToPostgresAndRedis(t *testing.T) {
	svc, pg, redis := newTestService(t)

	if err := svc.SubmitScore(ctx, leaderID, "alice", 1500); err != nil {
		t.Fatalf("SubmitScore returned an error: %v", err)
	}

	if got, ok := pg.score(leaderID, "alice"); !ok || got != 1500 {
		t.Errorf("PostgreSQL score = %d (exists=%v); want 1500", got, ok)
	}
	if got, ok := redis.score(leaderID, "alice"); !ok || got != 1500 {
		t.Errorf("Redis score = %d (exists=%v); want 1500", got, ok)
	}
}

func TestSubmitScore_ReplacesThePreviousScore(t *testing.T) {
	svc, pg, redis := newTestService(t)

	_ = svc.SubmitScore(ctx, leaderID, "alice", 1500)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 200)

	if got, _ := pg.score(leaderID, "alice"); got != 200 {
		t.Errorf("PostgreSQL score = %d; want 200 (submit sets the score, it does not add)", got)
	}
	if got, _ := redis.score(leaderID, "alice"); got != 200 {
		t.Errorf("Redis score = %d; want 200", got)
	}
}

// The service comment says decimals are truncated (99.9 becomes 99).
func TestSubmitScore_TruncatesDecimals(t *testing.T) {
	svc, pg, _ := newTestService(t)

	_ = svc.SubmitScore(ctx, leaderID, "alice", 99.9)

	if got, _ := pg.score(leaderID, "alice"); got != 99 {
		t.Errorf("stored score = %d; want 99", got)
	}
}

// PostgreSQL is the source of truth. If it fails, nothing may be written to
// Redis, otherwise the cache would show a score that was never really saved.
func TestSubmitScore_PostgresDown_ReturnsErrorAndLeavesRedisAlone(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.err = errDBDown

	if err := svc.SubmitScore(ctx, leaderID, "alice", 1500); err == nil {
		t.Error("SubmitScore returned no error although PostgreSQL is down")
	}
	if redis.callCount() != 0 {
		t.Errorf("Redis was called %d time(s); want 0", redis.callCount())
	}
}

// The README says "Redis is only a cache" and IncrementScore already keeps
// working when Redis is down. SubmitScore should behave the same way: the score
// IS saved in PostgreSQL, so telling the client "it failed" is wrong.
func TestSubmitScore_RedisDown_StillSucceeds(t *testing.T) {
	svc, pg, redis := newTestService(t)
	redis.err = errDBDown

	err := svc.SubmitScore(ctx, leaderID, "alice", 1500)

	if got, _ := pg.score(leaderID, "alice"); got != 1500 {
		t.Fatalf("PostgreSQL score = %d; want 1500", got)
	}
	if err != nil {
		t.Errorf("SubmitScore returned %q although the score was saved in PostgreSQL", err)
	}
}

// =============================================================================
// IncrementScore
// =============================================================================

func TestIncrementScore_AddsToTheExistingScore(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 1500)
	redis.seed(leaderID, "alice", 1500)

	newScore, err := svc.IncrementScore(ctx, leaderID, "alice", 500)

	if err != nil {
		t.Fatalf("IncrementScore returned an error: %v", err)
	}
	if newScore != 2000 {
		t.Errorf("new score = %d; want 2000", newScore)
	}
	if got, _ := redis.score(leaderID, "alice"); got != 2000 {
		t.Errorf("Redis score = %d; want 2000", got)
	}
}

func TestIncrementScore_NegativeDeltaSubtracts(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 1500)
	redis.seed(leaderID, "alice", 1500)

	newScore, _ := svc.IncrementScore(ctx, leaderID, "alice", -500)

	if newScore != 1000 {
		t.Errorf("new score = %d; want 1000", newScore)
	}
}

func TestIncrementScore_NewUserStartsFromZero(t *testing.T) {
	svc, _, _ := newTestService(t)

	newScore, err := svc.IncrementScore(ctx, leaderID, "newbie", 50)

	if err != nil {
		t.Fatalf("IncrementScore returned an error: %v", err)
	}
	if newScore != 50 {
		t.Errorf("new score = %d; want 50", newScore)
	}
}

func TestIncrementScore_PostgresDown_ReturnsErrorAndLeavesRedisAlone(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.err = errDBDown

	if _, err := svc.IncrementScore(ctx, leaderID, "alice", 500); err == nil {
		t.Error("IncrementScore returned no error although PostgreSQL is down")
	}
	if redis.callCount() != 0 {
		t.Errorf("Redis was called %d time(s); want 0", redis.callCount())
	}
}

// Documented behaviour: a Redis failure is logged but the request succeeds.
func TestIncrementScore_RedisDown_StillSucceeds(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 1500)
	redis.err = errDBDown

	newScore, err := svc.IncrementScore(ctx, leaderID, "alice", 500)

	if err != nil {
		t.Errorf("IncrementScore returned %q; want no error", err)
	}
	if newScore != 2000 {
		t.Errorf("new score = %d; want 2000", newScore)
	}
}

// Scenario: Redis was restarted (or the key expired), so the cache is empty,
// but PostgreSQL still knows alice has 1500 points. She earns 500 more.
// After that, the cache must say 2000, the same as PostgreSQL.
func TestIncrementScore_EmptyCache_CacheGetsTheRealTotal(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 1500) // only in PostgreSQL, Redis is empty

	newScore, err := svc.IncrementScore(ctx, leaderID, "alice", 500)
	if err != nil {
		t.Fatalf("IncrementScore returned an error: %v", err)
	}
	if newScore != 2000 {
		t.Fatalf("new score = %d; want 2000", newScore)
	}

	if cached, ok := redis.score(leaderID, "alice"); ok && cached != 2000 {
		t.Errorf("Redis now says alice has %d points but PostgreSQL says 2000; the cache is wrong", cached)
	}
}

// =============================================================================
// GetTopScores
// =============================================================================

func TestGetTopScores_ReturnsHighestFirst(t *testing.T) {
	svc, _, _ := newTestService(t)
	_ = svc.SubmitScore(ctx, leaderID, "bob", 200)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 300)
	_ = svc.SubmitScore(ctx, leaderID, "carol", 100)

	top, err := svc.GetTopScores(ctx, leaderID, 10, 0)

	if err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}
	want := []string{"alice", "bob", "carol"}
	if got := userIDs(top); !sameStrings(got, want) {
		t.Errorf("order = %v; want %v", got, want)
	}
}

func TestGetTopScores_Pagination(t *testing.T) {
	svc, _, _ := newTestService(t)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 300)
	_ = svc.SubmitScore(ctx, leaderID, "bob", 200)
	_ = svc.SubmitScore(ctx, leaderID, "carol", 100)

	page2, err := svc.GetTopScores(ctx, leaderID, 2, 2) // 2 per page, skip the first 2

	if err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}
	want := []string{"carol"}
	if got := userIDs(page2); !sameStrings(got, want) {
		t.Errorf("page 2 = %v; want %v", got, want)
	}
}

// Cache hit: when Redis has the data we must not bother PostgreSQL at all.
func TestGetTopScores_CacheHit_DoesNotCallPostgres(t *testing.T) {
	svc, pg, redis := newTestService(t)
	redis.seed(leaderID, "alice", 300)

	if _, err := svc.GetTopScores(ctx, leaderID, 10, 0); err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}

	if pg.callCount() != 0 {
		t.Errorf("PostgreSQL was called %d time(s); want 0 on a cache hit", pg.callCount())
	}
}

// Cache miss: Redis is empty, so the data comes from PostgreSQL...
func TestGetTopScores_CacheMiss_ReadsFromPostgres(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	pg.seed(leaderID, "bob", 200)

	top, err := svc.GetTopScores(ctx, leaderID, 10, 0)

	if err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}
	want := []string{"alice", "bob"}
	if got := userIDs(top); !sameStrings(got, want) {
		t.Errorf("result = %v; want %v", got, want)
	}
}

// ...and Redis is refilled in the background so the next read is fast.
func TestGetTopScores_CacheMiss_RefillsRedis(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 300)

	_, _ = svc.GetTopScores(ctx, leaderID, 10, 0)

	// The refill runs in a goroutine, so give it a moment to finish.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got, ok := redis.score(leaderID, "alice"); ok && got == 300 {
			return // success
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("Redis was not refilled after a cache miss")
}

func TestGetTopScores_RedisDown_FallsBackToPostgres(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	redis.err = errDBDown

	top, err := svc.GetTopScores(ctx, leaderID, 10, 0)

	if err != nil {
		t.Fatalf("GetTopScores returned %q; want the PostgreSQL data instead", err)
	}
	if len(top) != 1 || top[0].UserID != "alice" {
		t.Errorf("result = %v; want just alice", userIDs(top))
	}
}

func TestGetTopScores_BothDown_ReturnsError(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.err = errDBDown
	redis.err = errDBDown

	if _, err := svc.GetTopScores(ctx, leaderID, 10, 0); err == nil {
		t.Error("GetTopScores returned no error although both databases are down")
	}
}

// The service protects the databases from silly page sizes.
func TestGetTopScores_ClampsLimitAndOffset(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		offset     int
		wantLimit  int
		wantOffset int
	}{
		{"zero limit becomes the default 10", 0, 0, 10, 0},
		{"negative limit becomes the default 10", -5, 0, 10, 0},
		{"normal limit is kept", 50, 0, 50, 0},
		{"100 is still allowed", 100, 0, 100, 0},
		{"huge limit is capped at 100", 5000, 0, 100, 0},
		{"negative offset becomes 0", 10, -3, 10, 0},
		{"normal offset is kept", 10, 20, 10, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, redis := newTestService(t)

			_, _ = svc.GetTopScores(ctx, leaderID, tc.limit, tc.offset)

			if redis.gotLimit != tc.wantLimit || redis.gotOffset != tc.wantOffset {
				t.Errorf("service asked for limit=%d offset=%d; want limit=%d offset=%d",
					redis.gotLimit, redis.gotOffset, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

// Scenario: Redis was restarted, so the cache is empty. PostgreSQL still has
// three players. Then one new player (dave) submits a score, which puts ONLY
// dave in Redis. Asking for the top 10 must still return all four players.
func TestGetTopScores_CacheHasOnlySomePlayers_StillReturnsEveryone(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	pg.seed(leaderID, "bob", 200)
	pg.seed(leaderID, "carol", 100)

	_ = svc.SubmitScore(ctx, leaderID, "dave", 50)

	top, err := svc.GetTopScores(ctx, leaderID, 10, 0)

	if err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}
	want := []string{"alice", "bob", "carol", "dave"}
	if got := userIDs(top); !sameStrings(got, want) {
		t.Errorf("top scores = %v; want %v (players that are only in PostgreSQL went missing)", got, want)
	}
}

// Scenario: after a cache miss the service refills Redis with only the page
// that was asked for (here: the top 2). A later request for the top 10 must
// still return all 5 players, not just the 2 that happen to be cached.
func TestGetTopScores_SmallPageCachedFirst_BiggerPageStillComplete(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "p1", 500)
	pg.seed(leaderID, "p2", 400)
	pg.seed(leaderID, "p3", 300)
	pg.seed(leaderID, "p4", 200)
	pg.seed(leaderID, "p5", 100)

	_, _ = svc.GetTopScores(ctx, leaderID, 2, 0) // cache miss, refills Redis with the top 2

	// Wait for the background refill to finish.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := redis.score(leaderID, "p2"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	top, err := svc.GetTopScores(ctx, leaderID, 10, 0)

	if err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}
	if len(top) != 5 {
		t.Errorf("got %d players %v; want all 5", len(top), userIDs(top))
	}
}

// The API should answer the same way whether the data came from Redis or from
// PostgreSQL. Redis results include "rank"; PostgreSQL results must too.
func TestGetTopScores_CacheMiss_ResultsIncludeRank(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	pg.seed(leaderID, "bob", 200)

	top, err := svc.GetTopScores(ctx, leaderID, 10, 0)

	if err != nil {
		t.Fatalf("GetTopScores returned an error: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("got %d players; want 2", len(top))
	}
	for i, s := range top {
		if want := int64(i + 1); s.Rank != want {
			t.Errorf("%s has rank %d; want %d", s.UserID, s.Rank, want)
		}
	}
}

// =============================================================================
// GetUserRank
// =============================================================================

func TestGetUserRank_ReturnsPosition(t *testing.T) {
	svc, _, _ := newTestService(t)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 300)
	_ = svc.SubmitScore(ctx, leaderID, "bob", 200)
	_ = svc.SubmitScore(ctx, leaderID, "carol", 100)

	tests := map[string]int64{"alice": 1, "bob": 2, "carol": 3}
	for userID, want := range tests {
		got, err := svc.GetUserRank(ctx, leaderID, userID)
		if err != nil {
			t.Errorf("GetUserRank(%s) returned an error: %v", userID, err)
			continue
		}
		if got != want {
			t.Errorf("GetUserRank(%s) = %d; want %d", userID, got, want)
		}
	}
}

func TestGetUserRank_CacheHit_DoesNotCallPostgres(t *testing.T) {
	svc, pg, redis := newTestService(t)
	redis.seed(leaderID, "alice", 300)

	if _, err := svc.GetUserRank(ctx, leaderID, "alice"); err != nil {
		t.Fatalf("GetUserRank returned an error: %v", err)
	}

	if pg.callCount() != 0 {
		t.Errorf("PostgreSQL was called %d time(s); want 0 on a cache hit", pg.callCount())
	}
}

func TestGetUserRank_CacheMiss_FallsBackToPostgres(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	pg.seed(leaderID, "bob", 200)

	rank, err := svc.GetUserRank(ctx, leaderID, "bob")

	if err != nil {
		t.Fatalf("GetUserRank returned an error: %v", err)
	}
	if rank != 2 {
		t.Errorf("rank = %d; want 2", rank)
	}
}

func TestGetUserRank_RedisDown_FallsBackToPostgres(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	redis.err = errDBDown

	rank, err := svc.GetUserRank(ctx, leaderID, "alice")

	if err != nil {
		t.Fatalf("GetUserRank returned %q; want the PostgreSQL rank instead", err)
	}
	if rank != 1 {
		t.Errorf("rank = %d; want 1", rank)
	}
}

// A user that does not exist must give a "not found" error (HTTP 404),
// not rank 0 and not a 500.
func TestGetUserRank_UnknownUser_ReturnsNotFound(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)

	_, err := svc.GetUserRank(ctx, leaderID, "ghost")

	if err == nil {
		t.Fatal("GetUserRank returned no error for a user that does not exist")
	}
	if code := domain.GetErrorCode(err); code != 404 {
		t.Errorf("error code = %d; want 404", code)
	}
}

// Same scenario as the "cache has only some players" test above: Redis was
// restarted and only dave is cached. Dave is really in 4th place.
func TestGetUserRank_CacheHasOnlySomePlayers_RankIsStillCorrect(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	pg.seed(leaderID, "bob", 200)
	pg.seed(leaderID, "carol", 100)

	_ = svc.SubmitScore(ctx, leaderID, "dave", 50)

	rank, err := svc.GetUserRank(ctx, leaderID, "dave")

	if err != nil {
		t.Fatalf("GetUserRank returned an error: %v", err)
	}
	if rank != 4 {
		t.Errorf("dave's rank = %d; want 4 (three players in PostgreSQL have more points)", rank)
	}
}

// =============================================================================
// RemoveUserScore
// =============================================================================

func TestRemoveUserScore_RemovesFromPostgresAndRedis(t *testing.T) {
	svc, pg, redis := newTestService(t)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 300)

	if err := svc.RemoveUserScore(ctx, leaderID, "alice"); err != nil {
		t.Fatalf("RemoveUserScore returned an error: %v", err)
	}

	if _, ok := pg.score(leaderID, "alice"); ok {
		t.Error("alice is still in PostgreSQL")
	}
	if _, ok := redis.score(leaderID, "alice"); ok {
		t.Error("alice is still in Redis")
	}
}

func TestRemoveUserScore_RemovedUserDisappearsFromTop(t *testing.T) {
	svc, _, _ := newTestService(t)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 300)
	_ = svc.SubmitScore(ctx, leaderID, "bob", 200)

	_ = svc.RemoveUserScore(ctx, leaderID, "alice")
	top, _ := svc.GetTopScores(ctx, leaderID, 10, 0)

	want := []string{"bob"}
	if got := userIDs(top); !sameStrings(got, want) {
		t.Errorf("top scores = %v; want %v", got, want)
	}
}

func TestRemoveUserScore_PostgresDown_ReturnsErrorAndLeavesRedisAlone(t *testing.T) {
	svc, pg, redis := newTestService(t)
	redis.seed(leaderID, "alice", 300)
	pg.err = errDBDown

	if err := svc.RemoveUserScore(ctx, leaderID, "alice"); err == nil {
		t.Error("RemoveUserScore returned no error although PostgreSQL is down")
	}
	if _, ok := redis.score(leaderID, "alice"); !ok {
		t.Error("alice was removed from Redis although the PostgreSQL delete failed")
	}
}

// =============================================================================
// GetUserScore
// =============================================================================
// The score of one player is read from PostgreSQL: it is a single-row lookup
// by primary key (fast), and PostgreSQL is always right, while Redis can be
// stale or missing players.

func TestGetUserScore_ReturnsTheScore(t *testing.T) {
	svc, _, _ := newTestService(t)
	_ = svc.SubmitScore(ctx, leaderID, "alice", 300)

	got, err := svc.GetUserScore(ctx, leaderID, "alice")

	if err != nil {
		t.Fatalf("GetUserScore returned an error: %v", err)
	}
	if got != 300 {
		t.Errorf("score = %d; want 300", got)
	}
}

func TestGetUserScore_TrustsPostgresWhenRedisDisagrees(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	redis.seed(leaderID, "alice", 999) // a stale cache entry

	got, err := svc.GetUserScore(ctx, leaderID, "alice")

	if err != nil {
		t.Fatalf("GetUserScore returned an error: %v", err)
	}
	if got != 300 {
		t.Errorf("score = %d; want 300 (the value in PostgreSQL, the source of truth)", got)
	}
}

func TestGetUserScore_UnknownUser_ReturnsNotFound(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.seed(leaderID, "alice", 300)

	_, err := svc.GetUserScore(ctx, leaderID, "ghost")

	if code := domain.GetErrorCode(err); err == nil || code != 404 {
		t.Errorf("error = %v (code %d); want a 404 error", err, code)
	}
}

func TestGetUserScore_PostgresDown_ReturnsError(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.err = errDBDown

	if _, err := svc.GetUserScore(ctx, leaderID, "alice"); err == nil {
		t.Error("GetUserScore returned no error although PostgreSQL is down")
	}
}

// =============================================================================
// CountPlayers
// =============================================================================
// Counted in PostgreSQL. Redis ZCARD would be faster, but it is only right when
// the cache holds every player, and today nothing guarantees that (Phase 1b).

func TestCountPlayers_CountsEveryPlayer(t *testing.T) {
	svc, pg, redis := newTestService(t)
	pg.seed(leaderID, "alice", 300)
	pg.seed(leaderID, "bob", 200)
	pg.seed(leaderID, "carol", 100)
	redis.seed(leaderID, "alice", 300) // the cache only knows one of them

	got, err := svc.CountPlayers(ctx, leaderID)

	if err != nil {
		t.Fatalf("CountPlayers returned an error: %v", err)
	}
	if got != 3 {
		t.Errorf("player count = %d; want 3", got)
	}
}

func TestCountPlayers_EmptyLeaderboardIsZero(t *testing.T) {
	svc, _, _ := newTestService(t)

	got, err := svc.CountPlayers(ctx, leaderID)

	if err != nil {
		t.Fatalf("CountPlayers returned an error: %v", err)
	}
	if got != 0 {
		t.Errorf("player count = %d; want 0", got)
	}
}

func TestCountPlayers_PostgresDown_ReturnsError(t *testing.T) {
	svc, pg, _ := newTestService(t)
	pg.err = errDBDown

	if _, err := svc.CountPlayers(ctx, leaderID); err == nil {
		t.Error("CountPlayers returned no error although PostgreSQL is down")
	}
}

// =============================================================================
// CreateUser / CreateLeaderboard
// =============================================================================

func TestCreateUserAndLeaderboard_PassErrorsThrough(t *testing.T) {
	svc, pg, _ := newTestService(t)

	if err := svc.CreateUser(ctx, "alice", "Alice"); err != nil {
		t.Errorf("CreateUser returned an error: %v", err)
	}
	if err := svc.CreateLeaderboard(ctx, leaderID, "High Scores"); err != nil {
		t.Errorf("CreateLeaderboard returned an error: %v", err)
	}

	pg.err = domain.NewConflictError("resource already exists", nil)

	if err := svc.CreateUser(ctx, "alice", "Alice"); domain.GetErrorCode(err) != 409 {
		t.Errorf("CreateUser error code = %d; want 409", domain.GetErrorCode(err))
	}
	if err := svc.CreateLeaderboard(ctx, leaderID, "High Scores"); domain.GetErrorCode(err) != 409 {
		t.Errorf("CreateLeaderboard error code = %d; want 409", domain.GetErrorCode(err))
	}
}

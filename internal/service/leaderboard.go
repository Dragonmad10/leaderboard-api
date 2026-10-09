package service

import (
	"context"
	"log/slog"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
)

// leaderboardService implements the LeaderboardService interface.
// It coordinates between PostgreSQL (persistent storage, source of truth)
// and Redis (cache for real-time reads).
type leaderboardService struct {
	persistentRepo domain.LeaderboardPersistentRepository // PostgreSQL - source of truth
	cacheRepo      domain.LeaderboardCacheRepository      // Redis - real-time cache
}

// NewLeaderboardService creates a new service with the given repositories.
func NewLeaderboardService(pRepo domain.LeaderboardPersistentRepository, cRepo domain.LeaderboardCacheRepository) domain.LeaderboardService {
	return &leaderboardService{
		persistentRepo: pRepo,
		cacheRepo:      cRepo,
	}
}

// SubmitScore handles coordinated write to both PostgreSQL and Redis.
// Converts float64 score to int64 (truncating decimals).
// Auto-creates user and leaderboard if they don't exist (idempotent).
// Write path: PostgreSQL (source of truth) -> Redis (cache update)
func (s *leaderboardService) SubmitScore(ctx context.Context, leaderboardID string, userID string, score float64) error {
	intScore := int64(score)

	// 1. Ensure user and leaderboard exist (idempotent operation)
	if err := s.persistentRepo.EnsureUserAndLeaderboard(ctx, leaderboardID, userID, userID, leaderboardID); err != nil {
		return err
	}

	// 2. Write to PostgreSQL (Source of Truth)
	if err := s.persistentRepo.UpsertScore(ctx, leaderboardID, userID, intScore); err != nil {
		return err
	}

	// 3. Update Redis cache in real-time
	return s.cacheRepo.UpdateScore(ctx, leaderboardID, userID, intScore)
}

// IncrementScore atomically increments score in both PostgreSQL and Redis.
// Auto-creates user and leaderboard if they don't exist.
// Uses PostgreSQL's atomic UPSERT with RETURNING for the new score.
// Redis error is logged but doesn't fail the request (PostgreSQL already committed).
func (s *leaderboardService) IncrementScore(ctx context.Context, leaderboardID string, userID string, delta int64) (int64, error) {
	// 1. Ensure user and leaderboard exist
	if err := s.persistentRepo.EnsureUserAndLeaderboard(ctx, leaderboardID, userID, userID, leaderboardID); err != nil {
		return 0, err
	}

	// 2. Atomically increment in PostgreSQL and get new score
	newScore, err := s.persistentRepo.IncrementScore(ctx, leaderboardID, userID, delta)
	if err != nil {
		return 0, err
	}

	// 3. Increment/sync in Redis cache
	// Log cache error but don't abort - PostgreSQL is the source of truth
	_, err = s.cacheRepo.IncrementScore(ctx, leaderboardID, userID, delta)
	if err != nil {
		slog.Warn("Failed to increment score in Redis cache",
			"leaderboardID", leaderboardID,
			"userID", userID,
			"error", err)
	}

	return newScore, nil
}

// GetTopScores implements the Cache-Aside pattern with pagination support.
// Read path: Redis cache (fast) -> PostgreSQL fallback (source of truth)
// On cache miss: fetches from PostgreSQL, then asynchronously repopulates Redis cache (first page only).
func (s *leaderboardService) GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	// Validate and clamp parameters
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// 1. Try to get from Redis cache (fast path)
	top, err := s.cacheRepo.GetTop(ctx, leaderboardID, limit, offset)
	if err == nil && len(top) > 0 {
		return top, nil
	}

	// 2. Cache miss: fetch from PostgreSQL (source of truth)
	top, err = s.persistentRepo.GetTopScores(ctx, leaderboardID, limit, offset)
	if err != nil {
		return nil, err
	}

	// 3. Asynchronously repopulate Redis cache (only for first page / offset=0)
	// Subsequent pages don't need full cache repopulation
	if len(top) > 0 && offset == 0 {
		go func() {
			_ = s.cacheRepo.PopulateCache(context.Background(), leaderboardID, top)
		}()
	}

	return top, nil
}

// GetUserRank retrieves a user's rank using Cache-Aside pattern with PostgreSQL fallback.
// Read path: Redis (ZREVRANK + ZSCORE via pipelining) -> PostgreSQL (Window Function)
func (s *leaderboardService) GetUserRank(ctx context.Context, leaderboardID string, userID string) (int64, error) {
	// 1. Try to get from Redis cache
	rank, _, err := s.cacheRepo.GetUserRank(ctx, leaderboardID, userID)
	if err == nil && rank > 0 {
		return rank, nil
	}

	// 2. Cache miss: get from PostgreSQL using Window Function
	rank, err = s.persistentRepo.GetUserRank(ctx, leaderboardID, userID)
	if err != nil {
		return 0, err
	}

	return rank, nil
}

// RemoveUserScore deletes a user's score from both PostgreSQL and Redis.
// Delete path: PostgreSQL (source of truth) -> Redis (cache invalidation)
func (s *leaderboardService) RemoveUserScore(ctx context.Context, leaderboardID string, userID string) error {
	// 1. Delete from PostgreSQL (Source of Truth)
	if err := s.persistentRepo.DeleteUserScore(ctx, leaderboardID, userID); err != nil {
		return err
	}

	// 2. Delete from Redis cache
	return s.cacheRepo.RemoveUser(ctx, leaderboardID, userID)
}

// CreateUser explicitly creates a user in PostgreSQL.
func (s *leaderboardService) CreateUser(ctx context.Context, userID, username string) error {
	return s.persistentRepo.CreateUser(ctx, userID, username)
}

// CreateLeaderboard explicitly creates a leaderboard in PostgreSQL.
func (s *leaderboardService) CreateLeaderboard(ctx context.Context, leaderboardID, name string) error {
	return s.persistentRepo.CreateLeaderboard(ctx, leaderboardID, name)
}
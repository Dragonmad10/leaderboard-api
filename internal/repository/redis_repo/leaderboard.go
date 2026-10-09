package redis_repo

import (
	"context"
	"fmt"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/redis/go-redis/v9"
)

// leaderboardRepository implements LeaderboardCacheRepository using Redis Sorted Sets (ZSET).
// Provides O(log N) operations for leaderboard reads/writes:
//   - ZADD: Insert/update score
//   - ZINCRBY: Atomic increment
//   - ZREVRANGE: Top N scores with pagination
//   - ZREVRANK + ZSCORE: User rank and score (pipelined)
type leaderboardRepository struct {
	client *redis.Client
}

func NewLeaderboardRepository(client *redis.Client) domain.LeaderboardCacheRepository {
	return &leaderboardRepository{client: client}
}

// UpdateScore sets an absolute score for a user in the Sorted Set (ZSET).
// Uses ZADD which inserts or updates the member's score.
func (r *leaderboardRepository) UpdateScore(ctx context.Context, leaderboardID, userID string, score int64) error {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZAdd(ctx, key, redis.Z{
		Score:  float64(score),
		Member: userID,
	}).Err()
}

// IncrementScore atomically increments/decrements a user's score (ZINCRBY).
// Returns the new score as float64 (Redis ZINCRBY return type).
func (r *leaderboardRepository) IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (float64, error) {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZIncrBy(ctx, key, float64(delta), userID).Result()
}

// GetTop returns top N scores with pagination using ZREVRANGE (highest to lowest).
// Supports offset for pagination (page 2, page 3, etc.).
// Rank is calculated as offset + position (1-based).
func (r *leaderboardRepository) GetTop(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	if limit <= 0 {
		return []domain.UserScore{}, nil
	}

	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	// ZRevRangeWithScores uses inclusive indices: start to end
	start := int64(offset)
	end := int64(offset + limit - 1)
	zMembers, err := r.client.ZRevRangeWithScores(ctx, key, start, end).Result()
	if err != nil {
		return nil, err
	}

	userScores := make([]domain.UserScore, len(zMembers))
	for i, z := range zMembers {
		userID, ok := z.Member.(string)
		if !ok {
			userID = fmt.Sprintf("%v", z.Member)
		}

		// Rank = offset + i + 1 (1-based ranking)
		userScores[i] = domain.UserScore{
			UserID:        userID,
			LeaderboardID: leaderboardID,
			Score:         int64(z.Score),
			Rank:          int64(offset + i + 1),
		}
	}

	return userScores, nil
}

// GetUserRank returns user's rank (1-based) and score using pipelined ZREVRANK + ZSCORE.
// Pipelining reduces round-trips by sending both commands in a single request.
func (r *leaderboardRepository) GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, int64, error) {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)

	pipe := r.client.Pipeline()
	rankCmd := pipe.ZRevRank(ctx, key, userID)
	scoreCmd := pipe.ZScore(ctx, key, userID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		if err == redis.Nil {
			return 0, 0, nil // User not found in leaderboard
		}
		return 0, 0, err
	}

	rank, err := rankCmd.Result()
	if err != nil {
		if err == redis.Nil {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	score, err := scoreCmd.Result()
	if err != nil {
		return 0, 0, err
	}

	// Redis ZRevRank is 0-indexed, add +1 for 1-based rank
	return rank + 1, int64(score), nil
}

// RemoveUser removes a user from the leaderboard (ZREM).
func (r *leaderboardRepository) RemoveUser(ctx context.Context, leaderboardID, userID string) error {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZRem(ctx, key, userID).Err()
}

// PopulateCache bulk inserts multiple scores for cache warming (ZADD batch).
// Used to repopulate Redis cache from PostgreSQL on cache miss (first page only).
func (r *leaderboardRepository) PopulateCache(ctx context.Context, leaderboardID string, scores []domain.UserScore) error {
	if len(scores) == 0 {
		return nil
	}

	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	zMembers := make([]redis.Z, len(scores))

	for i, s := range scores {
		zMembers[i] = redis.Z{
			Score:  float64(s.Score),
			Member: s.UserID,
		}
	}

	// Insert all records in a single Redis call
	return r.client.ZAdd(ctx, key, zMembers...).Err()
}

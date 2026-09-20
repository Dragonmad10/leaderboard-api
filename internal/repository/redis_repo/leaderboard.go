package redis_repo

import (
	"context"
	"fmt"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"

	"github.com/redis/go-redis/v9"
)

type leaderboardRepository struct {
	client *redis.Client
}

func NewLeaderboardRepository(client *redis.Client) domain.LeaderboardRepository {
	return &leaderboardRepository{client: client}
}

func (r *leaderboardRepository) AddScore(ctx context.Context, leaderboardID, userID string, score float64) error {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: userID,
	}).Err()
}

func (r *leaderboardRepository) GetTop(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	zMembers, err := r.client.ZRevRangeWithScores(ctx, key, int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, err
	}

	userScores := make([]domain.UserScore, len(zMembers))
	for i, z := range zMembers {
		userScores[i] = domain.UserScore{
			UserID: z.Member.(string),
			Score:  z.Score,
			Rank:   int64(i + offset + 1),
		}
	}
	return userScores, nil
}

func (r *leaderboardRepository) GetRank(ctx context.Context, leaderboardID string, userID string) (int64, error) {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	rank, err := r.client.ZRevRank(ctx, key, userID).Result()
	if err != nil {
		return 0, err
	}
	return rank + 1, nil
}

func (r *leaderboardRepository) RemoveUser(ctx context.Context, leaderboardID string, userID string) error {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZRem(ctx, key, userID).Err()
}

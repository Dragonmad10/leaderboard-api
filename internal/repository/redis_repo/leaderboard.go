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

func NewLeaderboardRepository(client *redis.Client) domain.LeaderboardCacheRepository {
	return &leaderboardRepository{client: client}
}

// UpdateScore fija una puntuación absoluta para un usuario en el Sorted Set (ZSET)
func (r *leaderboardRepository) UpdateScore(ctx context.Context, leaderboardID, userID string, score int64) error {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZAdd(ctx, key, redis.Z{
		Score:  float64(score),
		Member: userID,
	}).Err()
}

// IncrementScore incrementa/decrementa atómicamente la puntuación de un usuario (ZINCRBY)
func (r *leaderboardRepository) IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (float64, error) {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZIncrBy(ctx, key, float64(delta), userID).Result()
}

// GetTop obtiene los N mejores jugadores ordenados de mayor a menor (ZREVRANGE)
// Soporta offset para paginación
func (r *leaderboardRepository) GetTop(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	if limit <= 0 {
		return []domain.UserScore{}, nil
	}

	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	// ZRevRangeWithScores usa índices inclusivos: offset a offset+limit-1
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

		// Rank real = offset + i + 1 (1-based)
		userScores[i] = domain.UserScore{
			UserID:        userID,
			LeaderboardID: leaderboardID,
			Score:         int64(z.Score),
			Rank:          int64(offset + i + 1),
		}
	}

	return userScores, nil
}

// GetUserRank obtiene la posición global (1-based) y la puntuación actual de un usuario.
// Utiliza Pipelining para empaquetar ZREVRANK y ZSCORE en una sola petición a Redis.
func (r *leaderboardRepository) GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, int64, error) {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)

	pipe := r.client.Pipeline()
	rankCmd := pipe.ZRevRank(ctx, key, userID)
	scoreCmd := pipe.ZScore(ctx, key, userID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		if err == redis.Nil {
			return 0, 0, nil // Usuario no encontrado en el leaderboard
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

	// Redis ZRevRank es 0-indexed, se le suma +1 para obtener la posición real
	return rank + 1, int64(score), nil
}

// RemoveUser elimina a un usuario del leaderboard (ZREM)
func (r *leaderboardRepository) RemoveUser(ctx context.Context, leaderboardID, userID string) error {
	key := fmt.Sprintf("leaderboard:%s", leaderboardID)
	return r.client.ZRem(ctx, key, userID).Err()
}

// PopulateCache inserta múltiples puntuaciones en lote (Bulk Upsert / Warmup)
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

	// Inserta todos los registros en una sola llamada a Redis
	return r.client.ZAdd(ctx, key, zMembers...).Err()
}

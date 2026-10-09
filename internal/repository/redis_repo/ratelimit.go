package redis_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// slidingWindowLuaScript implements a sliding window rate limiter using Redis Sorted Sets.
// Algorithm:
// 1. Remove expired entries (older than window)
// 2. Count current requests in window
// 3. If under limit: add current request with timestamp as score, set TTL, return allowed
// 4. If over limit: return rejected with reset time
const slidingWindowLuaScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member_id = ARGV[4]

local clear_before = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', clear_before)

local current_requests = redis.call('ZCARD', key)

if current_requests < limit then
    redis.call('ZADD', key, now, member_id)
    redis.call('PEXPIRE', key, window)
    return {1, limit - current_requests - 1, math.ceil(window / 1000)}
else
    return {0, 0, math.ceil(window / 1000)}
end
`

// rateLimiterRepo implements RateLimiterRepository using Redis + Lua script.
type rateLimiterRepo struct {
	client *redis.Client
	script *redis.Script
}

func NewRateLimiterRepository(client *redis.Client) domain.RateLimiterRepository {
	return &rateLimiterRepo{
		client: client,
		script: redis.NewScript(slidingWindowLuaScript),
	}
}

// Allow checks if a request is allowed under the sliding window rate limit.
// Uses a Lua script for atomic check-and-increment operation.
// Key format: "ratelimit:{identifier}" (e.g., "ratelimit:192.168.1.1")
// Member format: "{timestamp}:{uuid}" for uniqueness within same millisecond.
func (r *rateLimiterRepo) Allow(ctx context.Context, key string, limit int64, window time.Duration) (*domain.RateLimitResult, error) {
	now := time.Now().UnixNano() / int64(time.Millisecond)  // Current time in milliseconds
	windowMs := window.Milliseconds()
	memberID := fmt.Sprintf("%d:%s", now, uuid.New().String()) // Unique member per request

	formattedKey := fmt.Sprintf("ratelimit:%s", key)

	res, err := r.script.Run(ctx, r.client, []string{formattedKey}, now, windowMs, limit, memberID).Result()
	if err != nil {
		return nil, fmt.Errorf("error executing rate limit lua script: %w", err)
	}

	results, ok := res.([]interface{})
	if !ok || len(results) < 3 {
		return nil, fmt.Errorf("unexpected lua script response")
	}

	allowed := results[0].(int64) == 1
	remaining := results[1].(int64)
	resetInSeconds := results[2].(int64)

	return &domain.RateLimitResult{
		Allowed:   allowed,
		Limit:     limit,
		Remaining: remaining,
		ResetIn:   time.Duration(resetInSeconds) * time.Second,
	}, nil
}

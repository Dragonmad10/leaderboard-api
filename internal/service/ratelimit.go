package service

import (
	"context"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
)

// rateLimiterService implements the RateLimiterService interface.
// Delegates to the repository for sliding window rate limiting logic.
type rateLimiterService struct {
	repo domain.RateLimiterRepository
}

func NewRateLimiterService(repo domain.RateLimiterRepository) domain.RateLimiterService {
	return &rateLimiterService{repo: repo}
}

// CheckLimit checks if a request is allowed under the rate limit.
// Uses sliding window algorithm implemented in Redis via Lua script.
func (s *rateLimiterService) CheckLimit(ctx context.Context, identifier string, limit int64, window time.Duration) (*domain.RateLimitResult, error) {
	return s.repo.Allow(ctx, identifier, limit, window)
}

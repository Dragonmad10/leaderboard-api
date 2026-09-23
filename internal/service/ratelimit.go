package service

import (
	"context"
	"time"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
)

type rateLimiterService struct {
	repo domain.RateLimiterRepository
}

func NewRateLimiterService(repo domain.RateLimiterRepository) domain.RateLimiterService {
	return &rateLimiterService{repo: repo}
}

func (s *rateLimiterService) CheckLimit(ctx context.Context, identifier string, limit int64, window time.Duration) (*domain.RateLimitResult, error) {
	return s.repo.Allow(ctx, identifier, limit, window)
}

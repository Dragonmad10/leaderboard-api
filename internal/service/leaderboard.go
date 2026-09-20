package service

import (
	"context"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
)

type leaderboardService struct {
	repo domain.LeaderboardRepository
}

// NewLeaderboardService es el constructor (Inyección de dependencias)
func NewLeaderboardService(repo domain.LeaderboardRepository) domain.LeaderboardService {
	return &leaderboardService{repo: repo}
}

// Ejemplo de implementación de un caso de uso
func (s *leaderboardService) SubmitScore(ctx context.Context, leaderboardID, userID string, score float64) error {
	// Aquí podrías agregar validaciones de negocio (ej. score no puede ser negativo)
	return s.repo.AddScore(ctx, leaderboardID, userID, score)
}

func (s *leaderboardService) GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	return s.repo.GetTop(ctx, leaderboardID, limit, offset)
}

func (s *leaderboardService) GetUserRank(ctx context.Context, leaderboardID string, userID string) (int64, error) {
	return s.repo.GetRank(ctx, leaderboardID, userID)
}

func (s *leaderboardService) RemoveUserScore(ctx context.Context, leaderboardID string, userID string) error {
	return s.repo.RemoveUser(ctx, leaderboardID, userID)
}

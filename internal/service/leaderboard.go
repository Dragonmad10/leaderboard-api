package service

import (
	"context"
	"log/slog"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
)

type leaderboardService struct {
	persistentRepo domain.LeaderboardPersistentRepository
	cacheRepo      domain.LeaderboardCacheRepository
}

func NewLeaderboardService(pRepo domain.LeaderboardPersistentRepository, cRepo domain.LeaderboardCacheRepository) domain.LeaderboardService {
	return &leaderboardService{
		persistentRepo: pRepo,
		cacheRepo:      cRepo,
	}
}

// SubmitScore maneja la escritura coordinada (PostgreSQL + Redis)
// Convierte float64 a int64 (truncando decimales)
// Auto-crea usuario y leaderboard si no existen
func (s *leaderboardService) SubmitScore(ctx context.Context, leaderboardID string, userID string, score float64) error {
	intScore := int64(score)

	// 1. Asegurar que usuario y leaderboard existan (idempotente)
	if err := s.persistentRepo.EnsureUserAndLeaderboard(ctx, leaderboardID, userID, userID, leaderboardID); err != nil {
		return err
	}

	// 2. Guardar en PostgreSQL (Fuente de la Verdad)
	if err := s.persistentRepo.UpsertScore(ctx, leaderboardID, userID, intScore); err != nil {
		return err
	}

	// 3. Actualizar la caché de Redis en tiempo real
	return s.cacheRepo.UpdateScore(ctx, leaderboardID, userID, intScore)
}

// IncrementScore suma la puntuación de forma atómica en ambas BDDs
// Auto-crea usuario y leaderboard si no existen
func (s *leaderboardService) IncrementScore(ctx context.Context, leaderboardID string, userID string, delta int64) (int64, error) {
	// 1. Asegurar que usuario y leaderboard existan
	if err := s.persistentRepo.EnsureUserAndLeaderboard(ctx, leaderboardID, userID, userID, leaderboardID); err != nil {
		return 0, err
	}

	// 2. Incrementar en Postgres (atómico, retorna nuevo score)
	newScore, err := s.persistentRepo.IncrementScore(ctx, leaderboardID, userID, delta)
	if err != nil {
		return 0, err
	}

	// 3. Incrementar/Sincronizar en Redis
	// Loguear error de caché, pero no abortar si la BDD relacional ya guardó la verdad
	_, err = s.cacheRepo.IncrementScore(ctx, leaderboardID, userID, delta)
	if err != nil {
		slog.Warn("Failed to increment score in Redis cache",
			"leaderboardID", leaderboardID,
			"userID", userID,
			"error", err)
	}

	return newScore, nil
}

// GetTopScores implementa el patrón Cache-Fallback con soporte de paginación (offset)
func (s *leaderboardService) GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	// Validar parámetros
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// 1. Intentar obtener desde la caché de Redis (Rápido)
	top, err := s.cacheRepo.GetTop(ctx, leaderboardID, limit, offset)
	if err == nil && len(top) > 0 {
		return top, nil
	}

	// 2. Cache Miss: Obtener desde PostgreSQL
	top, err = s.persistentRepo.GetTopScores(ctx, leaderboardID, limit, offset)
	if err != nil {
		return nil, err
	}

	// 3. Reconstruir la caché de Redis en segundo plano (solo para offset=0, primera página)
	// Para páginas posteriores no tiene sentido repoblar toda la caché
	if len(top) > 0 && offset == 0 {
		go func() {
			_ = s.cacheRepo.PopulateCache(context.Background(), leaderboardID, top)
		}()
	}

	return top, nil
}

// GetUserRank obtiene el rank de un usuario (Cache-Aside con fallback a Postgres)
func (s *leaderboardService) GetUserRank(ctx context.Context, leaderboardID string, userID string) (int64, error) {
	// 1. Intentar obtener desde Redis
	rank, _, err := s.cacheRepo.GetUserRank(ctx, leaderboardID, userID)
	if err == nil && rank > 0 {
		return rank, nil
	}

	// 2. Cache Miss: Obtener desde PostgreSQL usando Window Function
	rank, err = s.persistentRepo.GetUserRank(ctx, leaderboardID, userID)
	if err != nil {
		return 0, err
	}

	return rank, nil
}

// RemoveUserScore elimina la puntuación de un usuario en ambas BDDs
func (s *leaderboardService) RemoveUserScore(ctx context.Context, leaderboardID string, userID string) error {
	// 1. Eliminar en PostgreSQL (Fuente de la Verdad)
	if err := s.persistentRepo.DeleteUserScore(ctx, leaderboardID, userID); err != nil {
		return err
	}

	// 2. Eliminar en Redis
	return s.cacheRepo.RemoveUser(ctx, leaderboardID, userID)
}

// CreateUser crea un usuario explícitamente
func (s *leaderboardService) CreateUser(ctx context.Context, userID, username string) error {
	return s.persistentRepo.CreateUser(ctx, userID, username)
}

// CreateLeaderboard crea un leaderboard explícitamente
func (s *leaderboardService) CreateLeaderboard(ctx context.Context, leaderboardID, name string) error {
	return s.persistentRepo.CreateLeaderboard(ctx, leaderboardID, name)
}
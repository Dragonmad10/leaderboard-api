package postgres

import (
	"context"
	"errors"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) domain.LeaderboardPersistentRepository {
	return &PostgresRepository{pool: pool}
}

// UpsertScore inserta o actualiza la puntuación fijando un valor absoluto
func (r *PostgresRepository) UpsertScore(ctx context.Context, leaderboardID, userID string, score int64) error {
	query := `
		INSERT INTO scores (leaderboard_id, user_id, score, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (leaderboard_id, user_id) 
		DO UPDATE SET score = EXCLUDED.score, updated_at = NOW();
	`
	_, err := r.pool.Exec(ctx, query, leaderboardID, userID, score)
	return r.wrapError(err, "user or leaderboard not found")
}

// IncrementScore incrementa atómicamente la puntuación en PostgreSQL
func (r *PostgresRepository) IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (int64, error) {
	query := `
		INSERT INTO scores (leaderboard_id, user_id, score, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (leaderboard_id, user_id) 
		DO UPDATE SET score = scores.score + EXCLUDED.score, updated_at = NOW()
		RETURNING score;
	`
	var newScore int64
	err := r.pool.QueryRow(ctx, query, leaderboardID, userID, delta).Scan(&newScore)
	return newScore, r.wrapError(err, "user or leaderboard not found")
}

// GetScore obtiene la puntuación actual de un usuario
func (r *PostgresRepository) GetScore(ctx context.Context, leaderboardID, userID string) (int64, error) {
	query := `SELECT score FROM scores WHERE leaderboard_id = $1 AND user_id = $2`
	var score int64
	err := r.pool.QueryRow(ctx, query, leaderboardID, userID).Scan(&score)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.NewNotFoundError("score not found", err)
		}
		return 0, domain.NewInternalError("failed to get score", err)
	}
	return score, nil
}

// DeleteUserScore elimina la puntuación de un usuario
func (r *PostgresRepository) DeleteUserScore(ctx context.Context, leaderboardID, userID string) error {
	query := `DELETE FROM scores WHERE leaderboard_id = $1 AND user_id = $2`
	_, err := r.pool.Exec(ctx, query, leaderboardID, userID)
	return err
}

// CountPlayers cuenta jugadores en un leaderboard
func (r *PostgresRepository) CountPlayers(ctx context.Context, leaderboardID string) (int64, error) {
	query := `SELECT COUNT(*) FROM scores WHERE leaderboard_id = $1`
	var count int64
	err := r.pool.QueryRow(ctx, query, leaderboardID).Scan(&count)
	return count, err
}

// GetUserRank obtiene el rank de un usuario usando Window Function (fallback cuando Redis falla)
func (r *PostgresRepository) GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, error) {
	query := `
		SELECT rank FROM (
			SELECT user_id, RANK() OVER (ORDER BY score DESC) as rank
			FROM scores
			WHERE leaderboard_id = $1
		) ranked
		WHERE user_id = $2
	`
	var rank int64
	err := r.pool.QueryRow(ctx, query, leaderboardID, userID).Scan(&rank)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.NewNotFoundError("user not found in leaderboard", err)
		}
		return 0, domain.NewInternalError("failed to get rank", err)
	}
	return rank, nil
}

func (r *PostgresRepository) GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]domain.UserScore, error) {
	query := `
		SELECT user_id, leaderboard_id, score 
		FROM scores 
		WHERE leaderboard_id = $1 
		ORDER BY score DESC 
		LIMIT $2 OFFSET $3;
	`
	rows, err := r.pool.Query(ctx, query, leaderboardID, limit, offset)
	if err != nil {
		return nil, domain.NewInternalError("failed to query top scores", err)
	}
	defer rows.Close()

	var result []domain.UserScore
	for rows.Next() {
		var s domain.UserScore
		if err := rows.Scan(&s.UserID, &s.LeaderboardID, &s.Score); err != nil {
			return nil, domain.NewInternalError("failed to scan score", err)
		}
		result = append(result, s)
	}
	return result, nil
}

// CreateUser crea un usuario si no existe (idempotente)
func (r *PostgresRepository) CreateUser(ctx context.Context, userID, username string) error {
	query := `
		INSERT INTO users (id, username) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err := r.pool.Exec(ctx, query, userID, username)
	return r.wrapError(err, "failed to create user")
}

// CreateLeaderboard crea un leaderboard si no existe (idempotente)
func (r *PostgresRepository) CreateLeaderboard(ctx context.Context, leaderboardID, name string) error {
	query := `
		INSERT INTO leaderboards (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err := r.pool.Exec(ctx, query, leaderboardID, name)
	return r.wrapError(err, "failed to create leaderboard")
}

// EnsureUserAndLeaderboard crea usuario y leaderboard si no existen
func (r *PostgresRepository) EnsureUserAndLeaderboard(ctx context.Context, leaderboardID, userID, username, leaderboardName string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.NewInternalError("failed to start transaction", err)
	}
	defer tx.Rollback(ctx)

	// Crear usuario
	_, err = tx.Exec(ctx, `INSERT INTO users (id, username) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, userID, username)
	if err != nil {
		return domain.NewInternalError("failed to create user", err)
	}

	// Crear leaderboard
	_, err = tx.Exec(ctx, `INSERT INTO leaderboards (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, leaderboardID, leaderboardName)
	if err != nil {
		return domain.NewInternalError("failed to create leaderboard", err)
	}

	return tx.Commit(ctx)
}

// wrapError convierte errores de PostgreSQL a errores de dominio
func (r *PostgresRepository) wrapError(err error, notFoundMsg string) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Foreign key violation (código 23503)
		if pgErr.Code == "23503" {
			return domain.NewNotFoundError(notFoundMsg, err)
		}
		// Unique violation (código 23505)
		if pgErr.Code == "23505" {
			return domain.NewConflictError("resource already exists", err)
		}
	}

	// Error de conexión u otro
	return domain.NewInternalError("database error", err)
}
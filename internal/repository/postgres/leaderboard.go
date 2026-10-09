package postgres

import (
	"context"
	"errors"

	"github.com/Dragonmad10/leaderboard-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements LeaderboardPersistentRepository using PostgreSQL.
// It is the "Source of Truth" for all persistent leaderboard data.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) domain.LeaderboardPersistentRepository {
	return &PostgresRepository{pool: pool}
}

// UpsertScore inserts or updates a user's absolute score in a leaderboard.
// Uses PostgreSQL ON CONFLICT for atomic upsert.
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

// IncrementScore atomically increments a user's score and returns the new value.
// Uses PostgreSQL's UPSERT with RETURNING clause for atomic read-modify-write.
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

// GetScore retrieves a user's current score in a leaderboard.
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

// DeleteUserScore removes a user's score from a leaderboard.
func (r *PostgresRepository) DeleteUserScore(ctx context.Context, leaderboardID, userID string) error {
	query := `DELETE FROM scores WHERE leaderboard_id = $1 AND user_id = $2`
	_, err := r.pool.Exec(ctx, query, leaderboardID, userID)
	return err
}

// CountPlayers returns the total number of players in a leaderboard.
func (r *PostgresRepository) CountPlayers(ctx context.Context, leaderboardID string) (int64, error) {
	query := `SELECT COUNT(*) FROM scores WHERE leaderboard_id = $1`
	var count int64
	err := r.pool.QueryRow(ctx, query, leaderboardID).Scan(&count)
	return count, err
}

// GetUserRank returns a user's rank using PostgreSQL Window Function (RANK() OVER).
// Used as fallback when Redis cache is unavailable.
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

// GetTopScores returns paginated top scores ordered by score descending.
// Uses index idx_scores_leaderboard_score for efficient sorting.
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

// CreateUser creates a user if not exists (idempotent).
// Uses ON CONFLICT DO NOTHING to avoid errors on duplicate IDs.
func (r *PostgresRepository) CreateUser(ctx context.Context, userID, username string) error {
	query := `
		INSERT INTO users (id, username) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err := r.pool.Exec(ctx, query, userID, username)
	return r.wrapError(err, "failed to create user")
}

// CreateLeaderboard creates a leaderboard if not exists (idempotent).
func (r *PostgresRepository) CreateLeaderboard(ctx context.Context, leaderboardID, name string) error {
	query := `
		INSERT INTO leaderboards (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err := r.pool.Exec(ctx, query, leaderboardID, name)
	return r.wrapError(err, "failed to create leaderboard")
}

// EnsureUserAndLeaderboard creates both user and leaderboard in a single transaction.
// Idempotent - uses ON CONFLICT DO NOTHING for both inserts.
func (r *PostgresRepository) EnsureUserAndLeaderboard(ctx context.Context, leaderboardID, userID, username, leaderboardName string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.NewInternalError("failed to start transaction", err)
	}
	defer tx.Rollback(ctx)

	// Create user (idempotent)
	_, err = tx.Exec(ctx, `INSERT INTO users (id, username) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, userID, username)
	if err != nil {
		return domain.NewInternalError("failed to create user", err)
	}

	// Create leaderboard (idempotent)
	_, err = tx.Exec(ctx, `INSERT INTO leaderboards (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, leaderboardID, leaderboardName)
	if err != nil {
		return domain.NewInternalError("failed to create leaderboard", err)
	}

	return tx.Commit(ctx)
}

// wrapError converts PostgreSQL errors to domain errors.
// Maps foreign key violations (23503) to NotFound, unique violations (23505) to Conflict.
func (r *PostgresRepository) wrapError(err error, notFoundMsg string) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Foreign key violation (code 23503) - referenced user/leaderboard doesn't exist
		if pgErr.Code == "23503" {
			return domain.NewNotFoundError(notFoundMsg, err)
		}
		// Unique violation (code 23505) - duplicate key
		if pgErr.Code == "23505" {
			return domain.NewConflictError("resource already exists", err)
		}
	}

	// Connection error or other
	return domain.NewInternalError("database error", err)
}
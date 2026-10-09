package domain

import "context"

// UserScore represents a user's score entry in a leaderboard.
type UserScore struct {
	UserID        string `json:"user_id"`
	LeaderboardID string `json:"leaderboard_id"`
	Score         int64  `json:"score"`
	Rank          int64  `json:"rank,omitempty"` // Omitted when not applicable (e.g., submit score)
}

// LeaderboardPersistentRepository defines operations for PostgreSQL (Source of Truth).
// All mutations must go through PostgreSQL first for data integrity.
type LeaderboardPersistentRepository interface {
	// UpsertScore inserts or updates a user's absolute score.
	UpsertScore(ctx context.Context, leaderboardID, userID string, score int64) error

	// IncrementScore atomically increments a user's score and returns the new value.
	IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (int64, error)

	// GetScore retrieves a user's current score.
	GetScore(ctx context.Context, leaderboardID, userID string) (int64, error)

	// GetTopScores returns paginated top scores ordered by score descending.
	GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)

	// GetUserRank returns a user's rank using PostgreSQL Window Function.
	GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, error)

	// DeleteUserScore removes a user's score from a leaderboard.
	DeleteUserScore(ctx context.Context, leaderboardID, userID string) error

	// CountPlayers returns the total number of players in a leaderboard.
	CountPlayers(ctx context.Context, leaderboardID string) (int64, error)

	// Management methods
	CreateUser(ctx context.Context, userID, username string) error
	CreateLeaderboard(ctx context.Context, leaderboardID, name string) error

	// EnsureUserAndLeaderboard creates user and leaderboard if they don't exist (idempotent).
	// Used to auto-create entities on first score submission.
	EnsureUserAndLeaderboard(ctx context.Context, leaderboardID, userID, username, leaderboardName string) error
}

// LeaderboardCacheRepository defines operations for Redis Sorted Sets (ZSET).
// Optimized for real-time reads: top scores, ranks, increments.
type LeaderboardCacheRepository interface {
	// UpdateScore sets an absolute score for a user in the ZSET.
	UpdateScore(ctx context.Context, leaderboardID, userID string, score int64) error

	// IncrementScore atomically increments a user's score (ZINCRBY).
	// Returns the new score as float64 (Redis ZINCRBY return type).
	IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (float64, error)

	// GetTop returns top N scores with pagination (ZREVRANGE).
	GetTop(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)

	// GetUserRank returns user's rank and score using pipelined ZREVRANK + ZSCORE.
	GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, int64, error)

	// RemoveUser removes a user from the leaderboard ZSET (ZREM).
	RemoveUser(ctx context.Context, leaderboardID, userID string) error

	// PopulateCache bulk inserts scores for cache warming (ZADD batch).
	PopulateCache(ctx context.Context, leaderboardID string, scores []UserScore) error
}

// LeaderboardService defines the business logic use cases for leaderboard operations.
type LeaderboardService interface {
	// SubmitScore records an absolute score for a user.
	SubmitScore(ctx context.Context, leaderboardID string, userID string, score float64) error

	// IncrementScore atomically adds a delta to a user's score.
	// Returns the new score.
	IncrementScore(ctx context.Context, leaderboardID string, userID string, delta int64) (int64, error)

	// GetTopScores returns paginated top scores (Cache-Aside pattern).
	GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)

	// GetUserRank returns a user's rank (Cache-Aside pattern).
	GetUserRank(ctx context.Context, leaderboardID string, userID string) (int64, error)

	// RemoveUserScore removes a user's score from the leaderboard.
	RemoveUserScore(ctx context.Context, leaderboardID string, userID string) error

	// Management methods
	CreateUser(ctx context.Context, userID, username string) error
	CreateLeaderboard(ctx context.Context, leaderboardID, name string) error
}

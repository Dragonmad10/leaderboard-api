package domain

import "context"

type UserScore struct {
	UserID        string `json:"user_id"`
	LeaderboardID string `json:"leaderboard_id"`
	Score         int64  `json:"score"`
	Rank          int64  `json:"rank,omitempty"`
}

// PersistentRepository define las operaciones en PostgreSQL (Fuente de la Verdad)
type LeaderboardPersistentRepository interface {
	UpsertScore(ctx context.Context, leaderboardID, userID string, score int64) error
	IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (int64, error)
	GetScore(ctx context.Context, leaderboardID, userID string) (int64, error)
	GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)
	GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, error)
	DeleteUserScore(ctx context.Context, leaderboardID, userID string) error
	CountPlayers(ctx context.Context, leaderboardID string) (int64, error)
	// Métodos de gestión
	CreateUser(ctx context.Context, userID, username string) error
	CreateLeaderboard(ctx context.Context, leaderboardID, name string) error
	EnsureUserAndLeaderboard(ctx context.Context, leaderboardID, userID, username, leaderboardName string) error
}

// CacheRepository define las operaciones en Redis ZSET (Rendimiento en tiempo real)
type LeaderboardCacheRepository interface {
	UpdateScore(ctx context.Context, leaderboardID, userID string, score int64) error
	IncrementScore(ctx context.Context, leaderboardID, userID string, delta int64) (float64, error)
	GetTop(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)
	GetUserRank(ctx context.Context, leaderboardID, userID string) (int64, int64, error)
	RemoveUser(ctx context.Context, leaderboardID, userID string) error
	PopulateCache(ctx context.Context, leaderboardID string, scores []UserScore) error
}

// Define los casos de uso del negocio
type LeaderboardService interface {
	SubmitScore(ctx context.Context, leaderboardID string, userID string, score float64) error
	IncrementScore(ctx context.Context, leaderboardID string, userID string, delta int64) (int64, error)
	GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)
	GetUserRank(ctx context.Context, leaderboardID string, userID string) (int64, error)
	RemoveUserScore(ctx context.Context, leaderboardID string, userID string) error
	// Métodos de gestión
	CreateUser(ctx context.Context, userID, username string) error
	CreateLeaderboard(ctx context.Context, leaderboardID, name string) error
}

package domain

import "context"

// UserScore representa la puntuación de un usuario en un leaderboard
type UserScore struct {
	UserID string  `json:"user_id"`
	Score  float64 `json:"score"`
	Rank   int64   `json:"rank,omitempty"` // omitempty para no enviarlo si no se ha calculado TODO:Entender por qué se usa omitempty
}

// Define cómo interactuamos con la base de datos (Redis)
type LeaderboardRepository interface {
	AddScore(ctx context.Context, leaderboardID string, userID string, score float64) error
	GetTop(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)
	GetRank(ctx context.Context, leaderboardID string, userID string) (int64, error)
	RemoveUser(ctx context.Context, leaderboardID string, userID string) error
}

// Define los casos de uso del negocio
type LeaderboardService interface {
	SubmitScore(ctx context.Context, leaderboardID string, userID string, score float64) error
	GetTopScores(ctx context.Context, leaderboardID string, limit int, offset int) ([]UserScore, error)
	GetUserRank(ctx context.Context, leaderboardID string, userID string) (int64, error)
	RemoveUserScore(ctx context.Context, leaderboardID string, userID string) error
}

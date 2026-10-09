-- Tabla de Usuarios
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Tabla de Leaderboards
CREATE TABLE IF NOT EXISTS leaderboards (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Tabla de Puntuaciones (relación 1 usuario a 1 leaderboard)
CREATE TABLE IF NOT EXISTS scores (
    leaderboard_id VARCHAR(64) NOT NULL REFERENCES leaderboards(id) ON DELETE CASCADE,
    user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    score BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (leaderboard_id, user_id)
);

-- Índice para acelerar ordenamientos de respaldo en Postgres
CREATE INDEX IF NOT EXISTS idx_scores_leaderboard_score ON scores(leaderboard_id, score DESC);
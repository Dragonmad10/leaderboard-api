package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates a new PostgreSQL connection pool with optimized settings.
// Configuration:
//   - MaxConns: 25 (maximum connections in pool)
//   - MinConns: 5 (minimum idle connections to maintain)
//   - MaxConnLifetime: 30 minutes (recycle connections periodically)
// Returns a pool ready for use, or error if connection fails.
func NewPostgresPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("error parsing postgres config: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("error connecting to postgres: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("error pinging postgres: %w", err)
	}

	return pool, nil
}

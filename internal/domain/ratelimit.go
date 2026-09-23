package domain

import (
	"context"
	"time"
)

// RateLimitResult representa el estado del límite para un cliente.
type RateLimitResult struct {
	Allowed   bool          `json:"allowed"`
	Limit     int64         `json:"limit"`
	Remaining int64         `json:"remaining"`
	ResetIn   time.Duration `json:"reset_in"`
}

// RateLimiterRepository define el contrato para el motor de almacenamiento del rate limiter.
type RateLimiterRepository interface {
	// Allow verifica si una petición identificada por key está dentro del límite para una ventana dada.
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (*RateLimitResult, error)
}

// RateLimiterService expone los casos de uso para limitar el tráfico.
type RateLimiterService interface {
	CheckLimit(ctx context.Context, identifier string, limit int64, window time.Duration) (*RateLimitResult, error)
}

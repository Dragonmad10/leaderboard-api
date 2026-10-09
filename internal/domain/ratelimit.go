package domain

import (
	"context"
	"time"
)

// RateLimitResult represents the rate limit status for a client.
type RateLimitResult struct {
	Allowed   bool          `json:"allowed"`    // Whether the request is allowed
	Limit     int64         `json:"limit"`      // Maximum requests allowed in the window
	Remaining int64         `json:"remaining"`  // Requests remaining in current window
	ResetIn   time.Duration `json:"reset_in"`   // Time until the window resets
}

// RateLimiterRepository defines the contract for the rate limiter storage engine.
type RateLimiterRepository interface {
	// Allow checks if a request identified by key is within the limit for the given window.
	// Uses sliding window algorithm for smooth rate limiting.
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (*RateLimitResult, error)
}

// RateLimiterService exposes the use cases for rate limiting traffic.
type RateLimiterService interface {
	CheckLimit(ctx context.Context, identifier string, limit int64, window time.Duration) (*RateLimitResult, error)
}

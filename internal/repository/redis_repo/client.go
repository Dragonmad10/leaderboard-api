package redis_repo

// This package uses the redis.Client directly from github.com/redis/go-redis/v9
// No separate client wrapper is needed - the client is created in main.go
// and passed to repository constructors.

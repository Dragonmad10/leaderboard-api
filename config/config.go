package config

import "os"

// Config holds application configuration loaded from environment variables
type Config struct {
	Port      string // HTTP server port (default: 8080)
	RedisAddr string // Redis server address (default: 127.0.0.1:6379)
}

// LoadConfig reads configuration from environment variables with sensible defaults
func LoadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379" // Default for local development outside Docker
	}

	return &Config{
		Port:      port,
		RedisAddr: redisAddr,
	}
}

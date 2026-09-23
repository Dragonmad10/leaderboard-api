package config

import "os"

type Config struct {
	Port      string
	RedisAddr string
}

func LoadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379" // Default para desarrollo local fuera de Docker
	}

	return &Config{
		Port:      port,
		RedisAddr: redisAddr,
	}
}

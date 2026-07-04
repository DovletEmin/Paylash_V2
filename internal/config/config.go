package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port          int
	DBURL         string
	MinioEndpoint string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL   bool
	CollaboraURL  string
	BaseURL       string
	JWTSecret     string
}

func Load() *Config {
	return &Config{
		Port:          getEnvInt("PAYLASH_PORT", 8080),
		DBURL:         getEnv("PAYLASH_DB_URL", "postgres://paylash:paylash_secret@localhost:5432/paylash?sslmode=disable"),
		MinioEndpoint: getEnv("PAYLASH_MINIO_ENDPOINT", "localhost:9000"),
		MinioAccessKey: getEnv("PAYLASH_MINIO_ACCESS_KEY", "paylash"),
		MinioSecretKey: getEnv("PAYLASH_MINIO_SECRET_KEY", "paylash_secret"),
		MinioUseSSL:   getEnvBool("PAYLASH_MINIO_USE_SSL", false),
		CollaboraURL:  getEnv("PAYLASH_COLLABORA_URL", "http://localhost:9980"),
		BaseURL:       getEnv("PAYLASH_BASE_URL", "http://localhost:8080"),
		JWTSecret:     getEnv("PAYLASH_JWT_SECRET", "paylash-dev-secret-change-me"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

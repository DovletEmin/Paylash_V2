package config

import (
	"os"
	"paylash/internal/models"
	"strconv"
	"time"
)

type Config struct {
	Port                int
	DBURL               string
	MinioEndpoint       string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioUseSSL         bool
	MinioPublicEndpoint string
	CollaboraURL        string
	// CollaboraHealthURL is Collabora's INTERNAL (Docker-network) discovery
	// endpoint, used only by /healthz. CollaboraURL above is deliberately
	// NOT reused for this: it's the public-facing address routed through
	// Caddy (which itself depends on this app being healthy — checking
	// through Caddy from here would be circular), and may use a
	// Caddy-issued internal TLS cert this process has no reason to trust.
	CollaboraHealthURL string
	BaseURL            string
	AllowRegistration  bool
	// SessionTTL is how long a normal login session lasts before it must be
	// renewed — PAYLASH_SESSION_TTL_HOURS, in hours. Impersonation sessions
	// stay a fixed, deliberately-shorter 2h regardless of this setting (see
	// db.CreateImpersonationSession).
	SessionTTL time.Duration
}

func Load() *Config {
	return &Config{
		Port:          getEnvInt("PAYLASH_PORT", 8080),
		DBURL:         getEnv("PAYLASH_DB_URL", "postgres://paylash:paylash_secret@localhost:5432/paylash?sslmode=disable"),
		MinioEndpoint: getEnv("PAYLASH_MINIO_ENDPOINT", "localhost:9000"),
		MinioAccessKey: getEnv("PAYLASH_MINIO_ACCESS_KEY", "paylash"),
		MinioSecretKey: getEnv("PAYLASH_MINIO_SECRET_KEY", "paylash_secret"),
		MinioUseSSL:   getEnvBool("PAYLASH_MINIO_USE_SSL", false),
		// Host:port the BROWSER can reach MinIO's S3 API on directly, used only
		// to sign presigned URLs for large resumable uploads (bulk bytes flow
		// straight from the browser to MinIO, bypassing the app entirely). Empty
		// disables that upload path — see internal/api/uploads.go.
		MinioPublicEndpoint: getEnv("PAYLASH_MINIO_PUBLIC_ENDPOINT", ""),
		CollaboraURL:        getEnv("PAYLASH_COLLABORA_URL", "http://localhost:9980"),
		CollaboraHealthURL:  getEnv("PAYLASH_COLLABORA_HEALTH_URL", "http://paylash-collabora:9980/hosting/discovery"),
		BaseURL:             getEnv("PAYLASH_BASE_URL", "http://localhost:8080"),
		// Self-registration is on by default (matches prior behavior, where
		// it wasn't configurable at all). Studios that rely solely on
		// admin-managed onboarding (manual + CSV/XLSX import) can close it —
		// an open registration endpoint on a LAN hands instant access to the
		// company-wide "common" space to anyone who can reach the server.
		AllowRegistration: getEnvBool("PAYLASH_ALLOW_REGISTRATION", true),
		SessionTTL:        time.Duration(getEnvInt("PAYLASH_SESSION_TTL_HOURS", int(models.DefaultSessionTTL.Hours()))) * time.Hour,
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

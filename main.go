package main

import (
	"embed"
	"log"
	"log/slog"
	"os"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/janitor"
	"paylash/internal/server"
	"paylash/internal/storage"
)

//go:embed web/*
var webFS embed.FS

func main() {
	// Every access-log line (see server.LoggingMiddleware) goes through
	// slog as structured JSON — one request_id-tagged object per line, easy
	// to grep/parse/feed to a log aggregator. One-off startup/fatal
	// messages below stay on the plain stdlib `log` package deliberately:
	// they're human-read-once boot notices, not something worth
	// structuring.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg := config.Load()

	// Connect to PostgreSQL
	database, err := db.Connect(cfg.DBURL)
	if err != nil {
		log.Fatal("failed to connect to database:", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.Migrate(); err != nil {
		log.Fatal("failed to run migrations:", err)
	}

	// Seed admin user
	if err := server.SeedAdmin(database); err != nil {
		log.Fatal("failed to seed admin:", err)
	}

	// Connect to MinIO
	minioClient, err := storage.NewMinioClient(
		cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioUseSSL, cfg.MinioPublicEndpoint,
	)
	if err != nil {
		log.Fatal("failed to connect to MinIO:", err)
	}

	// Daily background cleanup (expired trash, etc.)
	go janitor.Run(database, minioClient)

	// Start server
	srv := server.New(cfg, database, minioClient, webFS)
	if err := srv.Start(); err != nil {
		log.Fatal("server error:", err)
	}
}

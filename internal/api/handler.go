package api

import (
	"encoding/json"
	"net/http"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/storage"
)

type Handler struct {
	db    *db.DB
	minio *storage.MinioClient
	cfg   *config.Config
}

func NewHandler(database *db.DB, minioClient *storage.MinioClient, cfg *config.Config) *Handler {
	return &Handler{
		db:    database,
		minio: minioClient,
		cfg:   cfg,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

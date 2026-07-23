package api

import (
	"encoding/json"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/storage"
	"strconv"
)

type Handler struct {
	db              *db.DB
	minio           *storage.MinioClient
	cfg             *config.Config
	loginLimiter    *keyedLimiter
	registerLimiter *keyedLimiter
	commentLimiter  *keyedLimiter
	avatarLimiter   *keyedLimiter
}

func NewHandler(database *db.DB, minioClient *storage.MinioClient, cfg *config.Config) *Handler {
	return &Handler{
		db:              database,
		minio:           minioClient,
		cfg:             cfg,
		loginLimiter:    newLoginLimiter(),
		registerLimiter: newKeyedLimiter(registerMaxAttempts, registerWindow),
		commentLimiter:  newKeyedLimiter(commentMaxAttempts, commentWindow),
		avatarLimiter:   newKeyedLimiter(avatarMaxAttempts, avatarWindow),
	}
}

// PublicConfig exposes the handful of settings the login page needs before
// a session exists (e.g. whether to show the self-registration link) —
// nothing here is sensitive.
func (h *Handler) PublicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"allow_registration": h.cfg.AllowRegistration})
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

// parsePagination reads limit/offset query params, falling back to
// defaultLimit and clamping to [1, maxLimit] / [0, +inf) respectively.
func parsePagination(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// logAction records an admin-oversight-worthy event on behalf of the
// request's authenticated user. Best-effort: a logging failure is logged
// itself but never blocks or fails the action it describes.
func (h *Handler) logAction(r *http.Request, action, targetType string, targetID int, targetName string, details map[string]any) {
	user := authutil.GetUser(r)
	if user == nil {
		return
	}
	actorID := user.ID
	actorName := user.DisplayName
	if actorName == "" {
		actorName = user.Username
	}
	id := targetID
	if err := h.db.LogAction(&actorID, actorName, action, targetType, &id, targetName, details); err != nil {
		log.Printf("audit log: %v", err)
	}
}

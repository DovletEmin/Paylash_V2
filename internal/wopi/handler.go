package wopi

import (
	"encoding/json"
	"io"
	"net/http"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/storage"
	"strconv"
	"time"
)

type Handler struct {
	db    *db.DB
	minio *storage.MinioClient
	cfg   *config.Config
}

func NewHandler(database *db.DB, minioClient *storage.MinioClient, cfg *config.Config) *Handler {
	return &Handler{db: database, minio: minioClient, cfg: cfg}
}

func (h *Handler) CheckFileInfo(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	token := r.URL.Query().Get("access_token")
	wopiToken, err := h.db.GetWOPIToken(token)
	if err != nil || wopiToken.FileID != fileID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	user, err := h.db.GetUserByID(wopiToken.UserID)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	canWrite := wopiToken.Permission == "edit"

	// The origin Collabora's postMessage bridge is allowed to talk to — must
	// be the browser-facing app origin, not h.cfg.BaseURL (that's the
	// internal Docker hostname used for the WOPI callback URL, never seen by
	// a browser). Derived from the request itself (which arrives via Caddy
	// with the original Host/X-Forwarded-Proto preserved) instead of a new
	// config value, so it's correct for whatever domain Caddy is fronting.
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS == nil {
		scheme = "http"
	}
	postMessageOrigin := scheme + "://" + r.Host

	info := map[string]any{
		"BaseFileName":            f.Name,
		"Size":                    f.SizeBytes,
		"OwnerId":                strconv.Itoa(f.OwnerID),
		"UserId":                 strconv.Itoa(user.ID),
		"UserFriendlyName":       user.DisplayName,
		"UserCanWrite":           canWrite,
		"UserCanNotWriteRelative": true,
		"SupportsLocks":          true,
		"SupportsUpdate":         true,
		"SupportsRename":         false,
		"EnableOwnerTermination": true,
		"DisablePrint":           false,
		"DisableExport":          false,
		"DisableCopy":            false,
		"PostMessageOrigin":      postMessageOrigin,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (h *Handler) GetFile(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	token := r.URL.Query().Get("access_token")
	wopiToken, err := h.db.GetWOPIToken(token)
	if err != nil || wopiToken.FileID != fileID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	obj, err := h.minio.Download(r.Context(), f.MinioBucket, f.MinioKey)
	if err != nil {
		http.Error(w, "download failed", http.StatusInternalServerError)
		return
	}
	defer obj.Close()

	w.Header().Set("Content-Type", f.MimeType)
	io.Copy(w, obj)
}

func (h *Handler) PutFile(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid file id", http.StatusBadRequest)
		return
	}

	token := r.URL.Query().Get("access_token")
	wopiToken, err := h.db.GetWOPIToken(token)
	if err != nil || wopiToken.FileID != fileID || wopiToken.Permission != "edit" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	defer r.Body.Close()

	// Stream straight to MinIO instead of buffering the whole save in memory
	// first. Collabora always sends a Content-Length for WOPI PutFile; when
	// it's present, MinIO can do a single-shot PUT. Fall back to -1 (unknown
	// size), which minio-go handles by streaming through its own internal
	// multipart upload, so this never breaks even if that assumption is wrong.
	size := r.ContentLength
	if size < 0 {
		size = -1
	}
	// Quota check — every other content-writing path checks this. Only
	// possible pre-upload (matching the plain /api/files/upload pattern)
	// when Content-Length is known, which per the comment above is the
	// normal case for Collabora; skipped on the rare unknown-size fallback
	// rather than adding version-rollback logic to undo a save mid-edit,
	// which is a poor tradeoff for autosave (risking the user's in-progress
	// edits) against a narrow, self-bounding gap (versions are trimmed by
	// the janitor after 90 days regardless).
	if size >= 0 {
		if usage, err := h.db.GetStorageUsage(f.OwnerID, f.Scope, f.ProjectID); err == nil {
			if usage.UsedBytes-f.SizeBytes+size > usage.QuotaBytes {
				http.Error(w, "quota exceeded", http.StatusInsufficientStorage)
				return
			}
		}
	}
	if err := h.minio.Upload(r.Context(), f.MinioBucket, f.MinioKey, r.Body, size, f.MimeType); err != nil {
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}

	// The object's real size is authoritative from MinIO, not the
	// (possibly -1/unknown) Content-Length we started with.
	info, err := h.minio.GetObjectInfo(r.Context(), f.MinioBucket, f.MinioKey)
	if err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	if err := h.db.UpdateFileVersion(fileID, info.Size); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"LastModifiedTime": time.Now().UTC(),
	})
}

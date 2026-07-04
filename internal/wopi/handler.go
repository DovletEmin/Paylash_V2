package wopi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/storage"
	"strconv"
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
		"PostMessageOrigin":      "*",
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

	// Read body into a temporary buffer to get size
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}

	reader := bytes.NewReader(body)

	if err := h.minio.Upload(r.Context(), f.MinioBucket, f.MinioKey, reader, int64(len(body)), f.MimeType); err != nil {
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}

	if err := h.db.UpdateFileVersion(fileID, int64(len(body))); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"LastModifiedTime": f.UpdatedAt,
	})
}

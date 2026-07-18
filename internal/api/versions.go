package api

import (
	"fmt"
	"net/http"
	"paylash/internal/authutil"
	"strconv"
	"time"
)

func (h *Handler) ListFileVersions(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canAccess, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}
	versions, err := h.minio.ListVersions(r.Context(), f.MinioBucket, f.MinioKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "wersiýalary alyp bolmady")
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (h *Handler) RestoreFileVersion(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	versionID := r.PathValue("versionId")

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canEdit, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canEdit {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Restoring an old version can grow the file back past its current
	// size — every other content-writing path this round (regular upload,
	// resumable upload completion, blank-file creation) checks quota, this
	// one didn't. Checked against the version's real size, not the
	// current one, and billed to the file's owner regardless of who's
	// performing the restore (matches GetStorageUsage's ownerID semantics).
	if info, err := h.minio.GetVersionInfo(r.Context(), f.MinioBucket, f.MinioKey, versionID); err == nil {
		if usage, err := h.db.GetStorageUsage(f.OwnerID, f.Scope, f.ProjectID); err == nil {
			if usage.UsedBytes-f.SizeBytes+info.Size > usage.QuotaBytes {
				writeError(w, http.StatusForbidden, "ammar doly, ýer ýok")
				return
			}
		}
	}

	if err := h.minio.RestoreVersion(r.Context(), f.MinioBucket, f.MinioKey, versionID); err != nil {
		writeError(w, http.StatusInternalServerError, "dikeldip bolmady")
		return
	}
	// Keep our cached size/edit-counter in sync with the now-current content.
	if info, err := h.minio.GetObjectInfo(r.Context(), f.MinioBucket, f.MinioKey); err == nil {
		h.db.UpdateFileVersion(fileID, info.Size)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DownloadFileVersion(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	versionID := r.PathValue("versionId")

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canAccess, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	obj, err := h.minio.DownloadVersion(r.Context(), f.MinioBucket, f.MinioKey, versionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýly alyp bolmady")
		return
	}
	defer obj.Close()

	lastModified := time.Now()
	if info, err := h.minio.GetVersionInfo(r.Context(), f.MinioBucket, f.MinioKey, versionID); err == nil {
		lastModified = info.LastModified
	}

	w.Header().Set("Content-Type", f.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, f.Name))
	http.ServeContent(w, r, f.Name, lastModified, obj)
}

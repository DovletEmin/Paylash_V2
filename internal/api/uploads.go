package api

import (
	"fmt"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
	"strings"
	"time"
)

// uploadPartSize is the default multipart chunk size (64MB) — for a 100GB
// file that's ~1600 parts, comfortably under S3/MinIO's 10,000-part ceiling.
const uploadPartSize = 64 << 20

// maxUploadParts is the hard S3/MinIO limit on parts per multipart upload.
const maxUploadParts = 10000

// computeUploadParts picks a part size and count for a file of totalSize.
// It starts from uploadPartSize (64MB) and only grows the part size once
// that would need more than maxUploadParts parts — still well within a
// single part's 5GB S3/MinIO limit for anything this app realistically sees.
// totalSize must be > 0; callers validate that before calling this.
func computeUploadParts(totalSize int64) (partSize int64, partCount int) {
	partSize = uploadPartSize
	partCount = int((totalSize + partSize - 1) / partSize)
	if partCount > maxUploadParts {
		partSize = (totalSize + maxUploadParts - 1) / maxUploadParts
		partCount = int((totalSize + partSize - 1) / partSize)
	}
	return partSize, partCount
}

// InitUpload starts a resumable/chunked upload for a large file: quota is
// checked here, upfront, before any bytes are transferred (unlike
// /api/files/upload, which only finds out after the whole body already
// arrived) — then a MinIO multipart upload is opened and its id tracked in
// upload_sessions so the browser can resume after a reload/disconnect.
func (h *Handler) InitUpload(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if !h.minio.PublicEndpointConfigured() {
		writeError(w, http.StatusServiceUnavailable, "uly faýl ýüklemek üpjün edilmeýär")
		return
	}

	var req struct {
		FileName  string `json:"file_name"`
		Size      int64  `json:"size"`
		Scope     string `json:"scope"`
		ProjectID *int   `json:"project_id"`
		FolderID  *int   `json:"folder_id"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.FileName) == "" || req.Size <= 0 {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	scope := req.Scope
	var bucket string
	var projectID *int
	if scope == "project" {
		if req.ProjectID == nil || *req.ProjectID <= 0 {
			writeError(w, http.StatusBadRequest, "taslama saýlanmaly")
			return
		}
		projectID = req.ProjectID
		bucket = storage.ProjectBucket(*projectID)
	} else if scope == "common" {
		bucket = "common-files"
	} else {
		scope = "personal"
		bucket = storage.PersonalBucket(user.ID)
	}

	if !h.canEditScope(user, scope, projectID) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}
	if !destFolderValid(h.db, req.FolderID, scope, projectID, user.ID) {
		writeError(w, http.StatusBadRequest, "bukja tapylmady")
		return
	}

	usage, err := h.db.GetStorageUsage(user.ID, scope, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ammar maglumatyny alyp bolmady")
		return
	}
	if usage.UsedBytes+req.Size > usage.QuotaBytes {
		writeError(w, http.StatusForbidden, "ammar doly, ýer ýok")
		return
	}

	if err := h.minio.EnsureBucket(r.Context(), bucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar döredip bolmady")
		return
	}

	fileName := uniqueFileName(h.db, strings.TrimSpace(req.FileName), user.ID, scope, req.FolderID, projectID)
	key := fmt.Sprintf("%d/%s", user.ID, fileName)
	if req.FolderID != nil {
		key = fmt.Sprintf("%d/f%d/%s", user.ID, *req.FolderID, fileName)
	}

	partSize, partCount := computeUploadParts(req.Size)

	contentType := "application/octet-stream"
	uploadID, err := h.minio.InitMultipartUpload(r.Context(), bucket, key, contentType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýüklemäni başlap bolmady")
		return
	}

	session := &models.UploadSession{
		MinioUploadID: uploadID,
		Bucket:        bucket,
		ObjectKey:     key,
		OwnerID:       user.ID,
		Scope:         scope,
		ProjectID:     projectID,
		FolderID:      req.FolderID,
		FileName:      fileName,
		MimeType:      contentType,
		TotalSize:     req.Size,
		PartSize:      partSize,
		PartCount:     partCount,
	}
	if err := h.db.CreateUploadSession(session); err != nil {
		writeError(w, http.StatusInternalServerError, "sessiýa döredip bolmady")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": session.ID, "part_size": partSize, "part_count": partCount, "file_name": fileName,
	})
}

func (h *Handler) getOwnedUploadSession(w http.ResponseWriter, r *http.Request) *models.UploadSession {
	user := authutil.GetUser(r)
	session, err := h.db.GetUploadSession(r.PathValue("id"))
	if err != nil || session == nil || (session.OwnerID != user.ID && user.Role != "admin") {
		writeError(w, http.StatusNotFound, "sessiýa tapylmady")
		return nil
	}
	return session
}

// UploadPartURL hands back a presigned URL the browser PUTs one part's bytes
// to directly — the app server never sees the part's data.
func (h *Handler) UploadPartURL(w http.ResponseWriter, r *http.Request) {
	session := h.getOwnedUploadSession(w, r)
	if session == nil {
		return
	}
	if session.Status != "in_progress" {
		writeError(w, http.StatusConflict, "sessiýa tamamlandy")
		return
	}
	partNumber, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || partNumber < 1 || partNumber > session.PartCount {
		writeError(w, http.StatusBadRequest, "nädogry bölek belgisi")
		return
	}

	url, err := h.minio.PresignPartUpload(r.Context(), session.Bucket, session.ObjectKey, session.MinioUploadID, partNumber, time.Hour)
	if err != nil {
		log.Printf("presign part upload: %v", err)
		writeError(w, http.StatusInternalServerError, "salgy döredip bolmady")
		return
	}
	h.db.TouchUploadSession(session.ID)
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// UploadStatus reports which parts MinIO already has — the resume mechanism
// after a page reload or dropped connection: the frontend skips whatever's
// already in uploaded_parts and only (re-)sends what's missing.
func (h *Handler) UploadStatus(w http.ResponseWriter, r *http.Request) {
	session := h.getOwnedUploadSession(w, r)
	if session == nil {
		return
	}
	parts, err := h.minio.ListUploadedParts(r.Context(), session.Bucket, session.ObjectKey, session.MinioUploadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "maglumat alyp bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": session.Status, "part_size": session.PartSize, "part_count": session.PartCount,
		"total_size": session.TotalSize, "file_name": session.FileName, "uploaded_parts": parts,
	})
}

// CompleteUpload finalizes the MinIO multipart upload once every part has
// arrived and creates the file row — the final size comes from the real
// completed object in MinIO, not whatever the client claimed at init time,
// so quota accounting self-corrects even if the initial estimate was off.
func (h *Handler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	session := h.getOwnedUploadSession(w, r)
	if session == nil {
		return
	}

	var req struct {
		Parts []struct {
			PartNumber int    `json:"part_number"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Parts) == 0 {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	parts := make([]storage.CompletedPart, len(req.Parts))
	for i, p := range req.Parts {
		parts[i] = storage.CompletedPart{PartNumber: p.PartNumber, ETag: p.ETag}
	}

	// Resumable sessions can stay open a long time — that's the point of the
	// feature — so the destination folder someone chose at init may have
	// since been trashed or deleted by a collaborator. Re-check now, before
	// completing (not after): completing first and finding out CreateFile's
	// FK constraint rejects a deleted folder_id would leave the just-
	// committed MinIO object leaked with nothing tracking it. Checked here,
	// aborting the multipart upload is the clean rollback instead.
	if session.FolderID != nil {
		if folder, err := h.db.GetFolder(*session.FolderID); err != nil || folder == nil {
			if delErr := h.minio.AbortMultipartUpload(r.Context(), session.Bucket, session.ObjectKey, session.MinioUploadID); delErr != nil {
				log.Printf("abort upload for deleted folder, session %s: %v", session.ID, delErr)
			}
			if delErr := h.db.DeleteUploadSession(session.ID); delErr != nil {
				log.Printf("cleanup upload session %s: %v", session.ID, delErr)
			}
			writeError(w, http.StatusConflict, "bukja pozulan, ýüklemäni tamamlap bolmady")
			return
		}
	}

	size, err := h.minio.CompleteMultipartUpload(r.Context(), session.Bucket, session.ObjectKey, session.MinioUploadID, parts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tamamlap bolmady")
		return
	}

	// Presigned part URLs don't themselves enforce a byte ceiling, so a
	// client could declare a small size at init (to pass that upfront quota
	// check) and then upload far more through the parts. Now that we know
	// the real completed size, check it against quota for real — and if
	// it's over, delete the object again rather than leave a permanent
	// over-quota file behind. This doesn't prevent the wasted transfer, but
	// it does prevent it from ever becoming a real file.
	usage, err := h.db.GetStorageUsage(session.OwnerID, session.Scope, session.ProjectID)
	if err == nil && usage.UsedBytes+size > usage.QuotaBytes {
		if delErr := h.minio.Delete(r.Context(), session.Bucket, session.ObjectKey); delErr != nil {
			log.Printf("cleanup over-quota upload %s/%s: %v", session.Bucket, session.ObjectKey, delErr)
		}
		if delErr := h.db.DeleteUploadSession(session.ID); delErr != nil {
			log.Printf("cleanup upload session %s: %v", session.ID, delErr)
		}
		writeError(w, http.StatusForbidden, "ammar doly, ýer ýok")
		return
	}

	f := &models.File{
		Name:        session.FileName,
		MimeType:    session.MimeType,
		SizeBytes:   size,
		MinioBucket: session.Bucket,
		MinioKey:    session.ObjectKey,
		FolderID:    session.FolderID,
		OwnerID:     session.OwnerID,
		ProjectID:   session.ProjectID,
		Scope:       session.Scope,
	}
	if session.Scope == "common" {
		f.Visibility = "common"
	}
	if err := h.db.CreateFile(f); err != nil {
		writeError(w, http.StatusInternalServerError, "faýl maglumatyny saklap bolmady")
		return
	}
	if err := h.db.DeleteUploadSession(session.ID); err != nil {
		log.Printf("cleanup upload session %s: %v", session.ID, err)
	}
	h.logAction(r, "upload.complete", "file", f.ID, f.Name, map[string]any{
		"size_bytes": size, "part_count": len(req.Parts),
	})
	writeJSON(w, http.StatusCreated, f)
}

// AbortUpload cancels an in-progress upload and releases whatever parts
// MinIO had already buffered for it.
func (h *Handler) AbortUpload(w http.ResponseWriter, r *http.Request) {
	session := h.getOwnedUploadSession(w, r)
	if session == nil {
		return
	}
	if err := h.minio.AbortMultipartUpload(r.Context(), session.Bucket, session.ObjectKey, session.MinioUploadID); err != nil {
		log.Printf("abort upload %s: %v", session.ID, err)
	}
	if err := h.db.DeleteUploadSession(session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýatyryp bolmady")
		return
	}
	h.logAction(r, "upload.abort", "upload_session", 0, session.FileName, map[string]any{"session_id": session.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AdminListUploads lists every upload currently in progress across all
// employees — visibility into large uploads that might be stuck (the
// janitor only aborts sessions untouched for 24h; this lets an admin check
// or cancel sooner).
func (h *Handler) AdminListUploads(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.db.ListActiveUploadSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "maglumat alyp bolmady")
		return
	}
	if sessions == nil {
		sessions = []models.UploadSessionView{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

// AdminAbortUpload force-cancels any employee's in-progress upload.
func (h *Handler) AdminAbortUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, err := h.db.GetUploadSession(id)
	if err != nil || session == nil {
		writeError(w, http.StatusNotFound, "sessiýa tapylmady")
		return
	}
	if err := h.minio.AbortMultipartUpload(r.Context(), session.Bucket, session.ObjectKey, session.MinioUploadID); err != nil {
		log.Printf("admin abort upload %s: %v", id, err)
	}
	if err := h.db.DeleteUploadSession(id); err != nil {
		writeError(w, http.StatusInternalServerError, "ýatyryp bolmady")
		return
	}
	h.logAction(r, "upload.admin_abort", "upload_session", 0, session.FileName, map[string]any{"session_id": id, "owner_id": session.OwnerID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

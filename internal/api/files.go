package api

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"path/filepath"
	"paylash/internal/authutil"
	"paylash/internal/db"
	"paylash/internal/models"
	"paylash/internal/storage"
	"paylash/internal/thumbnail"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// uniqueFileName returns a name like "file (1).docx" if "file.docx" already exists.
func uniqueFileName(store *db.DB, name string, ownerID int, scope string, folderID *int, projectID *int) string {
	exists, err := store.FileNameExists(name, ownerID, scope, folderID, projectID)
	if err != nil || !exists {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i <= 100; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		exists, err = store.FileNameExists(candidate, ownerID, scope, folderID, projectID)
		if err != nil || !exists {
			return candidate
		}
	}
	// Fallback: timestamp-based
	return fmt.Sprintf("%s (%d)%s", base, time.Now().Unix(), ext)
}

// intPtrEqual compares two nullable IDs (folder_id, project_id) for
// equality, treating nil as its own distinct value rather than 0.
func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// destFolderValid reports whether folderID (if non-nil — nil means "scope
// root", always valid) is a real folder in the given scope/project and,
// for personal scope, owned by ownerID. Every path that accepts a
// client-supplied folder_id to place a NEW file/folder (upload, blank-file
// create, folder create, resumable-upload init) must check this, or a
// client can plant content into a mismatched-scope or another user's
// personal folder tree — same class of bug as the one fixed in MoveFile/
// MoveFolder, just on creation instead of move.
func destFolderValid(store *db.DB, folderID *int, scope string, projectID *int, ownerID int) bool {
	if folderID == nil {
		return true
	}
	folder, err := store.GetFolder(*folderID)
	if err != nil || folder == nil {
		return false
	}
	if folder.Scope != scope || !intPtrEqual(folder.ProjectID, projectID) {
		return false
	}
	if scope == "personal" && folder.OwnerID != ownerID {
		return false
	}
	return true
}

// projectPermLookup is the subset of *db.DB that the access-control
// decisions below depend on. Extracting it as an interface lets tests
// substitute a stub instead of needing a real Postgres connection.
type projectPermLookup interface {
	GetProjectMemberPermission(projectID, userID int) (string, error)
}

// canEditScope reports whether the user may create new content in the given
// scope (upload / create folder). "personal" is always the caller's own
// space here, so it's trivially allowed — this must NOT be used to check
// permission on an already-existing resource that might belong to someone
// else (use canEditFolder for that).
func (h *Handler) canEditScope(user *models.User, scope string, projectID *int) bool {
	return canEditScopeWith(h.db, user.Role, user.ID, scope, projectID)
}

func canEditScopeWith(lookup projectPermLookup, role string, userID int, scope string, projectID *int) bool {
	if role == "admin" {
		return true
	}
	switch scope {
	case "common", "personal":
		return true
	case "project":
		if projectID == nil {
			return false
		}
		perm, err := lookup.GetProjectMemberPermission(*projectID, userID)
		return err == nil && perm == "edit"
	}
	return false
}

// canEditFolder reports whether the user may rename/delete an existing folder.
// Unlike canEditScope, "personal" here requires actual ownership of the folder.
func (h *Handler) canEditFolder(user *models.User, folder *models.Folder) bool {
	return canEditFolderWith(h.db, user.Role, user.ID, folder)
}

func canEditFolderWith(lookup projectPermLookup, role string, userID int, folder *models.Folder) bool {
	if role == "admin" {
		return true
	}
	switch folder.Scope {
	case "personal":
		return folder.OwnerID == userID
	case "common":
		return true
	case "project":
		if folder.ProjectID == nil {
			return false
		}
		perm, err := lookup.GetProjectMemberPermission(*folder.ProjectID, userID)
		return err == nil && perm == "edit"
	}
	return false
}

// canViewFolder reports whether the user may read/download an existing
// folder — the view-level counterpart to canEditFolder (any project
// membership qualifies, not just "edit"), matching the access level
// ListFiles/ListFolderTree already require for the same scopes.
func (h *Handler) canViewFolder(user *models.User, folder *models.Folder) bool {
	return canViewFolderWith(h.db, user.Role, user.ID, folder)
}

func canViewFolderWith(lookup projectPermLookup, role string, userID int, folder *models.Folder) bool {
	if role == "admin" {
		return true
	}
	switch folder.Scope {
	case "personal":
		return folder.OwnerID == userID
	case "common":
		return true
	case "project":
		if folder.ProjectID == nil {
			return false
		}
		perm, err := lookup.GetProjectMemberPermission(*folder.ProjectID, userID)
		return err == nil && perm != ""
	}
	return false
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "personal"
	}
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")

	var folderID *int
	if fid := r.URL.Query().Get("folder_id"); fid != "" {
		if n, err := strconv.Atoi(fid); err == nil {
			folderID = &n
		}
	}

	var projectID *int
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		if n, err := strconv.Atoi(pid); err == nil {
			projectID = &n
		}
	}

	if scope == "project" {
		if projectID == nil {
			writeError(w, http.StatusBadRequest, "taslama saýlanmaly")
			return
		}
		if user.Role != "admin" {
			perm, err := h.db.GetProjectMemberPermission(*projectID, user.ID)
			if err != nil || perm == "" {
				writeError(w, http.StatusForbidden, "rugsat ýok")
				return
			}
		}
	}

	limit, offset := parsePagination(r, 50, 200)
	files, err := h.db.ListFiles(user.ID, projectID, scope, folderID, sort, order, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýllary alyp bolmady")
		return
	}
	folders, err := h.db.ListFolders(user.ID, projectID, scope, folderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bukjalary alyp bolmady")
		return
	}
	if files == nil {
		files = []models.File{}
	}
	if folders == nil {
		folders = []models.Folder{}
	}

	// Breadcrumb trail for the currently-open folder, root-most first, with
	// the folder itself appended last — the frontend renders every entry
	// but this one as a clickable ancestor link.
	var crumbs []models.FolderCrumb
	if folderID != nil {
		crumbs, err = h.db.GetFolderAncestors(*folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ýoly alyp bolmady")
			return
		}
		if here, err := h.db.GetFolder(*folderID); err == nil && here != nil {
			crumbs = append(crumbs, models.FolderCrumb{ID: here.ID, Name: here.Name})
		}
	}
	if crumbs == nil {
		crumbs = []models.FolderCrumb{}
	}

	writeJSON(w, http.StatusOK, models.FileListResponse{Files: files, Folders: folders, Breadcrumbs: crumbs})
}

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)

	// 100MB here is just the in-memory/temp-file threshold ParseMultipartForm
	// uses internally (Go spills anything larger to a temp file automatically)
	// — it is NOT a hard size cap. The real ceiling for this endpoint is
	// Caddy's request_body max_size. Files at/above Uploader.LARGE_FILE_THRESHOLD
	// (web/js/upload.js) skip this endpoint entirely and go through the
	// resumable direct-to-MinIO path in internal/api/uploads.go instead.
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "faýl juda uly")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "faýl tapylmady")
		return
	}
	defer file.Close()

	scope := r.FormValue("scope")
	if scope == "" {
		scope = "personal"
	}

	// Determine bucket and check quota
	var bucket string
	var projectID *int
	if scope == "project" {
		pidStr := r.FormValue("project_id")
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			writeError(w, http.StatusBadRequest, "taslama saýlanmaly")
			return
		}
		projectID = &pid
		bucket = storage.ProjectBucket(pid)
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

	// Check quota
	usage, err := h.db.GetStorageUsage(user.ID, scope, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ammar maglumatyny alyp bolmady")
		return
	}
	if usage.UsedBytes+header.Size > usage.QuotaBytes {
		writeError(w, http.StatusForbidden, "ammar doly, ýer ýok")
		return
	}

	if err := h.minio.EnsureBucket(r.Context(), bucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar döredip bolmady")
		return
	}

	// Build key
	var folderID *int
	if fid := r.FormValue("folder_id"); fid != "" {
		if n, err := strconv.Atoi(fid); err == nil {
			folderID = &n
		}
	}
	if !destFolderValid(h.db, folderID, scope, projectID, user.ID) {
		writeError(w, http.StatusBadRequest, "bukja tapylmady")
		return
	}

	// Auto-rename if duplicate name exists
	fileName := uniqueFileName(h.db, header.Filename, user.ID, scope, folderID, projectID)

	key := fmt.Sprintf("%d/%s", user.ID, fileName)
	if folderID != nil {
		key = fmt.Sprintf("%d/f%d/%s", user.ID, *folderID, fileName)
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if err := h.minio.Upload(r.Context(), bucket, key, file, header.Size, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, "faýly ýükläp bolmady")
		return
	}

	// Re-check quota post-upload: the check above and this upload aren't
	// atomic, so two uploads racing the same quota could both pass the
	// earlier check and jointly exceed it. Narrows (doesn't eliminate — that
	// would need a DB-level lock) the race window, same tradeoff already
	// accepted for the resumable-upload path's post-CompleteUpload check.
	if usage, err := h.db.GetStorageUsage(user.ID, scope, projectID); err == nil && usage.UsedBytes+header.Size > usage.QuotaBytes {
		if delErr := h.minio.Delete(r.Context(), bucket, key); delErr != nil {
			log.Printf("upload quota re-check: cleanup %s/%s: %v", bucket, key, delErr)
		}
		writeError(w, http.StatusForbidden, "ammar doly, ýer ýok")
		return
	}

	f := &models.File{
		Name:        fileName,
		MimeType:    contentType,
		SizeBytes:   header.Size,
		MinioBucket: bucket,
		MinioKey:    key,
		FolderID:    folderID,
		OwnerID:     user.ID,
		ProjectID:   projectID,
		Scope:       scope,
	}
	if scope == "common" {
		f.Visibility = "common"
	}
	if err := h.db.CreateFile(f); err != nil {
		writeError(w, http.StatusInternalServerError, "faýl maglumatyny saklap bolmady")
		return
	}

	writeJSON(w, http.StatusCreated, f)
}

func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFile(id)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}

	canAccess, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Every download (any size) streams straight through this process via
	// ServeContent below — it used to redirect files ≥50MB to a presigned
	// MinIO URL instead, but that URL was always http:// while the app
	// itself is served over https:// via Caddy, and browsers silently
	// block/warn on downloads that resolve to http from an https page. That
	// made every large-file download fail while small ones (served here)
	// worked fine. Streaming through the app avoids the mismatch entirely —
	// ServeContent doesn't buffer the object in memory either way (see
	// below), so this isn't a meaningful cost for a LAN tool.
	obj, err := h.minio.Download(r.Context(), f.MinioBucket, f.MinioKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýly alyp bolmady")
		return
	}
	defer obj.Close()

	// Determine if inline or attachment
	disposition := "attachment"
	ct := f.MimeType
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") ||
		ct == "application/pdf" || strings.HasPrefix(ct, "text/") {
		disposition = "inline"
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, f.Name))
	// obj is an io.ReadSeeker backed by MinIO — ServeContent seeks/streams
	// directly against it (Range requests included) instead of us buffering
	// the whole object in the process' memory first.
	http.ServeContent(w, r, f.Name, f.UpdatedAt, obj)
}

// isThumbnailableImage reports whether name is a format the stdlib image
// package can decode (jpeg/png/gif) — the only formats FileThumbnail will
// attempt to generate a preview for. Other "image" extensions the UI
// otherwise recognizes (webp, svg, bmp, tiff, ico) fall straight through to
// the frontend's generic-icon fallback instead of silently serving the full
// original as a "thumbnail".
func isThumbnailableImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif":
		return true
	}
	return false
}

// FileThumbnail serves a small cached JPEG preview of an image file instead
// of the full original — the file grid used to point straight at
// DownloadFile for thumbnails, which meant opening a folder full of photos
// fired off dozens of concurrent full-resolution downloads at once. The
// generated preview is cached in MinIO under a version-addressed key (see
// storage.ThumbnailKey), so a given file+version pair is decoded/resized
// exactly once no matter how many times it's viewed afterward, and the
// version-in-key scheme lets the response be marked immutable — the browser
// never has to re-request it either, as long as the frontend includes the
// file's version in the request URL (see gridCard/listRow in files.js).
func (h *Handler) FileThumbnail(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFile(id)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canAccess, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}
	if !isThumbnailableImage(f.Name) {
		writeError(w, http.StatusUnsupportedMediaType, "bu görnüş üçin kiçi surat ýok")
		return
	}

	key := storage.ThumbnailKey(f.ID, f.Version)
	writeThumbHeaders := func() {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+key+`"`)
	}

	// GetObjectInfo does a real HEAD-style stat, unlike Download/GetObject:
	// minio-go's GetObject returns an object handle lazily and doesn't
	// actually error until the first Read, so checking Download's error
	// alone would "succeed" even when nothing is cached yet — and then
	// silently write a truncated/empty body once io.Copy hit the real
	// error on its first read.
	if _, statErr := h.minio.GetObjectInfo(r.Context(), storage.ThumbnailBucket, key); statErr == nil {
		if cached, err := h.minio.Download(r.Context(), storage.ThumbnailBucket, key); err == nil {
			defer cached.Close()
			writeThumbHeaders()
			io.Copy(w, cached)
			return
		}
	}

	orig, err := h.minio.Download(r.Context(), f.MinioBucket, f.MinioKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	defer orig.Close()

	data, err := thumbnail.Generate(orig)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "kiçi surat döredip bolmady")
		return
	}

	if err := h.minio.EnsureBucket(r.Context(), storage.ThumbnailBucket); err != nil {
		log.Printf("thumbnail: ensure bucket: %v", err)
	} else if err := h.minio.Upload(r.Context(), storage.ThumbnailBucket, key, bytes.NewReader(data), int64(len(data)), "image/jpeg"); err != nil {
		log.Printf("thumbnail: cache write %s: %v", key, err)
	}

	writeThumbHeaders()
	w.Write(data)
}

func (h *Handler) RenameFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFile(id)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canEdit, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canEdit {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req models.RenameRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "at girizilmeli")
		return
	}

	newName := strings.TrimSpace(req.Name)
	if newName != f.Name {
		newName = uniqueFileName(h.db, newName, f.OwnerID, f.Scope, f.FolderID, f.ProjectID)
	}
	if err := h.db.RenameFile(id, newName); err != nil {
		writeError(w, http.StatusInternalServerError, "ady üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// MoveFile reassigns a file to a different folder within the same
// scope/project. Metadata-only (see db.MoveFile) — safe and cheap
// regardless of the file's size.
func (h *Handler) MoveFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFile(id)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canEdit, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canEdit {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req struct {
		FolderID *int `json:"folder_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	if req.FolderID != nil {
		destFolder, err := h.db.GetFolder(*req.FolderID)
		if err != nil || destFolder == nil {
			writeError(w, http.StatusNotFound, "bukja tapylmady")
			return
		}
		if destFolder.Scope != f.Scope || !intPtrEqual(destFolder.ProjectID, f.ProjectID) {
			writeError(w, http.StatusBadRequest, "faýly diňe şol bir ýerde göçürip bolýar")
			return
		}
		// Personal-scope folders belong to one person; a file individually
		// shared with edit access must never be relocatable into a
		// *different* user's personal folder tree via that share — it would
		// leave owner_id pointing at the original owner while folder_id
		// points into someone else's space, making the file invisible to
		// both (every personal-scope listing filters on owner_id) while
		// still occupying the original owner's quota and MinIO storage.
		if f.Scope == "personal" && destFolder.OwnerID != f.OwnerID {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
		if !h.canEditFolder(user, destFolder) {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
	}

	newName := f.Name
	if !intPtrEqual(f.FolderID, req.FolderID) {
		newName = uniqueFileName(h.db, f.Name, f.OwnerID, f.Scope, req.FolderID, f.ProjectID)
	}
	if err := h.db.MoveFile(id, newName, req.FolderID); err != nil {
		writeError(w, http.StatusInternalServerError, "göçürip bolmady")
		return
	}
	h.logAction(r, "file.move", "file", f.ID, f.Name, map[string]any{"folder_id": req.FolderID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFile(id)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canEdit, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canEdit {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Soft delete: move to trash instead of touching MinIO. Permanent
	// removal happens via /api/trash (manual purge) or the daily janitor
	// sweep 30 days later.
	if err := h.db.SoftDeleteFile(id); err != nil {
		writeError(w, http.StatusInternalServerError, "faýly pozup bolmady")
		return
	}
	h.logAction(r, "file.delete", "file", f.ID, f.Name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SearchFiles(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, []models.File{})
		return
	}
	files, err := h.db.SearchFiles(user.ID, user.Role == "admin", q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gözleg ýalňyşlygy")
		return
	}
	if files == nil {
		files = []models.File{}
	}
	writeJSON(w, http.StatusOK, files)
}

func (h *Handler) StorageUsage(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "personal"
	}
	var projectID *int
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		if n, err := strconv.Atoi(pid); err == nil {
			projectID = &n
		}
	}
	usage, err := h.db.GetStorageUsage(user.ID, scope, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ammar maglumatyny alyp bolmady")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (h *Handler) CreateBlankFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)

	var req models.CreateBlankFileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type != "docx" && req.Type != "xlsx" {
		writeError(w, http.StatusBadRequest, "nädogry faýl görnüşi (docx, xlsx)")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Täze dokument"
	}
	if !strings.HasSuffix(strings.ToLower(name), "."+req.Type) {
		name = name + "." + req.Type
	}

	scope := req.Scope
	if scope == "" {
		scope = "personal"
	}

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

	// Generate blank file content
	var fileBytes []byte
	var mimeType string
	var err error

	switch req.Type {
	case "docx":
		fileBytes, err = generateBlankDOCX()
		mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		fileBytes, err = generateBlankXLSX()
		mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýl döredip bolmady")
		return
	}

	// Check quota
	usage, err := h.db.GetStorageUsage(user.ID, scope, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ammar maglumatyny alyp bolmady")
		return
	}
	if usage.UsedBytes+int64(len(fileBytes)) > usage.QuotaBytes {
		writeError(w, http.StatusForbidden, "ammar doly, ýer ýok")
		return
	}

	if err := h.minio.EnsureBucket(r.Context(), bucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar döredip bolmady")
		return
	}

	// Auto-rename if duplicate name exists
	name = uniqueFileName(h.db, name, user.ID, scope, req.FolderID, projectID)

	key := fmt.Sprintf("%d/%s", user.ID, name)
	if req.FolderID != nil {
		key = fmt.Sprintf("%d/f%d/%s", user.ID, *req.FolderID, name)
	}

	reader := bytes.NewReader(fileBytes)
	if err := h.minio.Upload(r.Context(), bucket, key, reader, int64(len(fileBytes)), mimeType); err != nil {
		writeError(w, http.StatusInternalServerError, "faýly ýükläp bolmady")
		return
	}

	f := &models.File{
		Name:        name,
		MimeType:    mimeType,
		SizeBytes:   int64(len(fileBytes)),
		MinioBucket: bucket,
		MinioKey:    key,
		FolderID:    req.FolderID,
		OwnerID:     user.ID,
		ProjectID:   projectID,
		Scope:       scope,
	}
	if scope == "common" {
		f.Visibility = "common"
	}
	if err := h.db.CreateFile(f); err != nil {
		writeError(w, http.StatusInternalServerError, "faýl maglumatyny saklap bolmady")
		return
	}

	writeJSON(w, http.StatusCreated, f)
}

// generateBlankDOCX creates a minimal valid DOCX file
func generateBlankDOCX() ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t></w:t></w:r></w:p>
  </w:body>
</w:document>`

	files := map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels":         rels,
		"word/document.xml":   document,
	}

	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// generateBlankXLSX creates a minimal valid XLSX file using excelize, which
// already ships the full set of required OOXML parts (styles, core
// properties, etc.) — no need to hand-roll the archive ourselves.
func generateBlankXLSX() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Folders

// ListFolderTree returns every folder in a scope, flat (parent_id intact) —
// the frontend nests it client-side to render the "move to" picker.
func (h *Handler) ListFolderTree(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "personal"
	}
	var projectID *int
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		if n, err := strconv.Atoi(pid); err == nil {
			projectID = &n
		}
	}
	if scope == "project" {
		if projectID == nil {
			writeError(w, http.StatusBadRequest, "taslama saýlanmaly")
			return
		}
		if user.Role != "admin" {
			perm, err := h.db.GetProjectMemberPermission(*projectID, user.ID)
			if err != nil || perm == "" {
				writeError(w, http.StatusForbidden, "rugsat ýok")
				return
			}
		}
	}
	folders, err := h.db.ListAllFoldersInScope(user.ID, projectID, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bukjalary alyp bolmady")
		return
	}
	if folders == nil {
		folders = []models.Folder{}
	}
	writeJSON(w, http.StatusOK, folders)
}

func (h *Handler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	var req models.CreateFolderRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "bukja ady girizilmeli")
		return
	}

	scope := req.Scope
	if scope == "" {
		scope = "personal"
	}

	if scope == "project" && (req.ProjectID == nil || *req.ProjectID <= 0) {
		writeError(w, http.StatusBadRequest, "taslama saýlanmaly")
		return
	}
	if !h.canEditScope(user, scope, req.ProjectID) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}
	if !destFolderValid(h.db, req.ParentID, scope, req.ProjectID, user.ID) {
		writeError(w, http.StatusBadRequest, "bukja tapylmady")
		return
	}

	folder := &models.Folder{
		Name:     strings.TrimSpace(req.Name),
		ParentID: req.ParentID,
		OwnerID:  user.ID,
		Scope:    scope,
	}
	if scope == "project" {
		folder.ProjectID = req.ProjectID
	}

	if err := h.db.CreateFolder(folder); err != nil {
		writeError(w, http.StatusInternalServerError, "bukja döredip bolmady")
		return
	}
	writeJSON(w, http.StatusCreated, folder)
}

func (h *Handler) RenameFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	folder, err := h.db.GetFolder(id)
	if err != nil || folder == nil {
		writeError(w, http.StatusNotFound, "bukja tapylmady")
		return
	}
	if !h.canEditFolder(user, folder) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req models.RenameRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "at girizilmeli")
		return
	}

	if err := h.db.RenameFolder(id, strings.TrimSpace(req.Name)); err != nil {
		writeError(w, http.StatusInternalServerError, "ady üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// MoveFolder reassigns a folder to a different parent within the same
// scope/project, rejecting moves into itself or one of its own descendants.
func (h *Handler) MoveFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	folder, err := h.db.GetFolder(id)
	if err != nil || folder == nil {
		writeError(w, http.StatusNotFound, "bukja tapylmady")
		return
	}
	if !h.canEditFolder(user, folder) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req struct {
		ParentID *int `json:"parent_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	if req.ParentID != nil {
		if *req.ParentID == id {
			writeError(w, http.StatusBadRequest, "bukjany öz içine geçirip bolmaýar")
			return
		}
		destFolder, err := h.db.GetFolder(*req.ParentID)
		if err != nil || destFolder == nil {
			writeError(w, http.StatusNotFound, "bukja tapylmady")
			return
		}
		if destFolder.Scope != folder.Scope || !intPtrEqual(destFolder.ProjectID, folder.ProjectID) {
			writeError(w, http.StatusBadRequest, "bukjany diňe şol bir ýerde göçürip bolýar")
			return
		}
		// Same invariant as MoveFile: personal-scope folders belong to one
		// person, so — even for an admin, who canEditFolder always passes —
		// never let a personal folder be reparented under a DIFFERENT
		// owner's personal tree. ListFolders/ListAllFoldersInScope filter on
		// owner_id, so it would become invisible to both people while still
		// billed to the original owner's quota.
		if folder.Scope == "personal" && destFolder.OwnerID != folder.OwnerID {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
		if !h.canEditFolder(user, destFolder) {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
		descendantIDs, err := h.db.ListFolderAndDescendantIDs(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "barlap bolmady")
			return
		}
		for _, dID := range descendantIDs {
			if dID == *req.ParentID {
				writeError(w, http.StatusBadRequest, "bukjany öz aşaky bukjasyna geçirip bolmaýar")
				return
			}
		}
	}

	if err := h.db.MoveFolder(id, folder.Name, req.ParentID); err != nil {
		writeError(w, http.StatusInternalServerError, "göçürip bolmady")
		return
	}
	h.logAction(r, "folder.move", "folder", folder.ID, folder.Name, map[string]any{"parent_id": req.ParentID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	folder, err := h.db.GetFolder(id)
	if err != nil || folder == nil {
		writeError(w, http.StatusNotFound, "bukja tapylmady")
		return
	}
	if !h.canEditFolder(user, folder) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Soft delete: walk the whole subtree (this folder + nested folders) and
	// mark it and its files as trashed — folder_id is ON DELETE SET NULL, so
	// a plain DELETE would've silently orphaned the files instead of really
	// removing them, even though the UI tells the user "all files will be
	// deleted". Nothing touches MinIO here; permanent removal happens via
	// /api/trash (manual purge) or the daily janitor sweep 30 days later.
	folderIDs, err := h.db.ListFolderAndDescendantIDs(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bukjany pozup bolmady")
		return
	}
	if err := h.db.SoftDeleteFolderTree(folderIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "bukjany pozup bolmady")
		return
	}
	h.logAction(r, "folder.delete", "folder", folder.ID, folder.Name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DownloadFolder streams a folder (and everything nested under it) as a zip,
// built on the fly directly into the response — no temp file, no buffering
// the archive in memory first, same streaming principle as DownloadFile.
func (h *Handler) DownloadFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	folder, err := h.db.GetFolder(id)
	if err != nil || folder == nil {
		writeError(w, http.StatusNotFound, "bukja tapylmady")
		return
	}
	if !h.canViewFolder(user, folder) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	zipName := folder.Name + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))

	zw := zip.NewWriter(w)
	defer zw.Close()
	// No wrapping directory: the folder's own contents go straight into the
	// zip root, since it's the only thing in this archive.
	if err := h.writeFolderTree(r.Context(), zw, folder, ""); err != nil {
		log.Printf("folder download: %v", err)
	}
}

// writeFolderTree streams every file nested under folder (recursively) into
// zw, using zip.Store — studio files are overwhelmingly already-compressed
// CAD/image/render data, so skipping DEFLATE avoids burning CPU for no space
// savings on what's likely to be a very large folder. Every entry's path is
// prefixed with rootPrefix: empty when the folder is the only thing in the
// zip (DownloadFolder), or the folder's own name when it's one of several
// files/folders bundled together (BulkDownload) — otherwise two folders in
// the same bulk selection sharing a file name would collide.
func (h *Handler) writeFolderTree(ctx context.Context, zw *zip.Writer, folder *models.Folder, rootPrefix string) error {
	folderIDs, err := h.db.ListFolderAndDescendantIDs(folder.ID)
	if err != nil {
		return fmt.Errorf("list folder tree: %w", err)
	}
	allFolders, err := h.db.GetFoldersByIDs(folderIDs)
	if err != nil {
		return fmt.Errorf("get folders: %w", err)
	}
	files, err := h.db.ListFilesInFolders(folderIDs)
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	// Every id in folderIDs is folder.ID itself or one of its descendants, so
	// its whole ancestor chain up to folder.ID is guaranteed to be in
	// allFolders too — resolving in repeated passes handles allFolders
	// arriving in any order (a child can appear before its parent) without
	// needing a sort first.
	relPath := map[int]string{folder.ID: rootPrefix}
	remaining := make([]models.Folder, 0, len(allFolders))
	for _, f := range allFolders {
		if f.ID != folder.ID {
			remaining = append(remaining, f)
		}
	}
	for len(remaining) > 0 {
		next := remaining[:0]
		progressed := false
		for _, f := range remaining {
			if f.ParentID == nil {
				continue // can't happen for a real descendant of folder.ID; drop defensively
			}
			parentPath, ok := relPath[*f.ParentID]
			if !ok {
				next = append(next, f)
				continue
			}
			relPath[f.ID] = path.Join(parentPath, f.Name)
			progressed = true
		}
		remaining = next
		if !progressed {
			break
		}
	}

	// Explicit directory entries so empty subfolders still show up once
	// extracted, even though they carry no files of their own.
	for fID, rp := range relPath {
		if fID == folder.ID && rootPrefix == "" {
			continue // the root folder itself isn't a path inside its own zip
		}
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: rp + "/", Method: zip.Store}); err != nil {
			log.Printf("zip folder tree: create dir entry %s: %v", rp, err)
		}
	}

	for _, f := range files {
		if f.FolderID == nil {
			continue
		}
		parentPath, ok := relPath[*f.FolderID]
		if !ok {
			continue
		}
		entryName := path.Join(parentPath, f.Name)
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Store, Modified: f.UpdatedAt})
		if err != nil {
			log.Printf("zip folder tree: create entry %s: %v", entryName, err)
			continue
		}
		obj, err := h.minio.Download(ctx, f.MinioBucket, f.MinioKey)
		if err != nil {
			log.Printf("zip folder tree: open %s/%s: %v", f.MinioBucket, f.MinioKey, err)
			continue
		}
		if _, err := io.Copy(fw, obj); err != nil {
			log.Printf("zip folder tree: copy %s: %v", entryName, err)
		}
		obj.Close()
	}
	return nil
}

// BulkDownload zips an arbitrary set of files and/or folders together —
// exactly what the files-page multi-select bulk action bar needs, since
// downloading each selected item as its own separate browser download was
// clunky for anything more than a couple of files. IDs arrive as repeated
// query params (?file_id=1&file_id=2&folder_id=7) rather than a JSON body so
// the frontend can still trigger it with a plain same-origin <a download>
// link — consistent with every other download endpoint in this file — and
// never has to buffer the response as a blob in page memory.
func (h *Handler) BulkDownload(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	isAdmin := user.Role == "admin"
	fileIDs := parseIDList(r.URL.Query()["file_id"])
	folderIDs := parseIDList(r.URL.Query()["folder_id"])
	if len(fileIDs) == 0 && len(folderIDs) == 0 {
		writeError(w, http.StatusBadRequest, "hiç zat saýlanmady")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="paylash-files.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()

	// Top-level entries (loose files, and each selected folder's own name)
	// share one namespace in this combined zip, unlike a single-folder
	// download — dedup so e.g. two folders both containing "render.png"
	// don't collide, the same way uniqueFileName avoids upload collisions.
	used := map[string]int{}
	uniqueName := func(name string) string {
		used[name]++
		if used[name] == 1 {
			return name
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		return fmt.Sprintf("%s (%d)%s", base, used[name]-1, ext)
	}

	for _, id := range fileIDs {
		f, err := h.db.GetFile(id)
		if err != nil || f == nil {
			continue
		}
		if ok, _ := h.db.CanAccessFile(f.ID, user.ID, isAdmin, "view"); !ok {
			continue
		}
		entryName := uniqueName(f.Name)
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Store, Modified: f.UpdatedAt})
		if err != nil {
			log.Printf("bulk download: create entry %s: %v", entryName, err)
			continue
		}
		obj, err := h.minio.Download(r.Context(), f.MinioBucket, f.MinioKey)
		if err != nil {
			log.Printf("bulk download: open %s/%s: %v", f.MinioBucket, f.MinioKey, err)
			continue
		}
		if _, err := io.Copy(fw, obj); err != nil {
			log.Printf("bulk download: copy %s: %v", entryName, err)
		}
		obj.Close()
	}

	for _, id := range folderIDs {
		folder, err := h.db.GetFolder(id)
		if err != nil || folder == nil {
			continue
		}
		if !h.canViewFolder(user, folder) {
			continue
		}
		if err := h.writeFolderTree(r.Context(), zw, folder, uniqueName(folder.Name)); err != nil {
			log.Printf("bulk download: folder %d: %v", folder.ID, err)
		}
	}
}

// parseIDList parses a slice of decimal-string query values into ints,
// silently dropping anything that doesn't parse — a stray malformed id
// shouldn't fail the whole bulk request when the rest are valid.
func parseIDList(vals []string) []int {
	ids := make([]int, 0, len(vals))
	for _, v := range vals {
		if n, err := strconv.Atoi(v); err == nil {
			ids = append(ids, n)
		}
	}
	return ids
}

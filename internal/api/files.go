package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"paylash/internal/authutil"
	"paylash/internal/db"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// largeDownloadThreshold is the size at/above which DownloadFile redirects
// to a presigned MinIO URL instead of streaming through the app process —
// the download counterpart to Uploader.LARGE_FILE_THRESHOLD in
// web/js/upload.js, so big transfers bypass the server in both directions.
const largeDownloadThreshold = 50 << 20

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
	writeJSON(w, http.StatusOK, models.FileListResponse{Files: files, Folders: folders})
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

	// Large files: redirect to a short-lived presigned MinIO URL so the
	// bytes never transit through this process at all — symmetric with how
	// large uploads already bypass the server. It's a browser navigation
	// (window.open/<img>/<video> src), not a fetch, so this works with no
	// CORS involvement; PreviewPage's fetch() path for text files is
	// already covered by the MinIO CORS config the upload feature needs.
	// Small files, and any file when the public endpoint isn't configured,
	// keep streaming straight through here.
	if f.SizeBytes >= largeDownloadThreshold && h.minio.PublicEndpointConfigured() {
		url, err := h.minio.PresignDownload(r.Context(), f.MinioBucket, f.MinioKey, f.Name, 15*time.Minute)
		if err == nil {
			http.Redirect(w, r, url, http.StatusFound)
			return
		}
		// fall through to the proxied download below if presigning fails
	}

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
	if err := h.db.SoftDeleteFilesInFolders(folderIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "bukjany pozup bolmady")
		return
	}
	if err := h.db.SoftDeleteFolders(folderIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "bukjany pozup bolmady")
		return
	}
	h.logAction(r, "folder.delete", "folder", folder.ID, folder.Name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

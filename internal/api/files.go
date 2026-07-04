package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"paylash/internal/authutil"
	"paylash/internal/db"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
	"strings"
	"time"
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

// canEditScope reports whether the user may create new content in the given
// scope (upload / create folder). "personal" is always the caller's own
// space here, so it's trivially allowed — this must NOT be used to check
// permission on an already-existing resource that might belong to someone
// else (use canEditFolder for that).
func (h *Handler) canEditScope(user *models.User, scope string, projectID *int) bool {
	if user.Role == "admin" {
		return true
	}
	switch scope {
	case "common", "personal":
		return true
	case "project":
		if projectID == nil {
			return false
		}
		perm, err := h.db.GetProjectMemberPermission(*projectID, user.ID)
		return err == nil && perm == "edit"
	}
	return false
}

// canEditFolder reports whether the user may rename/delete an existing folder.
// Unlike canEditScope, "personal" here requires actual ownership of the folder.
func (h *Handler) canEditFolder(user *models.User, folder *models.Folder) bool {
	if user.Role == "admin" {
		return true
	}
	switch folder.Scope {
	case "personal":
		return folder.OwnerID == user.ID
	case "common":
		return true
	case "project":
		if folder.ProjectID == nil {
			return false
		}
		perm, err := h.db.GetProjectMemberPermission(*folder.ProjectID, user.ID)
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

	files, err := h.db.ListFiles(user.ID, projectID, scope, folderID, sort, order)
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

	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB max
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

	obj, err := h.minio.Download(r.Context(), f.MinioBucket, f.MinioKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýly alyp bolmady")
		return
	}
	defer obj.Close()

	// Read full content for http.ServeContent (supports Range requests for video/audio)
	data, err := io.ReadAll(obj)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýly okap bolmady")
		return
	}

	// Determine if inline or attachment
	disposition := "attachment"
	ct := f.MimeType
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") ||
		ct == "application/pdf" || strings.HasPrefix(ct, "text/") {
		disposition = "inline"
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, f.Name))
	http.ServeContent(w, r, f.Name, time.Now(), bytes.NewReader(data))
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

	if err := h.db.RenameFile(id, strings.TrimSpace(req.Name)); err != nil {
		writeError(w, http.StatusInternalServerError, "ady üýtgedip bolmady")
		return
	}
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

	if err := h.minio.Delete(r.Context(), f.MinioBucket, f.MinioKey); err != nil {
		writeError(w, http.StatusInternalServerError, "faýly pozup bolmady")
		return
	}
	if err := h.db.DeleteFile(id); err != nil {
		writeError(w, http.StatusInternalServerError, "faýl maglumatyny pozup bolmady")
		return
	}
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

// generateBlankXLSX creates a minimal valid XLSX file using excelize
func generateBlankXLSX() ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

	workbookRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`

	workbook := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets>
</workbook>`

	sheet := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData/>
</worksheet>`

	files := map[string]string{
		"[Content_Types].xml":       contentTypes,
		"_rels/.rels":               rels,
		"xl/_rels/workbook.xml.rels": workbookRels,
		"xl/workbook.xml":           workbook,
		"xl/worksheets/sheet1.xml":  sheet,
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

// Folders

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

	if err := h.db.DeleteFolder(id); err != nil {
		writeError(w, http.StatusInternalServerError, "bukjany pozup bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

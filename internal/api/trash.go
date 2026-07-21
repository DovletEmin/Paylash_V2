package api

import (
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
)

// resolveRestoreParent decides what folder/parent id a restored item should
// end up under: id unchanged if it's set and not itself still trashed,
// otherwise nil (the scope root) — restoring an item must never leave it
// pointing at a folder the user can't see because it's still in the trash.
func resolveRestoreParent(id *int, trashed bool) *int {
	if id == nil || trashed {
		return nil
	}
	return id
}

// ListTrash returns the caller's own trashed files/folders — or every
// trashed item, for an admin.
func (h *Handler) ListTrash(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	isAdmin := user.Role == "admin"

	files, err := h.db.ListTrashedFiles(user.ID, isAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýllary alyp bolmady")
		return
	}
	folders, err := h.db.ListTrashedFolders(user.ID, isAdmin)
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

func (h *Handler) RestoreFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFileIncludingTrash(id)
	if err != nil || f == nil || f.DeletedAt == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canEdit, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canEdit {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// If the file's folder is still trashed, restore it to the scope root
	// instead of leaving it pointing at a folder the user can't see.
	folderID := f.FolderID
	if folderID != nil {
		trashed, err := h.db.IsFolderTrashed(*folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "dikeltip bolmady")
			return
		}
		folderID = resolveRestoreParent(folderID, trashed)
	}
	name := uniqueFileName(h.db, f.Name, f.OwnerID, f.Scope, folderID, f.ProjectID)

	if err := h.db.RestoreFile(id, folderID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "dikeltip bolmady")
		return
	}
	h.logAction(r, "file.restore", "file", f.ID, name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) RestoreFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	folder, err := h.db.GetFolderIncludingTrash(id)
	if err != nil || folder == nil || folder.DeletedAt == nil {
		writeError(w, http.StatusNotFound, "bukja tapylmady")
		return
	}
	if !h.canEditFolder(user, folder) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	parentID := folder.ParentID
	if parentID != nil {
		trashed, err := h.db.IsFolderTrashed(*parentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "dikeltip bolmady")
			return
		}
		parentID = resolveRestoreParent(parentID, trashed)
	}

	if err := h.db.RestoreFolder(id, parentID); err != nil {
		writeError(w, http.StatusInternalServerError, "dikeltip bolmady")
		return
	}
	h.logAction(r, "folder.restore", "folder", folder.ID, folder.Name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) PurgeFile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	f, err := h.db.GetFileIncludingTrash(id)
	if err != nil || f == nil || f.DeletedAt == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	canEdit, err := h.db.CanAccessFile(f.ID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canEdit {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	if err := h.minio.Delete(r.Context(), f.MinioBucket, f.MinioKey); err != nil {
		log.Printf("purge object %s/%s: %v", f.MinioBucket, f.MinioKey, err)
	}
	h.purgeThumbnail(r, f.ID, f.Version)
	if err := h.db.PurgeFiles([]int{id}); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	h.logAction(r, "file.purge", "file", f.ID, f.Name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) PurgeFolder(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	folder, err := h.db.GetFolderIncludingTrash(id)
	if err != nil || folder == nil || folder.DeletedAt == nil {
		writeError(w, http.StatusNotFound, "bukja tapylmady")
		return
	}
	if !h.canEditFolder(user, folder) {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	if err := h.purgeFolderTree(r, id); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	h.logAction(r, "folder.purge", "folder", folder.ID, folder.Name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// EmptyTrash permanently purges every trashed file/folder the caller owns.
// Scoped to the caller regardless of role — emptying trash is a personal
// action, not an admin superpower that wipes everyone else's at once.
func (h *Handler) EmptyTrash(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)

	files, err := h.db.ListTrashedFiles(user.ID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	folders, err := h.db.ListTrashedFolders(user.ID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}

	// Purge root folders individually (each walks and purges its own
	// subtree); a nested folder that's also independently listed here gets
	// skipped the second time since its rows are already gone by then.
	for _, folder := range folders {
		if err := h.purgeFolderTree(r, folder.ID); err != nil {
			log.Printf("empty trash: purge folder %d: %v", folder.ID, err)
		}
	}
	for _, f := range files {
		if err := h.minio.Delete(r.Context(), f.MinioBucket, f.MinioKey); err != nil {
			log.Printf("empty trash: purge object %s/%s: %v", f.MinioBucket, f.MinioKey, err)
		}
		h.purgeThumbnail(r, f.ID, f.Version)
	}
	var fileIDs []int
	for _, f := range files {
		fileIDs = append(fileIDs, f.ID)
	}
	if err := h.db.PurgeFiles(fileIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	h.logAction(r, "trash.empty", "user", user.ID, user.Username, map[string]any{
		"files_purged": len(fileIDs), "folders_purged": len(folders),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// purgeFolderTree permanently removes a trashed folder and everything still
// nested under it: MinIO objects for every contained file, then the file
// rows, then the folder rows themselves.
func (h *Handler) purgeFolderTree(r *http.Request, folderID int) error {
	folderIDs, err := h.db.ListFolderAndDescendantIDs(folderID)
	if err != nil {
		return err
	}
	files, err := h.db.ListFilesInFolders(folderIDs)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := h.minio.Delete(r.Context(), f.MinioBucket, f.MinioKey); err != nil {
			log.Printf("purge object %s/%s: %v", f.MinioBucket, f.MinioKey, err)
		}
		h.purgeThumbnail(r, f.ID, f.Version)
	}
	if err := h.db.DeleteFilesInFolders(folderIDs); err != nil {
		return err
	}
	return h.db.PurgeFolders(folderIDs)
}

// purgeThumbnail best-effort deletes a file's cached thumbnail (if any) at
// its current version. A missing object is not an error; failures here are
// logged, not surfaced — they'd otherwise block a permanent-delete that has
// already succeeded on the parts that actually matter (the file's own data).
func (h *Handler) purgeThumbnail(r *http.Request, fileID, version int) {
	key := storage.ThumbnailKey(fileID, version)
	if err := h.minio.Delete(r.Context(), storage.ThumbnailBucket, key); err != nil {
		log.Printf("purge thumbnail %s: %v", key, err)
	}
}

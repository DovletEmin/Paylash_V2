package api

import (
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"strconv"
	"strings"
)

// canManageSharing decides who may create/revoke/update a file's shares,
// toggle public sharing, or view its share list. Deliberately stricter than
// plain content-edit access for personal-scope files: someone given edit
// access to one colleague's personal file should be able to edit its
// content, not unilaterally invite third parties, revoke the owner's other
// shares, or make it company-wide public — that stays the owner's (or
// admin's) call. Project/common scope keep the same collaborative model
// already established for rename/delete there (any project editor, anyone
// at all for common), since those are vetted shared workspaces rather than
// one person's private space.
func canManageSharingWith(lookup projectPermLookup, role string, userID int, f *models.File) (bool, error) {
	if role == "admin" || f.OwnerID == userID {
		return true, nil
	}
	switch f.Scope {
	case "common":
		return true, nil
	case "project":
		if f.ProjectID == nil {
			return false, nil
		}
		perm, err := lookup.GetProjectMemberPermission(*f.ProjectID, userID)
		return err == nil && perm == "edit", err
	}
	return f.Visibility == "common", nil
}

func (h *Handler) canManageSharing(user *models.User, f *models.File) (bool, error) {
	return canManageSharingWith(h.db, user.Role, user.ID, f)
}

func (h *Handler) ShareFile(w http.ResponseWriter, r *http.Request) {
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

	if canManage, err := h.canManageSharing(user, f); err != nil || !canManage {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req models.ShareRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	if req.Permission == "" {
		req.Permission = "view"
	}
	if req.Permission != "view" && req.Permission != "edit" {
		writeError(w, http.StatusBadRequest, "rugsat 'view' ýa-da 'edit' bolmaly")
		return
	}

	share, err := h.db.CreateShare(fileID, user.ID, req.UserID, req.Permission, req.IsPublic)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "paýlaşyp bolmady")
		return
	}
	h.logAction(r, "share.create", "file", fileID, f.Name, map[string]any{
		"shared_with": req.UserID, "permission": req.Permission, "is_public": req.IsPublic,
	})
	writeJSON(w, http.StatusCreated, share)
}

func (h *Handler) DeleteShare(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	sharedWithID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	if canManage, err := h.canManageSharing(user, f); err != nil || !canManage {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	if err := h.db.DeleteShare(fileID, sharedWithID); err != nil {
		writeError(w, http.StatusInternalServerError, "paýlaşmagy aýyryp bolmady")
		return
	}
	h.logAction(r, "share.revoke", "file", fileID, f.Name, map[string]any{"shared_with": sharedWithID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) UpdateSharePermission(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	sharedWithID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}
	if canManage, err := h.canManageSharing(user, f); err != nil || !canManage {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req struct {
		Permission string `json:"permission"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.Permission != "view" && req.Permission != "edit" {
		writeError(w, http.StatusBadRequest, "rugsat 'view' ýa-da 'edit' bolmaly")
		return
	}

	if err := h.db.UpdateSharePermission(fileID, sharedWithID, req.Permission); err != nil {
		writeError(w, http.StatusInternalServerError, "rugsady üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "permission": req.Permission})
}

func (h *Handler) SetPublicShare(w http.ResponseWriter, r *http.Request) {
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
	if canManage, err := h.canManageSharing(user, f); err != nil || !canManage {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req struct {
		IsPublic bool `json:"is_public"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	if err := h.db.SetPublicShare(fileID, user.ID, req.IsPublic); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SetVisibility(w http.ResponseWriter, r *http.Request) {
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
	if f.OwnerID != user.ID && user.Role != "admin" {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}
	var req models.VisibilityRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.Visibility != "private" && req.Visibility != "common" {
		writeError(w, http.StatusBadRequest, "visibility 'private' ýa-da 'common' bolmaly")
		return
	}
	// Only admin can broadcast a personal file to the whole company
	if req.Visibility == "common" && user.Role != "admin" {
		writeError(w, http.StatusForbidden, "diňe admin görnüşi üýtgedip biler")
		return
	}
	if err := h.db.SetFileVisibility(fileID, req.Visibility); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "visibility": req.Visibility})
}

func (h *Handler) SharedWithMe(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	list, err := h.db.GetSharedWithMe(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "paýlaşylan faýllary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.SharedFileView{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) SharedByMe(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	list, err := h.db.GetSharedByMe(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "paýlaşylan faýllary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.SharedByMeView{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) GetSharesForFile(w http.ResponseWriter, r *http.Request) {
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
	if canManage, err := h.canManageSharing(user, f); err != nil || !canManage {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	shares, err := h.db.GetSharesForFile(fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "paýlaşma maglumatyny alyp bolmady")
		return
	}
	if shares == nil {
		shares = []models.ShareView{}
	}
	writeJSON(w, http.StatusOK, shares)
}

// SearchUsers backs the share modal's recipient picker. An empty q is not a
// no-op: SearchUsers's ILIKE '%%' matches everyone, which is exactly what
// powers the "browse all people" dropdown (focusing the empty search field)
// alongside the existing type-to-filter behavior.
func (h *Handler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, err := h.db.SearchUsers(q, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gözleg ýalňyşlygy")
		return
	}
	if results == nil {
		results = []models.UserSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

// Collabora editor URL
func (h *Handler) CollaboraEditorURL(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.URL.Query().Get("file_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry faýl ID")
		return
	}

	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		writeError(w, http.StatusNotFound, "faýl tapylmady")
		return
	}

	isAdmin := user.Role == "admin"
	canAccess, err := h.db.CanAccessFile(fileID, user.ID, isAdmin, "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Determine permission
	perm := "view"
	isPDF := strings.HasSuffix(strings.ToLower(f.Name), ".pdf") || f.MimeType == "application/pdf"
	if !isPDF {
		canEdit, _ := h.db.CanAccessFile(fileID, user.ID, isAdmin, "edit")
		if canEdit {
			perm = "edit"
		}
	}

	token, err := h.db.CreateWOPIToken(fileID, user.ID, perm)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token döredip bolmady")
		return
	}

	// Build Collabora URL — WOPI src needs to point back to our server from Collabora's perspective
	wopiSrc := h.cfg.BaseURL + "/wopi/files/" + strconv.Itoa(fileID)
	editorURL := h.cfg.CollaboraURL + "/browser/dist/cool.html?WOPISrc=" + wopiSrc + "&access_token=" + token.Token

	writeJSON(w, http.StatusOK, map[string]string{
		"editor_url": editorURL,
		"token":      token.Token,
	})
}

// ListMyProjects returns the projects the current employee can access
// (with their permission on each) — used to render the sidebar.
func (h *Handler) ListMyProjects(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	list, err := h.db.ListProjectsForUser(user.ID, user.Role == "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "taslamalary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.ProjectView{}
	}
	writeJSON(w, http.StatusOK, list)
}

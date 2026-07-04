package api

import (
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"strconv"
	"strings"
)

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

	canAccess, err := h.db.CanAccessFile(fileID, user.ID, user.Role == "admin", "edit")
	if err != nil || !canAccess {
		if f.OwnerID != user.ID {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
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
	if f.OwnerID != user.ID && user.Role != "admin" {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	if err := h.db.DeleteShare(fileID, sharedWithID); err != nil {
		writeError(w, http.StatusInternalServerError, "paýlaşmagy aýyryp bolmady")
		return
	}
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
	if f.OwnerID != user.ID && user.Role != "admin" {
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
	if f.OwnerID != user.ID && user.Role != "admin" {
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
	if f.OwnerID != user.ID && user.Role != "admin" {
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

func (h *Handler) SearchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []models.UserSearchResult{})
		return
	}
	results, err := h.db.SearchUsers(q, 20)
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

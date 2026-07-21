package api

import (
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"strconv"
	"strings"
)

// maxCommentLength keeps a comment to a reasonable review-note size — this
// is meant for "third window from the left needs to be wider", not essays.
const maxCommentLength = 2000

func (h *Handler) ListFileComments(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	canAccess, err := h.db.CanAccessFile(fileID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	comments, err := h.db.ListComments(fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teswirleri alyp bolmady")
		return
	}
	if comments == nil {
		comments = []models.FileComment{}
	}
	writeJSON(w, http.StatusOK, comments)
}

func (h *Handler) CreateFileComment(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	fileID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	// Commenting is a view-level action — anyone who can see the file
	// (owner, project/common member, or someone it was shared with) should
	// be able to leave feedback on it, not just people with edit rights.
	canAccess, err := h.db.CanAccessFile(fileID, user.ID, user.Role == "admin", "view")
	if err != nil || !canAccess {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req struct {
		Body string   `json:"body"`
		XPct *float64 `json:"x_pct"`
		YPct *float64 `json:"y_pct"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "teswir boş bolup bilmez")
		return
	}
	if len(body) > maxCommentLength {
		writeError(w, http.StatusBadRequest, "teswir gaty uzyn")
		return
	}
	// A pin only makes sense as a point strictly inside the image — reject
	// anything outside 0–100% rather than silently clamping it, so a client
	// bug placing a pin off-canvas is visible instead of just looking odd.
	if req.XPct != nil && (*req.XPct < 0 || *req.XPct > 100) {
		writeError(w, http.StatusBadRequest, "nädogry pozisiýa")
		return
	}
	if req.YPct != nil && (*req.YPct < 0 || *req.YPct > 100) {
		writeError(w, http.StatusBadRequest, "nädogry pozisiýa")
		return
	}
	// A pin needs both coordinates or neither — one without the other can't
	// be placed on the image.
	if (req.XPct == nil) != (req.YPct == nil) {
		writeError(w, http.StatusBadRequest, "nädogry pozisiýa")
		return
	}

	c, err := h.db.CreateComment(fileID, user.ID, body, req.XPct, req.YPct)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "teswir goşup bolmady")
		return
	}
	if user.DisplayName != "" {
		c.UserName = user.DisplayName
	} else {
		c.UserName = user.Username
	}
	writeJSON(w, http.StatusCreated, c)
}

// DeleteFileComment allows the comment's own author, the file's owner, or an
// admin to remove it — the same "who gets to moderate feedback here" circle
// as everywhere else review-adjacent in this app.
func (h *Handler) DeleteFileComment(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	commentID, err := strconv.Atoi(r.PathValue("commentId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}

	comment, err := h.db.GetComment(commentID)
	if err != nil || comment == nil {
		writeError(w, http.StatusNotFound, "teswir tapylmady")
		return
	}

	isAdmin := user.Role == "admin"
	if comment.UserID != user.ID && !isAdmin {
		f, err := h.db.GetFile(comment.FileID)
		if err != nil || f == nil || f.OwnerID != user.ID {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
	}

	if err := h.db.DeleteComment(commentID); err != nil {
		writeError(w, http.StatusInternalServerError, "teswiri pozup bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

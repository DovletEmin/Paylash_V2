package api

import (
	"net/http"
	"strconv"
	"strings"

	"paylash/internal/authutil"
	"paylash/internal/db"
	"paylash/internal/models"
)

// maxAppPasswords caps how many device credentials one account may hold.
// Each one is a standing key to that person's files, so an account quietly
// accumulating dozens of them is a sign something is wrong, not a workflow.
const maxAppPasswords = 10

func (h *Handler) ListAppPasswords(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	list, err := h.db.ListAppPasswords(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "açarlary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.AppPassword{}
	}
	writeJSON(w, http.StatusOK, list)
}

// CreateAppPassword mints a credential for one device. The token is
// returned exactly once, in this response, and only its hash is stored —
// so a lost one is replaced, never recovered.
func (h *Handler) CreateAppPassword(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)

	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "at boş bolup bilmez")
		return
	}
	if len(name) > 100 {
		name = name[:100]
	}

	existing, err := h.db.ListAppPasswords(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "açarlary alyp bolmady")
		return
	}
	if len(existing) >= maxAppPasswords {
		writeError(w, http.StatusBadRequest, "açarlar gaty köp, köneleri pozuň")
		return
	}

	token, hash := db.NewAppPasswordToken()
	p, err := h.db.CreateAppPassword(user.ID, name, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "açar döredip bolmady")
		return
	}
	h.logAction(r, "app_password.create", "user", user.ID, name, nil)

	writeJSON(w, http.StatusCreated, map[string]any{
		"app_password": p,
		"token":        token,
		"username":     user.Username,
	})
}

func (h *Handler) DeleteAppPassword(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	// Scoped to the caller in SQL, so an id belonging to somebody else
	// simply matches nothing rather than needing a separate ownership check.
	if err := h.db.DeleteAppPassword(user.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "açary pozup bolmady")
		return
	}
	h.logAction(r, "app_password.delete", "user", user.ID, "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

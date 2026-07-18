package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// wouldRemoveLastAdmin reports whether changing targetRole to newRole would
// leave the studio with zero admins (and everyone locked out of the admin
// panel, with no way back in short of a manual DB edit).
func wouldRemoveLastAdmin(targetRole, newRole string, adminCount int) bool {
	return targetRole == "admin" && newRole != "admin" && adminCount <= 1
}

// AdminAuditLog returns admin-oversight events, newest first, optionally
// filtered by ?action= and/or ?actor_id=.
func (h *Handler) AdminAuditLog(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r, 50, 500)
	action := r.URL.Query().Get("action")
	actorID, _ := strconv.Atoi(r.URL.Query().Get("actor_id"))

	entries, err := h.db.ListAuditLog(limit, offset, action, actorID)
	if err != nil {
		log.Printf("list audit log: %v", err)
		writeError(w, http.StatusInternalServerError, "maglumat alyp bolmady")
		return
	}
	if entries == nil {
		entries = []models.AuditLogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// Admin Dashboard
func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	dash, err := h.db.GetDashboard()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "maglumat alyp bolmady")
		return
	}
	writeJSON(w, http.StatusOK, dash)
}

// Projects (admin-managed shared folders with an employee ACL)

func (h *Handler) AdminListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.ListAllProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "taslamalary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.Project{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) AdminCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		QuotaBytes int64  `json:"quota_bytes"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "taslama ady girizilmeli")
		return
	}
	if req.QuotaBytes <= 0 {
		req.QuotaBytes = 5 * 1024 * 1024 * 1024 // 5 GB
	}
	p, err := h.db.CreateProject(strings.TrimSpace(req.Name), req.QuotaBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "taslama döredip bolmady")
		return
	}
	if err := h.minio.EnsureBucket(r.Context(), storage.ProjectBucket(p.ID)); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar döredip bolmady")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) AdminUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	var req struct {
		Name       string `json:"name"`
		QuotaBytes int64  `json:"quota_bytes"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "at girizilmeli")
		return
	}
	if err := h.db.UpdateProject(id, strings.TrimSpace(req.Name), req.QuotaBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminDeleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	h.abortProjectUploadSessions(r.Context(), id)
	if err := h.db.DeleteProject(id); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	// Best-effort: the project's bucket is exclusively its own, so once the
	// DB rows are gone it's safe to wipe the bucket wholesale. A failure
	// here leaves an orphaned bucket but doesn't affect app correctness —
	// log it rather than failing a delete that already succeeded in the DB.
	bucket := storage.ProjectBucket(id)
	if err := h.minio.RemoveBucketAndAllObjects(r.Context(), bucket); err != nil {
		log.Printf("cleanup project bucket %s: %v", bucket, err)
	}
	h.logAction(r, "project.delete", "project", id, "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Project members — grant/revoke individual employees access to a project folder.

func (h *Handler) AdminListProjectMembers(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	list, err := h.db.ListProjectMembers(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gatnaşyjylary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.ProjectMemberView{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) AdminAddProjectMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	var req struct {
		UserID     int    `json:"user_id"`
		Permission string `json:"permission"`
	}
	if err := readJSON(r, &req); err != nil || req.UserID == 0 {
		writeError(w, http.StatusBadRequest, "işgär saýlanmaly")
		return
	}
	if req.Permission != "edit" {
		req.Permission = "view"
	}
	m, err := h.db.AddProjectMember(id, req.UserID, req.Permission)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "goşup bolmady")
		return
	}
	h.logAction(r, "project.member.add", "project", id, "", map[string]any{"user_id": req.UserID, "permission": req.Permission})
	writeJSON(w, http.StatusCreated, m)
}

func (h *Handler) AdminUpdateProjectMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	userID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}
	var req struct {
		Permission string `json:"permission"`
	}
	if err := readJSON(r, &req); err != nil || (req.Permission != "view" && req.Permission != "edit") {
		writeError(w, http.StatusBadRequest, "rugsat 'view' ýa-da 'edit' bolmaly")
		return
	}
	if err := h.db.UpdateProjectMemberPermission(id, userID, req.Permission); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	h.logAction(r, "project.member.update", "project", id, "", map[string]any{"user_id": userID, "permission": req.Permission})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminRemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	userID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}
	if err := h.db.RemoveProjectMember(id, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "aýyryp bolmady")
		return
	}
	h.logAction(r, "project.member.remove", "project", id, "", map[string]any{"user_id": userID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Users management
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	// The admin table does its own full-list client-side search (AdminPage.filterUsers),
	// so the default page is generous — realistic employee counts stay well under it —
	// with a hard cap only to guard against a pathological unbounded query.
	limit, offset := parsePagination(r, 500, 2000)
	users, err := h.db.ListUsers(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ulanyjylary alyp bolmady")
		return
	}
	if users == nil {
		users = []models.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	var req struct {
		Role        string `json:"role"`
		QuotaBytes  int64  `json:"quota_bytes"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.Role != "user" && req.Role != "admin" {
		req.Role = "user"
	}
	if target, _ := h.db.GetUserByID(id); target != nil {
		if n, err := h.db.CountAdmins(); err == nil && wouldRemoveLastAdmin(target.Role, req.Role, n) {
			writeError(w, http.StatusConflict, "soňky admini adaty ulanyja öwrüp bolmaýar")
			return
		}
	}
	var hash string
	if req.Password != "" {
		if len(req.Password) < 6 {
			writeError(w, http.StatusBadRequest, "parol azyndan 6 simwol bolmaly")
			return
		}
		h2, err := authutil.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
			return
		}
		hash = h2
	}
	if err := h.db.UpdateUser(id, req.Role, req.QuotaBytes, req.DisplayName, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	h.logAction(r, "user.update", "user", id, req.DisplayName, map[string]any{
		"role": req.Role, "quota_bytes": req.QuotaBytes, "password_changed": req.Password != "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	actor := authutil.GetUser(r)
	target, _ := h.db.GetUserByID(id)
	if target != nil {
		if n, err := h.db.CountAdmins(); err == nil && wouldRemoveLastAdmin(target.Role, "user", n) {
			writeError(w, http.StatusConflict, "soňky admini pozup bolmaýar")
			return
		}
	}
	// Common/project-scope files and folders are this employee's
	// contribution to shared work — they should survive the employee
	// leaving, so ownership moves to the admin performing the deletion
	// instead of being cascade-deleted along with the rest of the user's
	// rows. Personal-scope files/folders stay theirs alone and are removed
	// below.
	if err := h.db.ReassignNonPersonalFiles(id, actor.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	if err := h.db.ReassignNonPersonalFolders(id, actor.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	h.abortUserUploadSessions(r.Context(), id)
	if err := h.db.DeleteUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	h.wipePersonalBucket(r.Context(), id)
	targetName := ""
	if target != nil {
		targetName = target.Username
	}
	h.logAction(r, "user.delete", "user", id, targetName, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminDeleteAllUsers(w http.ResponseWriter, r *http.Request) {
	actor := authutil.GetUser(r)
	ids, err := h.db.ListNonAdminUserIDs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	for _, id := range ids {
		if err := h.db.ReassignNonPersonalFiles(id, actor.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "pozup bolmady")
			return
		}
		if err := h.db.ReassignNonPersonalFolders(id, actor.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "pozup bolmady")
			return
		}
		h.abortUserUploadSessions(r.Context(), id)
	}
	count, err := h.db.DeleteAllUsersExceptAdmin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}
	for _, id := range ids {
		h.wipePersonalBucket(r.Context(), id)
	}
	h.logAction(r, "user.delete_all", "user", 0, "", map[string]any{"deleted": count})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "deleted": count})
}

// abortProjectUploadSessions cancels any multipart uploads in flight to a
// project's bucket before it's deleted and wiped — see
// ListUploadSessionsByProject for why this needs to happen explicitly
// rather than relying on a cascade. Best-effort, same reasoning as
// abortUserUploadSessions below.
func (h *Handler) abortProjectUploadSessions(ctx context.Context, projectID int) {
	sessions, err := h.db.ListUploadSessionsByProject(projectID)
	if err != nil {
		log.Printf("list upload sessions for project %d: %v", projectID, err)
		return
	}
	for _, s := range sessions {
		if err := h.minio.AbortMultipartUpload(ctx, s.Bucket, s.ObjectKey, s.MinioUploadID); err != nil {
			log.Printf("abort upload session %s for deleted project %d: %v", s.ID, projectID, err)
		}
		if err := h.db.DeleteUploadSession(s.ID); err != nil {
			log.Printf("delete upload session %s for deleted project %d: %v", s.ID, projectID, err)
		}
	}
}

// abortUserUploadSessions cancels any multipart uploads a user has in
// progress before they're deleted. upload_sessions.owner_id cascades on
// user delete, so without this the tracking row would vanish while the
// MinIO-side parts it was the only reference to leak forever — the janitor
// (and everyone else) would have no way left to find and reclaim them.
// Best-effort, like wipePersonalBucket below: failures are logged, not
// surfaced, since the deletion this precedes must still proceed.
func (h *Handler) abortUserUploadSessions(ctx context.Context, userID int) {
	sessions, err := h.db.ListUploadSessionsByOwner(userID)
	if err != nil {
		log.Printf("list upload sessions for user %d: %v", userID, err)
		return
	}
	for _, s := range sessions {
		if err := h.minio.AbortMultipartUpload(ctx, s.Bucket, s.ObjectKey, s.MinioUploadID); err != nil {
			log.Printf("abort upload session %s for deleted user %d: %v", s.ID, userID, err)
		}
	}
}

// wipePersonalBucket removes the MinIO objects left behind once a user (and
// their remaining personal-scope file rows, via ON DELETE CASCADE) has been
// deleted — including the avatar image, which isn't tracked in the files
// table. Best-effort: failures are logged, not surfaced, since the DB
// delete this follows has already succeeded.
func (h *Handler) wipePersonalBucket(ctx context.Context, userID int) {
	bucket := storage.PersonalBucket(userID)
	if err := h.minio.RemoveBucketAndAllObjects(ctx, bucket); err != nil {
		log.Printf("cleanup personal bucket %s: %v", bucket, err)
	}
}

func (h *Handler) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 {
		writeError(w, http.StatusBadRequest, "ulanyjy ady azyndan 3 harp bolmaly")
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "parol azyndan 6 simwol bolmaly")
		return
	}
	exists, err := h.db.UserExists(req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýaňlyşlyk")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "bu ulanyjy ady eýýäm bar")
		return
	}
	hash, err := authutil.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýaňlyşlyk")
		return
	}
	regReq := &models.RegisterRequest{
		Username: req.Username,
		Password: req.Password,
		FullName: strings.TrimSpace(req.FullName),
	}
	user, err := h.db.CreateUser(regReq, hash, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ulanyjy döredip bolmady")
		return
	}
	if req.Role == "admin" {
		h.db.UpdateUser(user.ID, "admin", user.QuotaBytes, user.DisplayName, "")
	}
	if req.QuotaMB > 0 {
		h.db.UpdateUser(user.ID, req.Role, int64(req.QuotaMB)*1024*1024, user.DisplayName, "")
	}
	bucket := storage.PersonalBucket(user.ID)
	h.minio.EnsureBucket(r.Context(), bucket)
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) AdminBulkUserQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuotaMB int64 `json:"quota_mb"`
	}
	if err := readJSON(r, &req); err != nil || req.QuotaMB <= 0 {
		writeError(w, http.StatusBadRequest, "kwota girizilmeli")
		return
	}
	if err := h.db.SetAllUsersQuota(req.QuotaMB * 1024 * 1024); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminBulkProjectQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuotaMB int64 `json:"quota_mb"`
	}
	if err := readJSON(r, &req); err != nil || req.QuotaMB <= 0 {
		writeError(w, http.StatusBadRequest, "kwota girizilmeli")
		return
	}
	if err := h.db.SetAllProjectsQuota(req.QuotaMB * 1024 * 1024); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminImportUsers(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "faýl juda uly")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "faýl tapylmady")
		return
	}
	defer file.Close()

	name := strings.ToLower(header.Filename)
	var rows [][]string

	if strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".xls") {
		xlsx, err := excelize.OpenReader(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, "XLSX faýly okap bolmady")
			return
		}
		defer xlsx.Close()
		sheet := xlsx.GetSheetName(0)
		rows, err = xlsx.GetRows(sheet)
		if err != nil {
			writeError(w, http.StatusBadRequest, "XLSX sahypasyny okap bolmady")
			return
		}
	} else {
		reader := csv.NewReader(file)
		reader.LazyQuotes = true
		reader.TrimLeadingSpace = true
		rows, err = reader.ReadAll()
		if err != nil {
			writeError(w, http.StatusBadRequest, "CSV faýly okap bolmady")
			return
		}
	}

	if len(rows) < 2 {
		writeError(w, http.StatusBadRequest, "faýlda maglumat ýok (diňe başlyk bar)")
		return
	}

	type importResult struct {
		Username string `json:"username"`
		Success  bool   `json:"success"`
		Error    string `json:"error,omitempty"`
	}
	var results []importResult
	created := 0

	// Expected columns: username, password, full_name, [quota_mb]
	for i, row := range rows[1:] {
		if len(row) < 3 {
			results = append(results, importResult{Username: fmt.Sprintf("setir %d", i+2), Error: "ýeterlik sütün ýok (3 gerek)"})
			continue
		}
		username := strings.TrimSpace(row[0])
		password := strings.TrimSpace(row[1])
		fullName := strings.TrimSpace(row[2])
		quotaMB := 10240
		if len(row) >= 4 {
			if q, err := strconv.Atoi(strings.TrimSpace(row[3])); err == nil && q > 0 {
				quotaMB = q
			}
		}

		if len(username) < 3 {
			results = append(results, importResult{Username: username, Error: "ulanyjy ady azyndan 3 harp"})
			continue
		}
		if len(password) < 6 {
			results = append(results, importResult{Username: username, Error: "parol azyndan 6 simwol"})
			continue
		}

		exists, _ := h.db.UserExists(username)
		if exists {
			results = append(results, importResult{Username: username, Error: "eýýäm bar"})
			continue
		}

		hash, err := authutil.HashPassword(password)
		if err != nil {
			results = append(results, importResult{Username: username, Error: "parol hashlap bolmady"})
			continue
		}

		regReq := &models.RegisterRequest{
			Username: username,
			Password: password,
			FullName: fullName,
		}
		user, err := h.db.CreateUser(regReq, hash, true)
		if err != nil {
			results = append(results, importResult{Username: username, Error: "döredip bolmady"})
			continue
		}

		if quotaMB > 0 {
			h.db.UpdateUser(user.ID, "user", int64(quotaMB)*1024*1024, user.DisplayName, "")
		}

		bucket := storage.PersonalBucket(user.ID)
		h.minio.EnsureBucket(r.Context(), bucket)

		results = append(results, importResult{Username: username, Success: true})
		created++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"created": created,
		"total":   len(rows) - 1,
		"results": results,
	})
}

func (h *Handler) AdminGetPublicQuota(w http.ResponseWriter, r *http.Request) {
	val, err := h.db.GetSetting("public_quota_bytes")
	if err != nil {
		val = "53687091200"
	}
	bytes, _ := strconv.ParseInt(val, 10, 64)
	if bytes <= 0 {
		bytes = 53687091200
	}
	writeJSON(w, http.StatusOK, map[string]int64{"quota_bytes": bytes})
}

func (h *Handler) AdminSetPublicQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuotaMB int64 `json:"quota_mb"`
	}
	if err := readJSON(r, &req); err != nil || req.QuotaMB <= 0 {
		writeError(w, http.StatusBadRequest, "kwota girizilmeli")
		return
	}
	bytes := req.QuotaMB * 1024 * 1024
	if err := h.db.SetSetting("public_quota_bytes", strconv.FormatInt(bytes, 10)); err != nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

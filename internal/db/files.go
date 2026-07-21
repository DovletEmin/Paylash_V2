package db

import (
	"database/sql"
	"paylash/internal/models"
	"strconv"

	"github.com/lib/pq"
)

// Files

func (d *DB) CreateFile(f *models.File) error {
	if f.Visibility == "" {
		f.Visibility = "private"
	}
	return d.QueryRow(
		`INSERT INTO files (name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, created_at, updated_at`,
		f.Name, f.MimeType, f.SizeBytes, f.MinioBucket, f.MinioKey, f.FolderID, f.OwnerID, f.ProjectID, f.Scope, f.Visibility,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
}

func (d *DB) GetFile(id int) (*models.File, error) {
	f := &models.File{}
	err := d.QueryRow(
		`SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, deleted_at, created_at, updated_at
		 FROM files WHERE id = $1 AND deleted_at IS NULL`, id,
	).Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.DeletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// GetFileIncludingTrash fetches a file regardless of trash state — used by
// the trash-management endpoints (restore/purge), which need to act on
// already-trashed rows that GetFile deliberately hides.
func (d *DB) GetFileIncludingTrash(id int) (*models.File, error) {
	f := &models.File{}
	err := d.QueryRow(
		`SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, deleted_at, created_at, updated_at
		 FROM files WHERE id = $1`, id,
	).Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.DeletedAt, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (d *DB) ListFiles(ownerID int, projectID *int, scope string, folderID *int, sort, order string, limit, offset int) ([]models.File, error) {
	var q string
	var args []any
	var n int

	if scope == "common" {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at FROM files WHERE (visibility = 'common' OR scope = 'common') AND deleted_at IS NULL`
		n = 0
	} else if scope == "project" && projectID != nil {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at FROM files WHERE scope = 'project' AND project_id = $1 AND deleted_at IS NULL`
		args = []any{*projectID}
		n = 1
	} else {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at FROM files WHERE scope = $1 AND deleted_at IS NULL`
		args = []any{scope}
		n = 1

		if scope == "personal" {
			n++
			q += ` AND owner_id = $` + strconv.Itoa(n)
			args = append(args, ownerID)
		}
	}

	if folderID != nil {
		n++
		q += ` AND folder_id = $` + strconv.Itoa(n)
		args = append(args, *folderID)
	} else {
		q += ` AND folder_id IS NULL`
	}

	switch sort {
	case "name":
		q += ` ORDER BY name`
	case "size":
		q += ` ORDER BY size_bytes`
	case "date":
		q += ` ORDER BY updated_at`
	default:
		q += ` ORDER BY name`
	}
	if order == "desc" {
		q += ` DESC`
	}

	n++
	q += ` LIMIT $` + strconv.Itoa(n)
	args = append(args, limit)
	n++
	q += ` OFFSET $` + strconv.Itoa(n)
	args = append(args, offset)

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []models.File
	for rows.Next() {
		var f models.File
		if err := rows.Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ReassignNonPersonalFiles moves ownership of every common/project-scope
// file from fromUserID to toUserID. Used before deleting a user: their
// contribution to shared/project work should survive them (files.owner_id
// is ON DELETE CASCADE), only their private personal-scope files should not.
func (d *DB) ReassignNonPersonalFiles(fromUserID, toUserID int) error {
	_, err := d.Exec(
		`UPDATE files SET owner_id = $2 WHERE owner_id = $1 AND scope != 'personal'`,
		fromUserID, toUserID,
	)
	return err
}

// ReassignNonPersonalFolders is ReassignNonPersonalFiles' counterpart for
// folders — just as necessary, and for a sharper reason: folders.parent_id
// is ALSO ON DELETE CASCADE, so leaving a common/project folder's owner_id
// cascading on user delete wouldn't just lose that one employee's own
// folder, it would take every subfolder nested under it with it — created
// by anyone, not just the deleted user.
func (d *DB) ReassignNonPersonalFolders(fromUserID, toUserID int) error {
	_, err := d.Exec(
		`UPDATE folders SET owner_id = $2 WHERE owner_id = $1 AND scope != 'personal'`,
		fromUserID, toUserID,
	)
	return err
}

func (d *DB) RenameFile(id int, name string) error {
	_, err := d.Exec(`UPDATE files SET name = $1, updated_at = NOW() WHERE id = $2`, name, id)
	return err
}

// MoveFile reassigns a file to a different folder (nil = scope root) within
// the same scope/project — a metadata-only move. It deliberately does not
// touch the file's MinIO object: the key's folder-ID prefix is just how new
// uploads happen to be laid out in storage, not something reads depend on,
// so leaving it as-is avoids a copy of however large the object is.
func (d *DB) MoveFile(id int, name string, folderID *int) error {
	_, err := d.Exec(`UPDATE files SET name = $1, folder_id = $2, updated_at = NOW() WHERE id = $3`, name, folderID, id)
	return err
}

func (d *DB) UpdateFileVersion(id int, sizeBytes int64) error {
	_, err := d.Exec(`UPDATE files SET version = version + 1, size_bytes = $1, updated_at = NOW() WHERE id = $2`, sizeBytes, id)
	return err
}

// SearchFiles searches personal files owned by the user, shared/common files,
// and files in any project the user is a member of. Admins search everything.
func (d *DB) SearchFiles(userID int, isAdmin bool, query string) ([]models.File, error) {
	var q string
	var args []any

	if isAdmin {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at
		     FROM files WHERE name ILIKE $1 AND deleted_at IS NULL`
		args = []any{"%" + query + "%"}
	} else {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at
		     FROM files WHERE name ILIKE $1 AND deleted_at IS NULL AND (
		         owner_id = $2
		         OR visibility = 'common' OR scope = 'common'
		         OR project_id IN (SELECT project_id FROM project_members WHERE user_id = $2)
		     )`
		args = []any{"%" + query + "%", userID}
	}
	q += ` ORDER BY updated_at DESC LIMIT 50`

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []models.File
	for rows.Next() {
		var f models.File
		if err := rows.Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) SetFileVisibility(fileID int, visibility string) error {
	_, err := d.Exec(`UPDATE files SET visibility = $1, updated_at = NOW() WHERE id = $2`, visibility, fileID)
	return err
}

// FileNameExists checks if a file with the given name exists in the same scope/folder context.
func (d *DB) FileNameExists(name string, ownerID int, scope string, folderID *int, projectID *int) (bool, error) {
	q := `SELECT COUNT(*) FROM files WHERE name = $1 AND scope = $2 AND deleted_at IS NULL`
	args := []any{name, scope}
	n := 2

	if scope == "personal" {
		n++
		q += ` AND owner_id = $` + strconv.Itoa(n)
		args = append(args, ownerID)
	} else if scope == "project" && projectID != nil {
		n++
		q += ` AND project_id = $` + strconv.Itoa(n)
		args = append(args, *projectID)
	}

	if folderID != nil {
		n++
		q += ` AND folder_id = $` + strconv.Itoa(n)
		args = append(args, *folderID)
	} else {
		q += ` AND folder_id IS NULL`
	}

	var count int
	err := d.QueryRow(q, args...).Scan(&count)
	return count > 0, err
}

func (d *DB) GetStorageUsage(ownerID int, scope string, projectID *int) (*models.StorageUsage, error) {
	su := &models.StorageUsage{}
	if scope == "personal" {
		err := d.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE owner_id = $1 AND scope = 'personal'`, ownerID).Scan(&su.UsedBytes)
		if err != nil {
			return nil, err
		}
		err = d.QueryRow(`SELECT quota_bytes FROM users WHERE id = $1`, ownerID).Scan(&su.QuotaBytes)
		return su, err
	}
	if scope == "project" && projectID != nil {
		err := d.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE scope = 'project' AND project_id = $1`, *projectID).Scan(&su.UsedBytes)
		if err != nil {
			return nil, err
		}
		err = d.QueryRow(`SELECT quota_bytes FROM projects WHERE id = $1`, *projectID).Scan(&su.QuotaBytes)
		return su, err
	}
	// common scope
	err := d.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE visibility = 'common' OR scope = 'common'`).Scan(&su.UsedBytes)
	if err != nil {
		return nil, err
	}
	var qStr string
	err = d.QueryRow(`SELECT value FROM settings WHERE key = 'public_quota_bytes'`).Scan(&qStr)
	if err == nil {
		su.QuotaBytes, _ = strconv.ParseInt(qStr, 10, 64)
	}
	if su.QuotaBytes <= 0 {
		su.QuotaBytes = 50 * 1024 * 1024 * 1024 // 50 GB default
	}
	return su, nil
}

// Folders

func (d *DB) CreateFolder(f *models.Folder) error {
	return d.QueryRow(
		`INSERT INTO folders (name, parent_id, owner_id, project_id, scope)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		f.Name, f.ParentID, f.OwnerID, f.ProjectID, f.Scope,
	).Scan(&f.ID, &f.CreatedAt)
}

func (d *DB) ListFolders(ownerID int, projectID *int, scope string, parentID *int) ([]models.Folder, error) {
	var q string
	var args []any
	var n int

	if scope == "common" {
		q = `SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE scope = 'common' AND deleted_at IS NULL`
		n = 0
	} else {
		q = `SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE scope = $1 AND deleted_at IS NULL`
		args = []any{scope}
		n = 1
	}

	if scope == "personal" {
		n++
		q += ` AND owner_id = $` + strconv.Itoa(n)
		args = append(args, ownerID)
	} else if scope == "project" && projectID != nil {
		n++
		q += ` AND project_id = $` + strconv.Itoa(n)
		args = append(args, *projectID)
	}

	if parentID != nil {
		n++
		q += ` AND parent_id = $` + strconv.Itoa(n)
		args = append(args, *parentID)
	} else {
		q += ` AND parent_id IS NULL`
	}
	q += ` ORDER BY name`

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var folders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.CreatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// ListAllFoldersInScope returns every folder in a scope (regardless of
// nesting depth) — used to build the "move to" folder picker, which needs
// the whole tree rather than one level at a time.
func (d *DB) ListAllFoldersInScope(ownerID int, projectID *int, scope string) ([]models.Folder, error) {
	var q string
	var args []any
	var n int

	if scope == "common" {
		q = `SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE scope = 'common' AND deleted_at IS NULL`
	} else {
		q = `SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE scope = $1 AND deleted_at IS NULL`
		args = []any{scope}
		n = 1
	}

	if scope == "personal" {
		n++
		q += ` AND owner_id = $` + strconv.Itoa(n)
		args = append(args, ownerID)
	} else if scope == "project" && projectID != nil {
		n++
		q += ` AND project_id = $` + strconv.Itoa(n)
		args = append(args, *projectID)
	}
	q += ` ORDER BY name`

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var folders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.CreatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

func (d *DB) GetFolder(id int) (*models.Folder, error) {
	f := &models.Folder{}
	err := d.QueryRow(
		`SELECT id, name, parent_id, owner_id, project_id, scope, deleted_at, created_at FROM folders WHERE id = $1 AND deleted_at IS NULL`, id,
	).Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.DeletedAt, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// GetFolderIncludingTrash fetches a folder regardless of trash state — used
// by the trash-management endpoints (restore/purge).
func (d *DB) GetFolderIncludingTrash(id int) (*models.Folder, error) {
	f := &models.Folder{}
	err := d.QueryRow(
		`SELECT id, name, parent_id, owner_id, project_id, scope, deleted_at, created_at FROM folders WHERE id = $1`, id,
	).Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.DeletedAt, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (d *DB) RenameFolder(id int, name string) error {
	_, err := d.Exec(`UPDATE folders SET name = $1 WHERE id = $2`, name, id)
	return err
}

// MoveFolder reassigns a folder to a different parent (nil = scope root).
// Caller is responsible for the cycle check (target isn't the folder itself
// or one of its own descendants) — see ListFolderAndDescendantIDs.
func (d *DB) MoveFolder(id int, name string, parentID *int) error {
	_, err := d.Exec(`UPDATE folders SET name = $1, parent_id = $2 WHERE id = $3`, name, parentID, id)
	return err
}

// IsFolderTrashed reports whether a folder currently has deleted_at set.
// A missing folder is treated as "trashed" too, so restore logic that uses
// this to decide whether to keep a parent/folder reference falls back to
// the scope root instead of pointing at nothing.
func (d *DB) IsFolderTrashed(id int) (bool, error) {
	var deletedAt sql.NullTime
	err := d.QueryRow(`SELECT deleted_at FROM folders WHERE id = $1`, id).Scan(&deletedAt)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return deletedAt.Valid, nil
}

// ListFolderAndDescendantIDs returns folderID plus every folder nested under
// it (any depth), via a recursive CTE over parent_id.
func (d *DB) ListFolderAndDescendantIDs(folderID int) ([]int, error) {
	rows, err := d.Query(
		// The path array + NOT ... = ANY(path) guard makes this safe even if
		// a parent_id cycle somehow existed (it shouldn't — MoveFolder's
		// caller checks for that — but UNION ALL alone would recurse
		// forever against one instead of erroring or returning wrong data).
		`WITH RECURSIVE sub AS (
			SELECT id, ARRAY[id] AS path FROM folders WHERE id = $1
			UNION ALL
			SELECT f.id, sub.path || f.id
			FROM folders f JOIN sub ON f.parent_id = sub.id
			WHERE NOT f.id = ANY(sub.path)
		)
		SELECT id FROM sub`, folderID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListFilesInFolders returns every file whose folder_id is one of folderIDs.
func (d *DB) ListFilesInFolders(folderIDs []int) ([]models.File, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at
		 FROM files WHERE folder_id = ANY($1)`, pq.Array(folderIDs),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []models.File
	for rows.Next() {
		var f models.File
		if err := rows.Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetFoldersByIDs returns the full Folder rows for an arbitrary set of ids —
// unlike ListFolderAndDescendantIDs (bare ids only), used where the caller
// needs each folder's name/parent_id, e.g. to reconstruct a subtree's
// relative paths for a zip download.
func (d *DB) GetFoldersByIDs(ids []int) ([]models.Folder, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE id = ANY($1)`, pq.Array(ids),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var folders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.CreatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// DeleteFilesInFolders removes every file row whose folder_id is one of
// folderIDs. Callers must delete the corresponding MinIO objects first.
func (d *DB) DeleteFilesInFolders(folderIDs []int) error {
	if len(folderIDs) == 0 {
		return nil
	}
	_, err := d.Exec(`DELETE FROM files WHERE folder_id = ANY($1)`, pq.Array(folderIDs))
	return err
}

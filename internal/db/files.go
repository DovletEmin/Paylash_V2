package db

import (
	"database/sql"
	"paylash/internal/models"
	"strconv"
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
		`SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at
		 FROM files WHERE id = $1`, id,
	).Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (d *DB) ListFiles(ownerID int, projectID *int, scope string, folderID *int, sort, order string) ([]models.File, error) {
	var q string
	var args []any
	var n int

	if scope == "common" {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at FROM files WHERE (visibility = 'common' OR scope = 'common')`
		n = 0
	} else if scope == "project" && projectID != nil {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at FROM files WHERE scope = 'project' AND project_id = $1`
		args = []any{*projectID}
		n = 1
	} else {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at FROM files WHERE scope = $1`
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

func (d *DB) RenameFile(id int, name string) error {
	_, err := d.Exec(`UPDATE files SET name = $1, updated_at = NOW() WHERE id = $2`, name, id)
	return err
}

func (d *DB) DeleteFile(id int) error {
	_, err := d.Exec(`DELETE FROM files WHERE id = $1`, id)
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
		     FROM files WHERE name ILIKE $1`
		args = []any{"%" + query + "%"}
	} else {
		q = `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, created_at, updated_at
		     FROM files WHERE name ILIKE $1 AND (
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
	q := `SELECT COUNT(*) FROM files WHERE name = $1 AND scope = $2`
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
		q = `SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE scope = 'common'`
		n = 0
	} else {
		q = `SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE scope = $1`
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

func (d *DB) GetFolder(id int) (*models.Folder, error) {
	f := &models.Folder{}
	err := d.QueryRow(
		`SELECT id, name, parent_id, owner_id, project_id, scope, created_at FROM folders WHERE id = $1`, id,
	).Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (d *DB) RenameFolder(id int, name string) error {
	_, err := d.Exec(`UPDATE folders SET name = $1 WHERE id = $2`, name, id)
	return err
}

func (d *DB) DeleteFolder(id int) error {
	_, err := d.Exec(`DELETE FROM folders WHERE id = $1`, id)
	return err
}

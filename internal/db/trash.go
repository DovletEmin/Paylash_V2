package db

import (
	"time"

	"paylash/internal/models"

	"github.com/lib/pq"
)

// SoftDeleteFile marks a single file as trashed without touching MinIO or
// removing the row — the actual object/row removal happens later, either
// via a manual purge or the daily janitor sweep.
func (d *DB) SoftDeleteFile(id int) error {
	_, err := d.Exec(`UPDATE files SET deleted_at = NOW() WHERE id = $1`, id)
	return err
}

// SoftDeleteFilesInFolders marks every file in the given folders as trashed.
func (d *DB) SoftDeleteFilesInFolders(folderIDs []int) error {
	if len(folderIDs) == 0 {
		return nil
	}
	_, err := d.Exec(`UPDATE files SET deleted_at = NOW() WHERE folder_id = ANY($1)`, pq.Array(folderIDs))
	return err
}

// SoftDeleteFolders marks every given folder as trashed.
func (d *DB) SoftDeleteFolders(folderIDs []int) error {
	if len(folderIDs) == 0 {
		return nil
	}
	_, err := d.Exec(`UPDATE folders SET deleted_at = NOW() WHERE id = ANY($1)`, pq.Array(folderIDs))
	return err
}

// RestoreFile clears deleted_at, sets its final name/folder (the caller
// resolves name collisions and orphaned-parent fallback beforehand).
func (d *DB) RestoreFile(id int, folderID *int, name string) error {
	_, err := d.Exec(
		`UPDATE files SET deleted_at = NULL, folder_id = $2, name = $3, updated_at = NOW() WHERE id = $1`,
		id, folderID, name,
	)
	return err
}

// RestoreFolder clears deleted_at and sets its final parent (the caller
// resolves the orphaned-parent fallback beforehand).
func (d *DB) RestoreFolder(id int, parentID *int) error {
	_, err := d.Exec(`UPDATE folders SET deleted_at = NULL, parent_id = $2 WHERE id = $1`, id, parentID)
	return err
}

// ListTrashedFiles returns trashed files owned by userID, or every trashed
// file when isAdmin is true.
func (d *DB) ListTrashedFiles(userID int, isAdmin bool) ([]models.File, error) {
	q := `SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, deleted_at, created_at, updated_at
	      FROM files WHERE deleted_at IS NOT NULL`
	args := []any{}
	if !isAdmin {
		q += ` AND owner_id = $1`
		args = append(args, userID)
	}
	q += ` ORDER BY deleted_at DESC`

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []models.File
	for rows.Next() {
		var f models.File
		if err := rows.Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.DeletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ListTrashedFolders returns trashed folders owned by userID, or every
// trashed folder when isAdmin is true.
func (d *DB) ListTrashedFolders(userID int, isAdmin bool) ([]models.Folder, error) {
	q := `SELECT id, name, parent_id, owner_id, project_id, scope, deleted_at, created_at
	      FROM folders WHERE deleted_at IS NOT NULL`
	args := []any{}
	if !isAdmin {
		q += ` AND owner_id = $1`
		args = append(args, userID)
	}
	q += ` ORDER BY deleted_at DESC`

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var folders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.ParentID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.DeletedAt, &f.CreatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// ListExpiredTrashedFiles returns trashed files whose deleted_at is older
// than cutoff — the janitor's candidates for permanent purge.
func (d *DB) ListExpiredTrashedFiles(cutoff time.Time) ([]models.File, error) {
	rows, err := d.Query(
		`SELECT id, name, mime_type, size_bytes, minio_bucket, minio_key, folder_id, owner_id, project_id, scope, visibility, version, deleted_at, created_at, updated_at
		 FROM files WHERE deleted_at IS NOT NULL AND deleted_at < $1`, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []models.File
	for rows.Next() {
		var f models.File
		if err := rows.Scan(&f.ID, &f.Name, &f.MimeType, &f.SizeBytes, &f.MinioBucket, &f.MinioKey, &f.FolderID, &f.OwnerID, &f.ProjectID, &f.Scope, &f.Visibility, &f.Version, &f.DeletedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ListExpiredTrashedFolderIDs returns folder ids trashed before cutoff.
func (d *DB) ListExpiredTrashedFolderIDs(cutoff time.Time) ([]int, error) {
	rows, err := d.Query(`SELECT id FROM folders WHERE deleted_at IS NOT NULL AND deleted_at < $1`, cutoff)
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

// PurgeFiles hard-deletes the given file rows. Callers must have already
// removed the corresponding MinIO objects.
func (d *DB) PurgeFiles(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.Exec(`DELETE FROM files WHERE id = ANY($1)`, pq.Array(ids))
	return err
}

// PurgeFolders hard-deletes the given folder rows.
func (d *DB) PurgeFolders(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.Exec(`DELETE FROM folders WHERE id = ANY($1)`, pq.Array(ids))
	return err
}

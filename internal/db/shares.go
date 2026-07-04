package db

import (
	"paylash/internal/models"
)

func (d *DB) CreateShare(fileID, sharedBy int, sharedWith *int, permission string, isPublic bool) (*models.FileShare, error) {
	s := &models.FileShare{}
	err := d.QueryRow(
		`INSERT INTO file_shares (file_id, shared_by, shared_with, permission, is_public)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (file_id, shared_with) DO UPDATE SET permission = $4
		 RETURNING id, file_id, shared_by, shared_with, permission, is_public, created_at`,
		fileID, sharedBy, sharedWith, permission, isPublic,
	).Scan(&s.ID, &s.FileID, &s.SharedBy, &s.SharedWith, &s.Permission, &s.IsPublic, &s.CreatedAt)
	return s, err
}

func (d *DB) SetPublicShare(fileID, sharedBy int, isPublic bool) error {
	if isPublic {
		_, err := d.Exec(
			`INSERT INTO file_shares (file_id, shared_by, shared_with, permission, is_public)
			 VALUES ($1, $2, NULL, 'view', TRUE)
			 ON CONFLICT (file_id, shared_with) DO UPDATE SET is_public = TRUE`,
			fileID, sharedBy,
		)
		return err
	}
	_, err := d.Exec(`DELETE FROM file_shares WHERE file_id = $1 AND is_public = TRUE AND shared_with IS NULL`, fileID)
	return err
}

func (d *DB) DeleteShare(fileID, sharedWithID int) error {
	_, err := d.Exec(
		`DELETE FROM file_shares WHERE file_id = $1 AND shared_with = $2`,
		fileID, sharedWithID,
	)
	return err
}

func (d *DB) UpdateSharePermission(fileID, sharedWithID int, permission string) error {
	_, err := d.Exec(
		`UPDATE file_shares SET permission = $3 WHERE file_id = $1 AND shared_with = $2`,
		fileID, sharedWithID, permission,
	)
	return err
}

func (d *DB) GetSharesForFile(fileID int) ([]models.ShareView, error) {
	rows, err := d.Query(
		`SELECT fs.id, fs.file_id, fs.shared_by, fs.shared_with, fs.permission, fs.is_public,
		        COALESCE(u.display_name,'') AS full_name, COALESCE(u.username,'') AS username,
		        fs.created_at
		 FROM file_shares fs
		 LEFT JOIN users u ON fs.shared_with = u.id
		 WHERE fs.file_id = $1 ORDER BY fs.created_at`, fileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var shares []models.ShareView
	for rows.Next() {
		var s models.ShareView
		if err := rows.Scan(&s.ID, &s.FileID, &s.SharedBy, &s.SharedWith, &s.Permission, &s.IsPublic,
			&s.FullName, &s.Username, &s.CreatedAt); err != nil {
			return nil, err
		}
		shares = append(shares, s)
	}
	return shares, rows.Err()
}

// GetSharedWithMe returns files individually shared with the user, plus files
// visible to everyone via the common folder (visibility/scope = 'common').
func (d *DB) GetSharedWithMe(userID int) ([]models.SharedFileView, error) {
	q := `SELECT sub.* FROM (
		SELECT DISTINCT ON (f.id)
			f.id, f.name, f.mime_type, f.size_bytes, f.minio_bucket, f.minio_key,
			f.folder_id, f.owner_id, f.project_id, f.scope, f.visibility, f.version, f.created_at, f.updated_at,
			owner.display_name,
			COALESCE(fs.permission, 'view') AS perm,
			COALESCE(fs.created_at, f.updated_at) AS shared_at
		FROM files f
		JOIN users owner ON f.owner_id = owner.id
		LEFT JOIN file_shares fs ON fs.file_id = f.id AND fs.shared_with = $1
		WHERE f.owner_id != $1
		AND (
			fs.id IS NOT NULL
			OR f.visibility = 'common'
			OR f.scope = 'common'
		)
		ORDER BY f.id, fs.permission DESC NULLS LAST
	) sub ORDER BY sub.shared_at DESC`

	rows, err := d.Query(q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.SharedFileView
	for rows.Next() {
		var sv models.SharedFileView
		if err := rows.Scan(&sv.ID, &sv.Name, &sv.MimeType, &sv.SizeBytes, &sv.MinioBucket, &sv.MinioKey,
			&sv.FolderID, &sv.OwnerID, &sv.ProjectID, &sv.Scope, &sv.Visibility, &sv.Version, &sv.CreatedAt, &sv.UpdatedAt,
			&sv.SharedByName, &sv.Permission, &sv.SharedAt); err != nil {
			return nil, err
		}
		list = append(list, sv)
	}
	return list, rows.Err()
}

func (d *DB) GetSharedByMe(userID int) ([]models.SharedByMeView, error) {
	q := `SELECT
			f.id, f.name, f.mime_type, f.size_bytes, f.minio_bucket, f.minio_key,
			f.folder_id, f.owner_id, f.project_id, f.scope, f.visibility, f.version, f.created_at, f.updated_at,
			COALESCE(u.display_name, u.username, '') AS shared_with_name,
			fs.permission,
			fs.created_at AS shared_at
		FROM file_shares fs
		JOIN files f ON f.id = fs.file_id
		LEFT JOIN users u ON u.id = fs.shared_with
		WHERE fs.shared_by = $1 AND fs.shared_with IS NOT NULL
		ORDER BY fs.created_at DESC`

	rows, err := d.Query(q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.SharedByMeView
	for rows.Next() {
		var sv models.SharedByMeView
		if err := rows.Scan(&sv.ID, &sv.Name, &sv.MimeType, &sv.SizeBytes, &sv.MinioBucket, &sv.MinioKey,
			&sv.FolderID, &sv.OwnerID, &sv.ProjectID, &sv.Scope, &sv.Visibility, &sv.Version, &sv.CreatedAt, &sv.UpdatedAt,
			&sv.SharedWithName, &sv.Permission, &sv.SharedAt); err != nil {
			return nil, err
		}
		list = append(list, sv)
	}
	return list, rows.Err()
}

// CanAccessFile decides whether userID may access fileID with at least requiredPerm ("view" or "edit").
//
// Three independent rules, in priority order:
//  1. personal scope: only the owner, or someone the file was explicitly shared with.
//  2. common scope (or a personal file explicitly marked visibility='common'): every
//     authenticated employee gets full view+edit access — this is the shared company folder.
//  3. project scope: access is governed solely by project_members (admin bypasses this).
func (d *DB) CanAccessFile(fileID, userID int, isAdmin bool, requiredPerm string) (bool, error) {
	if isAdmin {
		return true, nil
	}

	var ownerID int
	var scope, visibility string
	var projectID *int
	err := d.QueryRow(`SELECT owner_id, scope, visibility, project_id FROM files WHERE id = $1`, fileID).
		Scan(&ownerID, &scope, &visibility, &projectID)
	if err != nil {
		return false, err
	}

	if ownerID == userID {
		return true, nil
	}

	if scope == "common" || visibility == "common" {
		return true, nil
	}

	if scope == "project" && projectID != nil {
		perm, err := d.GetProjectMemberPermission(*projectID, userID)
		if err != nil {
			return false, err
		}
		if perm == "" {
			return false, nil
		}
		if requiredPerm == "view" {
			return true, nil
		}
		return perm == "edit", nil
	}

	// Explicit point-to-point share (personal files shared with a specific colleague).
	var perm string
	err = d.QueryRow(
		`SELECT permission FROM file_shares WHERE file_id = $1 AND shared_with = $2`, fileID, userID,
	).Scan(&perm)
	if err == nil {
		if requiredPerm == "view" {
			return true, nil
		}
		return perm == "edit", nil
	}

	// Shared with the whole company via the public-share toggle.
	var isPublic bool
	err = d.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM file_shares WHERE file_id = $1 AND is_public = TRUE)`, fileID,
	).Scan(&isPublic)
	if err == nil && isPublic {
		return requiredPerm == "view", nil
	}

	return false, nil
}

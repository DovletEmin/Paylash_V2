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

// GetSharedWithMe returns files individually, explicitly shared with the
// user — i.e. exactly the rows in file_shares naming them as the recipient.
// It deliberately does NOT include common-scope/common-visibility files:
// those already have their own "Common" page, and surfacing every one of
// them here too meant a file "shared to common access" landed in literally
// every employee's Shared page, which is the opposite of what "shared with
// me" should mean. Grouped by SharedByID (who actually performed the share,
// via file_shares.shared_by) rather than the file's owner, since an admin or
// a common/project editor can share a file they don't own.
func (d *DB) GetSharedWithMe(userID int) ([]models.SharedFileView, error) {
	q := `SELECT
			f.id, f.name, f.mime_type, f.size_bytes, f.minio_bucket, f.minio_key,
			f.folder_id, f.owner_id, f.project_id, f.scope, f.visibility, f.version, f.created_at, f.updated_at,
			fs.shared_by, COALESCE(sharer.display_name, sharer.username, '') AS shared_by_name,
			fs.permission,
			fs.created_at AS shared_at
		FROM file_shares fs
		JOIN files f ON f.id = fs.file_id
		JOIN users sharer ON sharer.id = fs.shared_by
		WHERE fs.shared_with = $1 AND f.deleted_at IS NULL
		ORDER BY fs.created_at DESC`

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
			&sv.SharedByID, &sv.SharedByName, &sv.Permission, &sv.SharedAt); err != nil {
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
			fs.shared_with, COALESCE(u.display_name, u.username, '') AS shared_with_name,
			fs.permission,
			fs.created_at AS shared_at
		FROM file_shares fs
		JOIN files f ON f.id = fs.file_id
		JOIN users u ON u.id = fs.shared_with
		WHERE fs.shared_by = $1 AND fs.shared_with IS NOT NULL AND f.deleted_at IS NULL
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
			&sv.SharedWithID, &sv.SharedWithName, &sv.Permission, &sv.SharedAt); err != nil {
			return nil, err
		}
		list = append(list, sv)
	}
	return list, rows.Err()
}

// fileAccessFacts holds everything decideFileAccess needs to reach a
// decision, once fetched from the DB — separating the pure decision logic
// from the queries that gather it lets the logic itself be unit tested
// without a real Postgres connection.
type fileAccessFacts struct {
	ownerID       int
	scope         string
	visibility    string
	projectID     *int
	projectPerm   string // "" if not a project member (only set when scope == "project")
	directShare   string // "" if no point-to-point share with this user
	isPublicShare bool
}

// decideFileAccess is the single source of truth for file access decisions.
// Rules, in priority order:
//  1. admins and the owner always have full access.
//  2. common scope (or a personal file explicitly marked visibility='common'):
//     every authenticated employee gets full view+edit access.
//  3. project scope: access is governed solely by project_members.
//  4. otherwise: an explicit point-to-point share, or the public-share toggle
//     (view-only), or no access at all.
func decideFileAccess(f fileAccessFacts, userID int, isAdmin bool, requiredPerm string) bool {
	if isAdmin || f.ownerID == userID {
		return true
	}
	if f.scope == "common" || f.visibility == "common" {
		return true
	}
	if f.scope == "project" && f.projectID != nil {
		if f.projectPerm == "" {
			return false
		}
		if requiredPerm == "view" {
			return true
		}
		return f.projectPerm == "edit"
	}
	if f.directShare != "" {
		if requiredPerm == "view" {
			return true
		}
		return f.directShare == "edit"
	}
	if f.isPublicShare {
		return requiredPerm == "view"
	}
	return false
}

// CanAccessFile decides whether userID may access fileID with at least requiredPerm ("view" or "edit").
// It fetches the facts decideFileAccess needs and defers the actual decision to it.
func (d *DB) CanAccessFile(fileID, userID int, isAdmin bool, requiredPerm string) (bool, error) {
	if isAdmin {
		return true, nil
	}

	var f fileAccessFacts
	err := d.QueryRow(`SELECT owner_id, scope, visibility, project_id FROM files WHERE id = $1`, fileID).
		Scan(&f.ownerID, &f.scope, &f.visibility, &f.projectID)
	if err != nil {
		return false, err
	}

	if f.ownerID == userID || f.scope == "common" || f.visibility == "common" {
		return decideFileAccess(f, userID, false, requiredPerm), nil
	}

	if f.scope == "project" && f.projectID != nil {
		perm, err := d.GetProjectMemberPermission(*f.projectID, userID)
		if err != nil {
			return false, err
		}
		f.projectPerm = perm
		return decideFileAccess(f, userID, false, requiredPerm), nil
	}

	// Explicit point-to-point share (personal files shared with a specific colleague).
	var directPerm string
	if err := d.QueryRow(
		`SELECT permission FROM file_shares WHERE file_id = $1 AND shared_with = $2`, fileID, userID,
	).Scan(&directPerm); err == nil {
		f.directShare = directPerm
	}

	// Shared with the whole company via the public-share toggle.
	var isPublic bool
	if err := d.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM file_shares WHERE file_id = $1 AND is_public = TRUE)`, fileID,
	).Scan(&isPublic); err == nil {
		f.isPublicShare = isPublic
	}

	return decideFileAccess(f, userID, false, requiredPerm), nil
}

package db

import (
	"database/sql"
	"time"

	"paylash/internal/models"
)

func (d *DB) CreateUploadSession(s *models.UploadSession) error {
	s.ID = generateToken(16)
	return d.QueryRow(
		`INSERT INTO upload_sessions (id, minio_upload_id, bucket, object_key, owner_id, scope, project_id, folder_id, file_name, mime_type, total_size, part_size, part_count)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING created_at, updated_at`,
		s.ID, s.MinioUploadID, s.Bucket, s.ObjectKey, s.OwnerID, s.Scope, s.ProjectID, s.FolderID, s.FileName, s.MimeType, s.TotalSize, s.PartSize, s.PartCount,
	).Scan(&s.CreatedAt, &s.UpdatedAt)
}

func (d *DB) GetUploadSession(id string) (*models.UploadSession, error) {
	s := &models.UploadSession{}
	err := d.QueryRow(
		`SELECT id, minio_upload_id, bucket, object_key, owner_id, scope, project_id, folder_id, file_name, mime_type, total_size, part_size, part_count, status, created_at, updated_at
		 FROM upload_sessions WHERE id = $1`, id,
	).Scan(&s.ID, &s.MinioUploadID, &s.Bucket, &s.ObjectKey, &s.OwnerID, &s.Scope, &s.ProjectID, &s.FolderID, &s.FileName, &s.MimeType, &s.TotalSize, &s.PartSize, &s.PartCount, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (d *DB) DeleteUploadSession(id string) error {
	_, err := d.Exec(`DELETE FROM upload_sessions WHERE id = $1`, id)
	return err
}

func (d *DB) TouchUploadSession(id string) error {
	_, err := d.Exec(`UPDATE upload_sessions SET updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ListActiveUploadSessions returns every in_progress session with its
// owner's name attached — the admin-panel view into large uploads that are
// currently under way or stuck.
func (d *DB) ListActiveUploadSessions() ([]models.UploadSessionView, error) {
	rows, err := d.Query(
		`SELECT us.id, us.file_name, us.total_size, us.part_count, us.scope, us.status, us.created_at, us.updated_at,
		        u.id, u.username, u.display_name
		 FROM upload_sessions us
		 JOIN users u ON u.id = us.owner_id
		 WHERE us.status = 'in_progress'
		 ORDER BY us.updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []models.UploadSessionView
	for rows.Next() {
		var s models.UploadSessionView
		if err := rows.Scan(&s.ID, &s.FileName, &s.TotalSize, &s.PartCount, &s.Scope, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.OwnerID, &s.OwnerUsername, &s.OwnerDisplayName); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ListUploadSessionsByOwner returns every upload session (any status) owned
// by a user — used to abort in-progress multipart uploads before deleting
// the user, so their MinIO-side parts don't outlive the tracking row that's
// the only thing letting the janitor (or anyone) find and reclaim them.
func (d *DB) ListUploadSessionsByOwner(ownerID int) ([]models.UploadSession, error) {
	rows, err := d.Query(
		`SELECT id, minio_upload_id, bucket, object_key, owner_id, scope, project_id, folder_id, file_name, mime_type, total_size, part_size, part_count, status, created_at, updated_at
		 FROM upload_sessions WHERE owner_id = $1`, ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []models.UploadSession
	for rows.Next() {
		var s models.UploadSession
		if err := rows.Scan(&s.ID, &s.MinioUploadID, &s.Bucket, &s.ObjectKey, &s.OwnerID, &s.Scope, &s.ProjectID, &s.FolderID, &s.FileName, &s.MimeType, &s.TotalSize, &s.PartSize, &s.PartCount, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ListUploadSessionsByProject returns every upload session (any status) for
// a project — used to abort in-progress multipart uploads before deleting
// the project. upload_sessions.project_id is ON DELETE SET NULL (not
// CASCADE, since a session in flight to a since-deleted project's bucket
// isn't meaningfully "personal" or "common" either), so without this the
// row would survive as a zombie in_progress session with project_id NULL
// pointing at a bucket that no longer exists.
func (d *DB) ListUploadSessionsByProject(projectID int) ([]models.UploadSession, error) {
	rows, err := d.Query(
		`SELECT id, minio_upload_id, bucket, object_key, owner_id, scope, project_id, folder_id, file_name, mime_type, total_size, part_size, part_count, status, created_at, updated_at
		 FROM upload_sessions WHERE project_id = $1`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []models.UploadSession
	for rows.Next() {
		var s models.UploadSession
		if err := rows.Scan(&s.ID, &s.MinioUploadID, &s.Bucket, &s.ObjectKey, &s.OwnerID, &s.Scope, &s.ProjectID, &s.FolderID, &s.FileName, &s.MimeType, &s.TotalSize, &s.PartSize, &s.PartCount, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ListStaleUploadSessions returns in_progress sessions untouched since
// before cutoff — the janitor aborts these to reclaim the storage MinIO
// buffers for parts of an upload nobody ever finished or resumed.
func (d *DB) ListStaleUploadSessions(cutoff time.Time) ([]models.UploadSession, error) {
	rows, err := d.Query(
		`SELECT id, minio_upload_id, bucket, object_key, owner_id, scope, project_id, folder_id, file_name, mime_type, total_size, part_size, part_count, status, created_at, updated_at
		 FROM upload_sessions WHERE status = 'in_progress' AND updated_at < $1`, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []models.UploadSession
	for rows.Next() {
		var s models.UploadSession
		if err := rows.Scan(&s.ID, &s.MinioUploadID, &s.Bucket, &s.ObjectKey, &s.OwnerID, &s.Scope, &s.ProjectID, &s.FolderID, &s.FileName, &s.MimeType, &s.TotalSize, &s.PartSize, &s.PartCount, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

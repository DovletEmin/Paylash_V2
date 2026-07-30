package db

import (
	"database/sql"
	"fmt"
	"paylash/internal/models"
	"time"
)

// Projects — admin-created shared folders with an explicit employee ACL.

func (d *DB) CreateProject(name string, quotaBytes int64) (*models.Project, error) {
	p := &models.Project{}
	err := d.QueryRow(
		`INSERT INTO projects (name, quota_bytes, minio_bucket)
		 VALUES ($1, $2, '') RETURNING id, name, quota_bytes, minio_bucket, created_at`,
		name, quotaBytes,
	).Scan(&p.ID, &p.Name, &p.QuotaBytes, &p.MinioBucket, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.MinioBucket = fmt.Sprintf("project-%d", p.ID)
	_, err = d.Exec(`UPDATE projects SET minio_bucket = $1 WHERE id = $2`, p.MinioBucket, p.ID)
	return p, err
}

func (d *DB) ListAllProjects() ([]models.Project, error) {
	rows, err := d.Query(`SELECT id, name, quota_bytes, minio_bucket, created_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.QuotaBytes, &p.MinioBucket, &p.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (d *DB) UpdateProject(id int, name string, quotaBytes int64) error {
	_, err := d.Exec(`UPDATE projects SET name = $1, quota_bytes = $2 WHERE id = $3`, name, quotaBytes, id)
	return err
}

// DeleteProject removes a project along with its file rows. files.project_id
// is ON DELETE SET NULL (not CASCADE), so without this the file rows would
// survive as unreachable orphans — visible in no listing (scope='project'
// with project_id=NULL never matches ListFiles) while their MinIO objects
// leak forever. Folder rows do cascade automatically via project_id.
func (d *DB) DeleteProject(id int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM files WHERE project_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Project members — the ACL that grants employees access to a project folder.

func (d *DB) AddProjectMember(projectID, userID int, permission string) (*models.ProjectMember, error) {
	m := &models.ProjectMember{}
	err := d.QueryRow(
		`INSERT INTO project_members (project_id, user_id, permission)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, user_id) DO UPDATE SET permission = $3
		 RETURNING id, project_id, user_id, permission, created_at`,
		projectID, userID, permission,
	).Scan(&m.ID, &m.ProjectID, &m.UserID, &m.Permission, &m.CreatedAt)
	return m, err
}

func (d *DB) UpdateProjectMemberPermission(projectID, userID int, permission string) error {
	_, err := d.Exec(
		`UPDATE project_members SET permission = $1 WHERE project_id = $2 AND user_id = $3`,
		permission, projectID, userID,
	)
	return err
}

func (d *DB) RemoveProjectMember(projectID, userID int) error {
	_, err := d.Exec(`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID)
	return err
}

func (d *DB) ListProjectMembers(projectID int) ([]models.ProjectMemberView, error) {
	rows, err := d.Query(
		`SELECT pm.id, pm.project_id, pm.user_id, u.username, u.display_name, u.avatar_url, pm.permission, pm.created_at
		 FROM project_members pm
		 JOIN users u ON u.id = pm.user_id
		 WHERE pm.project_id = $1
		 ORDER BY u.username`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.ProjectMemberView
	for rows.Next() {
		var m models.ProjectMemberView
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL, &m.Permission, &m.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// ListProjectsForUser returns the projects a given employee has access to,
// along with their permission on each — used to render their sidebar.
// Admins implicitly get edit access to every project.
func (d *DB) ListProjectsForUser(userID int, isAdmin bool) ([]models.ProjectView, error) {
	if isAdmin {
		rows, err := d.Query(`SELECT id, name FROM projects ORDER BY name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var list []models.ProjectView
		for rows.Next() {
			var pv models.ProjectView
			if err := rows.Scan(&pv.ID, &pv.Name); err != nil {
				return nil, err
			}
			pv.Permission = "edit"
			list = append(list, pv)
		}
		return list, rows.Err()
	}

	rows, err := d.Query(
		`SELECT p.id, p.name, pm.permission
		 FROM project_members pm
		 JOIN projects p ON p.id = pm.project_id
		 WHERE pm.user_id = $1
		 ORDER BY p.name`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.ProjectView
	for rows.Next() {
		var pv models.ProjectView
		if err := rows.Scan(&pv.ID, &pv.Name, &pv.Permission); err != nil {
			return nil, err
		}
		list = append(list, pv)
	}
	return list, rows.Err()
}

// GetProjectMemberPermission returns the permission a user has on a project ("" if none).
func (d *DB) GetProjectMemberPermission(projectID, userID int) (string, error) {
	var perm string
	err := d.QueryRow(
		`SELECT permission FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID,
	).Scan(&perm)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return perm, err
}

// Dashboard

func (d *DB) GetDashboard() (*models.AdminDashboard, error) {
	dash := &models.AdminDashboard{}
	err := d.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'user'`).Scan(&dash.TotalUsers)
	if err != nil {
		return nil, err
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&dash.TotalProjects)
	_ = d.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&dash.TotalFiles)
	_ = d.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM files`).Scan(&dash.TotalBytes)
	return dash, nil
}

// GetStorageTrend returns one point per day for the last `days` days, each
// with the CUMULATIVE file count/total bytes as of that day — the admin
// dashboard's storage-growth chart. Counts every file regardless of
// deleted_at, matching GetDashboard's own TotalFiles/TotalBytes (a trashed
// file still occupies real MinIO space until purged), so the chart's final
// point always agrees with the dashboard's current-totals stat cards.
//
// Computed as a baseline (everything created before the window) plus daily
// deltas grouped in one query, then accumulated in Go — not a
// generate_series-of-days LEFT JOIN, which would run a correlated subquery
// per day and scale with days x files instead of just files.
func (d *DB) GetStorageTrend(days int) ([]models.StorageTrendPoint, error) {
	cutoff := time.Now().AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)

	var baseFiles int
	var baseBytes int64
	if err := d.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files WHERE created_at < $1`, cutoff,
	).Scan(&baseFiles, &baseBytes); err != nil {
		return nil, err
	}

	rows, err := d.Query(
		`SELECT created_at::date, COUNT(*), COALESCE(SUM(size_bytes), 0)
		 FROM files WHERE created_at >= $1
		 GROUP BY created_at::date`, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deltaFiles := map[string]int{}
	deltaBytes := map[string]int64{}
	for rows.Next() {
		var day time.Time
		var count int
		var bytes int64
		if err := rows.Scan(&day, &count, &bytes); err != nil {
			return nil, err
		}
		key := day.Format("2006-01-02")
		deltaFiles[key] = count
		deltaBytes[key] = bytes
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	points := make([]models.StorageTrendPoint, 0, days)
	runningFiles, runningBytes := baseFiles, baseBytes
	for i := 0; i < days; i++ {
		key := cutoff.AddDate(0, 0, i).Format("2006-01-02")
		runningFiles += deltaFiles[key]
		runningBytes += deltaBytes[key]
		points = append(points, models.StorageTrendPoint{Date: key, FileCount: runningFiles, TotalBytes: runningBytes})
	}
	return points, nil
}

package db

import (
	"database/sql"
	"fmt"
	"paylash/internal/models"
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

func (d *DB) GetProject(id int) (*models.Project, error) {
	p := &models.Project{}
	err := d.QueryRow(
		`SELECT id, name, quota_bytes, minio_bucket, created_at FROM projects WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.QuotaBytes, &p.MinioBucket, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (d *DB) UpdateProject(id int, name string, quotaBytes int64) error {
	_, err := d.Exec(`UPDATE projects SET name = $1, quota_bytes = $2 WHERE id = $3`, name, quotaBytes, id)
	return err
}

func (d *DB) DeleteProject(id int) error {
	_, err := d.Exec(`DELETE FROM projects WHERE id = $1`, id)
	return err
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
		`SELECT pm.id, pm.project_id, pm.user_id, u.username, u.display_name, pm.permission, pm.created_at
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
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.UserID, &m.Username, &m.DisplayName, &m.Permission, &m.CreatedAt); err != nil {
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

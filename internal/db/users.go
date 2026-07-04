package db

import (
	"database/sql"
	"paylash/internal/models"
)

func (d *DB) CreateUser(u *models.RegisterRequest, hash string) (*models.User, error) {
	user := &models.User{}
	err := d.QueryRow(
		`INSERT INTO users (username, password_hash, display_name, role)
		 VALUES ($1, $2, $3, 'user')
		 RETURNING id, username, display_name, role, quota_bytes, avatar_url, created_at`,
		u.Username, hash, u.FullName,
	).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.QuotaBytes, &user.AvatarURL, &user.CreatedAt)
	return user, err
}

func (d *DB) GetUserByUsername(username string) (*models.User, error) {
	u := &models.User{}
	err := d.QueryRow(
		`SELECT id, username, password_hash, display_name, role, quota_bytes, avatar_url, created_at
		 FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.QuotaBytes, &u.AvatarURL, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) GetUserByID(id int) (*models.User, error) {
	u := &models.User{}
	err := d.QueryRow(
		`SELECT id, username, password_hash, display_name, role, quota_bytes, avatar_url, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.QuotaBytes, &u.AvatarURL, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) SearchUsers(query string, limit int) ([]models.UserSearchResult, error) {
	rows, err := d.Query(
		`SELECT id, username, display_name
		 FROM users
		 WHERE role = 'user' AND (username ILIKE $1 OR display_name ILIKE $1)
		 ORDER BY username LIMIT $2`,
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []models.UserSearchResult
	for rows.Next() {
		var r models.UserSearchResult
		if err := rows.Scan(&r.ID, &r.Username, &r.DisplayName); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (d *DB) ListUsers() ([]models.User, error) {
	rows, err := d.Query(
		`SELECT id, username, display_name, role, quota_bytes, avatar_url, created_at
		 FROM users ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.QuotaBytes, &u.AvatarURL, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (d *DB) UpdateUser(id int, role string, quotaBytes int64, displayName string, passwordHash string) error {
	if passwordHash != "" {
		_, err := d.Exec(
			`UPDATE users SET role=$1, quota_bytes=$2, display_name=$3, password_hash=$4 WHERE id=$5`,
			role, quotaBytes, displayName, passwordHash, id,
		)
		return err
	}
	_, err := d.Exec(
		`UPDATE users SET role=$1, quota_bytes=$2, display_name=$3 WHERE id=$4`,
		role, quotaBytes, displayName, id,
	)
	return err
}

func (d *DB) DeleteUser(id int) error {
	_, err := d.Exec(`DELETE FROM users WHERE id = $1`, id)
	return err
}

func (d *DB) DeleteAllUsersExceptAdmin() (int64, error) {
	res, err := d.Exec(`DELETE FROM users WHERE role != 'admin'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) UpdateDisplayName(id int, name string) error {
	_, err := d.Exec(`UPDATE users SET display_name = $1 WHERE id = $2`, name, id)
	return err
}

func (d *DB) UpdatePassword(id int, hash string) error {
	_, err := d.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, id)
	return err
}

func (d *DB) UpdateAvatarURL(id int, url string) error {
	_, err := d.Exec(`UPDATE users SET avatar_url = $1 WHERE id = $2`, url, id)
	return err
}

func (d *DB) SetAllUsersQuota(quotaBytes int64) error {
	_, err := d.Exec(`UPDATE users SET quota_bytes = $1 WHERE role = 'user'`, quotaBytes)
	return err
}

func (d *DB) SetAllProjectsQuota(quotaBytes int64) error {
	_, err := d.Exec(`UPDATE projects SET quota_bytes = $1`, quotaBytes)
	return err
}

func (d *DB) UserExists(username string) (bool, error) {
	var exists bool
	err := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists)
	return exists, err
}

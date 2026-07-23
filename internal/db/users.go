package db

import (
	"database/sql"
	"paylash/internal/models"
)

// mustChangePassword should be true whenever the caller (not the user)
// chose the password — admin-provisioned and CSV-imported accounts — so the
// employee is forced to set their own on first login. Self-registration
// passes false since the user already picked their own password.
func (d *DB) CreateUser(u *models.RegisterRequest, hash string, mustChangePassword bool) (*models.User, error) {
	user := &models.User{}
	err := d.QueryRow(
		`INSERT INTO users (username, password_hash, display_name, role, must_change_password)
		 VALUES ($1, $2, $3, 'user', $4)
		 RETURNING id, username, display_name, role, quota_bytes, avatar_url, must_change_password, created_at`,
		u.Username, hash, u.FullName, mustChangePassword,
	).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.QuotaBytes, &user.AvatarURL, &user.MustChangePassword, &user.CreatedAt)
	return user, err
}

// CountAdmins is used to block operations that would leave the studio with
// zero admins (demoting or deleting the last one) and lock everyone out.
func (d *DB) CountAdmins() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n, err
}

func (d *DB) GetUserByUsername(username string) (*models.User, error) {
	u := &models.User{}
	err := d.QueryRow(
		`SELECT id, username, password_hash, display_name, role, quota_bytes, avatar_url, must_change_password, created_at
		 FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.QuotaBytes, &u.AvatarURL, &u.MustChangePassword, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) GetUserByID(id int) (*models.User, error) {
	u := &models.User{}
	err := d.QueryRow(
		`SELECT id, username, password_hash, display_name, role, quota_bytes, avatar_url, must_change_password, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.QuotaBytes, &u.AvatarURL, &u.MustChangePassword, &u.CreatedAt)
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

func (d *DB) ListUsers(limit, offset int) ([]models.User, error) {
	rows, err := d.Query(
		`SELECT id, username, display_name, role, quota_bytes, avatar_url, must_change_password, created_at
		 FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.QuotaBytes, &u.AvatarURL, &u.MustChangePassword, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// A non-empty passwordHash means the admin (not the employee) is setting
// the password, so must_change_password is set — same reasoning as
// CreateUser's mustChangePassword parameter.
func (d *DB) UpdateUser(id int, role string, quotaBytes int64, displayName string, passwordHash string) error {
	if passwordHash != "" {
		_, err := d.Exec(
			`UPDATE users SET role=$1, quota_bytes=$2, display_name=$3, password_hash=$4, must_change_password=TRUE WHERE id=$5`,
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

// ReassignAndDeleteUser reassigns fromUserID's common/project-scope content
// to toUserID and then deletes fromUserID, all in one transaction — so a
// crash mid-operation can never leave the account half-deleted (reassigned
// but not removed, which would just retry cleanly next time) or, worse,
// deleted while its shared contribution was still cascading away with it.
func (d *DB) ReassignAndDeleteUser(fromUserID, toUserID int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE files SET owner_id = $2 WHERE owner_id = $1 AND scope != 'personal'`, fromUserID, toUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE folders SET owner_id = $2 WHERE owner_id = $1 AND scope != 'personal'`, fromUserID, toUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id = $1`, fromUserID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListNonAdminUserIDs returns the IDs that DeleteAllUsersExceptAdmin would
// remove — callers use it to clean up per-user MinIO storage beforehand.
func (d *DB) ListNonAdminUserIDs() ([]int, error) {
	rows, err := d.Query(`SELECT id FROM users WHERE role != 'admin'`)
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

// UpdatePassword also clears must_change_password — any voluntary password
// change satisfies whatever required it, regardless of how it got set.
func (d *DB) UpdatePassword(id int, hash string) error {
	_, err := d.Exec(`UPDATE users SET password_hash = $1, must_change_password = FALSE WHERE id = $2`, hash, id)
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

// UnreadShareCount counts point-to-point shares made to userID since they
// last checked the Shared page (see notifications_seen_at) — the number the
// sidebar badge shows. Mirrors GetSharedWithMe's own filtering (deleted
// files excluded) so the badge count never disagrees with what the page
// itself actually lists.
func (d *DB) UnreadShareCount(userID int) (int, error) {
	var n int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM file_shares fs
		 JOIN files f ON f.id = fs.file_id
		 JOIN users u ON u.id = fs.shared_with
		 WHERE fs.shared_with = $1 AND f.deleted_at IS NULL AND fs.created_at > u.notifications_seen_at`,
		userID,
	).Scan(&n)
	return n, err
}

// MarkNotificationsSeen resets userID's checkpoint to now — called when they
// open the "shared with me" tab, clearing the badge for everything that
// exists as of that moment (including anything that arrived between the
// last poll and this call, which is the correct behavior: they're looking
// at the page right now).
func (d *DB) MarkNotificationsSeen(userID int) error {
	_, err := d.Exec(`UPDATE users SET notifications_seen_at = NOW() WHERE id = $1`, userID)
	return err
}

func (d *DB) UserExists(username string) (bool, error) {
	var exists bool
	err := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists)
	return exists, err
}

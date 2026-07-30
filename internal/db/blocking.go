package db

import "paylash/internal/models"

// BlockUser records that blockerID no longer wants to hear from blockedID —
// checked symmetrically by SendMessage (see IsEitherBlocked) before either
// side can send a new direct message, and by SearchChatUsers so a blocked
// person doesn't show up when starting a new DM. Existing message history is
// untouched.
func (d *DB) BlockUser(blockerID, blockedID int) error {
	_, err := d.Exec(
		`INSERT INTO blocked_users (blocker_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		blockerID, blockedID,
	)
	return err
}

func (d *DB) UnblockUser(blockerID, blockedID int) error {
	_, err := d.Exec(`DELETE FROM blocked_users WHERE blocker_id = $1 AND blocked_id = $2`, blockerID, blockedID)
	return err
}

func (d *DB) IsBlocked(blockerID, blockedID int) (bool, error) {
	var exists bool
	err := d.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM blocked_users WHERE blocker_id = $1 AND blocked_id = $2)`,
		blockerID, blockedID,
	).Scan(&exists)
	return exists, err
}

// IsEitherBlocked reports whether userA has blocked userB OR userB has
// blocked userA — the direct-message send gate is symmetric: once either
// side has blocked the other, neither can start a new message to them.
func (d *DB) IsEitherBlocked(userA, userB int) (bool, error) {
	var exists bool
	err := d.QueryRow(
		`SELECT EXISTS(
			SELECT 1 FROM blocked_users
			WHERE (blocker_id = $1 AND blocked_id = $2) OR (blocker_id = $2 AND blocked_id = $1)
		)`,
		userA, userB,
	).Scan(&exists)
	return exists, err
}

// ListBlockedUserIDs backs SearchChatUsers' exclusion filter.
func (d *DB) ListBlockedUserIDs(blockerID int) ([]int, error) {
	rows, err := d.Query(`SELECT blocked_id FROM blocked_users WHERE blocker_id = $1`, blockerID)
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

// ListBlockedUsers returns the people blockerID has blocked, with enough
// display info for a "Blocked users" management list.
func (d *DB) ListBlockedUsers(blockerID int) ([]models.UserSearchResult, error) {
	rows, err := d.Query(
		`SELECT u.id, u.username, COALESCE(u.display_name, u.username, ''), u.avatar_url
		 FROM blocked_users b JOIN users u ON u.id = b.blocked_id
		 WHERE b.blocker_id = $1 ORDER BY u.username`,
		blockerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.UserSearchResult
	for rows.Next() {
		var r models.UserSearchResult
		if err := rows.Scan(&r.ID, &r.Username, &r.DisplayName, &r.AvatarURL); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

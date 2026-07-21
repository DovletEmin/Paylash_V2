package db

import (
	"database/sql"

	"paylash/internal/models"
)

// CreateComment inserts a review note on a file. UserName is left blank —
// the caller already knows the requesting user's display name and fills it
// in without a second query.
func (d *DB) CreateComment(fileID, userID int, body string, xPct, yPct *float64) (*models.FileComment, error) {
	c := &models.FileComment{}
	err := d.QueryRow(
		`INSERT INTO file_comments (file_id, user_id, body, x_pct, y_pct)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, file_id, user_id, body, x_pct, y_pct, created_at`,
		fileID, userID, body, xPct, yPct,
	).Scan(&c.ID, &c.FileID, &c.UserID, &c.Body, &c.XPct, &c.YPct, &c.CreatedAt)
	return c, err
}

func (d *DB) ListComments(fileID int) ([]models.FileComment, error) {
	rows, err := d.Query(
		`SELECT fc.id, fc.file_id, fc.user_id, COALESCE(u.display_name, u.username, ''), fc.body, fc.x_pct, fc.y_pct, fc.created_at
		 FROM file_comments fc
		 JOIN users u ON u.id = fc.user_id
		 WHERE fc.file_id = $1
		 ORDER BY fc.created_at ASC`, fileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.FileComment
	for rows.Next() {
		var c models.FileComment
		if err := rows.Scan(&c.ID, &c.FileID, &c.UserID, &c.UserName, &c.Body, &c.XPct, &c.YPct, &c.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// GetComment fetches a bare comment row (no author join — callers that need
// it, like DeleteFileComment's permission check, only need FileID/UserID).
func (d *DB) GetComment(id int) (*models.FileComment, error) {
	c := &models.FileComment{}
	err := d.QueryRow(
		`SELECT id, file_id, user_id, body, x_pct, y_pct, created_at FROM file_comments WHERE id = $1`, id,
	).Scan(&c.ID, &c.FileID, &c.UserID, &c.Body, &c.XPct, &c.YPct, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d *DB) DeleteComment(id int) error {
	_, err := d.Exec(`DELETE FROM file_comments WHERE id = $1`, id)
	return err
}

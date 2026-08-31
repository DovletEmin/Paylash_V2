package db

import (
	"database/sql"
	"encoding/json"

	"paylash/internal/models"
)

// ListAnnotations returns every author's markup layer for one file, oldest
// author first so the paint order is stable between viewers — otherwise two
// people looking at the same image could see overlapping strokes stacked
// differently.
func (d *DB) ListAnnotations(fileID int) ([]models.FileAnnotation, error) {
	rows, err := d.Query(
		`SELECT fa.id, fa.file_id, fa.user_id, COALESCE(u.display_name, u.username, ''), u.avatar_url, fa.shapes, fa.updated_at
		 FROM file_annotations fa
		 JOIN users u ON u.id = fa.user_id
		 WHERE fa.file_id = $1
		 ORDER BY fa.created_at ASC, fa.id ASC`, fileID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.FileAnnotation
	for rows.Next() {
		var a models.FileAnnotation
		var raw []byte
		if err := rows.Scan(&a.ID, &a.FileID, &a.UserID, &a.UserName, &a.UserAvatar, &raw, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.Shapes = json.RawMessage(raw)
		list = append(list, a)
	}
	return list, rows.Err()
}

// SaveAnnotation writes the caller's own layer for a file, replacing
// whatever they had before. The UNIQUE(file_id, user_id) constraint turns
// this into an upsert, so the client never has to know whether it is
// creating or updating — it just posts the current state of its canvas.
func (d *DB) SaveAnnotation(fileID, userID int, shapes json.RawMessage) (*models.FileAnnotation, error) {
	a := &models.FileAnnotation{}
	var raw []byte
	err := d.QueryRow(
		`INSERT INTO file_annotations (file_id, user_id, shapes, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (file_id, user_id)
		 DO UPDATE SET shapes = EXCLUDED.shapes, updated_at = NOW()
		 RETURNING id, file_id, user_id, shapes, updated_at`,
		fileID, userID, []byte(shapes),
	).Scan(&a.ID, &a.FileID, &a.UserID, &raw, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	a.Shapes = json.RawMessage(raw)
	return a, nil
}

// GetAnnotation fetches one layer by id without the author join — callers
// that need it (the delete permission check) only want FileID/UserID.
func (d *DB) GetAnnotation(id int) (*models.FileAnnotation, error) {
	a := &models.FileAnnotation{}
	var raw []byte
	err := d.QueryRow(
		`SELECT id, file_id, user_id, shapes, updated_at FROM file_annotations WHERE id = $1`, id,
	).Scan(&a.ID, &a.FileID, &a.UserID, &raw, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Shapes = json.RawMessage(raw)
	return a, nil
}

// GetAnnotationFor looks up one author's layer on one file — the lookup
// SaveFileAnnotation needs to turn "the user erased everything" into a
// delete rather than storing an empty drawing.
func (d *DB) GetAnnotationFor(fileID, userID int) (*models.FileAnnotation, error) {
	a := &models.FileAnnotation{}
	var raw []byte
	err := d.QueryRow(
		`SELECT id, file_id, user_id, shapes, updated_at FROM file_annotations WHERE file_id = $1 AND user_id = $2`,
		fileID, userID,
	).Scan(&a.ID, &a.FileID, &a.UserID, &raw, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Shapes = json.RawMessage(raw)
	return a, nil
}

func (d *DB) DeleteAnnotation(id int) error {
	_, err := d.Exec(`DELETE FROM file_annotations WHERE id = $1`, id)
	return err
}

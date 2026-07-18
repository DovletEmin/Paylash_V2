package db

import (
	"database/sql"
	"encoding/json"
	"strconv"

	"paylash/internal/models"
)

// LogAction records one admin-oversight-worthy event. actorID/targetID are
// pointers so a missing actor (a blocked login attempt with no real
// authenticated user) or missing target can be stored as SQL NULL rather
// than a fabricated sentinel value. Best-effort by design — callers should
// log a failure and continue rather than let audit logging block the
// action it's describing.
func (d *DB) LogAction(actorID *int, actorName, action, targetType string, targetID *int, targetName string, details map[string]any) error {
	var detailsJSON []byte
	if details != nil {
		var err error
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			return err
		}
	}
	_, err := d.Exec(
		`INSERT INTO audit_log (actor_id, actor_name, action, target_type, target_id, target_name, details)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		actorID, actorName, action, targetType, targetID, targetName, detailsJSON,
	)
	return err
}

// ListAuditLog returns entries newest-first, optionally filtered by action
// and/or actor.
func (d *DB) ListAuditLog(limit, offset int, action string, actorID int) ([]models.AuditLogEntry, error) {
	q := `SELECT id, actor_id, actor_name, action, target_type, target_id, target_name, details, created_at FROM audit_log WHERE 1=1`
	var args []any
	n := 0
	if action != "" {
		n++
		q += ` AND action = $` + strconv.Itoa(n)
		args = append(args, action)
	}
	if actorID > 0 {
		n++
		q += ` AND actor_id = $` + strconv.Itoa(n)
		args = append(args, actorID)
	}
	q += ` ORDER BY created_at DESC`
	n++
	q += ` LIMIT $` + strconv.Itoa(n)
	args = append(args, limit)
	n++
	q += ` OFFSET $` + strconv.Itoa(n)
	args = append(args, offset)

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []models.AuditLogEntry
	for rows.Next() {
		var e models.AuditLogEntry
		// actor_id/target_id/details are nullable columns — scan them into
		// explicit nullable types rather than *int/json.RawMessage directly,
		// since database/sql doesn't natively support a **int scan target.
		var actorIDN, targetIDN sql.NullInt64
		var details []byte
		if err := rows.Scan(&e.ID, &actorIDN, &e.ActorName, &e.Action, &e.TargetType, &targetIDN, &e.TargetName, &details, &e.CreatedAt); err != nil {
			return nil, err
		}
		if actorIDN.Valid {
			v := int(actorIDN.Int64)
			e.ActorID = &v
		}
		if targetIDN.Valid {
			v := int(targetIDN.Int64)
			e.TargetID = &v
		}
		if len(details) > 0 {
			e.Details = details
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

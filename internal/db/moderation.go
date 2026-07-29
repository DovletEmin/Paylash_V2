package db

import (
	"database/sql"
	"paylash/internal/models"
)

// CreateChatReport files a moderation report against a message and/or a
// user within a conversation the reporter is a participant of. Exactly one
// of messageID/reportedUserID is expected to be set by the caller (the API
// layer), but nothing here enforces that — a report naming both, or a bare
// conversation-level report naming neither, is still a valid row.
// bodySnapshot is captured at report time so a later edit/delete of the
// message can't erase what was actually reported.
func (d *DB) CreateChatReport(reporterID, conversationID int, messageID, reportedUserID *int, reason, bodySnapshot string) (int, error) {
	var id int
	err := d.QueryRow(
		`INSERT INTO chat_reports (reporter_id, conversation_id, message_id, reported_user_id, reason, message_body_snapshot)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		reporterID, conversationID, messageID, reportedUserID, reason, bodySnapshot,
	).Scan(&id)
	return id, err
}

// ListChatReports returns moderation reports newest-first, optionally
// filtered to one status ("open" | "resolved" | "dismissed"); "" returns
// every status. See ChatReportView for what an admin can and can't see here.
func (d *DB) ListChatReports(status string) ([]models.ChatReportView, error) {
	query := `
		SELECT r.id, r.reporter_id, COALESCE(ru.display_name, ru.username, ''),
		       r.conversation_id, c.type, COALESCE(c.name, ''),
		       r.message_id, r.reported_user_id, COALESCE(tu.display_name, tu.username, ''),
		       r.reason, r.message_body_snapshot, r.status,
		       COALESCE(rb.display_name, rb.username, ''), r.resolved_at, r.created_at
		FROM chat_reports r
		JOIN conversations c ON c.id = r.conversation_id
		LEFT JOIN users ru ON ru.id = r.reporter_id
		LEFT JOIN users tu ON tu.id = r.reported_user_id
		LEFT JOIN users rb ON rb.id = r.resolved_by`
	args := []any{}
	if status != "" {
		query += ` WHERE r.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY r.created_at DESC`

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.ChatReportView
	for rows.Next() {
		var v models.ChatReportView
		var convType, convName, resolvedByName string
		var resolvedAt sql.NullTime
		if err := rows.Scan(
			&v.ID, &v.ReporterID, &v.ReporterName,
			&v.ConversationID, &convType, &convName,
			&v.MessageID, &v.ReportedUserID, &v.ReportedUserName,
			&v.Reason, &v.MessageBodySnapshot, &v.Status,
			&resolvedByName, &resolvedAt, &v.CreatedAt,
		); err != nil {
			return nil, err
		}
		v.ConversationType = convType
		if convType == "group" {
			v.ConversationLabel = convName
		}
		v.ResolvedByName = resolvedByName
		if resolvedAt.Valid {
			t := resolvedAt.Time
			v.ResolvedAt = &t
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// CountOpenChatReports backs the admin sidebar's report badge.
func (d *DB) CountOpenChatReports() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM chat_reports WHERE status = 'open'`).Scan(&n)
	return n, err
}

// ResolveChatReport marks a report resolved or dismissed by an admin —
// status must be "resolved" or "dismissed" (validated by the caller).
func (d *DB) ResolveChatReport(id, resolverID int, status string) error {
	_, err := d.Exec(
		`UPDATE chat_reports SET status = $1, resolved_by = $2, resolved_at = NOW() WHERE id = $3`,
		status, resolverID, id,
	)
	return err
}

package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"paylash/internal/models"
)

// SearchChatUsers is like SearchUsers but without the `role = 'user'`
// filter — SearchUsers deliberately hides the admin account from the share
// picker (nothing to "share" with someone who already sees everything), but
// messaging the studio lead is a completely normal thing to want in chat.
func (d *DB) SearchChatUsers(query string, excludeUserID, limit int) ([]models.UserSearchResult, error) {
	rows, err := d.Query(
		`SELECT id, username, display_name
		 FROM users
		 WHERE id != $1 AND (username ILIKE $2 OR display_name ILIKE $2)
		 ORDER BY username LIMIT $3`,
		excludeUserID, "%"+query+"%", limit,
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

// FindOrCreateDirectConversation returns the existing direct conversation
// between two users, creating one (plus both participant rows) if it
// doesn't exist yet. direct_user_low/high (always min/max of the pair) plus
// a partial unique index on them closes the race window completely — two
// people opening a DM with each other at the same instant can't end up with
// two conversations.
func (d *DB) FindOrCreateDirectConversation(userA, userB int) (*models.Conversation, error) {
	low, high := userA, userB
	if low > high {
		low, high = high, low
	}

	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int
	err = tx.QueryRow(
		`INSERT INTO conversations (type, direct_user_low, direct_user_high, created_by)
		 VALUES ('direct', $1, $2, $3)
		 ON CONFLICT (direct_user_low, direct_user_high) WHERE type = 'direct' DO NOTHING
		 RETURNING id`,
		low, high, userA,
	).Scan(&id)
	if err == sql.ErrNoRows {
		// Conflict hit — the pair already exists, ON CONFLICT DO NOTHING
		// returns no row, so fetch the existing conversation's id instead.
		err = tx.QueryRow(
			`SELECT id FROM conversations WHERE type = 'direct' AND direct_user_low = $1 AND direct_user_high = $2`,
			low, high,
		).Scan(&id)
	}
	if err != nil {
		return nil, err
	}

	// ON CONFLICT DO NOTHING here too: the "already existed" path above
	// re-enters this same insert, which would otherwise violate
	// conversation_participants' UNIQUE(conversation_id, user_id).
	if _, err := tx.Exec(
		`INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1,$2),($1,$3)
		 ON CONFLICT (conversation_id, user_id) DO NOTHING`,
		id, userA, userB,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetConversation(id)
}

// CreateGroupConversation always includes createdBy as a participant,
// de-duplicated against participantIDs in case the caller included them.
func (d *DB) CreateGroupConversation(name string, createdBy int, projectID *int, participantIDs []int) (*models.Conversation, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int
	err = tx.QueryRow(
		`INSERT INTO conversations (type, name, project_id, created_by)
		 VALUES ('group', $1, $2, $3) RETURNING id`,
		name, projectID, createdBy,
	).Scan(&id)
	if err != nil {
		return nil, err
	}

	ids := map[int]bool{createdBy: true}
	for _, uid := range participantIDs {
		ids[uid] = true
	}
	for uid := range ids {
		if _, err := tx.Exec(
			`INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1, $2)`,
			id, uid,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetConversation(id)
}

func (d *DB) GetConversation(id int) (*models.Conversation, error) {
	c := &models.Conversation{}
	err := d.QueryRow(
		`SELECT id, type, name, project_id, created_by, direct_user_low, direct_user_high, last_message_at, created_at
		 FROM conversations WHERE id = $1`, id,
	).Scan(&c.ID, &c.Type, &c.Name, &c.ProjectID, &c.CreatedBy, &c.DirectUserLow, &c.DirectUserHigh, &c.LastMessageAt, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// ListConversationsForUser returns every conversation userID participates
// in, newest-activity first, each with an unread count (messages since that
// user's own last_read_at, excluding their own), a last-message preview,
// and — for direct conversations — the other participant's info, so the
// client never needs a second round-trip to render the inbox.
func (d *DB) ListConversationsForUser(userID int) ([]models.ConversationView, error) {
	rows, err := d.Query(`
		SELECT c.id, c.type, c.name, c.project_id, c.created_by, c.last_message_at, c.created_at,
		       COALESCE(unread.cnt, 0) AS unread_count,
		       lm.body, lm.created_at,
		       other.user_id, other.username, other.display_name, other.avatar_url
		FROM conversations c
		JOIN conversation_participants cp ON cp.conversation_id = c.id AND cp.user_id = $1
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt FROM messages m
			WHERE m.conversation_id = c.id AND m.created_at > cp.last_read_at
			  AND m.deleted_at IS NULL AND m.sender_id IS DISTINCT FROM $1
		) unread ON true
		LEFT JOIN LATERAL (
			SELECT body, created_at FROM messages m2
			WHERE m2.conversation_id = c.id
			ORDER BY m2.created_at DESC, m2.id DESC LIMIT 1
		) lm ON true
		LEFT JOIN LATERAL (
			SELECT u.id AS user_id, u.username, u.display_name, u.avatar_url
			FROM conversation_participants cp2
			JOIN users u ON u.id = cp2.user_id
			WHERE cp2.conversation_id = c.id AND cp2.user_id != $1 AND c.type = 'direct'
			LIMIT 1
		) other ON true
		ORDER BY c.last_message_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.ConversationView
	for rows.Next() {
		var cv models.ConversationView
		var lastBody sql.NullString
		var lastAt sql.NullTime
		var otherID sql.NullInt64
		var otherUsername, otherDisplayName, otherAvatar sql.NullString
		if err := rows.Scan(
			&cv.ID, &cv.Type, &cv.Name, &cv.ProjectID, &cv.CreatedBy, &cv.LastMessageAt, &cv.CreatedAt,
			&cv.UnreadCount, &lastBody, &lastAt,
			&otherID, &otherUsername, &otherDisplayName, &otherAvatar,
		); err != nil {
			return nil, err
		}
		if lastBody.Valid {
			cv.LastMessageBody = lastBody.String
		}
		if lastAt.Valid {
			t := lastAt.Time
			cv.LastMessageAt = &t
		}
		if otherID.Valid {
			cv.OtherParticipant = &models.ParticipantView{
				UserID:      int(otherID.Int64),
				Username:    otherUsername.String,
				DisplayName: otherDisplayName.String,
				AvatarURL:   otherAvatar.String,
			}
		}
		list = append(list, cv)
	}
	return list, rows.Err()
}

func (d *DB) IsParticipant(conversationID, userID int) (bool, error) {
	var exists bool
	err := d.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id = $1 AND user_id = $2)`,
		conversationID, userID,
	).Scan(&exists)
	return exists, err
}

func (d *DB) RenameConversation(id int, name string) error {
	_, err := d.Exec(`UPDATE conversations SET name = $1 WHERE id = $2`, name, id)
	return err
}

func (d *DB) AddParticipants(conversationID int, userIDs []int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, uid := range userIDs {
		if _, err := tx.Exec(
			`INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1, $2)
			 ON CONFLICT (conversation_id, user_id) DO NOTHING`,
			conversationID, uid,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveParticipant removes a participant. If they were the conversation's
// creator, ownership transfers to another remaining participant (earliest to
// join) — group management (rename/add/remove) is creator-only by design, so
// without this a creator leaving their own group would permanently strand it
// with no one able to manage it, even though the group itself still exists
// and is still usable for messaging.
func (d *DB) RemoveParticipant(conversationID, userID int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var createdBy sql.NullInt64
	if err := tx.QueryRow(`SELECT created_by FROM conversations WHERE id = $1`, conversationID).Scan(&createdBy); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM conversation_participants WHERE conversation_id = $1 AND user_id = $2`, conversationID, userID); err != nil {
		return err
	}

	if createdBy.Valid && int(createdBy.Int64) == userID {
		var newOwner sql.NullInt64
		err := tx.QueryRow(
			`SELECT user_id FROM conversation_participants WHERE conversation_id = $1 ORDER BY joined_at LIMIT 1`,
			conversationID,
		).Scan(&newOwner)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if newOwner.Valid {
			if _, err := tx.Exec(`UPDATE conversations SET created_by = $1 WHERE id = $2`, newOwner.Int64, conversationID); err != nil {
				return err
			}
		}
		// No remaining participants: the group is now empty, nothing to
		// transfer to — fine as-is, matches how an empty direct conversation
		// (both sides having left, if that were possible) would look too.
	}

	return tx.Commit()
}

func (d *DB) ListParticipants(conversationID int) ([]models.ParticipantView, error) {
	rows, err := d.Query(
		`SELECT u.id, u.username, COALESCE(u.display_name, u.username, ''), u.avatar_url, cp.last_read_at
		 FROM conversation_participants cp
		 JOIN users u ON u.id = cp.user_id
		 WHERE cp.conversation_id = $1
		 ORDER BY u.username`, conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.ParticipantView
	for rows.Next() {
		var p models.ParticipantView
		if err := rows.Scan(&p.UserID, &p.Username, &p.DisplayName, &p.AvatarURL, &p.LastReadAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (d *DB) MarkConversationRead(conversationID, userID int) error {
	_, err := d.Exec(
		`UPDATE conversation_participants SET last_read_at = NOW() WHERE conversation_id = $1 AND user_id = $2`,
		conversationID, userID,
	)
	return err
}

// TotalUnreadCount sums unread messages (excluding the user's own) across
// every conversation they participate in — backs the sidebar chat badge.
func (d *DB) TotalUnreadCount(userID int) (int, error) {
	var count int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id AND cp.user_id = $1
		WHERE m.created_at > cp.last_read_at AND m.deleted_at IS NULL AND m.sender_id IS DISTINCT FROM $1`,
		userID,
	).Scan(&count)
	return count, err
}

// CreateMessage inserts the message and, in the same transaction, claims
// any already-uploaded attachments referenced by id (rejecting the whole
// send if one doesn't belong to this conversation/sender or was already
// claimed by another message — see the rows-affected check below). kind is
// "text" or "sticker"; replyToID is validated by the caller (API layer) to
// belong to the same conversation before this is reached.
func (d *DB) CreateMessage(conversationID, senderID int, body, kind string, replyToID *int, attachmentIDs []int) (*models.MessageView, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var msgID int
	var createdAt time.Time
	if err := tx.QueryRow(
		`INSERT INTO messages (conversation_id, sender_id, body, kind, reply_to_id) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		conversationID, senderID, body, kind, replyToID,
	).Scan(&msgID, &createdAt); err != nil {
		return nil, err
	}

	if len(attachmentIDs) > 0 {
		res, err := tx.Exec(
			`UPDATE message_attachments SET message_id = $1
			 WHERE id = ANY($2) AND conversation_id = $3 AND uploaded_by = $4 AND message_id IS NULL`,
			msgID, pq.Array(attachmentIDs), conversationID, senderID,
		)
		if err != nil {
			return nil, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if int(affected) != len(attachmentIDs) {
			return nil, fmt.Errorf("one or more attachments are invalid, already used, or not yours")
		}
	}

	if _, err := tx.Exec(`UPDATE conversations SET last_message_at = $1 WHERE id = $2`, createdAt, conversationID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetMessageView(msgID)
}

// messageViewCols/messageViewJoins are shared by GetMessageView and
// ListMessages so the two queries can't drift out of sync with each other.
// other_read.min_read_at is the earliest last_read_at among every OTHER
// participant — meaningful as a "read" cutoff for direct conversations
// (exactly one other participant); group conversations always fall back to
// a plain "sent" status (see scanMessageView), so what this resolves to for
// a group is never actually used.
const messageViewCols = `m.id, m.conversation_id, m.sender_id, m.body, m.kind, m.edited_at, m.reply_to_id, m.forwarded_from_name, m.deleted_at, m.created_at,
	COALESCE(u.display_name, u.username, ''), COALESCE(u.avatar_url, ''),
	COALESCE(c.type, ''), other_read.min_read_at,
	rm.id, COALESCE(ru.display_name, ru.username, ''), COALESCE(rm.body, ''), COALESCE(rm.kind, '')`

const messageViewJoins = `FROM messages m
	LEFT JOIN users u ON u.id = m.sender_id
	LEFT JOIN conversations c ON c.id = m.conversation_id
	LEFT JOIN LATERAL (
		SELECT MIN(cp.last_read_at) AS min_read_at
		FROM conversation_participants cp
		WHERE cp.conversation_id = m.conversation_id AND cp.user_id != m.sender_id
	) other_read ON true
	LEFT JOIN messages rm ON rm.id = m.reply_to_id
	LEFT JOIN users ru ON ru.id = rm.sender_id`

// scanner is satisfied by both *sql.Row and *sql.Rows — lets GetMessageView
// and ListMessages share one Scan-and-assemble routine for messageViewCols.
type scanner interface {
	Scan(dest ...any) error
}

func scanMessageView(row scanner) (*models.MessageView, error) {
	mv := &models.MessageView{}
	var convType string
	var otherRead sql.NullTime
	var replyID sql.NullInt64
	var replySender, replyBody, replyKind string
	if err := row.Scan(
		&mv.ID, &mv.ConversationID, &mv.SenderID, &mv.Body, &mv.Kind, &mv.EditedAt, &mv.ReplyToID, &mv.ForwardedFromName, &mv.DeletedAt, &mv.CreatedAt,
		&mv.SenderName, &mv.SenderAvatar,
		&convType, &otherRead,
		&replyID, &replySender, &replyBody, &replyKind,
	); err != nil {
		return nil, err
	}
	if mv.SenderID != nil {
		if convType == "direct" && otherRead.Valid && !mv.CreatedAt.After(otherRead.Time) {
			mv.Status = "read"
		} else {
			mv.Status = "sent"
		}
	}
	if replyID.Valid {
		mv.ReplyTo = &models.MessageReplyPreview{ID: int(replyID.Int64), SenderName: replySender, Body: replyBody, Kind: replyKind}
	}
	return mv, nil
}

// GetMessageView fetches a single message joined with sender info and
// resolved attachments — used to return the freshly-created/edited message
// and by the WS hub to build the broadcast payload.
func (d *DB) GetMessageView(id int) (*models.MessageView, error) {
	row := d.QueryRow(`SELECT `+messageViewCols+` `+messageViewJoins+` WHERE m.id = $1`, id)
	mv, err := scanMessageView(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	attachments, err := d.ListMessageAttachments(id)
	if err != nil {
		return nil, err
	}
	mv.Attachments = attachments
	return mv, nil
}

// ListMessages returns up to limit messages older than beforeID (0 means
// "most recent"), newest-first — keyset pagination, not offset/limit, so
// scrolling up through history stays correct while new messages keep
// arriving at the top. Messages requesterID has hidden-for-themselves
// (delete-for-me) are excluded — nobody else's view is affected.
func (d *DB) ListMessages(conversationID, requesterID, beforeID, limit int) ([]models.MessageView, error) {
	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = d.Query(
			`SELECT `+messageViewCols+` `+messageViewJoins+`
			 LEFT JOIN message_hidden_for hf ON hf.message_id = m.id AND hf.user_id = $4
			 WHERE m.conversation_id = $1 AND m.id < $2 AND hf.message_id IS NULL
			 ORDER BY m.created_at DESC, m.id DESC LIMIT $3`,
			conversationID, beforeID, limit, requesterID,
		)
	} else {
		rows, err = d.Query(
			`SELECT `+messageViewCols+` `+messageViewJoins+`
			 LEFT JOIN message_hidden_for hf ON hf.message_id = m.id AND hf.user_id = $3
			 WHERE m.conversation_id = $1 AND hf.message_id IS NULL
			 ORDER BY m.created_at DESC, m.id DESC LIMIT $2`,
			conversationID, limit, requesterID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	byID := map[int]*models.MessageView{}
	var list []models.MessageView
	for rows.Next() {
		mv, err := scanMessageView(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *mv)
		ids = append(ids, mv.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range list {
		byID[list[i].ID] = &list[i]
	}

	// One extra query for every attachment belonging to any message in this
	// page, rather than N — cheap at this app's scale and avoids a
	// per-message round trip.
	if len(ids) > 0 {
		attRows, err := d.Query(
			`SELECT id, message_id, conversation_id, uploaded_by, file_name, size_bytes, content_type, created_at
			 FROM message_attachments WHERE message_id = ANY($1)`, pq.Array(ids),
		)
		if err != nil {
			return nil, err
		}
		defer attRows.Close()
		for attRows.Next() {
			var a models.MessageAttachment
			if err := attRows.Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.UploadedBy, &a.FileName, &a.SizeBytes, &a.ContentType, &a.CreatedAt); err != nil {
				return nil, err
			}
			if a.MessageID != nil {
				if mv, ok := byID[*a.MessageID]; ok {
					mv.Attachments = append(mv.Attachments, a)
				}
			}
		}
		if err := attRows.Err(); err != nil {
			return nil, err
		}
	}
	return list, nil
}

func (d *DB) GetMessage(id int) (*models.Message, error) {
	m := &models.Message{}
	err := d.QueryRow(
		`SELECT id, conversation_id, sender_id, body, kind, edited_at, reply_to_id, forwarded_from_name, deleted_at, created_at FROM messages WHERE id = $1`, id,
	).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.Kind, &m.EditedAt, &m.ReplyToID, &m.ForwardedFromName, &m.DeletedAt, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// SoftDeleteMessage blanks the body and stamps deleted_at, deleting any
// attachments outright (both their MinIO objects, by the caller, and their
// rows here) — the message row itself stays so open tabs can be told it was
// deleted instead of an id just vanishing from an in-memory list.
func (d *DB) SoftDeleteMessage(id int) error {
	_, err := d.Exec(`UPDATE messages SET deleted_at = NOW(), body = '' WHERE id = $1`, id)
	return err
}

// EditMessage updates a text message's body and stamps edited_at — the
// caller (API layer) has already verified sender ownership, kind == "text",
// and that the message isn't deleted.
func (d *DB) EditMessage(id int, body string) (*models.MessageView, error) {
	if _, err := d.Exec(`UPDATE messages SET body = $1, edited_at = NOW() WHERE id = $2`, body, id); err != nil {
		return nil, err
	}
	return d.GetMessageView(id)
}

// HideMessageForUser is delete-for-me: userID stops seeing this message
// (ListMessages filters it out for them) without affecting anyone else's
// view or the underlying row.
func (d *DB) HideMessageForUser(messageID, userID int) error {
	_, err := d.Exec(
		`INSERT INTO message_hidden_for (message_id, user_id) VALUES ($1, $2) ON CONFLICT (message_id, user_id) DO NOTHING`,
		messageID, userID,
	)
	return err
}

// ForwardMessage copies a message's text/kind into each of
// targetConversationIDs as a brand-new message, labeled with the original
// sender's name captured right now (forwarded_from_name is denormalized —
// the target's viewers may never have access to the source conversation,
// and the source message can later be edited/deleted, but the label must
// survive both). Attachments are copied by reference: the new
// message_attachments rows point at the SAME minio_key as the original
// rather than re-uploading, so callers must delete attachments by
// reference-count (see CountAttachmentsByMinioKey) not unconditionally.
func (d *DB) ForwardMessage(sourceMessageID, forwarderID int, targetConversationIDs []int) ([]models.MessageView, error) {
	src, err := d.GetMessage(sourceMessageID)
	if err != nil {
		return nil, err
	}
	if src == nil || src.DeletedAt != nil {
		return nil, fmt.Errorf("message not found")
	}
	var senderName string
	if src.SenderID != nil {
		if err := d.QueryRow(`SELECT COALESCE(display_name, username, '') FROM users WHERE id = $1`, *src.SenderID).Scan(&senderName); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}
	srcAttachments, err := d.ListMessageAttachments(sourceMessageID)
	if err != nil {
		return nil, err
	}

	results := make([]models.MessageView, 0, len(targetConversationIDs))
	for _, convID := range targetConversationIDs {
		mv, err := d.forwardMessageInto(convID, forwarderID, src.Body, src.Kind, senderName, srcAttachments)
		if err != nil {
			return nil, err
		}
		if mv != nil {
			results = append(results, *mv)
		}
	}
	return results, nil
}

func (d *DB) forwardMessageInto(convID, forwarderID int, body, kind, forwardedFromName string, srcAttachments []models.MessageAttachment) (*models.MessageView, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var msgID int
	var createdAt time.Time
	if err := tx.QueryRow(
		`INSERT INTO messages (conversation_id, sender_id, body, kind, forwarded_from_name)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		convID, forwarderID, body, kind, forwardedFromName,
	).Scan(&msgID, &createdAt); err != nil {
		return nil, err
	}
	for _, a := range srcAttachments {
		if _, err := tx.Exec(
			`INSERT INTO message_attachments (message_id, conversation_id, uploaded_by, minio_key, file_name, size_bytes, content_type)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			msgID, convID, forwarderID, a.MinioKey, a.FileName, a.SizeBytes, a.ContentType,
		); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(`UPDATE conversations SET last_message_at = $1 WHERE id = $2`, createdAt, convID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetMessageView(msgID)
}

func (d *DB) CreateChatAttachment(conversationID, uploadedBy int, minioKey, fileName string, sizeBytes int64, contentType string) (*models.MessageAttachment, error) {
	a := &models.MessageAttachment{}
	err := d.QueryRow(
		`INSERT INTO message_attachments (conversation_id, uploaded_by, minio_key, file_name, size_bytes, content_type)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, message_id, conversation_id, uploaded_by, minio_key, file_name, size_bytes, content_type, created_at`,
		conversationID, uploadedBy, minioKey, fileName, sizeBytes, contentType,
	).Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.UploadedBy, &a.MinioKey, &a.FileName, &a.SizeBytes, &a.ContentType, &a.CreatedAt)
	return a, err
}

func (d *DB) GetChatAttachment(id int) (*models.MessageAttachment, error) {
	a := &models.MessageAttachment{}
	err := d.QueryRow(
		`SELECT id, message_id, conversation_id, uploaded_by, minio_key, file_name, size_bytes, content_type, created_at
		 FROM message_attachments WHERE id = $1`, id,
	).Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.UploadedBy, &a.MinioKey, &a.FileName, &a.SizeBytes, &a.ContentType, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListMessageAttachments includes minio_key (json:"-", never serialized) —
// callers that need to delete an attachment's underlying object, or copy it
// onto a forwarded message, need the key even though clients never see it.
func (d *DB) ListMessageAttachments(messageID int) ([]models.MessageAttachment, error) {
	rows, err := d.Query(
		`SELECT id, message_id, conversation_id, uploaded_by, minio_key, file_name, size_bytes, content_type, created_at
		 FROM message_attachments WHERE message_id = $1 ORDER BY id`, messageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.MessageAttachment
	for rows.Next() {
		var a models.MessageAttachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.UploadedBy, &a.MinioKey, &a.FileName, &a.SizeBytes, &a.ContentType, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// ListOrphanedChatAttachments returns attachments uploaded but never
// claimed by a sent message, older than cutoff — the janitor deletes both
// the MinIO object and this row for each one.
func (d *DB) ListOrphanedChatAttachments(cutoff time.Time) ([]models.MessageAttachment, error) {
	rows, err := d.Query(
		`SELECT id, message_id, conversation_id, uploaded_by, minio_key, file_name, size_bytes, content_type, created_at
		 FROM message_attachments WHERE message_id IS NULL AND created_at < $1`, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.MessageAttachment
	for rows.Next() {
		var a models.MessageAttachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.UploadedBy, &a.MinioKey, &a.FileName, &a.SizeBytes, &a.ContentType, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// DeleteChatAttachment removes just the row. Since forwarding a message
// copies its attachment rows onto the SAME minio_key rather than
// re-uploading, callers must delete this row FIRST and then check
// CountAttachmentsByMinioKey before touching the MinIO object — deleting the
// object first (the janitor's ordering, safe there because orphans are
// never shared) would risk breaking a sibling row that still points at it.
func (d *DB) DeleteChatAttachment(id int) error {
	_, err := d.Exec(`DELETE FROM message_attachments WHERE id = $1`, id)
	return err
}

// CountAttachmentsByMinioKey is how a caller decides whether it's safe to
// delete the underlying MinIO object after removing one message_attachments
// row — 0 means this was the last row referencing that key.
func (d *DB) CountAttachmentsByMinioKey(minioKey string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM message_attachments WHERE minio_key = $1`, minioKey).Scan(&n)
	return n, err
}

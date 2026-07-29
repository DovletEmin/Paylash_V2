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
// People excludeUserID has blocked are left out too, so starting a "new DM"
// never re-surfaces someone they deliberately blocked.
func (d *DB) SearchChatUsers(query string, excludeUserID, limit int) ([]models.UserSearchResult, error) {
	rows, err := d.Query(
		`SELECT id, username, display_name
		 FROM users
		 WHERE id != $1 AND (username ILIKE $2 OR display_name ILIKE $2)
		   AND id NOT IN (SELECT blocked_id FROM blocked_users WHERE blocker_id = $1)
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
		`SELECT id, type, name, project_id, created_by, direct_user_low, direct_user_high, avatar_url, pinned_message_id, last_message_at, created_at
		 FROM conversations WHERE id = $1`, id,
	).Scan(&c.ID, &c.Type, &c.Name, &c.ProjectID, &c.CreatedBy, &c.DirectUserLow, &c.DirectUserHigh, &c.AvatarURL, &c.PinnedMessageID, &c.LastMessageAt, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// ListConversationsForUser returns every conversation userID participates
// in — pinned ones first (most-recently-pinned first), then the rest by
// newest activity — each with an unread count (messages since that user's
// own last_read_at, excluding their own), a last-message preview, and — for
// direct conversations — the other participant's info, so the client never
// needs a second round-trip to render the inbox. archived selects which set:
// false (the normal inbox) returns only conversations the user hasn't
// archived; true returns ONLY the archived ones (the separate archived view).
func (d *DB) ListConversationsForUser(userID int, archived bool) ([]models.ConversationView, error) {
	rows, err := d.Query(`
		SELECT c.id, c.type, c.name, c.project_id, c.created_by, c.last_message_at, c.created_at,
		       COALESCE(unread.cnt, 0) AS unread_count,
		       lm.body, lm.created_at,
		       other.user_id, other.username, other.display_name, other.avatar_url,
		       cp.muted AND (cp.muted_until IS NULL OR cp.muted_until > NOW()), cp.muted_until,
		       cp.pinned_at IS NOT NULL, cp.archived_at IS NOT NULL
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
		WHERE (cp.archived_at IS NOT NULL) = $2
		ORDER BY (cp.pinned_at IS NULL), cp.pinned_at DESC, c.last_message_at DESC`, userID, archived)
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
		var mutedUntil sql.NullTime
		if err := rows.Scan(
			&cv.ID, &cv.Type, &cv.Name, &cv.ProjectID, &cv.CreatedBy, &cv.LastMessageAt, &cv.CreatedAt,
			&cv.UnreadCount, &lastBody, &lastAt,
			&otherID, &otherUsername, &otherDisplayName, &otherAvatar,
			&cv.Muted, &mutedUntil, &cv.Pinned, &cv.Archived,
		); err != nil {
			return nil, err
		}
		if mutedUntil.Valid {
			t := mutedUntil.Time
			cv.MutedUntil = &t
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

// SearchMessages finds text messages matching query within the conversations
// userID participates in (or within a single conversation when conversationID
// > 0). Deleted messages and ones the user hid for themselves are excluded, so
// a search can never surface something the user can't otherwise see. Newest
// match first.
func (d *DB) SearchMessages(userID int, query string, conversationID, limit int) ([]models.MessageSearchResult, error) {
	rows, err := d.Query(`
		SELECT m.id, m.conversation_id, c.type, COALESCE(c.name, ''),
		       m.sender_id, COALESCE(su.display_name, su.username, ''),
		       m.body, m.created_at, COALESCE(other.name, '')
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		JOIN conversation_participants cp ON cp.conversation_id = c.id AND cp.user_id = $1
		LEFT JOIN users su ON su.id = m.sender_id
		LEFT JOIN message_hidden_for hf ON hf.message_id = m.id AND hf.user_id = $1
		LEFT JOIN LATERAL (
			SELECT COALESCE(u.display_name, u.username, '') AS name
			FROM conversation_participants cp2 JOIN users u ON u.id = cp2.user_id
			WHERE cp2.conversation_id = c.id AND cp2.user_id <> $1 AND c.type = 'direct'
			LIMIT 1
		) other ON true
		WHERE m.deleted_at IS NULL AND hf.message_id IS NULL AND m.kind = 'text'
		  AND m.body ILIKE $2
		  AND ($3 = 0 OR m.conversation_id = $3)
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $4`,
		userID, "%"+query+"%", conversationID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []models.MessageSearchResult
	for rows.Next() {
		var r models.MessageSearchResult
		var otherName string
		if err := rows.Scan(
			&r.MessageID, &r.ConversationID, &r.ConversationType, &r.ConversationLabel,
			&r.SenderID, &r.SenderName, &r.Body, &r.CreatedAt, &otherName,
		); err != nil {
			return nil, err
		}
		if r.ConversationType == "direct" {
			r.ConversationLabel = otherName
		}
		results = append(results, r)
	}
	return results, rows.Err()
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
		// Prefer handing ownership to an existing admin (they're already
		// trusted with group management) over a plain member; earliest to
		// join breaks ties within either group.
		var newOwner sql.NullInt64
		err := tx.QueryRow(
			`SELECT user_id FROM conversation_participants WHERE conversation_id = $1
			 ORDER BY (role = 'admin') DESC, joined_at LIMIT 1`,
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
		`SELECT u.id, u.username, COALESCE(u.display_name, u.username, ''), u.avatar_url, cp.last_read_at, u.last_seen_at, cp.role,
		        cp.muted AND (cp.muted_until IS NULL OR cp.muted_until > NOW())
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
		var lastSeen sql.NullTime
		if err := rows.Scan(&p.UserID, &p.Username, &p.DisplayName, &p.AvatarURL, &p.LastReadAt, &lastSeen, &p.Role, &p.Muted); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			p.LastSeenAt = &t
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetParticipantRole returns "member" or "admin" for a participant, or ""
// (with no error) if userID isn't a participant of conversationID at all.
func (d *DB) GetParticipantRole(conversationID, userID int) (string, error) {
	var role string
	err := d.QueryRow(
		`SELECT role FROM conversation_participants WHERE conversation_id = $1 AND user_id = $2`,
		conversationID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

// SetParticipantRole promotes/demotes a participant — the caller (API layer)
// has already verified the requester is the conversation's owner.
func (d *DB) SetParticipantRole(conversationID, userID int, role string) error {
	_, err := d.Exec(
		`UPDATE conversation_participants SET role = $1 WHERE conversation_id = $2 AND user_id = $3`,
		role, conversationID, userID,
	)
	return err
}

// SetConversationMuted/SetConversationPinned/SetConversationArchived set one
// user's own preference for a conversation — never affects any other
// participant's view of it. Pinned/archived use a timestamp (non-null =
// active) so ListConversationsForUser can sort pinned conversations by how
// recently they were pinned.
//
// until is the mute's expiry (nil = muted forever, until unmuted) — always
// reset to nil when muted is false, so a stale expiry can never resurface
// once someone has explicitly unmuted a conversation.
func (d *DB) SetConversationMuted(conversationID, userID int, muted bool, until *time.Time) error {
	if !muted {
		until = nil
	}
	_, err := d.Exec(
		`UPDATE conversation_participants SET muted = $1, muted_until = $2 WHERE conversation_id = $3 AND user_id = $4`,
		muted, until, conversationID, userID,
	)
	return err
}

func (d *DB) SetConversationPinned(conversationID, userID int, pinned bool) error {
	var expr string
	if pinned {
		expr = "NOW()"
	} else {
		expr = "NULL"
	}
	_, err := d.Exec(
		`UPDATE conversation_participants SET pinned_at = `+expr+` WHERE conversation_id = $1 AND user_id = $2`,
		conversationID, userID,
	)
	return err
}

func (d *DB) SetConversationArchived(conversationID, userID int, archived bool) error {
	var expr string
	if archived {
		expr = "NOW()"
	} else {
		expr = "NULL"
	}
	_, err := d.Exec(
		`UPDATE conversation_participants SET archived_at = `+expr+` WHERE conversation_id = $1 AND user_id = $2`,
		conversationID, userID,
	)
	return err
}

// ListMutedConversationIDs backs the client-side mute cache (App._mutedConvIds)
// that gates in-app toast/sound/native-notification for a live "message.new"
// event — fetched once at login and kept in sync from there rather than
// re-fetched on every message.
func (d *DB) ListMutedConversationIDs(userID int) ([]int, error) {
	rows, err := d.Query(
		`SELECT conversation_id FROM conversation_participants
		 WHERE user_id = $1 AND muted = TRUE AND (muted_until IS NULL OR muted_until > NOW())`,
		userID,
	)
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

// SetConversationAvatar sets (or, with key="", clears) a group's photo.
func (d *DB) SetConversationAvatar(conversationID int, key string) error {
	_, err := d.Exec(`UPDATE conversations SET avatar_url = $1 WHERE id = $2`, key, conversationID)
	return err
}

// SetPinnedMessage pins messageID as the conversation's one pinned message
// (replacing any previous pin).
func (d *DB) SetPinnedMessage(conversationID, messageID int) error {
	_, err := d.Exec(`UPDATE conversations SET pinned_message_id = $1 WHERE id = $2`, messageID, conversationID)
	return err
}

// ClearPinnedMessage unconditionally unpins a conversation — the explicit
// "unpin" action, whatever is currently pinned.
func (d *DB) ClearPinnedMessage(conversationID int) error {
	_, err := d.Exec(`UPDATE conversations SET pinned_message_id = NULL WHERE id = $1`, conversationID)
	return err
}

// ClearPinnedMessageIfEquals unpins a conversation's pinned message, but only
// if it's currently pinned to exactly messageID — a defensive safety net run
// when a message is deleted, without racing a concurrent re-pin to a
// different message. Reports whether it actually cleared anything, so the
// caller only needs to tell participants when the pin genuinely changed.
func (d *DB) ClearPinnedMessageIfEquals(conversationID, messageID int) (bool, error) {
	res, err := d.Exec(
		`UPDATE conversations SET pinned_message_id = NULL WHERE id = $1 AND pinned_message_id = $2`,
		conversationID, messageID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UpdateLastSeen stamps a user's last-seen time — called when their last live
// WS connection drops, so a DM header can show "last seen X".
func (d *DB) UpdateLastSeen(userID int) error {
	_, err := d.Exec(`UPDATE users SET last_seen_at = NOW() WHERE id = $1`, userID)
	return err
}

// ListConversationContactIDs returns every user id that shares at least one
// conversation — direct OR group — with userID: the audience for that user's
// presence (online/offline) broadcasts. A direct partner sees it flip in
// their DM header; a fellow group member sees it in that group's member
// list/subtitle (ChatPage.handlePresence already patches both from a single
// "presence" event — this used to only cover direct partners, so a group-only
// contact's online dot never updated live).
func (d *DB) ListConversationContactIDs(userID int) ([]int, error) {
	rows, err := d.Query(
		`SELECT DISTINCT cp2.user_id
		 FROM conversation_participants cp1
		 JOIN conversation_participants cp2
		   ON cp2.conversation_id = cp1.conversation_id AND cp2.user_id != cp1.user_id
		 WHERE cp1.user_id = $1`,
		userID,
	)
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

// MarkDelivered records that userID's client has now received the given
// messages — either live (broadcast to an open WebSocket) or by fetching
// them (initial load or the after_id resync). Idempotent (ON CONFLICT DO
// NOTHING): a message delivered twice (e.g. live AND then re-fetched) keeps
// its FIRST delivered_at rather than getting bumped forward.
func (d *DB) MarkDelivered(messageIDs []int, userID int) error {
	if len(messageIDs) == 0 {
		return nil
	}
	_, err := d.Exec(
		`INSERT INTO message_deliveries (message_id, user_id)
		 SELECT unnest($1::int[]), $2
		 ON CONFLICT (message_id, user_id) DO NOTHING`,
		pq.Array(messageIDs), userID,
	)
	return err
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
// (exactly one other participant); other_delivered.min_delivered_at is the
// same idea for the "delivered" tick, backed by message_deliveries (see
// markDelivered). group conversations always fall back to a plain "sent"
// status (see scanMessageView), so what these resolve to for a group is
// never actually used.
const messageViewCols = `m.id, m.conversation_id, m.sender_id, m.body, m.kind, m.edited_at, m.reply_to_id, m.forwarded_from_name, m.deleted_at, m.created_at,
	COALESCE(u.display_name, u.username, ''), COALESCE(u.avatar_url, ''),
	COALESCE(c.type, ''), other_read.min_read_at, other_delivered.min_delivered_at,
	rm.id, COALESCE(ru.display_name, ru.username, ''), COALESCE(rm.body, ''), COALESCE(rm.kind, ''),
	m.link_preview_url, COALESCE(lp.title, ''), COALESCE(lp.description, ''), COALESCE(lp.image_key, '')`

const messageViewJoins = `FROM messages m
	LEFT JOIN users u ON u.id = m.sender_id
	LEFT JOIN conversations c ON c.id = m.conversation_id
	LEFT JOIN LATERAL (
		SELECT MIN(cp.last_read_at) AS min_read_at
		FROM conversation_participants cp
		WHERE cp.conversation_id = m.conversation_id AND cp.user_id != m.sender_id
	) other_read ON true
	LEFT JOIN LATERAL (
		SELECT MIN(md.delivered_at) AS min_delivered_at
		FROM message_deliveries md
		WHERE md.message_id = m.id AND md.user_id != m.sender_id
	) other_delivered ON true
	LEFT JOIN messages rm ON rm.id = m.reply_to_id
	LEFT JOIN users ru ON ru.id = rm.sender_id
	LEFT JOIN link_previews lp ON lp.url = m.link_preview_url`

// scanner is satisfied by both *sql.Row and *sql.Rows — lets GetMessageView
// and ListMessages share one Scan-and-assemble routine for messageViewCols.
type scanner interface {
	Scan(dest ...any) error
}

func scanMessageView(row scanner) (*models.MessageView, error) {
	mv := &models.MessageView{}
	var convType string
	var otherRead, otherDelivered sql.NullTime
	var replyID sql.NullInt64
	var replySender, replyBody, replyKind string
	var linkURL sql.NullString
	var lpTitle, lpDescription, lpImageKey string
	if err := row.Scan(
		&mv.ID, &mv.ConversationID, &mv.SenderID, &mv.Body, &mv.Kind, &mv.EditedAt, &mv.ReplyToID, &mv.ForwardedFromName, &mv.DeletedAt, &mv.CreatedAt,
		&mv.SenderName, &mv.SenderAvatar,
		&convType, &otherRead, &otherDelivered,
		&replyID, &replySender, &replyBody, &replyKind,
		&linkURL, &lpTitle, &lpDescription, &lpImageKey,
	); err != nil {
		return nil, err
	}
	if mv.SenderID != nil {
		switch {
		case convType == "direct" && otherRead.Valid && !mv.CreatedAt.After(otherRead.Time):
			mv.Status = "read"
		case convType == "direct" && otherDelivered.Valid:
			mv.Status = "delivered"
		default:
			mv.Status = "sent"
		}
	}
	if replyID.Valid {
		mv.ReplyTo = &models.MessageReplyPreview{ID: int(replyID.Int64), SenderName: replySender, Body: replyBody, Kind: replyKind}
	}
	// A row only exists once the async fetch actually resolved something
	// worth showing (see UpsertLinkPreview) — link_preview_url can be set
	// while the lp join is still pending (title empty), which the client
	// should treat the same as "no preview yet".
	if linkURL.Valid && lpTitle != "" {
		mv.LinkPreview = &models.LinkPreview{URL: linkURL.String, Title: lpTitle, Description: lpDescription, HasImage: lpImageKey != ""}
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
	reactions, err := d.ListReactionGroups(id)
	if err != nil {
		return nil, err
	}
	mv.Reactions = reactions
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

	var list []models.MessageView
	for rows.Next() {
		mv, err := scanMessageView(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return d.hydrateMessageViews(list, requesterID)
}

// ListMessagesAfter returns messages newer than afterID, oldest first — the
// forward-gap-fill counterpart to ListMessages' backward-scrolling
// pagination. Used to resync a conversation after a WebSocket reconnect
// (see ChatSocket.onReconnect in chatSocket.js) without re-fetching the
// whole visible page. Capped at limit: if MORE than that arrived while
// disconnected, the client detects a full page and falls back to reloading
// the conversation from scratch instead of looping this forever.
func (d *DB) ListMessagesAfter(conversationID, requesterID, afterID, limit int) ([]models.MessageView, error) {
	rows, err := d.Query(
		`SELECT `+messageViewCols+` `+messageViewJoins+`
		 LEFT JOIN message_hidden_for hf ON hf.message_id = m.id AND hf.user_id = $4
		 WHERE m.conversation_id = $1 AND m.id > $2 AND hf.message_id IS NULL
		 ORDER BY m.created_at ASC, m.id ASC LIMIT $3`,
		conversationID, afterID, limit, requesterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.MessageView
	for rows.Next() {
		mv, err := scanMessageView(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return d.hydrateMessageViews(list, requesterID)
}

// hydrateMessageViews batches in attachments, reactions, and (for the
// requester's own messages) group read receipts across a whole page of
// messages — one query per concern instead of N. Shared by ListMessages and
// ListMessagesAfter so the two pagination directions can't drift out of
// sync with each other.
func (d *DB) hydrateMessageViews(list []models.MessageView, requesterID int) ([]models.MessageView, error) {
	if len(list) == 0 {
		return list, nil
	}
	ids := make([]int, len(list))
	byID := make(map[int]*models.MessageView, len(list))
	for i := range list {
		ids[i] = list[i].ID
		byID[list[i].ID] = &list[i]
	}

	// One extra query for every attachment belonging to any message in this
	// page, rather than N — cheap at this app's scale and avoids a
	// per-message round trip.
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

	// Same batched pattern for reactions: one query for the whole page.
	reactByMsg, err := d.listReactionGroupsForMessages(ids)
	if err != nil {
		return nil, err
	}
	for msgID, groups := range reactByMsg {
		if mv, ok := byID[msgID]; ok {
			mv.Reactions = groups
		}
	}

	// Group read receipts: which other participants have read each of the
	// REQUESTER'S OWN messages. Scoped to sender_id = requester so a member
	// never sees who read someone else's message.
	readRows, err := d.Query(
		`SELECT m.id, cp.user_id
		 FROM messages m
		 JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id
		   AND cp.user_id <> m.sender_id AND cp.last_read_at >= m.created_at
		 WHERE m.id = ANY($1) AND m.sender_id = $2`,
		pq.Array(ids), requesterID,
	)
	if err != nil {
		return nil, err
	}
	defer readRows.Close()
	for readRows.Next() {
		var msgID, uid int
		if err := readRows.Scan(&msgID, &uid); err != nil {
			return nil, err
		}
		if mv, ok := byID[msgID]; ok {
			mv.ReadUserIDs = append(mv.ReadUserIDs, uid)
		}
	}
	if err := readRows.Err(); err != nil {
		return nil, err
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

// ToggleReaction adds userID's emoji reaction to a message, or removes it if
// it was already there — the delete-or-insert makes a repeated tap toggle.
// Returns true if the reaction was added, false if it was removed. The caller
// (API layer) has already validated emoji against the allowlist.
func (d *DB) ToggleReaction(messageID, userID int, emoji string) (bool, error) {
	res, err := d.Exec(`DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`, messageID, userID, emoji)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, err
	} else if n > 0 {
		return false, nil
	}
	if _, err := d.Exec(
		`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		messageID, userID, emoji,
	); err != nil {
		return false, err
	}
	return true, nil
}

// groupReactionRows turns (emoji, user_id) rows already ordered by created_at
// into per-emoji groups, preserving the order each emoji first appeared.
func groupReactionRows(rows *sql.Rows) ([]models.MessageReactionGroup, error) {
	var groups []models.MessageReactionGroup
	idx := map[string]int{}
	for rows.Next() {
		var emoji string
		var userID int
		if err := rows.Scan(&emoji, &userID); err != nil {
			return nil, err
		}
		if i, ok := idx[emoji]; ok {
			groups[i].UserIDs = append(groups[i].UserIDs, userID)
		} else {
			idx[emoji] = len(groups)
			groups = append(groups, models.MessageReactionGroup{Emoji: emoji, UserIDs: []int{userID}})
		}
	}
	return groups, rows.Err()
}

// ListReactionGroups returns one message's reactions grouped by emoji, in the
// order each emoji was first used — used to build the message.reaction WS
// payload and to hydrate a single freshly-created/edited message.
func (d *DB) ListReactionGroups(messageID int) ([]models.MessageReactionGroup, error) {
	rows, err := d.Query(`SELECT emoji, user_id FROM message_reactions WHERE message_id = $1 ORDER BY created_at, user_id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return groupReactionRows(rows)
}

// ListConversationMedia backs the shared-media gallery ("Media" / "Files"
// tabs in group/DM info): attachments belonging to conversationID, newest
// first, keyset-paginated by attachment id exactly like ListMessages'
// history scrolling. mediaOnly=true returns images/videos; false returns
// everything else (documents, voice notes, plain audio). Only attachments on
// a claimed (message_id set), non-deleted, not-hidden-for-requesterID
// message are visible — the same rule ListMessages already enforces for
// messages themselves.
func (d *DB) ListConversationMedia(conversationID, requesterID int, mediaOnly bool, beforeID, limit int) ([]models.MessageAttachment, error) {
	typeFilter := `(a.content_type LIKE 'image/%' OR a.content_type LIKE 'video/%')`
	if !mediaOnly {
		typeFilter = `NOT ` + typeFilter
	}
	base := `SELECT a.id, a.message_id, a.conversation_id, a.uploaded_by, a.file_name, a.size_bytes, a.content_type, a.created_at
		FROM message_attachments a
		JOIN messages m ON m.id = a.message_id
		LEFT JOIN message_hidden_for hf ON hf.message_id = m.id AND hf.user_id = $2
		WHERE a.conversation_id = $1 AND m.deleted_at IS NULL AND hf.message_id IS NULL AND ` + typeFilter

	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = d.Query(base+` AND a.id < $3 ORDER BY a.id DESC LIMIT $4`, conversationID, requesterID, beforeID, limit)
	} else {
		rows, err = d.Query(base+` ORDER BY a.id DESC LIMIT $3`, conversationID, requesterID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.MessageAttachment
	for rows.Next() {
		var a models.MessageAttachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.ConversationID, &a.UploadedBy, &a.FileName, &a.SizeBytes, &a.ContentType, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// GetLinkPreviewCache returns url's cached preview if it was fetched within
// maxAge, or nil (with no error) on a cache miss or stale entry — the
// caller (fetchLinkPreview) re-fetches from scratch in either case.
func (d *DB) GetLinkPreviewCache(url string, maxAge time.Duration) (*models.LinkPreview, error) {
	var title, description, imageKey string
	var fetchedAt time.Time
	err := d.QueryRow(
		`SELECT title, description, image_key, fetched_at FROM link_previews WHERE url = $1`, url,
	).Scan(&title, &description, &imageKey, &fetchedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Since(fetchedAt) > maxAge {
		return nil, nil
	}
	return &models.LinkPreview{URL: url, Title: title, Description: description, HasImage: imageKey != ""}, nil
}

// UpsertLinkPreview caches (or refreshes) one URL's unfurled metadata —
// shared across every message that URL ever appears in.
func (d *DB) UpsertLinkPreview(url, title, description, imageKey string) error {
	_, err := d.Exec(
		`INSERT INTO link_previews (url, title, description, image_key, fetched_at) VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (url) DO UPDATE SET title = $2, description = $3, image_key = $4, fetched_at = NOW()`,
		url, title, description, imageKey,
	)
	return err
}

// SetMessageLinkPreview attaches an already-cached url to messageID — called
// once an async fetch (or a cache hit) resolves.
func (d *DB) SetMessageLinkPreview(messageID int, url string) error {
	_, err := d.Exec(`UPDATE messages SET link_preview_url = $1 WHERE id = $2`, url, messageID)
	return err
}

// ClearMessageLinkPreview detaches whatever link preview messageID has —
// used when an edit removes the URL that produced it.
func (d *DB) ClearMessageLinkPreview(messageID int) error {
	_, err := d.Exec(`UPDATE messages SET link_preview_url = NULL WHERE id = $1`, messageID)
	return err
}

// GetLinkPreviewImageKey resolves the MinIO object key for a message's
// cached link-preview image ("" if the message has no preview, or its
// preview has no image) — backs the image-serving endpoint.
func (d *DB) GetLinkPreviewImageKey(messageID int) (string, error) {
	var key string
	err := d.QueryRow(
		`SELECT COALESCE(lp.image_key, '') FROM messages m LEFT JOIN link_previews lp ON lp.url = m.link_preview_url WHERE m.id = $1`,
		messageID,
	).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return key, err
}

// listReactionGroupsForMessages batches ListReactionGroups across a page of
// messages — one query for the whole page instead of N.
func (d *DB) listReactionGroupsForMessages(ids []int) (map[int][]models.MessageReactionGroup, error) {
	rows, err := d.Query(
		`SELECT message_id, emoji, user_id FROM message_reactions WHERE message_id = ANY($1) ORDER BY message_id, created_at, user_id`,
		pq.Array(ids),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byMsg := map[int][]models.MessageReactionGroup{}
	idx := map[int]map[string]int{}
	for rows.Next() {
		var msgID, userID int
		var emoji string
		if err := rows.Scan(&msgID, &emoji, &userID); err != nil {
			return nil, err
		}
		if idx[msgID] == nil {
			idx[msgID] = map[string]int{}
		}
		if i, ok := idx[msgID][emoji]; ok {
			byMsg[msgID][i].UserIDs = append(byMsg[msgID][i].UserIDs, userID)
		} else {
			idx[msgID][emoji] = len(byMsg[msgID])
			byMsg[msgID] = append(byMsg[msgID], models.MessageReactionGroup{Emoji: emoji, UserIDs: []int{userID}})
		}
	}
	return byMsg, rows.Err()
}

package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

type DB struct {
	*sql.DB
}

func Connect(dsn string) (*DB, error) {
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	return &DB{conn}, nil
}

// Migrate applies every statement below, in order, on every startup. Each
// one must be safe to re-run (IF NOT EXISTS / IF EXISTS / ON CONFLICT) rather
// than tracked by a numbered/versioned migration tool — deliberately, for an
// app this size: no schema-version table to get out of sync, no separate
// migration-file build step, and no new dependency to fetch. The trade-off
// (no rollback story, statements accumulate forever) is acceptable at this
// scale; revisit with a real tool (golang-migrate/goose) if the schema
// starts changing often enough for that to bite.
func (d *DB) Migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            SERIAL PRIMARY KEY,
			username      VARCHAR(100) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			display_name  VARCHAR(255) DEFAULT '',
			role          VARCHAR(20) DEFAULT 'user',
			quota_bytes   BIGINT DEFAULT 1073741824,
			created_at    TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id           SERIAL PRIMARY KEY,
			name         VARCHAR(255) NOT NULL UNIQUE,
			quota_bytes  BIGINT DEFAULT 5368709120,
			minio_bucket VARCHAR(255),
			created_at   TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS project_members (
			id         SERIAL PRIMARY KEY,
			project_id INT REFERENCES projects(id) ON DELETE CASCADE,
			user_id    INT REFERENCES users(id) ON DELETE CASCADE,
			permission VARCHAR(20) NOT NULL DEFAULT 'view',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(project_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS folders (
			id         SERIAL PRIMARY KEY,
			name       VARCHAR(255) NOT NULL,
			parent_id  INT REFERENCES folders(id) ON DELETE CASCADE,
			owner_id   INT REFERENCES users(id) ON DELETE CASCADE,
			project_id INT REFERENCES projects(id) ON DELETE CASCADE,
			scope      VARCHAR(20) NOT NULL DEFAULT 'personal',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			id           SERIAL PRIMARY KEY,
			name         VARCHAR(500) NOT NULL,
			mime_type    VARCHAR(255) DEFAULT '',
			size_bytes   BIGINT NOT NULL DEFAULT 0,
			minio_bucket VARCHAR(255) NOT NULL,
			minio_key    VARCHAR(1000) NOT NULL,
			folder_id    INT REFERENCES folders(id) ON DELETE SET NULL,
			owner_id     INT REFERENCES users(id) ON DELETE CASCADE,
			project_id   INT REFERENCES projects(id) ON DELETE SET NULL,
			scope        VARCHAR(20) NOT NULL DEFAULT 'personal',
			version      INT DEFAULT 1,
			created_at   TIMESTAMPTZ DEFAULT NOW(),
			updated_at   TIMESTAMPTZ DEFAULT NOW()
		)`,
		// visibility must exist before any migration below references it —
		// on a brand-new database the files table above is just created
		// without that column (it was bolted on later in this app's history).
		`ALTER TABLE files ADD COLUMN IF NOT EXISTS visibility VARCHAR(20) NOT NULL DEFAULT 'private'`,
		`CREATE TABLE IF NOT EXISTS file_shares (
			id          SERIAL PRIMARY KEY,
			file_id     INT REFERENCES files(id) ON DELETE CASCADE,
			shared_by   INT REFERENCES users(id) ON DELETE CASCADE,
			shared_with INT REFERENCES users(id) ON DELETE CASCADE,
			permission  VARCHAR(20) DEFAULT 'view',
			is_public   BOOLEAN DEFAULT FALSE,
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(file_id, shared_with)
		)`,
		`CREATE TABLE IF NOT EXISTS wopi_tokens (
			id         SERIAL PRIMARY KEY,
			token      VARCHAR(255) NOT NULL UNIQUE,
			file_id    INT REFERENCES files(id) ON DELETE CASCADE,
			user_id    INT REFERENCES users(id) ON DELETE CASCADE,
			permission VARCHAR(20) DEFAULT 'view',
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id         VARCHAR(255) PRIMARY KEY,
			user_id    INT REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_owner ON files(owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_project ON files(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_folder ON files(folder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_folders_owner ON folders(owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_folders_project ON folders(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_folders_parent ON folders(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_shares_file ON file_shares(file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_shares_with ON file_shares(shared_with)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_wopi_tokens_token ON wopi_tokens(token)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_project ON project_members(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_user ON project_members(user_id)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_url VARCHAR(500) DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE files ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL`,
		`ALTER TABLE folders ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL`,
		`CREATE INDEX IF NOT EXISTS idx_files_deleted_at ON files(deleted_at)`,
		`CREATE INDEX IF NOT EXISTS idx_folders_deleted_at ON folders(deleted_at)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id          SERIAL PRIMARY KEY,
			actor_id    INT REFERENCES users(id) ON DELETE SET NULL,
			actor_name  VARCHAR(255) NOT NULL DEFAULT '',
			action      VARCHAR(50) NOT NULL,
			target_type VARCHAR(50) NOT NULL DEFAULT '',
			target_id   INT,
			target_name VARCHAR(500) NOT NULL DEFAULT '',
			details     JSONB,
			created_at  TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log(actor_id)`,
		`CREATE TABLE IF NOT EXISTS upload_sessions (
			id              VARCHAR(64) PRIMARY KEY,
			minio_upload_id TEXT NOT NULL,
			bucket          VARCHAR(255) NOT NULL,
			object_key      VARCHAR(1000) NOT NULL,
			owner_id        INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			scope           VARCHAR(20) NOT NULL,
			project_id      INT REFERENCES projects(id) ON DELETE SET NULL,
			folder_id       INT REFERENCES folders(id) ON DELETE SET NULL,
			file_name       VARCHAR(500) NOT NULL,
			mime_type       VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
			total_size      BIGINT NOT NULL,
			part_size       BIGINT NOT NULL,
			part_count      INT NOT NULL,
			status          VARCHAR(20) NOT NULL DEFAULT 'in_progress',
			created_at      TIMESTAMPTZ DEFAULT NOW(),
			updated_at      TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_upload_sessions_owner ON upload_sessions(owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_upload_sessions_status ON upload_sessions(status, updated_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key   VARCHAR(100) PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO settings (key, value) VALUES ('public_quota_bytes', '53687091200') ON CONFLICT DO NOTHING`,
		// Single per-user checkpoint rather than a per-share read flag: the
		// Shared page badge only ever needs "how many arrived since I last
		// looked", not which individual shares were seen. Defaults to NOW()
		// so migrating an existing database doesn't suddenly flood everyone
		// with a badge for every share that already existed.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS notifications_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS file_comments (
			id         SERIAL PRIMARY KEY,
			file_id    INT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			body       TEXT NOT NULL,
			x_pct      DOUBLE PRECISION,
			y_pct      DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_comments_file ON file_comments(file_id)`,
		// Trigram indexes back SearchFiles'/SearchUsers' `name/username/display_name
		// ILIKE '%q%'` queries — a leading wildcard can't use a plain B-tree index,
		// but pg_trgm's GIN index matches substrings directly and the planner picks
		// it up automatically with no query-side changes needed.
		`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
		`CREATE INDEX IF NOT EXISTS idx_files_name_trgm ON files USING GIN (name gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_users_username_trgm ON users USING GIN (username gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_users_display_name_trgm ON users USING GIN (display_name gin_trgm_ops)`,

		// Chat: direct + group conversations. direct_user_low/high (always
		// min/max of the two participant ids) let a direct conversation be
		// found-or-created race-free with one INSERT ... ON CONFLICT rather
		// than a query-then-create that two people opening a DM at the same
		// instant could duplicate.
		`CREATE TABLE IF NOT EXISTS conversations (
			id               SERIAL PRIMARY KEY,
			type             VARCHAR(20) NOT NULL,
			name             VARCHAR(255),
			project_id       INT REFERENCES projects(id) ON DELETE SET NULL,
			created_by       INT REFERENCES users(id) ON DELETE SET NULL,
			direct_user_low  INT REFERENCES users(id) ON DELETE SET NULL,
			direct_user_high INT REFERENCES users(id) ON DELETE SET NULL,
			last_message_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_conversations_direct_pair
			ON conversations(direct_user_low, direct_user_high) WHERE type = 'direct'`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_last_message ON conversations(last_message_at DESC)`,
		`CREATE TABLE IF NOT EXISTS conversation_participants (
			id              SERIAL PRIMARY KEY,
			conversation_id INT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			user_id         INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			last_read_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(conversation_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_participants_conversation ON conversation_participants(conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_conv_participants_user ON conversation_participants(user_id)`,
		// sender_id is SET NULL (not CASCADE like file_comments.user_id) —
		// deleting an employee must not silently erase their half of every
		// group conversation's history. Delete is soft (deleted_at + blank
		// body, row stays) so an already-open tab on another device can be
		// told "this message was deleted" via the same live WS event instead
		// of having to splice an id out of an in-memory list.
		`CREATE TABLE IF NOT EXISTS messages (
			id              SERIAL PRIMARY KEY,
			conversation_id INT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			sender_id       INT REFERENCES users(id) ON DELETE SET NULL,
			body            TEXT NOT NULL DEFAULT '',
			deleted_at      TIMESTAMPTZ,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conversation_created
			ON messages(conversation_id, created_at DESC, id DESC)`,
		// message_id stays NULL until a composer actually sends the message
		// that references it — an attachment uploaded but never sent is an
		// "unclaimed" row the janitor purges after a day (see
		// idx_msg_attachments_unclaimed / ListOrphanedChatAttachments).
		`CREATE TABLE IF NOT EXISTS message_attachments (
			id              SERIAL PRIMARY KEY,
			message_id      INT REFERENCES messages(id) ON DELETE CASCADE,
			conversation_id INT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			uploaded_by     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			minio_key       VARCHAR(1000) NOT NULL,
			file_name       VARCHAR(500) NOT NULL,
			size_bytes      BIGINT NOT NULL,
			content_type    VARCHAR(255) NOT NULL DEFAULT 'application/octet-stream',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_attachments_message ON message_attachments(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_attachments_unclaimed ON message_attachments(created_at) WHERE message_id IS NULL`,

		// Chat v2: sticker messages, editing, reply-to, and forwarding.
		// forwarded_from_name is captured at forward time rather than a FK to
		// the original message — the recipient may never have had access to
		// the source conversation, and the source message can later be
		// deleted, but the "forwarded from X" label must survive both.
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS kind VARCHAR(20) NOT NULL DEFAULT 'text'`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_to_id INT REFERENCES messages(id) ON DELETE SET NULL`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS forwarded_from_name VARCHAR(255)`,
		// Delete-for-me: a per-viewer hide flag, distinct from messages.deleted_at
		// (which is delete-for-everyone, sender-only). A row here means this one
		// user no longer sees the message; everyone else is unaffected.
		`CREATE TABLE IF NOT EXISTS message_hidden_for (
			message_id INT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			hidden_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (message_id, user_id)
		)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS chat_notify_level VARCHAR(20) NOT NULL DEFAULT 'full'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS chat_notify_sound BOOLEAN NOT NULL DEFAULT TRUE`,

		// Emoji reactions on messages. PK (message_id, user_id, emoji) lets one
		// user place several distinct reactions on the same message while making
		// re-adding the same one idempotent (toggle = delete-or-insert). The
		// emoji is always from a server-side allowlist (see reactionSet), never
		// free text, so it's safe to render.
		`CREATE TABLE IF NOT EXISTS message_reactions (
			message_id INT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			emoji      VARCHAR(16) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (message_id, user_id, emoji)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_message_reactions_message ON message_reactions(message_id)`,

		// Presence: last_seen_at is stamped when a user's last live WebSocket
		// connection drops (see chatHub), so "online / last seen X" can be shown
		// in a DM header. NULL until they've connected at least once. Online
		// status itself is in-memory hub state, never persisted.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ`,

		// Trigram index backing chat message search (body ILIKE '%q%') — same
		// mechanism already used for file/user search; pg_trgm is enabled above.
		`CREATE INDEX IF NOT EXISTS idx_messages_body_trgm ON messages USING GIN (body gin_trgm_ops)`,

		// Web Push subscriptions (RFC 8291). One row per browser subscription;
		// endpoint is unique so a re-subscribe upserts. p256dh/auth are the
		// subscription's public key and auth secret (base64url). Delivered to
		// only when the recipient has no live WS connection (app closed).
		`CREATE TABLE IF NOT EXISTS push_subscriptions (
			id         SERIAL PRIMARY KEY,
			user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			endpoint   TEXT NOT NULL UNIQUE,
			p256dh     TEXT NOT NULL,
			auth       TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user ON push_subscriptions(user_id)`,

		// Group roles: 'member' (default) or 'admin'. The conversation's
		// creator is always its owner (conversations.created_by, unchanged)
		// and is never represented here — an admin can do everything an
		// owner can EXCEPT promote/demote other admins (see requireOwner /
		// requireManager in internal/api/chat.go).
		`ALTER TABLE conversation_participants ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'member'`,

		// Per-user, per-conversation preferences — none of these affect what
		// any OTHER participant sees. muted: skip sound/toast/native/push
		// notifications (the conversation still appears and still counts
		// toward unread). pinned_at: non-null sorts this conversation to the
		// top of the inbox. archived_at: non-null hides it from the main
		// inbox (visible in a separate archived view instead).
		`ALTER TABLE conversation_participants ADD COLUMN IF NOT EXISTS muted BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE conversation_participants ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMPTZ`,
		`ALTER TABLE conversation_participants ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ`,

		// Group photo: avatar_url stores just the object key inside
		// storage.ChatAttachmentsBucket (mirrors message_attachments.minio_key
		// — never a full bucket/key pair, since it's always that one bucket).
		// pinned_message_id: a single pinned message per conversation:
		// ON DELETE SET NULL so a hard-deleted message (never happens today —
		// deletes are soft — but safe regardless) just quietly unpins rather
		// than leaving a dangling reference.
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS avatar_url VARCHAR(500) NOT NULL DEFAULT ''`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS pinned_message_id INT REFERENCES messages(id) ON DELETE SET NULL`,

		// User-to-user blocking: a row means blocker_id has blocked
		// blocked_id. Checked symmetrically when sending a direct message
		// (see SendMessage) — while either side has blocked the other,
		// neither can message the other in their DM. Existing history stays
		// visible; this only stops new sends.
		`CREATE TABLE IF NOT EXISTS blocked_users (
			blocker_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			blocked_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (blocker_id, blocked_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_blocked_users_blocked ON blocked_users(blocked_id)`,

		// Link preview cache: unfurled Open Graph metadata for a URL, keyed by
		// the URL itself so the same link shared in many messages (or many
		// conversations) is only ever fetched once. image_key stores the
		// downloaded preview image inside storage.ChatAttachmentsBucket rather
		// than the remote image URL — same rule as every other image in this
		// app (avatars, attachments): the client is never pointed at a raw
		// third-party URL, both to avoid mixed-content/CSP issues and so
		// rendering a preview doesn't leak every viewer's IP to the linked
		// site on each render. See internal/api/linkpreview.go.
		`CREATE TABLE IF NOT EXISTS link_previews (
			url         TEXT PRIMARY KEY,
			title       VARCHAR(300) NOT NULL DEFAULT '',
			description VARCHAR(500) NOT NULL DEFAULT '',
			image_key   VARCHAR(500) NOT NULL DEFAULT '',
			fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// NULL until the async unfurl (see maybeUnfurlLink) resolves, or
		// forever if the message has no URL or nothing came back worth
		// showing. ON DELETE SET NULL: a cache row is never load-bearing for
		// the message row that references it.
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS link_preview_url TEXT REFERENCES link_previews(url) ON DELETE SET NULL`,

		// Admin "log in as" impersonation: a session with impersonator_id set
		// belongs to the ADMIN in that column, acting as user_id (the
		// target). ON DELETE SET NULL rather than CASCADE — deleting the
		// admin account must not silently delete an in-progress
		// impersonation session out from under the target user.
		`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS impersonator_id INT REFERENCES users(id) ON DELETE SET NULL`,

		// Gates the first-login welcome tour. Defaults TRUE so this backfills
		// every EXISTING row as already onboarded — the tour must only ever
		// appear for accounts created after this migration; CreateUser
		// explicitly inserts FALSE for those. SeedAdmin's raw INSERT also
		// omits it deliberately, so the bootstrap admin never sees it either.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS onboarding_completed BOOLEAN NOT NULL DEFAULT TRUE`,

		// Timed mute: NULL means "muted has no expiry" (forever, once muted =
		// TRUE — exactly what every pre-existing muted row already means, so
		// this needs no backfill) — a non-null value in the past means
		// "effectively unmuted", checked lazily wherever muted is read rather
		// than by a background job clearing it. See SetConversationMuted.
		`ALTER TABLE conversation_participants ADD COLUMN IF NOT EXISTS muted_until TIMESTAMPTZ`,

		// Chat moderation: a report against a message OR a user (exactly one
		// of the two ids set), raised by any participant who can see it.
		// status starts 'open'; an admin resolves or dismisses it (see
		// AdminListChatReports/AdminResolveChatReport) — this is the ONLY
		// path that ever lets a system admin see reported message content;
		// there is no standing admin access to chat otherwise (see
		// requireParticipant's comment in internal/api/chat.go).
		`CREATE TABLE IF NOT EXISTS chat_reports (
			id               SERIAL PRIMARY KEY,
			reporter_id      INT REFERENCES users(id) ON DELETE SET NULL,
			conversation_id  INT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			message_id       INT REFERENCES messages(id) ON DELETE SET NULL,
			reported_user_id INT REFERENCES users(id) ON DELETE SET NULL,
			reason           VARCHAR(500) NOT NULL DEFAULT '',
			-- Snapshotted at report time so a later edit/delete of the
			-- message can't erase what was actually reported.
			message_body_snapshot TEXT NOT NULL DEFAULT '',
			status           VARCHAR(20) NOT NULL DEFAULT 'open',
			resolved_by      INT REFERENCES users(id) ON DELETE SET NULL,
			resolved_at      TIMESTAMPTZ,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_reports_status ON chat_reports(status, created_at DESC)`,

		// Per-recipient delivery tracking for the sent → delivered → read
		// message-status tick (direct conversations only — see
		// scanMessageView). A row means the message has actually reached
		// user_id's client, whether live (broadcast to an open WebSocket) or
		// on next fetch (ListMessages/ListMessagesAfter) if they were
		// offline when it was sent — see markDelivered's call sites.
		`CREATE TABLE IF NOT EXISTS message_deliveries (
			message_id   INT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			user_id      INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			delivered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (message_id, user_id)
		)`,

		// Attendance (check-in/check-out): one row per employee per calendar
		// work_date. expected_start_min/expected_end_min/grace_minutes are a
		// SNAPSHOT of the company-wide schedule (see settings key
		// 'attendance_schedule') taken at check-in time — stored as plain
		// minutes-since-midnight rather than a SQL TIME column to sidestep
		// lib/pq's TIME scanning quirks entirely. This means a later change to
		// the schedule never silently rewrites whether a past day counts as
		// "late" — late/early/worked-minutes are always computed from what was
		// actually in effect that day. needs_review is set by the nightly
		// janitor sweep for a record whose check_out_at is still NULL after its
		// work_date has passed (see internal/janitor) — deliberately never
		// auto-filled with a guessed checkout time; an admin must resolve it.
		`CREATE TABLE IF NOT EXISTS attendance_records (
			id                 SERIAL PRIMARY KEY,
			user_id            INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			work_date          DATE NOT NULL,
			check_in_at        TIMESTAMPTZ NOT NULL,
			check_out_at       TIMESTAMPTZ,
			expected_start_min INT NOT NULL,
			expected_end_min   INT NOT NULL,
			grace_minutes      INT NOT NULL DEFAULT 0,
			-- Also snapshotted: whether work_date's weekday was a configured
			-- workday at check-in time, so a later change to the schedule's
			-- workday set can't retroactively brand a Saturday check-in "late".
			is_workday         BOOLEAN NOT NULL DEFAULT TRUE,
			needs_review       BOOLEAN NOT NULL DEFAULT FALSE,
			notes              VARCHAR(500) NOT NULL DEFAULT '',
			created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(user_id, work_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_attendance_records_user ON attendance_records(user_id, work_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_attendance_records_date ON attendance_records(work_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_attendance_records_needs_review ON attendance_records(needs_review) WHERE needs_review`,
	}

	for _, m := range migrations {
		if _, err := d.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}
	log.Println("database migrations completed")
	return nil
}

func (d *DB) GetSetting(key string) (string, error) {
	var val string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = $1`, key).Scan(&val)
	return val, err
}

func (d *DB) SetSetting(key, value string) error {
	_, err := d.Exec(`INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = $2`, key, value)
	return err
}

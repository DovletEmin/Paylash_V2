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

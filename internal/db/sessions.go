package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"paylash/internal/models"
	"time"
)

func (d *DB) CreateSession(userID int, ttl time.Duration) (*models.Session, error) {
	token := generateToken(32)
	s := &models.Session{
		ID:        token,
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
	}
	_, err := d.Exec(
		`INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		s.ID, s.UserID, s.ExpiresAt,
	)
	return s, err
}

// CreateImpersonationSession is CreateSession's counterpart for an admin's
// "log in as" action — the resulting session belongs to targetUserID (every
// normal permission check treats it as exactly their session) but carries
// adminID in impersonator_id, so the audit log and the "you're viewing as X"
// banner can still tell who's really behind it. Deliberately short-lived
// (2 hours, not the normal 7 days): an impersonation session left open
// indefinitely is a much bigger blast radius than a forgotten login.
func (d *DB) CreateImpersonationSession(targetUserID, adminID int) (*models.Session, error) {
	token := generateToken(32)
	s := &models.Session{
		ID:             token,
		UserID:         targetUserID,
		ExpiresAt:      time.Now().Add(2 * time.Hour),
		ImpersonatorID: &adminID,
	}
	_, err := d.Exec(
		`INSERT INTO sessions (id, user_id, expires_at, impersonator_id) VALUES ($1, $2, $3, $4)`,
		s.ID, s.UserID, s.ExpiresAt, adminID,
	)
	return s, err
}

func (d *DB) GetSession(token string) (*models.Session, error) {
	s := &models.Session{}
	var impersonatorID sql.NullInt64
	err := d.QueryRow(
		`SELECT id, user_id, expires_at, created_at, impersonator_id FROM sessions WHERE id = $1 AND expires_at > NOW()`, token,
	).Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt, &impersonatorID)
	if err != nil {
		return nil, err
	}
	if impersonatorID.Valid {
		v := int(impersonatorID.Int64)
		s.ImpersonatorID = &v
	}
	return s, nil
}

// GetSessionUser resolves a session token AND its owner in one round trip.
//
// AuthMiddleware runs on every authenticated request, and it needs both — so
// doing this as GetSession followed by GetUserByID meant two sequential
// queries per API call, on the single hottest path in the app. The app also
// polls in the background (notification and chat-unread counts), so an idle
// tab was paying that twice a tick for nothing.
//
// Both lookups are primary-key hits, so the join costs the same as either
// half did alone. A nil session with a nil error means "no usable session":
// either the token is unknown, or it has expired. The user can never be
// missing for a session that exists — sessions.user_id cascades on delete —
// but callers that want to tell the two apart can still fall back to
// GetSession, which is what the middleware does on the failure path.
func (d *DB) GetSessionUser(token string) (*models.Session, *models.User, error) {
	s := &models.Session{}
	u := &models.User{}
	var impersonatorID sql.NullInt64
	err := d.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, s.created_at, s.impersonator_id,
		        u.id, u.username, u.password_hash, u.display_name, u.role, u.quota_bytes,
		        u.avatar_url, u.must_change_password, u.chat_notify_level, u.chat_notify_sound,
		        u.onboarding_completed, u.attendance_tracked, u.created_at
		 FROM sessions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.id = $1 AND s.expires_at > NOW()`, token,
	).Scan(
		&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt, &impersonatorID,
		&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.QuotaBytes,
		&u.AvatarURL, &u.MustChangePassword, &u.ChatNotifyLevel, &u.ChatNotifySound,
		&u.OnboardingCompleted, &u.AttendanceTracked, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if impersonatorID.Valid {
		v := int(impersonatorID.Int64)
		s.ImpersonatorID = &v
	}
	return s, u, nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE id = $1`, token)
	return err
}

// DeleteOtherSessions removes every session belonging to userID except
// keepToken (the session making the request) — used after a password change
// so a stolen/shared session doesn't quietly survive the very fix meant to
// kick it out.
func (d *DB) DeleteOtherSessions(userID int, keepToken string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE user_id = $1 AND id != $2`, userID, keepToken)
	return err
}

// DeleteAllSessionsForUser removes every session belonging to userID,
// including the caller's own if they happen to share one — used when an
// admin resets someone else's password, where there's no "current session"
// to exempt.
func (d *DB) DeleteAllSessionsForUser(userID int) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (d *DB) CleanExpiredSessions() error {
	_, err := d.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
	return err
}

// WOPI tokens

func (d *DB) CreateWOPIToken(fileID, userID int, permission string) (*models.WOPIToken, error) {
	token := generateToken(32)
	t := &models.WOPIToken{
		Token:      token,
		FileID:     fileID,
		UserID:     userID,
		Permission: permission,
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	_, err := d.Exec(
		`INSERT INTO wopi_tokens (token, file_id, user_id, permission, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		t.Token, t.FileID, t.UserID, t.Permission, t.ExpiresAt,
	)
	return t, err
}

func (d *DB) GetWOPIToken(token string) (*models.WOPIToken, error) {
	t := &models.WOPIToken{}
	err := d.QueryRow(
		`SELECT id, token, file_id, user_id, permission, expires_at, created_at
		 FROM wopi_tokens WHERE token = $1 AND expires_at > NOW()`, token,
	).Scan(&t.ID, &t.Token, &t.FileID, &t.UserID, &t.Permission, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (d *DB) CleanExpiredTokens() error {
	_, err := d.Exec(`DELETE FROM wopi_tokens WHERE expires_at < NOW()`)
	return err
}

func generateToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

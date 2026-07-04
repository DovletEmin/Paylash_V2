package db

import (
	"crypto/rand"
	"encoding/hex"
	"paylash/internal/models"
	"time"
)

func (d *DB) CreateSession(userID int) (*models.Session, error) {
	token := generateToken(32)
	s := &models.Session{
		ID:        token,
		UserID:    userID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	_, err := d.Exec(
		`INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		s.ID, s.UserID, s.ExpiresAt,
	)
	return s, err
}

func (d *DB) GetSession(token string) (*models.Session, error) {
	s := &models.Session{}
	err := d.QueryRow(
		`SELECT id, user_id, expires_at, created_at FROM sessions WHERE id = $1 AND expires_at > NOW()`, token,
	).Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE id = $1`, token)
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

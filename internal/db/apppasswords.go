package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"

	"paylash/internal/models"
)

// appPasswordBytes is the raw entropy behind a device credential. 20 bytes
// is 160 bits, which is what lets the stored hash be a plain SHA-256 rather
// than a slow password hash — see the app_passwords comment in db.go.
const appPasswordBytes = 20

// NewAppPasswordToken mints a device credential. The plaintext is returned
// once, to be shown to the user and never stored; only its hash is kept.
func NewAppPasswordToken() (token, hash string) {
	b := make([]byte, appPasswordBytes)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	token = hex.EncodeToString(b)
	return token, HashAppPassword(token)
}

func HashAppPassword(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (d *DB) CreateAppPassword(userID int, name, hash string) (*models.AppPassword, error) {
	p := &models.AppPassword{}
	err := d.QueryRow(
		`INSERT INTO app_passwords (user_id, name, token_hash) VALUES ($1, $2, $3)
		 RETURNING id, user_id, name, created_at`,
		userID, name, hash,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt)
	return p, err
}

func (d *DB) ListAppPasswords(userID int) ([]models.AppPassword, error) {
	rows, err := d.Query(
		`SELECT id, user_id, name, created_at, last_used_at
		 FROM app_passwords WHERE user_id = $1 ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.AppPassword
	for rows.Next() {
		var p models.AppPassword
		var last sql.NullTime
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			p.LastUsedAt = &t
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// DeleteAppPassword revokes one credential, scoped to its owner so a caller
// can never revoke somebody else's by guessing an id.
func (d *DB) DeleteAppPassword(userID, id int) error {
	_, err := d.Exec(`DELETE FROM app_passwords WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (d *DB) DeleteAllAppPasswords(userID int) error {
	_, err := d.Exec(`DELETE FROM app_passwords WHERE user_id = $1`, userID)
	return err
}

// UserByAppPassword resolves a device credential to its owner. It also
// stamps last_used_at, which is the only way someone can tell a forgotten
// mapping on a machine they no longer use is still live.
//
// The update is deliberately throttled to once a minute: a single file
// operation over WebDAV is many requests, and writing a row on every one of
// them would turn a read-only mount into a steady stream of writes.
func (d *DB) UserByAppPassword(token string) (*models.User, error) {
	hash := HashAppPassword(token)
	var userID int
	err := d.QueryRow(
		`UPDATE app_passwords SET last_used_at = NOW()
		 WHERE token_hash = $1
		   AND (last_used_at IS NULL OR last_used_at < NOW() - INTERVAL '1 minute')
		 RETURNING user_id`, hash,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		// Either the token is unknown, or it is known and was used moments
		// ago — the throttle above excluded it from the UPDATE. Only the
		// second case has a row to find.
		err = d.QueryRow(`SELECT user_id FROM app_passwords WHERE token_hash = $1`, hash).Scan(&userID)
		if err == sql.ErrNoRows {
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return d.GetUserByID(userID)
}

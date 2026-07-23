package authutil

import (
	"net/http"
	"paylash/internal/models"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserKey contextKey = "user"

// Minimum username/password length, enforced identically wherever an
// account gets a username or password: self-registration, admin-created
// users, and CSV/XLSX bulk import — one shared rule instead of the same two
// magic numbers (3 and 6) repeated at every call site.
const (
	MinUsernameLen = 3
	MinPasswordLen = 6
)

// ValidUsername reports whether username (after trimming whitespace) meets
// the minimum length requirement. It does not trim its input for the
// caller — callers that also want the trimmed value should trim first and
// pass the result in.
func ValidUsername(username string) bool {
	return len(strings.TrimSpace(username)) >= MinUsernameLen
}

// ValidPassword reports whether password meets the minimum length
// requirement. Deliberately not trimmed — leading/trailing whitespace in a
// password is significant, unlike in a username.
func ValidPassword(password string) bool {
	return len(password) >= MinPasswordLen
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func GetUser(r *http.Request) *models.User {
	if u, ok := r.Context().Value(UserKey).(*models.User); ok {
		return u
	}
	return nil
}

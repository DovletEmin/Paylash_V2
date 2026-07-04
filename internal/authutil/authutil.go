package authutil

import (
	"net/http"
	"paylash/internal/models"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserKey contextKey = "user"

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

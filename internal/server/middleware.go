package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/db"
	"strings"
	"time"
)

func AuthMiddleware(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil {
				http.Error(w, `{"error":"ulgama giriň"}`, http.StatusUnauthorized)
				return
			}
			session, err := database.GetSession(cookie.Value)
			if err != nil {
				http.Error(w, `{"error":"möhleti geçen sessiýa"}`, http.StatusUnauthorized)
				return
			}
			user, err := database.GetUserByID(session.UserID)
			if err != nil || user == nil {
				http.Error(w, `{"error":"ulanyjy tapylmady"}`, http.StatusUnauthorized)
				return
			}
			// The frontend already blocks all interaction behind an
			// un-dismissable "change your password" modal for an account
			// flagged must_change_password — this is the same rule enforced
			// server-side, so hitting the API directly (curl, devtools)
			// can't route around it. Only the handful of endpoints needed to
			// view/change the password and sign out stay reachable.
			if user.MustChangePassword && !mustChangePasswordAllowed(r) {
				http.Error(w, `{"error":"parolyňyzy üýtgetmeli","code":"must_change_password"}`, http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), authutil.UserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func mustChangePasswordAllowed(r *http.Request) bool {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/avatar/") {
		return true
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/auth/me", "PATCH /api/auth/profile", "POST /api/auth/logout-others":
		return true
	}
	return false
}

func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := authutil.GetUser(r)
		if user == nil || user.Role != "admin" {
			http.Error(w, `{"error":"admin rugsady gerek"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func SeedAdmin(database *db.DB) error {
	existing, err := database.GetUserByUsername("admin")
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if existing != nil {
		return nil
	}
	hash, err := authutil.HashPassword("admin123")
	if err != nil {
		return err
	}
	_, err = database.Exec(
		`INSERT INTO users (username, password_hash, display_name, role, must_change_password)
		 VALUES ('admin', $1, 'Administrator', 'admin', TRUE)
		 ON CONFLICT (username) DO NOTHING`, hash,
	)
	if err != nil {
		return err
	}
	log.Println("admin user seeded (admin / admin123)")
	return nil
}

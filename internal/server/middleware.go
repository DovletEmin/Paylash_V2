package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/db"
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
			ctx := context.WithValue(r.Context(), authutil.UserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
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
		`INSERT INTO users (username, password_hash, display_name, role)
		 VALUES ('admin', $1, 'Administrator', 'admin')
		 ON CONFLICT (username) DO NOTHING`, hash,
	)
	if err != nil {
		return err
	}
	log.Println("admin user seeded (admin / admin123)")
	return nil
}

package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"net"
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
			// One query, not two: this runs on every authenticated request,
			// and the app polls in the background, so a second round trip
			// here was pure overhead on the hottest path in the app. See
			// db.GetSessionUser.
			session, user, err := database.GetSessionUser(cookie.Value)
			if err != nil {
				http.Error(w, `{"error":"möhleti geçen sessiýa"}`, http.StatusUnauthorized)
				return
			}
			if session == nil || user == nil {
				// The join cannot say WHICH half was missing, and the two
				// cases have different messages. Resolving that costs a
				// second query — but only here, on a request that has
				// already failed to authenticate.
				if s, sErr := database.GetSession(cookie.Value); sErr == nil && s != nil {
					http.Error(w, `{"error":"ulanyjy tapylmady"}`, http.StatusUnauthorized)
					return
				}
				http.Error(w, `{"error":"möhleti geçen sessiýa"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), authutil.UserKey, user)
			// An impersonation session's ImpersonatorID names the admin
			// behind it — GetUser still resolves to the impersonated
			// target above (every normal permission check must treat this
			// request as genuinely theirs), but the audit log and the
			// frontend's "viewing as" banner need to find the real actor
			// too. A dangling reference (admin account deleted since) just
			// means the session behaves as a plain one from here on.
			if session.ImpersonatorID != nil {
				if admin, err := database.GetUserByID(*session.ImpersonatorID); err == nil && admin != nil {
					ctx = context.WithValue(ctx, authutil.ImpersonatorKey, admin)
				}
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

// ManagerOrAdminMiddleware gates read-only admin-panel views a manager may
// also see (currently: attendance listing/analytics/export/schedule) — a
// manager can never reach a route wrapped in AdminMiddleware instead, which
// is how every state-changing admin action (including attendance
// corrections) stays admin-only.
func ManagerOrAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := authutil.GetUser(r)
		if user == nil || (user.Role != "admin" && user.Role != "manager") {
			http.Error(w, `{"error":"admin ýa-da menejer rugsady gerek"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestIDContextKey is unexported so only this package can mint/read it —
// callers elsewhere get it through RequestIDFromContext.
type requestIDContextKey struct{}

// RequestIDFromContext returns the correlation id LoggingMiddleware attached
// to this request's context, or "" outside of a real request (e.g. a test
// that never went through the middleware chain). Handlers can fold this into
// their own log.Printf/slog lines so a single request's server-side
// footprint — access log line, any error logs a handler wrote, and the
// X-Request-Id the client can see in devtools — all share one id.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// statusRecorder captures the status code a handler wrote — plain
// http.ResponseWriter has no way to read that back afterward.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack forwards to the underlying ResponseWriter's http.Hijacker. Without
// this, statusRecorder embeds the http.ResponseWriter *interface* — which
// doesn't declare Hijack() — so wrapping it silently drops hijack support
// even though the real writer underneath still has it. gorilla/websocket's
// Upgrade() needs to type-assert its way to a Hijacker to take over the raw
// connection; failing that assertion makes it write back its own 500, which
// is exactly what broke every /api/chat/ws connection once this middleware
// started wrapping the whole mux.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("underlying ResponseWriter does not support Hijack")
	}
	return hj.Hijack()
}

// LoggingMiddleware logs one structured line per request (method, route
// PATTERN — not the raw URL, see metrics.observe — status, duration, remote
// IP, request id) and records the same request into m for /metrics. The
// request id is also echoed back as the X-Request-Id response header, so a
// user-reported issue ("it broke around 14:32") can be correlated to an
// exact server-side log line via the browser's network tab.
func LoggingMiddleware(m *metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := newRequestID()
		w.Header().Set("X-Request-Id", reqID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, reqID))

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)

		pattern := r.Pattern
		if pattern == "" {
			pattern = r.URL.Path
		}
		m.observe(r.Method, pattern, rec.status, dur)

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		}
		slog.LogAttrs(r.Context(), level, "http_request",
			slog.String("request_id", reqID),
			slog.String("method", r.Method),
			slog.String("route", pattern),
			slog.Int("status", rec.status),
			slog.Int64("duration_ms", dur.Milliseconds()),
			slog.String("remote_ip", clientIPForLog(r)),
		)
	})
}

// clientIPForLog mirrors internal/api's clientIP (unexported there, and this
// package has no reason to import api just for it) — prefers
// X-Forwarded-For, set by Caddy's reverse_proxy in front of this app, since
// r.RemoteAddr behind a reverse proxy is just the proxy's own address.
func clientIPForLog(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i >= 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	return r.RemoteAddr
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

package dav

import (
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/net/webdav"

	"paylash/internal/db"
	"paylash/internal/storage"
)

// Handler serves the WebDAV endpoint. It authenticates every request with
// HTTP Basic against a device credential (never the account password — see
// the app_passwords comment in internal/db/db.go) and then builds a
// filesystem scoped to whoever presented it.
type Handler struct {
	db     *db.DB
	minio  *storage.MinioClient
	prefix string
	// One lock system for the whole server rather than one per request:
	// LOCK is how Word, AutoCAD and Explorer say "I have this open", and a
	// lock that vanished with the request that took it would be useless.
	// In-memory is the right scope — locks are advisory and meaningless
	// across a restart, when every client has lost its handle anyway.
	locks webdav.LockSystem
}

func NewHandler(database *db.DB, minioClient *storage.MinioClient, prefix string) *Handler {
	return &Handler{db: database, minio: minioClient, prefix: prefix, locks: webdav.NewMemLS()}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, token, ok := r.BasicAuth()
	if !ok || token == "" {
		h.challenge(w)
		return
	}
	user, err := h.db.UserByAppPassword(strings.TrimSpace(token))
	if err != nil {
		slog.Error("dav: credential lookup failed", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		h.challenge(w)
		return
	}
	// The same block the browser gets: someone who has not yet set their
	// own password must not be able to route around that with a drive.
	if user.MustChangePassword {
		http.Error(w, "change your password in the web app first", http.StatusForbidden)
		return
	}

	// Refused here rather than inside the filesystem, because the library's
	// status codes are fixed per method and ignore the error's meaning: a
	// Mkdir failure is always 405 and an OpenFile failure always 404,
	// whatever went wrong. Screening the fixed skeleton of the tree up front
	// is the only way a client is told "forbidden" when that is the truth.
	if isWriteMethod(r.Method) && protectedTarget(splitPath(strings.TrimPrefix(r.URL.Path, h.prefix))) {
		http.Error(w, "this part of the tree cannot be changed from the drive", http.StatusForbidden)
		return
	}

	dav := &webdav.Handler{
		Prefix:     h.prefix,
		FileSystem: NewFS(h.db, h.minio, user),
		LockSystem: h.locks,
		Logger: func(req *http.Request, err error) {
			// A 404 from a probing client is normal traffic — Explorer and
			// macOS both hunt for desktop.ini, .DS_Store and friends on
			// every directory. Only real failures are worth a line.
			if err == nil || isExpectedDavError(err) {
				return
			}
			slog.Warn("dav",
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.String("user", user.Username),
				slog.String("error", err.Error()))
		},
	}
	dav.ServeHTTP(w, r)
}

// challenge asks for credentials. The realm names the app so the operating
// system's prompt says what it is asking for.
func (h *Handler) challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Paylash"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// isWriteMethod reports whether a request would modify the tree.
func isWriteMethod(m string) bool {
	switch m {
	case "PUT", "DELETE", "MKCOL", "MOVE", "COPY", "PROPPATCH":
		return true
	}
	return false
}

// protectedTarget reports whether a path names part of the fixed skeleton
// of the tree — the root, the three spaces, and the project directories.
// Those exist because of what the database says, not because of what is on
// anyone's drive: a project is created in the admin panel, and a space is
// not a thing that can be renamed or deleted at all.
func protectedTarget(parts []string) bool {
	if len(parts) <= 1 {
		return true // "/", "/personal", "/common", "/projects"
	}
	return parts[0] == dirProjects && len(parts) == 2 // "/projects/<name>"
}

func isExpectedDavError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "file does not exist") ||
		strings.Contains(s, "read-only") ||
		strings.Contains(s, "file already exists")
}

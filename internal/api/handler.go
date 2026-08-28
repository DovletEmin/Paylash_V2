package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/models"
	"paylash/internal/storage"
	"paylash/internal/webpush"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	db                    *db.DB
	minio                 *storage.MinioClient
	cfg                   *config.Config
	commentLimiter        *keyedLimiter
	avatarLimiter         *keyedLimiter
	messageLimiter        *keyedLimiter
	chatAttachmentLimiter *keyedLimiter
	chatHub               *chatHub
	// Web Push: nil disables push entirely (key generation failed, or the
	// build is running somewhere it can never reach a push service). pushClient
	// has a short timeout so an unreachable push service never ties up a
	// goroutine for long. pushBatcher coalesces bursts of pushes to the same
	// recipient into one (see pushDigestWindow).
	vapid       *webpush.VAPIDKeys
	pushClient  *http.Client
	pushBatcher *pushBatcher
}

func NewHandler(database *db.DB, minioClient *storage.MinioClient, cfg *config.Config) *Handler {
	h := &Handler{
		db:                    database,
		minio:                 minioClient,
		cfg:                   cfg,
		commentLimiter:        newKeyedLimiter(commentMaxAttempts, commentWindow),
		avatarLimiter:         newKeyedLimiter(avatarMaxAttempts, avatarWindow),
		messageLimiter:        newKeyedLimiter(messageMaxAttempts, messageWindow),
		chatAttachmentLimiter: newKeyedLimiter(chatAttachmentMaxAttempts, chatAttachmentWindow),
		chatHub:               newChatHub(),
		pushClient:            &http.Client{Timeout: 10 * time.Second},
	}
	h.pushBatcher = newPushBatcher(h)
	h.initPush()
	return h
}

// PublicConfig exposes the handful of settings the login page needs before
// a session exists (e.g. whether to show the self-registration link) —
// nothing here is sensitive.
func (h *Handler) PublicConfig(w http.ResponseWriter, r *http.Request) {
	vapidKey := ""
	if h.vapid != nil {
		vapidKey = h.vapid.PublicB64
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allow_registration": h.cfg.AllowRegistration,
		"vapid_public_key":   vapidKey,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// setContentDisposition names a download correctly whatever the file is
// called. A bare filename="…" parameter is ISO-8859-1 by RFC 6266, so the raw
// UTF-8 bytes of a Cyrillic or Turkmen name are undefined there: browsers
// variously guess right, show mojibake, or give up and save the file as
// "download" — and this deployment names nearly everything in Russian or
// Turkmen. An unescaped quote in the name breaks the header outright. So we
// send both forms: an ASCII-only fallback for anything that ignores the
// second, and the RFC 5987 filename* that every current browser prefers.
func setContentDisposition(w http.ResponseWriter, disposition, name string) {
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, asciiFilename(name), rfc5987Escape(name)))
}

// asciiFilename replaces everything outside printable ASCII — and the quote
// and backslash that would end the quoted string early — with an underscore.
func asciiFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if strings.Trim(out, "_.") == "" {
		return "download" // a name with nothing ASCII in it at all
	}
	return out
}

// rfc5987Escape percent-encodes for an ext-value. Deliberately stricter than
// url.PathEscape, which leaves ";" "," "=" and ":" alone — any of those would
// terminate or corrupt the header parameter it sits in.
func rfc5987Escape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// maxJSONBodyBytes caps every plain-JSON request body this app accepts —
// forms, chat messages (already capped far lower at maxMessageLength),
// bulk id lists. All of them are small; this only exists to stop a
// malicious or broken client from streaming an unbounded body into
// readJSON. Actual file bytes never go through this path — uploads use
// multipart/form-data with their own limits (see UploadFile, uploads.go).
const maxJSONBodyBytes = 2 << 20 // 2MB

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	// w is nil here deliberately — readJSON has no http.ResponseWriter, and
	// MaxBytesReader has accepted a nil ResponseWriter since Go 1.19 (it just
	// skips closing the connection early on overflow; the caller still gets
	// a clean error from Decode).
	r.Body = http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes)
	return json.NewDecoder(r.Body).Decode(v)
}

// parsePagination reads limit/offset query params, falling back to
// defaultLimit and clamping to [1, maxLimit] / [0, +inf) respectively.
func parsePagination(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// logAction records an admin-oversight-worthy event on behalf of the
// request's authenticated user. Best-effort: a logging failure is logged
// itself but never blocks or fails the action it describes.
//
// During an admin impersonation session (see authutil.GetImpersonator),
// the ADMIN is recorded as the actor — not the impersonated user GetUser
// resolves to for every normal permission check — with the target user's
// name folded into details, so the audit log always shows who was really
// responsible rather than silently attributing the action to whoever was
// being impersonated at the time.
func (h *Handler) logAction(r *http.Request, action, targetType string, targetID int, targetName string, details map[string]any) {
	user := authutil.GetUser(r)
	if user == nil {
		return
	}
	actor := user
	if impersonator := authutil.GetImpersonator(r); impersonator != nil {
		actor = impersonator
		if details == nil {
			details = map[string]any{}
		}
		details["impersonating"] = user.Username
		details["impersonating_id"] = user.ID
	}
	actorID := actor.ID
	actorName := displayNameOrUsername(actor)
	id := targetID
	if err := h.db.LogAction(&actorID, actorName, action, targetType, &id, targetName, details); err != nil {
		log.Printf("audit log: %v", err)
	}
}

// setSessionCookie writes session as the browser's "session" cookie — the
// one piece shared by Login, Impersonate, and ExitImpersonation, all of
// which mint a session and need the browser to start using it immediately.
func setSessionCookie(w http.ResponseWriter, r *http.Request, session *models.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

// displayNameOrUsername is the actor-name fallback used everywhere an audit
// entry needs a human-readable name for a user — their chosen display name,
// or their username when they haven't set one.
func displayNameOrUsername(u *models.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// clientIP extracts the caller's address for an audit-log entry — prefers
// X-Forwarded-For (set automatically by Caddy's reverse_proxy in front of
// this app) since r.RemoteAddr behind a reverse proxy is just the proxy's
// own address, not the real client's.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// A comma-separated chain (proxy1, proxy2, ...) — the first entry is
		// the original client.
		if i := strings.IndexByte(fwd, ','); i >= 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	return r.RemoteAddr
}

// logAuthAction is logAction's counterpart for the auth endpoints
// (register/login/logout) — those run before AuthMiddleware ever puts a
// user in the request context (login/register have no session yet; a
// failed login never gets one at all), so unlike logAction this takes the
// actor and target explicitly rather than reading them off the request.
// actorID is nil for a login attempt that never actually authenticated as
// anyone (unknown username, or a real account with the wrong password) —
// attributing a failed attempt AS the account it targeted would misleadingly
// suggest that account performed it.
func (h *Handler) logAuthAction(r *http.Request, actorID *int, actorName, action string, targetID *int, targetName string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	details["ip"] = clientIP(r)
	if err := h.db.LogAction(actorID, actorName, action, "user", targetID, targetName, details); err != nil {
		log.Printf("audit log: %v", err)
	}
}

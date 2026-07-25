package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
	"strings"
	"time"
)

// totpIssuer labels this app in an authenticator's account list.
const totpIssuer = "Paýlaş"

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.AllowRegistration {
		writeError(w, http.StatusForbidden, "hasaba durmak öçürilen, admin bilen habarlaşyň")
		return
	}
	ipKey := "ip:" + clientIP(r)
	if h.registerLimiter.blocked(ipKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.registerLimiter.record(ipKey)
	var req models.RegisterRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if !authutil.ValidUsername(req.Username) {
		writeError(w, http.StatusBadRequest, "ulanyjy ady azyndan 3 harp bolmaly")
		return
	}
	if !authutil.ValidPassword(req.Password) {
		writeError(w, http.StatusBadRequest, "parol azyndan 6 simwol bolmaly")
		return
	}

	exists, err := h.db.UserExists(req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "bu ulanyjy ady eýýäm bar")
		return
	}

	hash, err := authutil.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}

	user, err := h.db.CreateUser(&req, hash, "user", models.DefaultQuotaBytes, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hasap döredip bolmady")
		return
	}

	// Create personal bucket in MinIO
	bucket := storage.PersonalBucket(user.ID)
	if err := h.minio.EnsureBucket(r.Context(), bucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar döredip bolmady")
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	username := strings.TrimSpace(req.Username)
	userKey := "u:" + strings.ToLower(username)
	ipKey := "ip:" + clientIP(r)

	if h.loginLimiter.blocked(userKey) || h.loginLimiter.blocked(ipKey) {
		if err := h.db.LogAction(nil, username, "login.blocked", "", nil, "", map[string]any{"ip": clientIP(r)}); err != nil {
			log.Printf("audit log: %v", err)
		}
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}

	user, err := h.db.GetUserByUsername(username)
	if err != nil || user == nil {
		h.loginLimiter.record(userKey)
		h.loginLimiter.record(ipKey)
		writeError(w, http.StatusUnauthorized, "nädogry ulanyjy ady ýa-da parol")
		return
	}

	if !authutil.CheckPassword(req.Password, user.PasswordHash) {
		h.loginLimiter.record(userKey)
		h.loginLimiter.record(ipKey)
		writeError(w, http.StatusUnauthorized, "nädogry ulanyjy ady ýa-da parol")
		return
	}

	// Second factor. The password was correct, but for a 2FA account no
	// session is issued until a valid TOTP (or recovery) code is also given.
	// A missing code is signalled with a distinct machine-readable code so the
	// client knows to prompt for it rather than treat it as a bad password.
	if user.TOTPEnabled {
		code := strings.TrimSpace(req.TOTPCode)
		if code == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "2FA koduny giriziň", "code": "totp_required"})
			return
		}
		secret, _, recoveryJSON, err := h.db.GetTOTPState(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
			return
		}
		if !authutil.VerifyTOTP(secret, code, time.Now()) && !h.consumeRecoveryCode(user.ID, code, recoveryJSON) {
			h.loginLimiter.record(userKey)
			h.loginLimiter.record(ipKey)
			if err := h.db.LogAction(nil, username, "login.totp_failed", "", nil, "", map[string]any{"ip": clientIP(r)}); err != nil {
				log.Printf("audit log: %v", err)
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "2FA kody nädogry", "code": "totp_invalid"})
			return
		}
	}

	h.loginLimiter.reset(userKey)
	h.loginLimiter.reset(ipKey)

	session, err := h.db.CreateSession(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sessiýa döredip bolmady")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, user)
}

// consumeRecoveryCode checks code against the account's stored (hashed)
// recovery codes; on a match it removes that code (single-use) and returns
// true. Comparison is constant-time. recoveryJSON is a JSON array of hashes.
func (h *Handler) consumeRecoveryCode(userID int, code, recoveryJSON string) bool {
	if recoveryJSON == "" {
		return false
	}
	var hashes []string
	if err := json.Unmarshal([]byte(recoveryJSON), &hashes); err != nil {
		return false
	}
	target := authutil.HashRecoveryCode(code)
	for i, hsh := range hashes {
		if subtle.ConstantTimeCompare([]byte(hsh), []byte(target)) == 1 {
			hashes = append(hashes[:i], hashes[i+1:]...)
			b, _ := json.Marshal(hashes)
			if err := h.db.UpdateRecoveryCodes(userID, string(b)); err != nil {
				log.Printf("consume recovery code: %v", err)
			}
			return true
		}
	}
	return false
}

// TOTPSetup generates a fresh secret (pending, not yet enabled) and returns it
// plus the otpauth:// URI to enroll in an authenticator app. Not enabled until
// TOTPEnable confirms a code, so an abandoned setup never locks anyone out.
func (h *Handler) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	secret, err := authutil.GenerateTOTPSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	if err := h.db.SetTOTPSecret(user.ID, secret); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":      secret,
		"otpauth_uri": authutil.TOTPURI(totpIssuer, user.Username, secret),
	})
}

// TOTPEnable verifies the first code against the pending secret, turns 2FA on,
// and returns one-time recovery codes (shown to the user exactly once).
func (h *Handler) TOTPEnable(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	secret, enabled, _, err := h.db.GetTOTPState(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	if enabled {
		writeError(w, http.StatusBadRequest, "2FA eýýäm işjeň")
		return
	}
	if secret == "" {
		writeError(w, http.StatusBadRequest, "ilki 2FA-ny gurnaň")
		return
	}
	if !authutil.VerifyTOTP(secret, req.Code, time.Now()) {
		writeError(w, http.StatusBadRequest, "kod nädogry")
		return
	}
	codes, err := authutil.GenerateRecoveryCodes(8)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = authutil.HashRecoveryCode(c)
	}
	b, _ := json.Marshal(hashes)
	if err := h.db.EnableTOTP(user.ID, string(b)); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	h.logAction(r, "auth.2fa_enabled", "user", user.ID, user.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "recovery_codes": codes})
}

// TOTPDisable turns 2FA off — gated on the account password (re-auth) so a
// walked-away, still-logged-in session can't silently strip the second factor.
func (h *Handler) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	full, err := h.db.GetUserByID(user.ID)
	if err != nil || full == nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	if !authutil.CheckPassword(req.Password, full.PasswordHash) {
		writeError(w, http.StatusForbidden, "parol nädogry")
		return
	}
	if err := h.db.DisableTOTP(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	h.logAction(r, "auth.2fa_disabled", "user", user.ID, user.Username, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LogoutOthers invalidates every session for the caller's account except the
// one making this request — an explicit "log out of other devices" action,
// independent of a password change (e.g. after losing a device, or just as
// routine hygiene).
func (h *Handler) LogoutOthers(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	cookie, err := r.Cookie("session")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	if err := h.db.DeleteOtherSessions(user.ID, cookie.Value); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		h.db.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	writeJSON(w, http.StatusOK, user)
}
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giri\u0148")
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "n\u00e4dogry maglumat")
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name != "" && name != user.DisplayName {
		if err := h.db.UpdateDisplayName(user.ID, name); err != nil {
			writeError(w, http.StatusInternalServerError, "ady \u00fc\u00fdtgedip bolmady")
			return
		}
	}
	if req.NewPassword != "" {
		if !authutil.ValidPassword(req.NewPassword) {
			writeError(w, http.StatusBadRequest, "t\u00e4ze parol a\u017cyndan 6 simwol bolmaly")
			return
		}
		full, err := h.db.GetUserByID(user.ID)
		if err != nil || full == nil {
			writeError(w, http.StatusInternalServerError, "\u00fda\u0148ly\u015flyk")
			return
		}
		if !authutil.CheckPassword(req.OldPassword, full.PasswordHash) {
			writeError(w, http.StatusForbidden, "k\u00f6ne parol n\u00e4dogry")
			return
		}
		hash, err := authutil.HashPassword(req.NewPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "\u00fda\u0148ly\u015flyk")
			return
		}
		if err := h.db.UpdatePassword(user.ID, hash); err != nil {
			writeError(w, http.StatusInternalServerError, "paroly \u00fc\u00fdtgedip bolmady")
			return
		}
		// A changed password should immediately kick out any *other* session
		// for this account \u2014 otherwise a session opened before the password
		// leaked/was shared stays valid for up to 7 more days regardless.
		// The request's own session (the one used to make this very change)
		// is kept, so the user isn't logged out of the tab they're using.
		if cookie, err := r.Cookie("session"); err == nil {
			if err := h.db.DeleteOtherSessions(user.ID, cookie.Value); err != nil {
				log.Printf("invalidate other sessions after password change for user %d: %v", user.ID, err)
			}
		}
	}
	updated, _ := h.db.GetUserByID(user.ID)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "ulgama giriň")
		return
	}
	userKey := strconv.Itoa(user.ID)
	if h.avatarLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.avatarLimiter.record(userKey)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "faýl juda uly (maks 5MB)")
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		writeError(w, http.StatusBadRequest, "faýl tapylmady")
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		writeError(w, http.StatusBadRequest, "diňe surat faýly rugsat berilýär")
		return
	}

	bucket := storage.PersonalBucket(user.ID)
	if err := h.minio.EnsureBucket(r.Context(), bucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar ýalňyşlygy")
		return
	}
	key := fmt.Sprintf("avatar/%d%s", time.Now().Unix(), extFromMime(ct))
	if err := h.minio.Upload(r.Context(), bucket, key, file, header.Size, ct); err != nil {
		writeError(w, http.StatusInternalServerError, "ýükläp bolmady")
		return
	}

	avatarURL := bucket + "/" + key
	if err := h.db.UpdateAvatarURL(user.ID, avatarURL); err != nil {
		writeError(w, http.StatusInternalServerError, "ýatda saklap bolmady")
		return
	}

	updated, _ := h.db.GetUserByID(user.ID)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (h *Handler) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, err := h.db.GetUserByID(id)
	if err != nil || user == nil || user.AvatarURL == "" {
		writeError(w, http.StatusNotFound, "awatar tapylmady")
		return
	}

	parts := strings.SplitN(user.AvatarURL, "/", 2)
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, "awatar tapylmady")
		return
	}

	obj, err := h.minio.Download(r.Context(), parts[0], parts[1])
	if err != nil {
		writeError(w, http.StatusNotFound, "awatar tapylmady")
		return
	}
	defer obj.Close()

	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Type", mimeFromExt(filepath.Ext(parts[1])))
	io.Copy(w, obj)
}

func extFromMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// mimeFromExt is extFromMime's inverse — used to serve the avatar back with
// its real content type instead of hardcoding image/jpeg for every format.
func mimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
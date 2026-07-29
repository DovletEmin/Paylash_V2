package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/webpush"
	"sync"
	"time"
)

// pushSubscriber is the VAPID "sub" contact — an identifier for the app
// operator that a push service can reach out to. It never receives real mail;
// it just has to be a valid mailto/https per RFC 8292.
const pushSubscriber = "mailto:admin@paylash.local"

// initPush loads the VAPID keypair from settings, generating and persisting
// one on first run. A stable keypair matters: the public key is baked into
// every browser subscription, so regenerating it would silently invalidate
// them all. Any failure just leaves push disabled (h.vapid stays nil) — never
// fatal, since the app is fully usable without it.
func (h *Handler) initPush() {
	priv, errP := h.db.GetSetting("vapid_private")
	pub, errU := h.db.GetSetting("vapid_public")
	if errP != nil || errU != nil || priv == "" || pub == "" {
		newPriv, newPub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			log.Printf("web push disabled: generate VAPID keys: %v", err)
			return
		}
		if err := h.db.SetSetting("vapid_private", newPriv); err != nil {
			log.Printf("web push disabled: persist VAPID private: %v", err)
			return
		}
		if err := h.db.SetSetting("vapid_public", newPub); err != nil {
			log.Printf("web push disabled: persist VAPID public: %v", err)
			return
		}
		priv, pub = newPriv, newPub
	}
	vk, err := webpush.ParseVAPIDKeys(priv, pub)
	if err != nil {
		log.Printf("web push disabled: parse VAPID keys: %v", err)
		return
	}
	h.vapid = vk
	log.Println("web push enabled")
}

// PushSubscribe stores (or refreshes) the caller's browser push subscription.
func (h *Handler) PushSubscribe(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := readJSON(r, &req); err != nil || req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if err := h.db.SavePushSubscription(user.ID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PushUnsubscribe removes a subscription by endpoint (the browser dropped it,
// or the user turned notifications off).
func (h *Handler) PushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := readJSON(r, &req); err != nil || req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if err := h.db.DeletePushSubscription(req.Endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk ýüze çykdy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// pushContentFor builds the notification title/body for a recipient, honouring
// their chat notification privacy level — the same three levels the in-app and
// native (tab-open) notifications respect. It avoids server-side i18n (which
// this app doesn't have) by leaning on language-neutral content: the real
// message text, the sticker emoji itself, an attachment's filename, or a bare
// emoji fallback.
func pushContentFor(level string, msg *models.MessageView) (title, body string) {
	sender := msg.SenderName
	if sender == "" {
		sender = "Paýlaş"
	}
	switch level {
	case "hidden":
		return "Paýlaş", "💬"
	case "sender_only":
		return sender, "💬"
	default: // "full"
		if msg.Kind == "sticker" {
			return sender, msg.Body
		}
		if msg.Body != "" {
			return sender, msg.Body
		}
		if len(msg.Attachments) > 0 {
			return sender, "📎 " + msg.Attachments[0].FileName
		}
		return sender, "💬"
	}
}

// pushChatMessage hands msg off to the batcher for every participant (other
// than the sender) who's eligible for a push right now — no live WS
// connection (app closed, the one case in-app/native notifications can't
// cover) and hasn't muted this conversation. Best-effort: run in a goroutine
// by callers.
func (h *Handler) pushChatMessage(convID, senderID int, msg *models.MessageView) {
	if h.vapid == nil {
		return
	}
	participants, err := h.db.ListParticipants(convID)
	if err != nil {
		return
	}
	for _, p := range participants {
		if p.UserID == senderID || p.Muted || h.chatHub.isOnline(p.UserID) {
			continue
		}
		h.pushBatcher.enqueue(p.UserID, convID, msg)
	}
}

// pushDigestWindow is how long the batcher waits, per recipient, after the
// first eligible message before actually sending a push — a short debounce
// so a burst (several messages in a row, or several different conversations
// going quiet at once) collapses into one push instead of one per message,
// without meaningfully delaying the "phone is asleep" notification case a
// lone message still hits.
const pushDigestWindow = 4 * time.Second

// pendingPush accumulates everything enqueue()'d for one recipient during
// one debounce window.
type pendingPush struct {
	timer      *time.Timer
	count      int
	convIDs    map[int]bool
	lastConvID int
	lastMsg    *models.MessageView
}

// pushBatcher coalesces per-recipient push notifications across
// pushDigestWindow — a lone message still produces exactly one push, worded
// identically to before (see sendPushDigest), so the common case is
// unchanged; only a genuine burst gets combined.
type pushBatcher struct {
	h       *Handler
	mu      sync.Mutex
	pending map[int]*pendingPush
}

func newPushBatcher(h *Handler) *pushBatcher {
	return &pushBatcher{h: h, pending: make(map[int]*pendingPush)}
}

func (b *pushBatcher) enqueue(recipientID, convID int, msg *models.MessageView) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pending[recipientID]
	if p == nil {
		p = &pendingPush{convIDs: make(map[int]bool)}
		b.pending[recipientID] = p
		p.timer = time.AfterFunc(pushDigestWindow, func() { b.flush(recipientID) })
	}
	p.count++
	p.convIDs[convID] = true
	p.lastConvID = convID
	p.lastMsg = msg
}

func (b *pushBatcher) flush(recipientID int) {
	b.mu.Lock()
	p := b.pending[recipientID]
	delete(b.pending, recipientID)
	b.mu.Unlock()
	if p == nil {
		return
	}
	b.h.sendPushDigest(recipientID, p)
}

// sendPushDigest delivers the single push a debounce window collapsed to —
// re-checking online/mute state right before sending (not trusting whatever
// it was at enqueue time, several seconds earlier) since the recipient may
// have opened the app or muted the conversation in the meantime.
func (h *Handler) sendPushDigest(recipientID int, p *pendingPush) {
	if h.chatHub.isOnline(recipientID) {
		return
	}
	recipient, err := h.db.GetUserByID(recipientID)
	if err != nil || recipient == nil {
		return
	}
	var title, body string
	if p.count == 1 && p.lastMsg != nil {
		title, body = pushContentFor(recipient.ChatNotifyLevel, p.lastMsg)
	} else {
		title, body = pushDigestContentFor(recipient.ChatNotifyLevel, p)
	}
	payload, err := json.Marshal(map[string]any{
		"title": title, "body": body, "conversation_id": p.lastConvID,
	})
	if err != nil {
		return
	}
	h.sendPushToUser(recipientID, payload)
}

// sendPushToUser delivers payload to every push subscription userID has
// registered, pruning any the push service reports as gone (404/410).
// Shared by the chat digest path above and every other feature that wants
// to push a native notification — see pushShareNotification/
// pushCommentNotification.
func (h *Handler) sendPushToUser(userID int, payload []byte) {
	subs, err := h.db.ListPushSubscriptions(userID)
	if err != nil {
		return
	}
	for _, s := range subs {
		sub := &webpush.Subscription{Endpoint: s.Endpoint, P256dh: s.P256dh, Auth: s.Auth}
		status, err := webpush.Send(h.pushClient, h.vapid, pushSubscriber, sub, payload, 3600)
		if err != nil {
			log.Printf("web push to user %d: %v", userID, err)
			continue
		}
		if status == http.StatusNotFound || status == http.StatusGone {
			if err := h.db.DeletePushSubscription(s.Endpoint); err != nil {
				log.Printf("prune dead push subscription: %v", err)
			}
		}
	}
}

// pushShareNotification tells recipientID that fileName was just shared
// with them — fire-and-forget, called via `go` by ShareFile. Unlike chat's
// push, there's no live-connection signal to skip on here (file shares have
// no WebSocket): the in-app unread-count poll and this push are just two
// independent channels for the same event, so a push always fires as long
// as the recipient has a registered subscription at all.
func (h *Handler) pushShareNotification(recipientID int, sharerName, fileName string) {
	if h.vapid == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"title": sharerName,
		"body":  fmt.Sprintf("Paýlaşdy: %s", fileName),
		"url":   "/#shared",
	})
	if err != nil {
		return
	}
	h.sendPushToUser(recipientID, payload)
}

// pushCommentMaxPreview caps how much of a comment's body rides along in
// the push notification itself — long enough for context, short enough to
// fit a native OS notification without the OS truncating it first.
const pushCommentMaxPreview = 120

// pushCommentNotification tells the file's owner and every other distinct
// commenter on the same file (the file's own "who's watching this thread"
// set — no separate follow/subscribe model to manage) that a new comment
// landed, except the person who just posted it. Fire-and-forget, called via
// `go` by CreateFileComment.
func (h *Handler) pushCommentNotification(fileID, commenterID int, commenterName, commentBody string) {
	if h.vapid == nil {
		return
	}
	f, err := h.db.GetFile(fileID)
	if err != nil || f == nil {
		return
	}
	recipients := map[int]bool{}
	if f.OwnerID != commenterID {
		recipients[f.OwnerID] = true
	}
	if comments, err := h.db.ListComments(fileID); err == nil {
		for _, c := range comments {
			if c.UserID != commenterID {
				recipients[c.UserID] = true
			}
		}
	}
	if len(recipients) == 0 {
		return
	}
	preview := commentBody
	if r := []rune(preview); len(r) > pushCommentMaxPreview {
		preview = string(r[:pushCommentMaxPreview]) + "…"
	}
	payload, err := json.Marshal(map[string]any{
		"title": fmt.Sprintf("%s — %s", commenterName, f.Name),
		"body":  preview,
	})
	if err != nil {
		return
	}
	for uid := range recipients {
		h.sendPushToUser(uid, payload)
	}
}

// pushDigestContentFor builds the title/body for a COMBINED push (more than
// one qualifying message landed in one debounce window) — honors the same
// privacy level as pushContentFor, but a digest spanning multiple messages/
// conversations has no single sender or body to show even at "full", so it
// falls back to a count summary instead.
func pushDigestContentFor(level string, p *pendingPush) (title, body string) {
	if level == "hidden" {
		return "Paýlaş", "💬"
	}
	if len(p.convIDs) > 1 {
		return "Paýlaş", fmt.Sprintf("%d täze habar, %d söhbetde", p.count, len(p.convIDs))
	}
	return "Paýlaş", fmt.Sprintf("%d täze habar", p.count)
}

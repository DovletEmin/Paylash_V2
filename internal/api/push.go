package api

import (
	"encoding/json"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/webpush"
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

// pushChatMessage delivers a web push for msg to every participant (other than
// the sender) who has NO live WS connection — i.e. whose app is closed, the
// one case the in-app/native notifications can't cover. Best-effort: run in a
// goroutine by callers, and a delivery error (including an unreachable push
// service on an offline LAN) is logged and shrugged off. A push service
// reporting the endpoint gone (404/410) prunes that dead subscription.
func (h *Handler) pushChatMessage(convID, senderID int, msg *models.MessageView) {
	if h.vapid == nil {
		return
	}
	participants, err := h.db.ListParticipants(convID)
	if err != nil {
		return
	}
	for _, p := range participants {
		if p.UserID == senderID || h.chatHub.isOnline(p.UserID) {
			continue
		}
		recipient, err := h.db.GetUserByID(p.UserID)
		if err != nil || recipient == nil {
			continue
		}
		title, body := pushContentFor(recipient.ChatNotifyLevel, msg)
		payload, err := json.Marshal(map[string]any{
			"title": title, "body": body, "conversation_id": convID,
		})
		if err != nil {
			continue
		}
		subs, err := h.db.ListPushSubscriptions(p.UserID)
		if err != nil {
			continue
		}
		for _, s := range subs {
			sub := &webpush.Subscription{Endpoint: s.Endpoint, P256dh: s.P256dh, Auth: s.Auth}
			status, err := webpush.Send(h.pushClient, h.vapid, pushSubscriber, sub, payload, 3600)
			if err != nil {
				log.Printf("web push to user %d: %v", p.UserID, err)
				continue
			}
			if status == http.StatusNotFound || status == http.StatusGone {
				if err := h.db.DeletePushSubscription(s.Endpoint); err != nil {
					log.Printf("prune dead push subscription: %v", err)
				}
			}
		}
	}
}

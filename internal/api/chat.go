package api

import (
	"crypto/rand"
	"encoding/hex"
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

const (
	maxMessageLength         = 4000
	maxAttachmentsPerMessage = 10
	maxChatAttachmentSize    = 50 << 20 // 50MB
)

// requireParticipant is the privacy boundary for every conversation-scoped
// handler: unlike CanAccessFile-style checks elsewhere in this app, there is
// deliberately no admin bypass here — chats are fully private between their
// participants, an explicit exception to this app's usual "admin sees
// everything" posture.
func requireParticipant(h *Handler, w http.ResponseWriter, r *http.Request, conversationID int) (*models.User, bool) {
	user := authutil.GetUser(r)
	ok, err := h.db.IsParticipant(conversationID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return nil, false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return nil, false
	}
	return user, true
}

func conversationIDFromPath(r *http.Request) (int, error) {
	return strconv.Atoi(r.PathValue("id"))
}

// participantIDs pulls just the user ids out of a participant list — the
// shape hub.broadcast wants, needed at every send/edit/delete/forward call
// site.
func participantIDs(participants []models.ParticipantView) []int {
	ids := make([]int, 0, len(participants))
	for _, p := range participants {
		ids = append(ids, p.UserID)
	}
	return ids
}

// SearchChatUsers backs the "new DM" / "add to group" picker.
func (h *Handler) SearchChatUsers(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results, err := h.db.SearchChatUsers(q, user.ID, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gözleg ýalňyşlygy")
		return
	}
	if results == nil {
		results = []models.UserSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

// SearchChatMessages searches the caller's conversations for text matching q.
// An optional conversation_id narrows it to one conversation (the caller must
// be a participant). Deleted/hidden messages are excluded by the query.
func (h *Handler) SearchChatMessages(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, http.StatusOK, []models.MessageSearchResult{})
		return
	}
	convID := 0
	if v := r.URL.Query().Get("conversation_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			// Scoped search must still respect membership.
			ok, err := h.db.IsParticipant(n, user.ID)
			if err != nil || !ok {
				writeError(w, http.StatusForbidden, "rugsat ýok")
				return
			}
			convID = n
		}
	}
	results, err := h.db.SearchMessages(user.ID, q, convID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gözleg ýalňyşlygy")
		return
	}
	if results == nil {
		results = []models.MessageSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	archived := r.URL.Query().Get("archived") == "1"
	list, err := h.db.ListConversationsForUser(user.ID, archived)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sözleşmeleri alyp bolmady")
		return
	}
	if list == nil {
		list = []models.ConversationView{}
	}
	// Flip the DM partner's live online dot on in the inbox list.
	for i := range list {
		if list[i].OtherParticipant != nil {
			list[i].OtherParticipant.Online = h.chatHub.isOnline(list[i].OtherParticipant.UserID)
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	var req struct {
		Type           string `json:"type"`
		UserID         int    `json:"user_id"`
		Name           string `json:"name"`
		ParticipantIDs []int  `json:"participant_ids"`
		ProjectID      *int   `json:"project_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	if req.Type == "direct" {
		if req.UserID <= 0 || req.UserID == user.ID {
			writeError(w, http.StatusBadRequest, "nädogry ulanyjy")
			return
		}
		conv, err := h.db.FindOrCreateDirectConversation(user.ID, req.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sözleşme döredip bolmady")
			return
		}
		writeJSON(w, http.StatusCreated, conv)
		return
	}

	if req.Type == "group" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "topara at gerek")
			return
		}
		conv, err := h.db.CreateGroupConversation(name, user.ID, req.ProjectID, req.ParticipantIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "topar döredip bolmady")
			return
		}
		h.chatHub.broadcast(append(req.ParticipantIDs, user.ID), map[string]any{
			"type": "conversation.updated", "conversation_id": conv.ID,
		})
		writeJSON(w, http.StatusCreated, conv)
		return
	}

	writeError(w, http.StatusBadRequest, "nädogry görnüş")
}

func (h *Handler) GetConversationDetail(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	if _, ok := requireParticipant(h, w, r, convID); !ok {
		return
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "tapylmady")
		return
	}
	participants, err := h.db.ListParticipants(convID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	// Online status is live hub state, not in the DB — stamp it here.
	for i := range participants {
		participants[i].Online = h.chatHub.isOnline(participants[i].UserID)
	}
	var pinnedMessage *models.MessageView
	if conv.PinnedMessageID != nil {
		pinnedMessage, err = h.db.GetMessageView(*conv.PinnedMessageID)
		if err != nil {
			pinnedMessage = nil
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": conv, "participants": participants, "pinned_message": pinnedMessage})
}

// requireCreator loads the conversation and checks the requester is its
// OWNER (the creator) — used only for actions no admin may do: promoting or
// demoting another admin. There is no wider admin override here either, same
// privacy stance as requireParticipant.
func requireCreator(h *Handler, w http.ResponseWriter, convID int, userID int) (*models.Conversation, bool) {
	conv, err := h.db.GetConversation(convID)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "tapylmady")
		return nil, false
	}
	if conv.Type != "group" {
		writeError(w, http.StatusBadRequest, "diňe topar üçin")
		return nil, false
	}
	if conv.CreatedBy == nil || *conv.CreatedBy != userID {
		writeError(w, http.StatusForbidden, "diňe topary dörediji üýtgedip biler")
		return nil, false
	}
	return conv, true
}

// requireManager is the relaxed permission tier for day-to-day group
// management — rename, add members, remove a non-admin member, pin a
// message, set the group photo. The owner (creator) or any admin may do
// these; promoting/demoting admins themselves stays owner-only (see
// requireCreator / SetParticipantRole).
func requireManager(h *Handler, w http.ResponseWriter, convID int, userID int) (*models.Conversation, bool) {
	conv, err := h.db.GetConversation(convID)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "tapylmady")
		return nil, false
	}
	if conv.Type != "group" {
		writeError(w, http.StatusBadRequest, "diňe topar üçin")
		return nil, false
	}
	if conv.CreatedBy != nil && *conv.CreatedBy == userID {
		return conv, true
	}
	role, err := h.db.GetParticipantRole(convID, userID)
	if err != nil || role != "admin" {
		writeError(w, http.StatusForbidden, "diňe dolandyryjylar üýtgedip biler")
		return nil, false
	}
	return conv, true
}

func (h *Handler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	if _, ok := requireManager(h, w, convID, user.ID); !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "at boş bolup bilmez")
		return
	}
	if err := h.db.RenameConversation(convID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	notifyConversationUpdated(h, convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AddParticipants(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	if _, ok := requireManager(h, w, convID, user.ID); !ok {
		return
	}
	var req struct {
		UserIDs []int `json:"user_ids"`
	}
	if err := readJSON(r, &req); err != nil || len(req.UserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if err := h.db.AddParticipants(convID, req.UserIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "goşup bolmady")
		return
	}
	notifyConversationUpdated(h, convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveParticipant allows: the owner or an admin removing someone else, or
// any participant removing themselves (leaving, which never needs anyone's
// permission). An admin (not the owner) may only remove a plain member —
// removing the owner or another admin stays owner-only, so admins can't
// kick each other out.
func (h *Handler) RemoveParticipant(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	targetID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	if targetID != user.ID {
		conv, ok := requireManager(h, w, convID, user.ID)
		if !ok {
			return
		}
		isOwner := conv.CreatedBy != nil && *conv.CreatedBy == user.ID
		if !isOwner {
			if conv.CreatedBy != nil && *conv.CreatedBy == targetID {
				writeError(w, http.StatusForbidden, "diňe eýesi özüni aýryp biler")
				return
			}
			targetRole, err := h.db.GetParticipantRole(convID, targetID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
				return
			}
			if targetRole == "admin" {
				writeError(w, http.StatusForbidden, "diňe eýesi admini aýryp biler")
				return
			}
		}
	}
	if err := h.db.RemoveParticipant(convID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "aýryp bolmady")
		return
	}
	notifyConversationUpdated(h, convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SetParticipantRole promotes a member to admin or demotes an admin back to
// member — owner-only (requireCreator), and the owner can't change their own
// role through this endpoint (they're always the top of the hierarchy via
// Conversation.CreatedBy, not the role column).
func (h *Handler) SetParticipantRole(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	targetID, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	if _, ok := requireCreator(h, w, convID, user.ID); !ok {
		return
	}
	if targetID == user.ID {
		writeError(w, http.StatusBadRequest, "öz roluňyzy üýtgedip bilmersiňiz")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil || (req.Role != "admin" && req.Role != "member") {
		writeError(w, http.StatusBadRequest, "nädogry rol")
		return
	}
	if isMember, err := h.db.IsParticipant(convID, targetID); err != nil || !isMember {
		writeError(w, http.StatusNotFound, "agza tapylmady")
		return
	}
	if err := h.db.SetParticipantRole(convID, targetID, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	notifyConversationUpdated(h, convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	beforeID := 0
	if v := r.URL.Query().Get("before_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			beforeID = n
		}
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	list, err := h.db.ListMessages(convID, user.ID, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "habarlary alyp bolmady")
		return
	}
	if list == nil {
		list = []models.MessageView{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}

	// A direct conversation blocked by either side rejects new sends outright
	// (existing history stays visible — see IsEitherBlocked). Groups are
	// unaffected: blocking is a DM-privacy feature, not a way to be silently
	// removed from shared group conversations.
	if conv, err := h.db.GetConversation(convID); err == nil && conv != nil && conv.Type == "direct" && conv.DirectUserLow != nil && conv.DirectUserHigh != nil {
		other := *conv.DirectUserLow
		if other == user.ID {
			other = *conv.DirectUserHigh
		}
		if blocked, err := h.db.IsEitherBlocked(user.ID, other); err == nil && blocked {
			writeError(w, http.StatusForbidden, "bu ulanyja habar iberip bolmaýar")
			return
		}
	}

	userKey := strconv.Itoa(user.ID)
	if h.messageLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.messageLimiter.record(userKey)

	var req struct {
		Body          string `json:"body"`
		AttachmentIDs []int  `json:"attachment_ids"`
		Kind          string `json:"kind"`
		ReplyToID     *int   `json:"reply_to_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}

	kind := req.Kind
	if kind == "" {
		kind = "text"
	}
	if kind != "text" && kind != "sticker" && kind != "voice" {
		writeError(w, http.StatusBadRequest, "nädogry görnüş")
		return
	}
	body := strings.TrimSpace(req.Body)
	if kind == "sticker" {
		// A sticker message IS the emoji (no separate text), sends alone, and
		// its body must be one of the curated set — not just "short" — so a
		// hand-crafted client can't smuggle markup into a field rendered
		// without HTML-escaping. See isValidSticker / stickers.go.
		if !isValidSticker(body) {
			writeError(w, http.StatusBadRequest, "nädogry stiker")
			return
		}
		if len(req.AttachmentIDs) > 0 {
			writeError(w, http.StatusBadRequest, "stikere goşundy goşup bolmaz")
			return
		}
	} else if kind == "voice" {
		// A voice message IS its one audio attachment — body carries no text
		// (any typed text is just discarded rather than rejected, since the
		// client never means to send it as a caption here).
		if len(req.AttachmentIDs) != 1 {
			writeError(w, http.StatusBadRequest, "ses habary bir faýldan ybarat bolmaly")
			return
		}
		body = ""
	} else {
		if body == "" && len(req.AttachmentIDs) == 0 {
			writeError(w, http.StatusBadRequest, "habar boş bolup bilmez")
			return
		}
		if len(body) > maxMessageLength {
			writeError(w, http.StatusBadRequest, "habar gaty uzyn")
			return
		}
	}
	if len(req.AttachmentIDs) > maxAttachmentsPerMessage {
		writeError(w, http.StatusBadRequest, "goşundylar gaty köp")
		return
	}
	if req.ReplyToID != nil {
		replyMsg, err := h.db.GetMessage(*req.ReplyToID)
		if err != nil || replyMsg == nil || replyMsg.ConversationID != convID {
			writeError(w, http.StatusBadRequest, "nädogry jogap")
			return
		}
	}

	msg, err := h.db.CreateMessage(convID, user.ID, body, kind, req.ReplyToID, req.AttachmentIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "habar iberip bolmady")
		return
	}

	// Written before the WS broadcast below (not after) — the broadcast is
	// an in-process channel send that can reach an already-open browser
	// socket faster than this HTTP response completes its round trip, so
	// sending the response first makes the sender's own POST resolve before
	// their WS echo in the common case. The client still has to dedup by
	// message id regardless (see chat.js), since ordering here is a latency
	// improvement, not a guarantee.
	writeJSON(w, http.StatusCreated, msg)

	participants, err := h.db.ListParticipants(convID)
	if err == nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "message.new", "conversation_id": convID, "message": msg,
		})
	}
	// Web push reaches participants whose app is fully closed (no live socket)
	// — fire-and-forget so it never delays the response or fails the send.
	go h.pushChatMessage(convID, user.ID, msg)
}

// EditMessage updates a text message's body — sender-only, text-kind-only,
// and only while the message hasn't been (delete-for-everyone) deleted.
func (h *Handler) EditMessage(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	msgID, err := strconv.Atoi(r.PathValue("messageId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry habar ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	msg, err := h.db.GetMessage(msgID)
	if err != nil || msg == nil || msg.ConversationID != convID {
		writeError(w, http.StatusNotFound, "habar tapylmady")
		return
	}
	if msg.SenderID == nil || *msg.SenderID != user.ID {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}
	if msg.DeletedAt != nil {
		writeError(w, http.StatusBadRequest, "pozulan habary üýtgedip bolmaz")
		return
	}
	if msg.Kind != "text" {
		writeError(w, http.StatusBadRequest, "diňe tekst habaryny üýtgedip bolar")
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "habar boş bolup bilmez")
		return
	}
	if len(body) > maxMessageLength {
		writeError(w, http.StatusBadRequest, "habar gaty uzyn")
		return
	}

	updated, err := h.db.EditMessage(msgID, body)
	if err != nil || updated == nil {
		writeError(w, http.StatusInternalServerError, "üýtgedip bolmady")
		return
	}
	writeJSON(w, http.StatusOK, updated)

	participants, err := h.db.ListParticipants(convID)
	if err == nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "message.edited", "conversation_id": convID, "message": updated,
		})
	}
}

// ForwardMessage copies a message (text/kind/attachments) into one or more
// conversations the requester is a participant of — the requester must also
// be a participant of the source conversation, but the recipients of the
// forward don't need to be (that's the whole point of forwarding).
func (h *Handler) ForwardMessage(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	msgID, err := strconv.Atoi(r.PathValue("messageId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry habar ID")
		return
	}
	msg, err := h.db.GetMessage(msgID)
	if err != nil || msg == nil || msg.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "habar tapylmady")
		return
	}
	if ok, err := h.db.IsParticipant(msg.ConversationID, user.ID); err != nil || !ok {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	var req struct {
		ConversationIDs []int  `json:"conversation_ids"`
		Caption         string `json:"caption"`
	}
	if err := readJSON(r, &req); err != nil || len(req.ConversationIDs) == 0 {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if len(req.ConversationIDs) > 20 {
		writeError(w, http.StatusBadRequest, "gaty köp sözleşme saýlandy")
		return
	}
	caption := strings.TrimSpace(req.Caption)
	if len(caption) > maxMessageLength {
		writeError(w, http.StatusBadRequest, "teswir gaty uzyn")
		return
	}
	for _, cid := range req.ConversationIDs {
		if ok, err := h.db.IsParticipant(cid, user.ID); err != nil || !ok {
			writeError(w, http.StatusForbidden, "rugsat ýok")
			return
		}
	}

	userKey := strconv.Itoa(user.ID)
	if h.messageLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.messageLimiter.record(userKey)

	forwarded, err := h.db.ForwardMessage(msgID, user.ID, req.ConversationIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ugradyp bolmady")
		return
	}
	writeJSON(w, http.StatusOK, forwarded)

	for i, cid := range req.ConversationIDs {
		if i >= len(forwarded) {
			break
		}
		participants, err := h.db.ListParticipants(cid)
		if err == nil {
			h.chatHub.broadcast(participantIDs(participants), map[string]any{
				"type": "message.new", "conversation_id": cid, "message": forwarded[i],
			})
		}
		go h.pushChatMessage(cid, user.ID, &forwarded[i])

		// An optional caption rides along as its own plain-text follow-up
		// message in the same target conversation, right after the forwarded
		// copy — same two-message shape a caption produces in Telegram, and
		// it reuses CreateMessage/broadcast/push exactly as a normal send
		// would rather than needing a caption column on messages.
		if caption != "" {
			if capMsg, err := h.db.CreateMessage(cid, user.ID, caption, "text", nil, nil); err == nil {
				if participants, err := h.db.ListParticipants(cid); err == nil {
					h.chatHub.broadcast(participantIDs(participants), map[string]any{
						"type": "message.new", "conversation_id": cid, "message": capMsg,
					})
				}
				go h.pushChatMessage(cid, user.ID, capMsg)
			}
		}
	}
}

// DeleteMessage supports two modes via ?for=me|everyone (default
// "everyone", matching the original behavior before delete-for-me existed):
//   - for=me: hides the message for the requester only, any participant,
//     no broadcast (purely local, but persisted so it stays hidden across
//     that user's other devices/tabs too).
//   - for=everyone: the original sender-only soft delete — no admin
//     override, consistent with chats being fully private: an admin who
//     can't see a conversation's content has no way to legitimately decide
//     it needs moderating.
func (h *Handler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	msgID, err := strconv.Atoi(r.PathValue("messageId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry habar ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	msg, err := h.db.GetMessage(msgID)
	if err != nil || msg == nil || msg.ConversationID != convID {
		writeError(w, http.StatusNotFound, "habar tapylmady")
		return
	}

	forWhom := r.URL.Query().Get("for")
	if forWhom == "" {
		forWhom = "everyone"
	}
	if forWhom == "me" {
		if err := h.db.HideMessageForUser(msgID, user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if forWhom != "everyone" {
		writeError(w, http.StatusBadRequest, "nädogry parametr")
		return
	}

	if msg.SenderID == nil || *msg.SenderID != user.ID {
		writeError(w, http.StatusForbidden, "rugsat ýok")
		return
	}

	// Soft-delete the message FIRST — this is the fast, reliable, purely-DB
	// state change that must actually happen. Attachment cleanup below talks
	// to MinIO and can fail transiently; if it did and this ran afterward
	// instead, a mid-loop failure would abort the request with the message
	// still not marked deleted at all, while one or more of its attachments
	// were already gone — a worse, silently-inconsistent state than a
	// best-effort cleanup that just leaves one orphaned object to notice.
	if err := h.db.SoftDeleteMessage(msgID); err != nil {
		writeError(w, http.StatusInternalServerError, "pozup bolmady")
		return
	}

	attachments, err := h.db.ListMessageAttachments(msgID)
	if err == nil {
		for _, a := range attachments {
			// Delete this row FIRST, then only remove the underlying MinIO
			// object once nothing else references the same minio_key —
			// forwarding a message copies its attachment rows onto the SAME
			// object rather than re-uploading, so removing one message's
			// copy must never take the object out from under another
			// message (in another conversation) that still points at it.
			if err := h.db.DeleteChatAttachment(a.ID); err != nil {
				log.Printf("delete chat attachment row %d: %v", a.ID, err)
				continue
			}
			remaining, err := h.db.CountAttachmentsByMinioKey(a.MinioKey)
			if err != nil {
				log.Printf("count chat attachment refs %s: %v", a.MinioKey, err)
				continue
			}
			if remaining == 0 {
				if err := h.minio.Delete(r.Context(), storage.ChatAttachmentsBucket, a.MinioKey); err != nil {
					log.Printf("delete chat attachment object %s: %v", a.MinioKey, err)
				}
			}
		}
	}

	participants, err := h.db.ListParticipants(convID)
	if err == nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "message.deleted", "conversation_id": convID, "message_id": msgID,
		})
	}

	// If the message being deleted was the conversation's pinned one, unpin
	// it too rather than leaving the pinned-message bar pointing at a
	// tombstone — cleared conditionally so this can't race a concurrent
	// re-pin to a different message that happened in between.
	if cleared, err := h.db.ClearPinnedMessageIfEquals(convID, msgID); err == nil && cleared && participants != nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "conversation.pinned", "conversation_id": convID, "message_id": nil,
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ToggleReaction adds or removes the caller's emoji reaction on a message.
// The emoji must be one of the allowlist (see reactionSet) — like stickers,
// reactions are rendered by the client, so free text is never accepted.
func (h *Handler) ToggleReaction(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	msgID, err := strconv.Atoi(r.PathValue("messageId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry habar ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}

	userKey := strconv.Itoa(user.ID)
	if h.messageLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.messageLimiter.record(userKey)

	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if !isValidReaction(req.Emoji) {
		writeError(w, http.StatusBadRequest, "nädogry reaksiýa")
		return
	}

	msg, err := h.db.GetMessage(msgID)
	if err != nil || msg == nil || msg.ConversationID != convID || msg.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "habar tapylmady")
		return
	}

	if _, err := h.db.ToggleReaction(msgID, user.ID, req.Emoji); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	groups, err := h.db.ListReactionGroups(msgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	if groups == nil {
		groups = []models.MessageReactionGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": msgID, "reactions": groups})

	participants, err := h.db.ListParticipants(convID)
	if err == nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "message.reaction", "conversation_id": convID, "message_id": msgID, "reactions": groups,
		})
	}
}

func (h *Handler) UploadChatAttachment(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}

	userKey := strconv.Itoa(user.ID)
	if h.chatAttachmentLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.chatAttachmentLimiter.record(userKey)

	if err := r.ParseMultipartForm(maxChatAttachmentSize); err != nil {
		writeError(w, http.StatusBadRequest, "faýl juda uly")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "faýl tapylmady")
		return
	}
	defer file.Close()
	if header.Size > maxChatAttachmentSize {
		writeError(w, http.StatusBadRequest, "faýl juda uly")
		return
	}

	if err := h.minio.EnsureBucket(r.Context(), storage.ChatAttachmentsBucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar döredip bolmady")
		return
	}

	ext := ""
	if i := strings.LastIndex(header.Filename, "."); i >= 0 {
		ext = header.Filename[i:]
	}
	key := fmt.Sprintf("%d/%s%s", convID, randomHexToken(16), ext)
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if err := h.minio.Upload(r.Context(), storage.ChatAttachmentsBucket, key, file, header.Size, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, "faýly ýükläp bolmady")
		return
	}

	attachment, err := h.db.CreateChatAttachment(convID, user.ID, key, header.Filename, header.Size, contentType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (h *Handler) DownloadChatAttachment(w http.ResponseWriter, r *http.Request) {
	attID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	att, err := h.db.GetChatAttachment(attID)
	if err != nil || att == nil {
		writeError(w, http.StatusNotFound, "tapylmady")
		return
	}
	if _, ok := requireParticipant(h, w, r, att.ConversationID); !ok {
		return
	}

	obj, err := h.minio.Download(r.Context(), storage.ChatAttachmentsBucket, att.MinioKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "faýly alyp bolmady")
		return
	}
	defer obj.Close()

	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, att.FileName))
	http.ServeContent(w, r, att.FileName, att.CreatedAt, obj)
}

func (h *Handler) MarkConversationRead(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	if err := h.db.MarkConversationRead(convID, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	// Tell every OTHER open tab in this conversation that this reader just
	// caught up, so they can flip that reader's message ticks from "sent" to
	// "read" live instead of waiting for a full reload.
	participants, err := h.db.ListParticipants(convID)
	if err == nil {
		others := make([]int, 0, len(participants))
		for _, p := range participants {
			if p.UserID != user.ID {
				others = append(others, p.UserID)
			}
		}
		h.chatHub.broadcast(others, map[string]any{
			"type": "conversation.read", "conversation_id": convID, "user_id": user.ID, "last_read_at": time.Now(),
		})
	}
}

func (h *Handler) ChatUnreadCount(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	count, err := h.db.TotalUnreadCount(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// UpdateConversationPrefs sets any subset of the caller's own mute/pin/
// archive preference for one conversation — never visible to, or settable
// for, any other participant. Broadcast afterward is scoped to the SAME
// user's own other connections only (multi-tab/device sync), never to the
// rest of the conversation.
func (h *Handler) UpdateConversationPrefs(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	var req struct {
		Muted    *bool `json:"muted"`
		Pinned   *bool `json:"pinned"`
		Archived *bool `json:"archived"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.Muted != nil {
		if err := h.db.SetConversationMuted(convID, user.ID, *req.Muted); err != nil {
			writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
			return
		}
	}
	if req.Pinned != nil {
		if err := h.db.SetConversationPinned(convID, user.ID, *req.Pinned); err != nil {
			writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
			return
		}
	}
	if req.Archived != nil {
		if err := h.db.SetConversationArchived(convID, user.ID, *req.Archived); err != nil {
			writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	h.chatHub.broadcast([]int{user.ID}, map[string]any{
		"type": "conversation.prefs", "conversation_id": convID,
		"muted": req.Muted, "pinned": req.Pinned, "archived": req.Archived,
	})
}

// ListMutedConversations backs the client-side mute cache (App._mutedConvIds)
// that gates in-app toast/sound/native notification for a live "message.new"
// — fetched once at login, not on every message.
func (h *Handler) ListMutedConversations(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	ids, err := h.db.ListMutedConversationIDs(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	if ids == nil {
		ids = []int{}
	}
	writeJSON(w, http.StatusOK, ids)
}

// PinMessage sets a conversation's single pinned message. Either participant
// may pin in a direct conversation; in a group, owner or admin only
// (requireManager) — mirrors who may rename/manage the group.
func (h *Handler) PinMessage(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	msgID, err := strconv.Atoi(r.PathValue("messageId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry habar ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "tapylmady")
		return
	}
	if conv.Type == "group" {
		if _, ok := requireManager(h, w, convID, user.ID); !ok {
			return
		}
	}
	msg, err := h.db.GetMessage(msgID)
	if err != nil || msg == nil || msg.ConversationID != convID || msg.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "habar tapylmady")
		return
	}
	if err := h.db.SetPinnedMessage(convID, msgID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	if participants, err := h.db.ListParticipants(convID); err == nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "conversation.pinned", "conversation_id": convID, "message_id": msgID,
		})
	}
}

// UnpinMessage clears whatever is currently pinned — same permission rule as
// PinMessage.
func (h *Handler) UnpinMessage(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil || conv == nil {
		writeError(w, http.StatusNotFound, "tapylmady")
		return
	}
	if conv.Type == "group" {
		if _, ok := requireManager(h, w, convID, user.ID); !ok {
			return
		}
	}
	if err := h.db.ClearPinnedMessage(convID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	if participants, err := h.db.ListParticipants(convID); err == nil {
		h.chatHub.broadcast(participantIDs(participants), map[string]any{
			"type": "conversation.pinned", "conversation_id": convID, "message_id": nil,
		})
	}
}

// maxGroupAvatarSize mirrors UploadAvatar's personal-avatar cap (auth.go).
const maxGroupAvatarSize = 5 << 20

// UploadGroupAvatar sets a group's photo — owner or admin only.
func (h *Handler) UploadGroupAvatar(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	user, ok := requireParticipant(h, w, r, convID)
	if !ok {
		return
	}
	if _, ok := requireManager(h, w, convID, user.ID); !ok {
		return
	}
	userKey := strconv.Itoa(user.ID)
	if h.avatarLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.avatarLimiter.record(userKey)
	if err := r.ParseMultipartForm(maxGroupAvatarSize); err != nil {
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
	if err := h.minio.EnsureBucket(r.Context(), storage.ChatAttachmentsBucket); err != nil {
		writeError(w, http.StatusInternalServerError, "ammar ýalňyşlygy")
		return
	}
	key := fmt.Sprintf("avatars/%d/%d%s", convID, time.Now().Unix(), extFromMime(ct))
	if err := h.minio.Upload(r.Context(), storage.ChatAttachmentsBucket, key, file, header.Size, ct); err != nil {
		writeError(w, http.StatusInternalServerError, "ýükläp bolmady")
		return
	}
	if err := h.db.SetConversationAvatar(convID, key); err != nil {
		writeError(w, http.StatusInternalServerError, "ýatda saklap bolmady")
		return
	}
	notifyConversationUpdated(h, convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ServeGroupAvatar streams a group's photo — gated the same as any other
// chat content: the requester must be a participant.
func (h *Handler) ServeGroupAvatar(w http.ResponseWriter, r *http.Request) {
	convID, err := conversationIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	if _, ok := requireParticipant(h, w, r, convID); !ok {
		return
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil || conv == nil || conv.AvatarURL == "" {
		writeError(w, http.StatusNotFound, "surat tapylmady")
		return
	}
	obj, err := h.minio.Download(r.Context(), storage.ChatAttachmentsBucket, conv.AvatarURL)
	if err != nil {
		writeError(w, http.StatusNotFound, "surat tapylmady")
		return
	}
	defer obj.Close()
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Type", mimeFromExt(filepath.Ext(conv.AvatarURL)))
	io.Copy(w, obj)
}

// BlockUser stops the target from being able to message the caller directly
// (see IsEitherBlocked, checked by SendMessage) and hides them from the
// caller's own "new DM" search — existing message history is untouched.
func (h *Handler) BlockUser(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	targetID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || targetID == user.ID {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}
	if err := h.db.BlockUser(user.ID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) UnblockUser(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	targetID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy ID")
		return
	}
	if err := h.db.UnblockUser(user.ID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListBlockedUsers backs the "Blocked users" list in the profile modal.
func (h *Handler) ListBlockedUsers(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	list, err := h.db.ListBlockedUsers(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	if list == nil {
		list = []models.UserSearchResult{}
	}
	writeJSON(w, http.StatusOK, list)
}

// UpdateNotificationPrefs is not conversation-scoped — it's a per-user
// setting, same privacy-preference surface as theme/password (see
// App.showProfileModal on the frontend).
func (h *Handler) UpdateNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	var req struct {
		Level string `json:"level"`
		Sound bool   `json:"sound"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	if req.Level != "full" && req.Level != "sender_only" && req.Level != "hidden" {
		writeError(w, http.StatusBadRequest, "nädogry derejesi")
		return
	}
	if err := h.db.UpdateChatNotifyPrefs(user.ID, req.Level, req.Sound); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	updated, err := h.db.GetUserByID(user.ID)
	if err != nil || updated == nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// randomHexToken mirrors internal/db's unexported generateToken — package
// api has no reason to import db just for this, and it's a few lines.
func randomHexToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// notifyConversationUpdated tells every current participant to refetch the
// conversation detail — used for rename/add/remove rather than trying to
// keep a second wire representation of membership in sync over the socket.
func notifyConversationUpdated(h *Handler, convID int) {
	participants, err := h.db.ListParticipants(convID)
	if err != nil {
		return
	}
	h.chatHub.broadcast(participantIDs(participants), map[string]any{"type": "conversation.updated", "conversation_id": convID})
}

// onUserOnline fires when a user opens their first live socket — tell everyone
// they share ANY conversation (direct or group) with so a DM header, or a
// fellow group member's row, can flip to "online".
func (h *Handler) onUserOnline(userID int) {
	contacts, err := h.db.ListConversationContactIDs(userID)
	if err != nil || len(contacts) == 0 {
		return
	}
	h.chatHub.broadcast(contacts, map[string]any{"type": "presence", "user_id": userID, "online": true})
}

// onUserOffline fires when a user's last socket drops — persist their
// last-seen time and tell everyone sharing a conversation with them that
// they're now offline.
func (h *Handler) onUserOffline(userID int) {
	if err := h.db.UpdateLastSeen(userID); err != nil {
		log.Printf("update last_seen for %d: %v", userID, err)
	}
	contacts, err := h.db.ListConversationContactIDs(userID)
	if err != nil || len(contacts) == 0 {
		return
	}
	h.chatHub.broadcast(contacts, map[string]any{"type": "presence", "user_id": userID, "online": false, "last_seen_at": time.Now()})
}

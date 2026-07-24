package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"paylash/internal/storage"
	"strconv"
	"strings"
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

func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	user := authutil.GetUser(r)
	list, err := h.db.ListConversationsForUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sözleşmeleri alyp bolmady")
		return
	}
	if list == nil {
		list = []models.ConversationView{}
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
	writeJSON(w, http.StatusOK, map[string]any{"conversation": conv, "participants": participants})
}

// requireCreator loads the conversation and checks the requester created it
// — used by rename/add-participants/remove-someone-else. Only the creator
// may manage group membership; there is no admin override here either, same
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
	if _, ok := requireCreator(h, w, convID, user.ID); !ok {
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
	if _, ok := requireCreator(h, w, convID, user.ID); !ok {
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

// RemoveParticipant allows two things: the creator removing anyone, or any
// participant removing themselves (leaving) — a participant leaving never
// needs the creator's permission.
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
		if _, ok := requireCreator(h, w, convID, user.ID); !ok {
			return
		}
	}
	if err := h.db.RemoveParticipant(convID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "aýryp bolmady")
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
	if _, ok := requireParticipant(h, w, r, convID); !ok {
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
	list, err := h.db.ListMessages(convID, beforeID, limit)
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

	userKey := strconv.Itoa(user.ID)
	if h.messageLimiter.blocked(userKey) {
		writeError(w, http.StatusTooManyRequests, "köp synanyşyk boldy, birazdan gaýtadan synanyşyň")
		return
	}
	h.messageLimiter.record(userKey)

	var req struct {
		Body          string `json:"body"`
		AttachmentIDs []int  `json:"attachment_ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" && len(req.AttachmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "habar boş bolup bilmez")
		return
	}
	if len(body) > maxMessageLength {
		writeError(w, http.StatusBadRequest, "habar gaty uzyn")
		return
	}
	if len(req.AttachmentIDs) > maxAttachmentsPerMessage {
		writeError(w, http.StatusBadRequest, "goşundylar gaty köp")
		return
	}

	msg, err := h.db.CreateMessage(convID, user.ID, body, req.AttachmentIDs)
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
		ids := make([]int, 0, len(participants))
		for _, p := range participants {
			ids = append(ids, p.UserID)
		}
		h.chatHub.broadcast(ids, map[string]any{
			"type": "message.new", "conversation_id": convID, "message": msg,
		})
	}
}

// DeleteMessage is author-only — no admin override, consistent with chats
// being fully private: an admin who can't see a conversation's content has
// no way to legitimately decide it needs moderating.
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
			if err := h.minio.Delete(r.Context(), storage.ChatAttachmentsBucket, a.MinioKey); err != nil {
				log.Printf("delete chat attachment object %s: %v", a.MinioKey, err)
				continue
			}
			if err := h.db.DeleteChatAttachment(a.ID); err != nil {
				log.Printf("delete chat attachment row %d: %v", a.ID, err)
			}
		}
	}

	participants, err := h.db.ListParticipants(convID)
	if err == nil {
		ids := make([]int, 0, len(participants))
		for _, p := range participants {
			ids = append(ids, p.UserID)
		}
		h.chatHub.broadcast(ids, map[string]any{
			"type": "message.deleted", "conversation_id": convID, "message_id": msgID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	ids := make([]int, 0, len(participants))
	for _, p := range participants {
		ids = append(ids, p.UserID)
	}
	h.chatHub.broadcast(ids, map[string]any{"type": "conversation.updated", "conversation_id": convID})
}

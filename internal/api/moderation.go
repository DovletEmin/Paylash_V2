package api

import (
	"net/http"
	"paylash/internal/authutil"
	"paylash/internal/models"
	"strconv"
	"strings"
)

// maxReportReasonLength bounds the free-text reason a reporter can attach —
// generous for a real explanation, a brake on a scripted client.
const maxReportReasonLength = 1000

// ReportMessage lets any participant flag one message for admin review —
// the only path that ever lets a system admin see chat content (see
// requireParticipant's own comment on why chats otherwise have zero
// standing admin access). The message's current body is snapshotted into
// the report so a later edit/delete by its sender can't erase what was
// actually reported.
func (h *Handler) ReportMessage(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > maxReportReasonLength {
		reason = reason[:maxReportReasonLength]
	}
	var reportedUserID *int
	if msg.SenderID != nil {
		id := *msg.SenderID
		reportedUserID = &id
	}
	if _, err := h.db.CreateChatReport(user.ID, convID, &msgID, reportedUserID, reason, msg.Body); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	h.logAction(r, "chat.report_message", "message", msgID, "", map[string]any{"conversation_id": convID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// ReportUser lets any participant flag a fellow participant for admin
// review without pointing at one specific message (e.g. a pattern of
// behavior across several messages, or a group-photo/name issue).
func (h *Handler) ReportUser(w http.ResponseWriter, r *http.Request) {
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
	if targetID == user.ID {
		writeError(w, http.StatusBadRequest, "özüňizi habar berip bolmaýar")
		return
	}
	if isMember, err := h.db.IsParticipant(convID, targetID); err != nil || !isMember {
		writeError(w, http.StatusBadRequest, "nädogry ulanyjy")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "nädogry maglumat")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > maxReportReasonLength {
		reason = reason[:maxReportReasonLength]
	}
	if _, err := h.db.CreateChatReport(user.ID, convID, nil, &targetID, reason, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	h.logAction(r, "chat.report_user", "user", targetID, "", map[string]any{"conversation_id": convID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// AdminListChatReports is the ONLY window a system admin has into chat
// content — every row is a reporter-initiated, timestamped snapshot, never
// a live view of a conversation. ?status= filters ("open" default; "all"
// for every status).
func (h *Handler) AdminListChatReports(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if status == "all" {
		status = ""
	}
	reports, err := h.db.ListChatReports(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "maglumat alyp bolmady")
		return
	}
	if reports == nil {
		reports = []models.ChatReportView{}
	}
	writeJSON(w, http.StatusOK, reports)
}

// AdminResolveChatReport closes out a report as either "resolved" (action
// was taken, or the report was valid and noted) or "dismissed" (no action
// warranted) — either way it stops counting toward the open-reports badge.
// This never gives the admin any further access to the source conversation;
// closing a report is the end of this feature's reach into chat, not a
// stepping stone to browsing it.
func (h *Handler) AdminResolveChatReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "nädogry ID")
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := readJSON(r, &req); err != nil || (req.Action != "resolved" && req.Action != "dismissed") {
		writeError(w, http.StatusBadRequest, "nädogry hereket")
		return
	}
	actor := authutil.GetUser(r)
	if err := h.db.ResolveChatReport(id, actor.ID, req.Action); err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	h.logAction(r, "chat.report_"+req.Action, "chat_report", id, "", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AdminOpenChatReportCount backs the admin sidebar's report badge.
func (h *Handler) AdminOpenChatReportCount(w http.ResponseWriter, r *http.Request) {
	n, err := h.db.CountOpenChatReports()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ýalňyşlyk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

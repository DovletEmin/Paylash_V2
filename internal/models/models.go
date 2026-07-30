package models

import (
	"encoding/json"
	"time"
)

type User struct {
	ID                 int       `json:"id"`
	Username           string    `json:"username"`
	PasswordHash       string    `json:"-"`
	DisplayName        string    `json:"full_name"`
	Role               string    `json:"role"`
	QuotaBytes         int64     `json:"quota_bytes"`
	AvatarURL          string    `json:"avatar_url"`
	MustChangePassword bool      `json:"must_change_password"`
	// ChatNotifyLevel controls how much a chat notification reveals:
	// "full" (sender + text), "sender_only" (sender, generic text), or
	// "hidden" (generic title + text) — mirrors Telegram's notification
	// privacy options.
	ChatNotifyLevel string    `json:"chat_notify_level"`
	ChatNotifySound bool      `json:"chat_notify_sound"`
	// OnboardingCompleted gates the first-login welcome tour — false only
	// for accounts CreateUser inserted after the tour shipped; every
	// pre-existing account was backfilled TRUE by the migration itself.
	OnboardingCompleted bool      `json:"onboarding_completed"`
	CreatedAt           time.Time `json:"created_at"`
}

// Project is an admin-created folder with an explicit member list (ACL).
type Project struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	QuotaBytes  int64     `json:"quota_bytes"`
	MinioBucket string    `json:"minio_bucket"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectMember grants a user access to a project with a given permission.
type ProjectMember struct {
	ID         int       `json:"id"`
	ProjectID  int       `json:"project_id"`
	UserID     int       `json:"user_id"`
	Permission string    `json:"permission"` // 'view' | 'edit'
	CreatedAt  time.Time `json:"created_at"`
}

// ProjectMemberView is a project member joined with user display info.
type ProjectMemberView struct {
	ID          int       `json:"id"`
	ProjectID   int       `json:"project_id"`
	UserID      int       `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"full_name"`
	AvatarURL   string    `json:"avatar_url"`
	Permission  string    `json:"permission"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectView is a project joined with the current user's permission on it.
type ProjectView struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Permission string `json:"permission"`
}

type Folder struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	ParentID  *int       `json:"parent_id"`
	OwnerID   int        `json:"owner_id"`
	ProjectID *int       `json:"project_id"`
	Scope     string     `json:"scope"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type File struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	MimeType    string     `json:"mime_type"`
	SizeBytes   int64      `json:"size_bytes"`
	MinioBucket string     `json:"minio_bucket"`
	MinioKey    string     `json:"minio_key"`
	FolderID    *int       `json:"folder_id"`
	OwnerID     int        `json:"owner_id"`
	ProjectID   *int       `json:"project_id"`
	Scope       string     `json:"scope"`
	Visibility  string     `json:"visibility"`
	Version     int        `json:"version"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type FileShare struct {
	ID         int       `json:"id"`
	FileID     int       `json:"file_id"`
	SharedBy   int       `json:"shared_by"`
	SharedWith *int      `json:"shared_with"`
	Permission string    `json:"permission"`
	IsPublic   bool      `json:"is_public"`
	CreatedAt  time.Time `json:"created_at"`
}

type WOPIToken struct {
	ID         int       `json:"id"`
	Token      string    `json:"token"`
	FileID     int       `json:"file_id"`
	UserID     int       `json:"user_id"`
	Permission string    `json:"permission"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type ShareView struct {
	ID         int       `json:"id"`
	FileID     int       `json:"file_id"`
	SharedBy   int       `json:"shared_by"`
	SharedWith *int      `json:"shared_with"`
	Permission string    `json:"permission"`
	IsPublic   bool      `json:"is_public"`
	FullName   string    `json:"full_name"`
	Username   string    `json:"username"`
	AvatarURL  string    `json:"avatar_url"`
	CreatedAt  time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    int       `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	// ImpersonatorID is set only on a session created by an admin's "log in
	// as" action (see internal/api/admin.go's Impersonate/ExitImpersonation)
	// — it names the ADMIN, while UserID above is the impersonated target.
	// nil for every ordinary session.
	ImpersonatorID *int `json:"-"`
}

// API request/response types

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

// DefaultQuotaBytes is the personal storage allowance a new account gets
// when nothing more specific was requested (self-registration, or an admin
// leaving the quota field blank) — 1 GiB.
const DefaultQuotaBytes int64 = 1 << 30

// DefaultSessionTTL is how long a normal login session lasts before it must
// be renewed if PAYLASH_SESSION_TTL_HOURS isn't set (see config.Load).
const DefaultSessionTTL = 7 * 24 * time.Hour

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateFolderRequest struct {
	Name      string `json:"name"`
	ParentID  *int   `json:"parent_id"`
	Scope     string `json:"scope"`
	ProjectID *int   `json:"project_id"`
}

type RenameRequest struct {
	Name string `json:"name"`
}

type CreateBlankFileRequest struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // docx, xlsx
	Scope     string `json:"scope"`
	FolderID  *int   `json:"folder_id"`
	ProjectID *int   `json:"project_id"`
}

type VisibilityRequest struct {
	Visibility string `json:"visibility"`
}

type ShareRequest struct {
	UserID     *int   `json:"user_id"`
	Permission string `json:"permission"`
	IsPublic   bool   `json:"is_public"`
}

type FileListResponse struct {
	Files       []File        `json:"files"`
	Folders     []Folder      `json:"folders"`
	Breadcrumbs []FolderCrumb `json:"breadcrumbs,omitempty"`
}

// FolderCrumb is the minimal id+name pair the breadcrumb trail needs for
// each ancestor of the currently-open folder, root-most first.
type FolderCrumb struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type StorageUsage struct {
	UsedBytes  int64 `json:"used_bytes"`
	QuotaBytes int64 `json:"quota_bytes"`
}

// StorageTrendPoint is one day's CUMULATIVE totals (not a daily delta) —
// ready to plot directly as a running-total chart on the admin dashboard.
type StorageTrendPoint struct {
	Date       string `json:"date"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
}

type AdminDashboard struct {
	TotalUsers    int   `json:"total_users"`
	TotalProjects int   `json:"total_projects"`
	TotalFiles    int   `json:"total_files"`
	TotalBytes    int64 `json:"total_bytes"`
}

type SharedFileView struct {
	File
	SharedByID     int       `json:"shared_by_id"`
	SharedByName   string    `json:"shared_by_name"`
	SharedByAvatar string    `json:"shared_by_avatar"`
	Permission     string    `json:"permission"`
	SharedAt       time.Time `json:"shared_at"`
}

// FileComment is a review note on a file, optionally pinned to a point on an
// image/drawing (XPct/YPct, both 0–100, nil for a plain unpinned comment) —
// backs the comments panel on the media preview page.
type FileComment struct {
	ID         int       `json:"id"`
	FileID     int       `json:"file_id"`
	UserID     int       `json:"user_id"`
	UserName   string    `json:"user_name"`
	UserAvatar string    `json:"user_avatar"`
	Body       string    `json:"body"`
	XPct       *float64  `json:"x_pct,omitempty"`
	YPct       *float64  `json:"y_pct,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type SharedByMeView struct {
	File
	SharedWithID     int       `json:"shared_with_id"`
	SharedWithName   string    `json:"shared_with_name"`
	SharedWithAvatar string    `json:"shared_with_avatar"`
	Permission       string    `json:"permission"`
	SharedAt         time.Time `json:"shared_at"`
}

// UploadSession tracks an in-progress resumable/chunked large-file upload —
// the browser talks to MinIO directly for the actual bytes (see
// internal/api/uploads.go), this row is just enough state to resume after a
// reload and to finalize/abort the underlying MinIO multipart upload.
type UploadSession struct {
	ID            string    `json:"id"`
	MinioUploadID string    `json:"-"`
	Bucket        string    `json:"-"`
	ObjectKey     string    `json:"-"`
	OwnerID       int       `json:"owner_id"`
	Scope         string    `json:"scope"`
	ProjectID     *int      `json:"project_id"`
	FolderID      *int      `json:"folder_id"`
	FileName      string    `json:"file_name"`
	MimeType      string    `json:"mime_type"`
	TotalSize     int64     `json:"total_size"`
	PartSize      int64     `json:"part_size"`
	PartCount     int       `json:"part_count"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// UploadSessionView is an in-progress upload session joined with its
// owner's name — the admin panel's view into large uploads under way.
type UploadSessionView struct {
	ID               string    `json:"id"`
	FileName         string    `json:"file_name"`
	TotalSize        int64     `json:"total_size"`
	PartCount        int       `json:"part_count"`
	Scope            string    `json:"scope"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	OwnerID          int       `json:"owner_id"`
	OwnerUsername    string    `json:"owner_username"`
	OwnerDisplayName string    `json:"owner_display_name"`
}

type AuditLogEntry struct {
	ID         int             `json:"id"`
	ActorID    *int            `json:"actor_id"`
	ActorName  string          `json:"actor_name"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   *int            `json:"target_id"`
	TargetName string          `json:"target_name"`
	Details    json.RawMessage `json:"details,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type UserSearchResult struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"full_name"`
	AvatarURL   string `json:"avatar_url"`
}

// PushSubscription is a stored browser Web Push subscription.
type PushSubscription struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Endpoint  string    `json:"endpoint"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	CreatedAt time.Time `json:"created_at"`
}

// Conversation is a direct (1-on-1) or group chat. ProjectID is a
// prefill-only convenience (pre-populates the participant picker at
// creation time) — it has no ongoing effect on membership or access.
type Conversation struct {
	ID             int       `json:"id"`
	Type           string    `json:"type"` // "direct" | "group"
	Name           *string   `json:"name,omitempty"`
	ProjectID      *int      `json:"project_id,omitempty"`
	CreatedBy      *int      `json:"created_by"`
	DirectUserLow  *int      `json:"-"`
	DirectUserHigh *int      `json:"-"`
	// AvatarURL is the object key inside storage.ChatAttachmentsBucket for a
	// group's photo ("" = none, direct conversations never have one) —
	// json:"-" like MessageAttachment.MinioKey, since it's only ever
	// resolved server-side by the avatar-serving endpoint, never sent raw.
	AvatarURL string `json:"-"`
	// HasAvatar mirrors AvatarURL != "" — computed by every query that
	// populates a Conversation, not scanned from a column — so the client
	// can decide whether to even attempt GET .../avatar without needing the
	// raw storage key. Without this every group with no photo (the common
	// case) fires a doomed request on each render, same problem AvatarURL's
	// own comment describes for why it's hidden in the first place.
	HasAvatar bool `json:"has_avatar"`
	// PinnedMessageID is the single pinned message for this conversation, if
	// any. Resolved to a full MessageView (PinnedMessage) by handlers that
	// return it to the client — this raw id is what's actually stored.
	PinnedMessageID *int      `json:"-"`
	LastMessageAt   time.Time `json:"last_message_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// ConversationView is a conversation as rendered in the conversation list —
// joined with enough to show a preview without a second round-trip.
type ConversationView struct {
	Conversation
	UnreadCount     int     `json:"unread_count"`
	LastMessageBody string  `json:"last_message_body,omitempty"`
	LastMessageAt   *time.Time `json:"last_message_preview_at,omitempty"`
	// OtherParticipant is set only for type="direct" — the one other person
	// in the DM, so the client never needs a second call to know who it's
	// showing.
	OtherParticipant *ParticipantView `json:"other_participant,omitempty"`
	// Muted/Pinned/Archived are the REQUESTER's own preferences for this
	// conversation (from their conversation_participants row) — never
	// another participant's. Muted already factors in MutedUntil (false once
	// it's in the past) so callers never need to compare the two themselves.
	Muted      bool       `json:"muted"`
	MutedUntil *time.Time `json:"muted_until,omitempty"`
	Pinned     bool       `json:"pinned"`
	Archived   bool       `json:"archived"`
}

type ParticipantView struct {
	UserID      int        `json:"user_id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"full_name"`
	AvatarURL   string     `json:"avatar_url"`
	LastReadAt  time.Time  `json:"last_read_at"`
	// Online is filled from the in-memory WS hub (never the DB) by the handler
	// that returns participants; LastSeenAt is the persisted timestamp of their
	// last disconnect, used to render "last seen X" when they're offline.
	Online     bool       `json:"online"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	// Role is "member" or "admin" — the conversation's owner (Conversation.
	// CreatedBy) is never "admin" here, ownership is tracked separately.
	Role string `json:"role"`
	// Muted is this row's own user's mute preference for the conversation —
	// json:"-" because a participant must never learn whether ANOTHER
	// participant has muted the chat, only pushChatMessage (server-side) and
	// the owning user's own client read it.
	Muted bool `json:"-"`
}

// Message is a chat message, optionally soft-deleted (DeletedAt set, Body
// blanked) — the row stays so already-open tabs can be told it was deleted
// instead of just having an id vanish from an in-memory list.
type Message struct {
	ID                int        `json:"id"`
	ConversationID    int        `json:"conversation_id"`
	SenderID          *int       `json:"sender_id"`
	Body              string     `json:"body"`
	Kind              string     `json:"kind"` // "text" | "sticker"
	EditedAt          *time.Time `json:"edited_at,omitempty"`
	ReplyToID         *int       `json:"reply_to_id,omitempty"`
	ForwardedFromName *string    `json:"forwarded_from_name,omitempty"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// MessageReplyPreview is the lightweight snapshot of a replied-to message
// shown atop the reply — captured at read time so a long deleted/edited
// original doesn't change what the reply preview shows.
type MessageReplyPreview struct {
	ID         int    `json:"id"`
	SenderName string `json:"sender_name"`
	Body       string `json:"body"`
	Kind       string `json:"kind"`
}

// MessageView is a message joined with its sender's display info and
// resolved attachments — what the client actually renders.
type MessageView struct {
	Message
	SenderName   string                `json:"sender_name"`
	SenderAvatar string                `json:"sender_avatar"`
	Attachments  []MessageAttachment   `json:"attachments,omitempty"`
	ReplyTo      *MessageReplyPreview  `json:"reply_to,omitempty"`
	// Status is computed, not stored — "sent" or "read", meaningful only for
	// the requester's own messages in direct conversations (group read
	// receipts are out of scope; group messages are always "sent").
	Status string `json:"status,omitempty"`
	// Reactions groups this message's emoji reactions by emoji, each with the
	// ids of the users who placed it. The client derives count (len) and
	// whether the viewer reacted (membership) itself — this keeps the payload
	// viewer-independent, so the same struct serves both list-load and the
	// message.reaction WS broadcast.
	Reactions []MessageReactionGroup `json:"reactions,omitempty"`
	// ReadUserIDs lists the OTHER participants who have read this message
	// (last_read_at >= created_at). Populated ONLY for the requester's own
	// messages (a group member never learns who read someone else's message)
	// and only on the per-viewer list fetch — deliberately never on a WS
	// broadcast, which every participant would receive. Backs group "read by
	// N of M" receipts; for DMs the plain Status field is used instead.
	ReadUserIDs []int `json:"read_user_ids,omitempty"`
	// LinkPreview is the unfurled preview of the first URL in this message's
	// body, if any was found and resolved yet. See LinkPreview.
	LinkPreview *LinkPreview `json:"link_preview,omitempty"`
}

// LinkPreview is unfurled Open Graph metadata for the first URL found in a
// text message, resolved asynchronously after send/edit (see
// internal/api/linkpreview.go) — absent from MessageView until then, or
// forever if the fetch found nothing worth showing. HasImage tells the
// client whether to point an <img> at the message's link-preview-image
// endpoint; the real image (if any) is never sent inline here.
type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	HasImage    bool   `json:"has_image"`
}

// MessageReactionGroup is one emoji and everyone who reacted with it on a
// given message.
type MessageReactionGroup struct {
	Emoji   string `json:"emoji"`
	UserIDs []int  `json:"user_ids"`
}

// MessageSearchResult is one hit from chat message search — enough for the
// client to show the match in context and jump to it. ConversationLabel is the
// group's name, or (for a direct chat) the other participant's display name.
type MessageSearchResult struct {
	MessageID         int       `json:"message_id"`
	ConversationID    int       `json:"conversation_id"`
	ConversationType  string    `json:"conversation_type"`
	ConversationLabel string    `json:"conversation_label"`
	SenderID          *int      `json:"sender_id"`
	SenderName        string    `json:"sender_name"`
	Body              string    `json:"body"`
	CreatedAt         time.Time `json:"created_at"`
}

// ChatReportView is a moderation report against a message and/or a user
// within a conversation, as an admin reviews it. This is the ONLY shape in
// which chat content ever reaches a system admin — MessageBodySnapshot is
// exactly (and only) what was reported, captured at report time, not a
// window into the rest of the conversation. See internal/api's
// AdminListChatReports and requireParticipant's own comment on why chats
// otherwise have zero standing admin access.
type ChatReportView struct {
	ID                  int        `json:"id"`
	ReporterID          *int       `json:"reporter_id"`
	ReporterName        string     `json:"reporter_name"`
	ConversationID      int        `json:"conversation_id"`
	ConversationType    string     `json:"conversation_type"`
	ConversationLabel   string     `json:"conversation_label,omitempty"`
	MessageID           *int       `json:"message_id,omitempty"`
	ReportedUserID      *int       `json:"reported_user_id,omitempty"`
	ReportedUserName    string     `json:"reported_user_name,omitempty"`
	Reason              string     `json:"reason"`
	MessageBodySnapshot string     `json:"message_body_snapshot,omitempty"`
	Status              string     `json:"status"`
	ResolvedByName      string     `json:"resolved_by_name,omitempty"`
	ResolvedAt          *time.Time `json:"resolved_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// MessageAttachment is a file attached to a chat message. MinioKey is never
// sent to the client (tighter than File.MinioKey, which is) — attachments
// are only ever fetched by id through an authenticated download endpoint
// that re-checks conversation membership.
type MessageAttachment struct {
	ID             int       `json:"id"`
	MessageID      *int      `json:"message_id,omitempty"`
	ConversationID int       `json:"conversation_id"`
	UploadedBy     int       `json:"uploaded_by"`
	MinioKey       string    `json:"-"`
	FileName       string    `json:"file_name"`
	SizeBytes      int64     `json:"size_bytes"`
	ContentType    string    `json:"content_type"`
	CreatedAt      time.Time `json:"created_at"`
}

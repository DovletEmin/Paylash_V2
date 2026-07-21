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
	CreatedAt          time.Time `json:"created_at"`
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
	CreatedAt  time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    int       `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// API request/response types

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

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

type AdminDashboard struct {
	TotalUsers    int   `json:"total_users"`
	TotalProjects int   `json:"total_projects"`
	TotalFiles    int   `json:"total_files"`
	TotalBytes    int64 `json:"total_bytes"`
}

type SharedFileView struct {
	File
	SharedByID   int       `json:"shared_by_id"`
	SharedByName string    `json:"shared_by_name"`
	Permission   string    `json:"permission"`
	SharedAt     time.Time `json:"shared_at"`
}

// FileComment is a review note on a file, optionally pinned to a point on an
// image/drawing (XPct/YPct, both 0–100, nil for a plain unpinned comment) —
// backs the comments panel on the media preview page.
type FileComment struct {
	ID        int       `json:"id"`
	FileID    int       `json:"file_id"`
	UserID    int       `json:"user_id"`
	UserName  string    `json:"user_name"`
	Body      string    `json:"body"`
	XPct      *float64  `json:"x_pct,omitempty"`
	YPct      *float64  `json:"y_pct,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type SharedByMeView struct {
	File
	SharedWithID   int       `json:"shared_with_id"`
	SharedWithName string    `json:"shared_with_name"`
	Permission     string    `json:"permission"`
	SharedAt       time.Time `json:"shared_at"`
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
}

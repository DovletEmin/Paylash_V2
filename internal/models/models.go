package models

import "time"

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"full_name"`
	Role         string    `json:"role"`
	QuotaBytes   int64     `json:"quota_bytes"`
	AvatarURL    string    `json:"avatar_url"`
	CreatedAt    time.Time `json:"created_at"`
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
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	ParentID  *int      `json:"parent_id"`
	OwnerID   int       `json:"owner_id"`
	ProjectID *int      `json:"project_id"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
}

type File struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	MimeType    string    `json:"mime_type"`
	SizeBytes   int64     `json:"size_bytes"`
	MinioBucket string    `json:"minio_bucket"`
	MinioKey    string    `json:"minio_key"`
	FolderID    *int      `json:"folder_id"`
	OwnerID     int       `json:"owner_id"`
	ProjectID   *int      `json:"project_id"`
	Scope       string    `json:"scope"`
	Visibility  string    `json:"visibility"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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
	Files   []File   `json:"files"`
	Folders []Folder `json:"folders"`
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
	SharedByName string    `json:"owner_name"`
	Permission   string    `json:"permission"`
	SharedAt     time.Time `json:"shared_at"`
}

type SharedByMeView struct {
	File
	SharedWithName string    `json:"shared_with_name"`
	Permission     string    `json:"permission"`
	SharedAt       time.Time `json:"shared_at"`
}

type UserSearchResult struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"full_name"`
}

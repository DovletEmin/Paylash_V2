package server

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"paylash/internal/api"
	"paylash/internal/config"
	"paylash/internal/db"
	"paylash/internal/storage"
	"paylash/internal/wopi"
)

type Server struct {
	cfg   *config.Config
	db    *db.DB
	minio *storage.MinioClient
	mux   *http.ServeMux
}

func New(cfg *config.Config, database *db.DB, minioClient *storage.MinioClient, webFS embed.FS) *Server {
	s := &Server{
		cfg:   cfg,
		db:    database,
		minio: minioClient,
		mux:   http.NewServeMux(),
	}
	s.routes(webFS)
	return s
}

func (s *Server) routes(webFS embed.FS) {
	auth := AuthMiddleware(s.db)
	h := api.NewHandler(s.db, s.minio, s.cfg)
	wopiH := wopi.NewHandler(s.db, s.minio, s.cfg)

	// Public routes
	s.mux.HandleFunc("GET /api/public/config", h.PublicConfig)
	s.mux.HandleFunc("POST /api/auth/register", h.Register)
	s.mux.HandleFunc("POST /api/auth/login", h.Login)
	s.mux.HandleFunc("POST /api/auth/logout", h.Logout)

	// Authenticated routes
	s.mux.Handle("GET /api/auth/me", auth(http.HandlerFunc(h.Me)))
	s.mux.Handle("PATCH /api/auth/profile", auth(http.HandlerFunc(h.UpdateProfile)))
	s.mux.Handle("POST /api/auth/avatar", auth(http.HandlerFunc(h.UploadAvatar)))
	s.mux.Handle("GET /api/avatar/{id}", auth(http.HandlerFunc(h.ServeAvatar)))

	// Files
	s.mux.Handle("GET /api/files", auth(http.HandlerFunc(h.ListFiles)))
	s.mux.Handle("POST /api/files/upload", auth(http.HandlerFunc(h.UploadFile)))
	s.mux.Handle("POST /api/files/create", auth(http.HandlerFunc(h.CreateBlankFile)))
	s.mux.Handle("GET /api/files/{id}/download", auth(http.HandlerFunc(h.DownloadFile)))
	s.mux.Handle("PATCH /api/files/{id}", auth(http.HandlerFunc(h.RenameFile)))
	s.mux.Handle("PATCH /api/files/{id}/move", auth(http.HandlerFunc(h.MoveFile)))
	s.mux.Handle("DELETE /api/files/{id}", auth(http.HandlerFunc(h.DeleteFile)))
	s.mux.Handle("GET /api/search", auth(http.HandlerFunc(h.SearchFiles)))
	s.mux.Handle("GET /api/storage/usage", auth(http.HandlerFunc(h.StorageUsage)))

	// File versions (MinIO bucket versioning)
	s.mux.Handle("GET /api/files/{id}/versions", auth(http.HandlerFunc(h.ListFileVersions)))
	s.mux.Handle("POST /api/files/{id}/versions/{versionId}/restore", auth(http.HandlerFunc(h.RestoreFileVersion)))
	s.mux.Handle("GET /api/files/{id}/versions/{versionId}/download", auth(http.HandlerFunc(h.DownloadFileVersion)))

	// Large-file resumable upload (presigned multipart direct-to-MinIO)
	s.mux.Handle("POST /api/uploads/init", auth(http.HandlerFunc(h.InitUpload)))
	s.mux.Handle("GET /api/uploads/{id}", auth(http.HandlerFunc(h.UploadStatus)))
	s.mux.Handle("GET /api/uploads/{id}/parts/{n}/url", auth(http.HandlerFunc(h.UploadPartURL)))
	s.mux.Handle("POST /api/uploads/{id}/complete", auth(http.HandlerFunc(h.CompleteUpload)))
	s.mux.Handle("DELETE /api/uploads/{id}", auth(http.HandlerFunc(h.AbortUpload)))

	// Folders
	s.mux.Handle("GET /api/folders/tree", auth(http.HandlerFunc(h.ListFolderTree)))
	s.mux.Handle("POST /api/folders", auth(http.HandlerFunc(h.CreateFolder)))
	s.mux.Handle("PATCH /api/folders/{id}", auth(http.HandlerFunc(h.RenameFolder)))
	s.mux.Handle("PATCH /api/folders/{id}/move", auth(http.HandlerFunc(h.MoveFolder)))
	s.mux.Handle("DELETE /api/folders/{id}", auth(http.HandlerFunc(h.DeleteFolder)))
	s.mux.Handle("GET /api/folders/{id}/download", auth(http.HandlerFunc(h.DownloadFolder)))

	// Sharing
	s.mux.Handle("POST /api/files/{id}/share", auth(http.HandlerFunc(h.ShareFile)))
	s.mux.Handle("PATCH /api/files/{id}/share/{userId}", auth(http.HandlerFunc(h.UpdateSharePermission)))
	s.mux.Handle("DELETE /api/files/{id}/share/{userId}", auth(http.HandlerFunc(h.DeleteShare)))
	s.mux.Handle("PATCH /api/files/{id}/share/public", auth(http.HandlerFunc(h.SetPublicShare)))
	s.mux.Handle("PATCH /api/files/{id}/visibility", auth(http.HandlerFunc(h.SetVisibility)))
	s.mux.Handle("GET /api/shared-with-me", auth(http.HandlerFunc(h.SharedWithMe)))
	s.mux.Handle("GET /api/shared-by-me", auth(http.HandlerFunc(h.SharedByMe)))
	s.mux.Handle("GET /api/files/{id}/shares", auth(http.HandlerFunc(h.GetSharesForFile)))
	s.mux.Handle("GET /api/users/search", auth(http.HandlerFunc(h.SearchUsers)))

	// Trash (soft-delete)
	s.mux.Handle("GET /api/trash", auth(http.HandlerFunc(h.ListTrash)))
	s.mux.Handle("DELETE /api/trash", auth(http.HandlerFunc(h.EmptyTrash)))
	s.mux.Handle("POST /api/trash/files/{id}/restore", auth(http.HandlerFunc(h.RestoreFile)))
	s.mux.Handle("DELETE /api/trash/files/{id}", auth(http.HandlerFunc(h.PurgeFile)))
	s.mux.Handle("POST /api/trash/folders/{id}/restore", auth(http.HandlerFunc(h.RestoreFolder)))
	s.mux.Handle("DELETE /api/trash/folders/{id}", auth(http.HandlerFunc(h.PurgeFolder)))

	// Projects the current employee can see (for the sidebar)
	s.mux.Handle("GET /api/projects", auth(http.HandlerFunc(h.ListMyProjects)))

	// Collabora
	s.mux.Handle("GET /api/collabora/editor-url", auth(http.HandlerFunc(h.CollaboraEditorURL)))

	// Admin routes
	s.mux.Handle("GET /api/admin/dashboard", auth(AdminMiddleware(http.HandlerFunc(h.AdminDashboard))))
	s.mux.Handle("GET /api/admin/audit-log", auth(AdminMiddleware(http.HandlerFunc(h.AdminAuditLog))))
	s.mux.Handle("GET /api/admin/projects", auth(AdminMiddleware(http.HandlerFunc(h.AdminListProjects))))
	s.mux.Handle("POST /api/admin/projects", auth(AdminMiddleware(http.HandlerFunc(h.AdminCreateProject))))
	s.mux.Handle("PATCH /api/admin/projects/{id}", auth(AdminMiddleware(http.HandlerFunc(h.AdminUpdateProject))))
	s.mux.Handle("DELETE /api/admin/projects/{id}", auth(AdminMiddleware(http.HandlerFunc(h.AdminDeleteProject))))
	s.mux.Handle("GET /api/admin/projects/{id}/members", auth(AdminMiddleware(http.HandlerFunc(h.AdminListProjectMembers))))
	s.mux.Handle("POST /api/admin/projects/{id}/members", auth(AdminMiddleware(http.HandlerFunc(h.AdminAddProjectMember))))
	s.mux.Handle("PATCH /api/admin/projects/{id}/members/{userId}", auth(AdminMiddleware(http.HandlerFunc(h.AdminUpdateProjectMember))))
	s.mux.Handle("DELETE /api/admin/projects/{id}/members/{userId}", auth(AdminMiddleware(http.HandlerFunc(h.AdminRemoveProjectMember))))
	s.mux.Handle("GET /api/admin/users", auth(AdminMiddleware(http.HandlerFunc(h.AdminListUsers))))
	s.mux.Handle("POST /api/admin/users", auth(AdminMiddleware(http.HandlerFunc(h.AdminCreateUser))))
	s.mux.Handle("PATCH /api/admin/users/{id}", auth(AdminMiddleware(http.HandlerFunc(h.AdminUpdateUser))))
	s.mux.Handle("DELETE /api/admin/users/{id}", auth(AdminMiddleware(http.HandlerFunc(h.AdminDeleteUser))))
	s.mux.Handle("DELETE /api/admin/users/all", auth(AdminMiddleware(http.HandlerFunc(h.AdminDeleteAllUsers))))
	s.mux.Handle("POST /api/admin/users/bulk-quota", auth(AdminMiddleware(http.HandlerFunc(h.AdminBulkUserQuota))))
	s.mux.Handle("POST /api/admin/projects/bulk-quota", auth(AdminMiddleware(http.HandlerFunc(h.AdminBulkProjectQuota))))
	s.mux.Handle("POST /api/admin/users/import", auth(AdminMiddleware(http.HandlerFunc(h.AdminImportUsers))))
	s.mux.Handle("GET /api/admin/public-quota", auth(AdminMiddleware(http.HandlerFunc(h.AdminGetPublicQuota))))
	s.mux.Handle("PATCH /api/admin/public-quota", auth(AdminMiddleware(http.HandlerFunc(h.AdminSetPublicQuota))))
	s.mux.Handle("GET /api/admin/uploads", auth(AdminMiddleware(http.HandlerFunc(h.AdminListUploads))))
	s.mux.Handle("DELETE /api/admin/uploads/{id}", auth(AdminMiddleware(http.HandlerFunc(h.AdminAbortUpload))))

	// WOPI endpoints (accessed by Collabora, token-based auth)
	s.mux.HandleFunc("GET /wopi/files/{id}", wopiH.CheckFileInfo)
	s.mux.HandleFunc("GET /wopi/files/{id}/contents", wopiH.GetFile)
	s.mux.HandleFunc("POST /wopi/files/{id}/contents", wopiH.PutFile)

	// Static frontend
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal("cannot load embedded web files:", err)
	}
	fileServer := http.FileServer(http.FS(webSub))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// SPA: serve index.html for non-file paths
		if r.URL.Path != "/" {
			// Check if file exists
			f, err := webSub.(fs.ReadFileFS).ReadFile(r.URL.Path[1:])
			if err != nil || f == nil {
				// Serve index.html for SPA routing
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	// The frontend and API are always served same-origin (embedded SPA +
	// API on this same binary, fronted by Caddy under one hostname) — no
	// cross-origin CORS headers are needed, so none are sent.
	handler := LoggingMiddleware(s.mux)
	log.Printf("Paylash server starting on http://localhost%s", addr)
	return http.ListenAndServe(addr, handler)
}

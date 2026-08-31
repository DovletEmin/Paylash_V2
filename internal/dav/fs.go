package dav

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"paylash/internal/db"
	"paylash/internal/models"
	"paylash/internal/storage"
)

// commonBucket is the company-wide space's bucket. Spelled as a literal in
// internal/api too; kept in one place here so the two cannot drift.
const commonBucket = "common-files"

// FS is one signed-in user's view of the storage, shaped as a filesystem.
// A fresh one is built per request from the credential presented, so it can
// hold the user and never has to re-derive who is asking.
type FS struct {
	db    *db.DB
	minio *storage.MinioClient
	user  *models.User
}

func NewFS(database *db.DB, minioClient *storage.MinioClient, user *models.User) *FS {
	return &FS{db: database, minio: minioClient, user: user}
}

var _ webdav.FileSystem = (*FS)(nil)

/* ── FileInfo ─────────────────────────────────────────────────────────── */

type fileInfo struct {
	name  string
	size  int64
	mtime time.Time
	dir   bool
}

func (f *fileInfo) Name() string { return f.name }
func (f *fileInfo) Size() int64  { return f.size }
func (f *fileInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (f *fileInfo) ModTime() time.Time { return f.mtime }
func (f *fileInfo) IsDir() bool        { return f.dir }
func (f *fileInfo) Sys() any           { return nil }

func (n *node) info() os.FileInfo {
	if n.kind == kindFile {
		return &fileInfo{name: n.file.Name, size: n.file.SizeBytes, mtime: n.file.UpdatedAt}
	}
	mtime := time.Now()
	if n.folder != nil {
		mtime = n.folder.CreatedAt
	}
	return &fileInfo{name: n.name, dir: true, mtime: mtime}
}

/* ── FileSystem ───────────────────────────────────────────────────────── */

func (f *FS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	n, err := f.resolve(splitPath(name))
	if err != nil {
		return nil, err
	}
	return n.info(), nil
}

func (f *FS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	parts := splitPath(name)
	if len(parts) == 0 {
		return os.ErrExist
	}
	parent, err := f.resolve(parts[:len(parts)-1])
	if err != nil {
		return err
	}
	// The fixed top of the tree is not a place new directories can appear:
	// a project comes into being in the admin panel, not by mkdir.
	if parent.kind == kindRoot || parent.kind == kindProjects {
		return errReadOnly
	}
	if !parent.writable {
		return errReadOnly
	}
	leaf := parts[len(parts)-1]
	if existing, err := f.findFolder(parent, leaf); err != nil {
		return err
	} else if existing != nil {
		return os.ErrExist
	}
	folder := &models.Folder{
		Name: leaf, ParentID: parent.folderID(), OwnerID: f.user.ID,
		ProjectID: parent.projectID(), Scope: parent.scope,
	}
	return f.db.CreateFolder(folder)
}

func (f *FS) RemoveAll(ctx context.Context, name string) error {
	parts := splitPath(name)
	if len(parts) == 0 {
		return errReadOnly
	}
	n, err := f.resolve(parts)
	if err != nil {
		return err
	}
	if !n.writable || n.kind == kindRoot || n.kind == kindScope || n.kind == kindProjects || n.kind == kindProject {
		return errReadOnly
	}
	// Soft delete, exactly as the browser's delete does — a file removed
	// from the drive lands in the same Trash and is recoverable for the
	// same 30 days. Deleting from a mapped drive is far too easy for it to
	// be the one destructive path in the app.
	if n.kind == kindFile {
		return f.db.SoftDeleteFile(n.file.ID)
	}
	// Same tree walk the API's folder delete uses, so everything nested
	// below lands in the trash together instead of being orphaned.
	ids, err := f.db.ListFolderAndDescendantIDs(n.folder.ID)
	if err != nil {
		return err
	}
	return f.db.SoftDeleteFolderTree(ids)
}

func (f *FS) Rename(ctx context.Context, oldName, newName string) error {
	oldParts, newParts := splitPath(oldName), splitPath(newName)
	if len(oldParts) == 0 || len(newParts) == 0 {
		return errReadOnly
	}
	src, err := f.resolve(oldParts)
	if err != nil {
		return err
	}
	if !src.writable || (src.kind != kindFile && src.kind != kindFolder) {
		return errReadOnly
	}
	dstParent, err := f.resolve(newParts[:len(newParts)-1])
	if err != nil {
		return err
	}
	if !dstParent.writable || dstParent.kind == kindRoot || dstParent.kind == kindProjects {
		return errReadOnly
	}
	// Moving between spaces would have to re-key the object into another
	// bucket and re-evaluate quotas against a different owner; the browser
	// does not offer it either, so a cross-space drag is refused rather
	// than half-performed.
	if dstParent.scope != src.scope || !samePtr(dstParent.projectID(), src.projectID()) {
		return errReadOnly
	}
	leaf := newParts[len(newParts)-1]
	if src.kind == kindFile {
		return f.db.MoveFile(src.file.ID, leaf, dstParent.folderID())
	}
	return f.db.MoveFolder(src.folder.ID, leaf, dstParent.folderID())
}

func samePtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (f *FS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	parts := splitPath(name)
	writing := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_APPEND|os.O_TRUNC) != 0

	n, err := f.resolve(parts)
	if err == nil {
		if n.kind == kindFile && writing {
			if !n.writable {
				return nil, errReadOnly
			}
			return f.newWriter(ctx, n.file, nil, "")
		}
		if writing && n.kind != kindFile {
			return nil, errReadOnly // cannot write over a directory
		}
		if n.kind == kindFile {
			return f.openRead(ctx, n)
		}
		return f.openDir(n)
	}
	if !os.IsNotExist(err) || !writing || flag&os.O_CREATE == 0 || len(parts) == 0 {
		return nil, err
	}

	// Creating a new file: resolve its parent and start an empty writer.
	parent, perr := f.resolve(parts[:len(parts)-1])
	if perr != nil {
		return nil, perr
	}
	if parent.kind == kindRoot || parent.kind == kindProjects || !parent.writable {
		return nil, errReadOnly
	}
	return f.newWriter(ctx, nil, parent, parts[len(parts)-1])
}

/* ── Directories ──────────────────────────────────────────────────────── */

// dirFile answers Readdir from a snapshot taken when it was opened. WebDAV
// reads a directory once per PROPFIND, so there is nothing to keep live.
type dirFile struct {
	info    os.FileInfo
	entries []os.FileInfo
	pos     int
}

func (d *dirFile) Close() error { return nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, fmt.Errorf("dav: %s is a directory", d.info.Name())
}
func (d *dirFile) Write([]byte) (int, error) { return 0, errReadOnly }
func (d *dirFile) Seek(int64, int) (int64, error) {
	return 0, fmt.Errorf("dav: %s is a directory", d.info.Name())
}
func (d *dirFile) Stat() (os.FileInfo, error) { return d.info, nil }

func (d *dirFile) Readdir(count int) ([]os.FileInfo, error) {
	if count <= 0 {
		rest := d.entries[d.pos:]
		d.pos = len(d.entries)
		return rest, nil
	}
	if d.pos >= len(d.entries) {
		return nil, io.EOF
	}
	end := d.pos + count
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

func (f *FS) openDir(n *node) (webdav.File, error) {
	var entries []os.FileInfo
	now := time.Now()

	switch n.kind {
	case kindRoot:
		entries = []os.FileInfo{
			&fileInfo{name: dirPersonal, dir: true, mtime: now},
			&fileInfo{name: dirCommon, dir: true, mtime: now},
			&fileInfo{name: dirProjects, dir: true, mtime: now},
		}
	case kindProjects:
		projects, err := f.db.ListProjectsForUser(f.user.ID, f.user.Role == "admin")
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			entries = append(entries, &fileInfo{name: p.Name, dir: true, mtime: now})
		}
	default:
		folders, err := f.listFolders(n)
		if err != nil {
			return nil, err
		}
		for _, fo := range folders {
			entries = append(entries, &fileInfo{name: fo.Name, dir: true, mtime: fo.CreatedAt})
		}
		files, err := f.listFiles(n)
		if err != nil {
			return nil, err
		}
		for _, fi := range files {
			entries = append(entries, &fileInfo{name: fi.Name, size: fi.SizeBytes, mtime: fi.UpdatedAt})
		}
	}
	return &dirFile{info: n.info(), entries: entries}, nil
}

/* ── Reading ──────────────────────────────────────────────────────────── */

// readFile streams an object straight out of MinIO. Download already hands
// back a ReadSeekCloser, which is exactly what WebDAV wants for range
// requests — so opening a 2GB drawing costs nothing until it is read.
type readFile struct {
	io.ReadSeekCloser
	info os.FileInfo
}

func (r *readFile) Write([]byte) (int, error) { return 0, errReadOnly }
func (r *readFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("dav: not a directory")
}
func (r *readFile) Stat() (os.FileInfo, error) { return r.info, nil }

func (f *FS) openRead(ctx context.Context, n *node) (webdav.File, error) {
	obj, err := f.minio.Download(ctx, n.file.MinioBucket, n.file.MinioKey)
	if err != nil {
		return nil, err
	}
	// minio-go defers the real request until the first Read, so a missing
	// object would otherwise surface as an empty file rather than an error.
	// Stat it first and fail honestly.
	if _, err := f.minio.GetObjectInfo(ctx, n.file.MinioBucket, n.file.MinioKey); err != nil {
		obj.Close()
		return nil, err
	}
	return &readFile{ReadSeekCloser: obj, info: n.info()}, nil
}

/* ── Writing ──────────────────────────────────────────────────────────── */

// writeFile buffers a save to a temporary file and commits it to MinIO on
// Close.
//
// This is not laziness: an object store has no random-access write, while
// WebDAV clients seek freely and only reveal the final size when they are
// done. Buffering is the only way to turn one into the other. It also means
// a save that is interrupted half way leaves the stored file untouched,
// rather than truncated — which for a drawing someone is working on is the
// difference between an inconvenience and a lost afternoon.
type writeFile struct {
	fs     *FS
	ctx    context.Context
	tmp    *os.File
	info   *fileInfo
	file   *models.File // existing file being overwritten, or nil
	parent *node        // where a new file goes
	name   string
	closed bool
}

func (f *FS) newWriter(ctx context.Context, existing *models.File, parent *node, name string) (webdav.File, error) {
	tmp, err := os.CreateTemp("", "paylash-dav-*")
	if err != nil {
		return nil, err
	}
	n := name
	if existing != nil {
		n = existing.Name
	}
	return &writeFile{
		fs: f, ctx: ctx, tmp: tmp, file: existing, parent: parent, name: n,
		info: &fileInfo{name: n, mtime: time.Now()},
	}, nil
}

func (w *writeFile) Write(p []byte) (int, error) {
	n, err := w.tmp.Write(p)
	w.info.size += int64(n)
	return n, err
}

func (w *writeFile) Read(p []byte) (int, error)                { return w.tmp.Read(p) }
func (w *writeFile) Seek(off int64, whence int) (int64, error) { return w.tmp.Seek(off, whence) }
func (w *writeFile) Readdir(int) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("dav: not a directory")
}
func (w *writeFile) Stat() (os.FileInfo, error) { return w.info, nil }

func (w *writeFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	defer func() {
		name := w.tmp.Name()
		w.tmp.Close()
		os.Remove(name)
	}()

	size, err := w.tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	if w.file != nil {
		return w.commitOverwrite(size)
	}
	return w.commitCreate(size)
}

// commitOverwrite replaces an existing file's bytes under the same key, so
// MinIO's bucket versioning captures the previous content as a version —
// the same history the browser's "versions" dialog shows.
func (w *writeFile) commitOverwrite(size int64) error {
	f := w.file
	if err := w.fs.checkQuota(f.OwnerID, f.Scope, f.ProjectID, size-f.SizeBytes); err != nil {
		return err
	}
	if err := w.fs.minio.Upload(w.ctx, f.MinioBucket, f.MinioKey, w.tmp, size, f.MimeType); err != nil {
		return err
	}
	info, err := w.fs.minio.GetObjectInfo(w.ctx, f.MinioBucket, f.MinioKey)
	if err != nil {
		return err
	}
	return w.fs.db.UpdateFileVersion(f.ID, info.Size)
}

func (w *writeFile) commitCreate(size int64) error {
	p := w.parent
	owner := w.fs.user.ID
	if err := w.fs.checkQuota(owner, p.scope, p.projectID(), size); err != nil {
		return err
	}

	bucket := storage.PersonalBucket(owner)
	switch p.scope {
	case "common":
		bucket = commonBucket
	case "project":
		bucket = storage.ProjectBucket(*p.projectID())
	}
	if err := w.fs.minio.EnsureBucket(w.ctx, bucket); err != nil {
		return err
	}

	key := davObjectKey(owner, p, w.name)
	ct := mime.TypeByExtension(strings.ToLower(path.Ext(w.name)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	if err := w.fs.minio.Upload(w.ctx, bucket, key, w.tmp, size, ct); err != nil {
		return err
	}
	info, err := w.fs.minio.GetObjectInfo(w.ctx, bucket, key)
	if err != nil {
		return err
	}
	return w.fs.db.CreateFile(&models.File{
		Name: w.name, MimeType: ct, SizeBytes: info.Size,
		MinioBucket: bucket, MinioKey: key, FolderID: p.folderID(),
		OwnerID: owner, ProjectID: p.projectID(), Scope: p.scope,
		Visibility: "private", Version: 1,
	})
}

// davObjectKey mirrors how the upload handler lays objects out, so a file
// created from the drive is indistinguishable from one uploaded in the
// browser.
func davObjectKey(ownerID int, p *node, name string) string {
	if p.folder != nil {
		return fmt.Sprintf("%d/f%d/%s", ownerID, p.folder.ID, name)
	}
	return fmt.Sprintf("%d/%s", ownerID, name)
}

// checkQuota refuses a write that would push the space past its limit,
// using the same accounting the browser upload path uses. delta may be
// negative when a file shrinks.
func (f *FS) checkQuota(ownerID int, scope string, projectID *int, delta int64) error {
	if delta <= 0 {
		return nil
	}
	usage, err := f.db.GetStorageUsage(ownerID, scope, projectID)
	if err != nil {
		return nil // accounting unavailable — do not block the save on it
	}
	if usage.UsedBytes+delta > usage.QuotaBytes {
		return fmt.Errorf("dav: quota exceeded")
	}
	return nil
}

// Package dav exposes the file storage over WebDAV, so the studio's
// spaces can be mounted as a network drive and CAD software can open and
// save files in place instead of going through download-edit-upload by hand.
//
// The tree a client sees is fixed and shallow at the top:
//
//	/personal/…              the caller's own files
//	/common/…                the company-wide space
//	/projects/<Project>/…    one directory per project they belong to
//
// Those three names are stable identifiers, not translated labels: a drive
// mapping is remembered by the operating system for months, and a path that
// changed with the user's interface language would break every one of them.
//
// Nothing here re-implements the permission model. Every operation resolves
// a path to the same (scope, project, folder) triple the HTTP API works in
// and then asks the same questions the API asks, so a person sees exactly
// the same files on the drive as in the browser.
package dav

import (
	"errors"
	"os"
	"strings"

	"paylash/internal/models"
)

// Top-level directory names. See the package comment for why they are not
// localised.
const (
	dirPersonal = "personal"
	dirCommon   = "common"
	dirProjects = "projects"
)

type nodeKind int

const (
	kindRoot     nodeKind = iota // /
	kindScope                    // /personal, /common
	kindProjects                 // /projects
	kindProject                  // /projects/<name>
	kindFolder                   // a folder inside a scope
	kindFile
)

// node is a resolved path: what it is, and the records behind it.
type node struct {
	kind    nodeKind
	scope   string // "personal" | "common" | "project"
	project *models.Project
	folder  *models.Folder // nil at the root of a scope
	file    *models.File
	name    string // the last path segment, for FileInfo
	// writable records whether the caller may modify things HERE, decided
	// once during resolution so no caller has to remember to ask.
	writable bool
}

// splitPath normalises a WebDAV path into its segments. WebDAV clients are
// inconsistent about trailing slashes and about sending "/" versus "".
func splitPath(name string) []string {
	name = strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/")
	if name == "" {
		return nil
	}
	parts := strings.Split(name, "/")
	out := parts[:0]
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		out = append(out, p)
	}
	return out
}

var errNotFound = os.ErrNotExist

// resolve walks a path to a node, enforcing access as it goes. A path the
// caller may not see is reported as "not found" rather than "forbidden":
// a drive listing should not become a way to probe for the existence of
// other people's projects.
func (f *FS) resolve(parts []string) (*node, error) {
	if len(parts) == 0 {
		return &node{kind: kindRoot, name: "/"}, nil
	}

	switch parts[0] {
	case dirPersonal:
		return f.resolveInScope(&node{kind: kindScope, scope: "personal", name: dirPersonal, writable: true}, parts[1:])
	case dirCommon:
		// The company-wide space is writable by everyone who can reach it,
		// matching the browser: uploading into "common" is a normal action.
		return f.resolveInScope(&node{kind: kindScope, scope: "common", name: dirCommon, writable: true}, parts[1:])
	case dirProjects:
		return f.resolveProject(parts[1:])
	}
	return nil, errNotFound
}

func (f *FS) resolveProject(parts []string) (*node, error) {
	if len(parts) == 0 {
		return &node{kind: kindProjects, name: dirProjects}, nil
	}
	projects, err := f.db.ListProjectsForUser(f.user.ID, f.user.Role == "admin")
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.Name != parts[0] {
			continue
		}
		proj := &models.Project{ID: p.ID, Name: p.Name}
		n := &node{
			kind: kindProject, scope: "project", project: proj, name: p.Name,
			// An admin sees every project through ListProjectsForUser but is
			// not necessarily a member; treat their access as edit, matching
			// every other admin path in the app.
			writable: p.Permission == "edit" || f.user.Role == "admin",
		}
		return f.resolveInScope(n, parts[1:])
	}
	return nil, errNotFound
}

// resolveInScope walks the folder chain below a scope root, then optionally
// a final file. Each step is a lookup by NAME within one parent, which is
// how a filesystem path maps onto this app's id-keyed folder tree.
func (f *FS) resolveInScope(root *node, parts []string) (*node, error) {
	cur := root
	for i, seg := range parts {
		last := i == len(parts)-1

		folder, err := f.findFolder(cur, seg)
		if err != nil {
			return nil, err
		}
		if folder != nil {
			cur = &node{
				kind: kindFolder, scope: root.scope, project: root.project,
				folder: folder, name: folder.Name, writable: root.writable,
			}
			continue
		}
		if !last {
			return nil, errNotFound // a non-final segment must be a directory
		}
		file, err := f.findFile(cur, seg)
		if err != nil {
			return nil, err
		}
		if file == nil {
			return nil, errNotFound
		}
		return &node{
			kind: kindFile, scope: root.scope, project: root.project,
			folder: cur.folder, file: file, name: file.Name, writable: root.writable,
		}, nil
	}
	return cur, nil
}

// folderID is the parent id a listing or a new record should use — nil at
// the root of a scope, which is how the rest of the app spells "top level".
func (n *node) folderID() *int {
	if n.folder == nil {
		return nil
	}
	return &n.folder.ID
}

func (n *node) projectID() *int {
	if n.project == nil {
		return nil
	}
	return &n.project.ID
}

// ownerScope is the (ownerID, projectID, scope) triple the db layer wants.
// Personal content is keyed to the caller; everything else is keyed to the
// space it lives in, exactly as the HTTP handlers do it.
func (f *FS) ownerFor(n *node) int { return f.user.ID }

func (f *FS) findFolder(parent *node, name string) (*models.Folder, error) {
	folders, err := f.db.ListFolders(f.ownerFor(parent), parent.projectID(), parent.scope, parent.folderID())
	if err != nil {
		return nil, err
	}
	for i := range folders {
		if folders[i].Name == name {
			return &folders[i], nil
		}
	}
	return nil, nil
}

func (f *FS) findFile(parent *node, name string) (*models.File, error) {
	files, err := f.listFiles(parent)
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].Name == name {
			return &files[i], nil
		}
	}
	return nil, nil
}

// listFiles returns everything directly inside a directory node. The limit
// mirrors what the browser asks for on a folder page; a directory holding
// more than this is already unusable in the app itself.
const davListLimit = 5000

func (f *FS) listFiles(n *node) ([]models.File, error) {
	if n.kind == kindRoot || n.kind == kindProjects {
		return nil, nil
	}
	return f.db.ListFiles(f.ownerFor(n), n.projectID(), n.scope, n.folderID(), "name", "asc", davListLimit, 0)
}

func (f *FS) listFolders(n *node) ([]models.Folder, error) {
	if n.kind == kindRoot || n.kind == kindProjects {
		return nil, nil
	}
	return f.db.ListFolders(f.ownerFor(n), n.projectID(), n.scope, n.folderID())
}

// errReadOnly is returned for a write into a space the caller may only view.
//
// Note what this does NOT get you: x/net/webdav picks its status code per
// method and never inspects the error, so this surfaces as 405 from MKCOL
// and 404 from PUT rather than 403. The refusal is what matters here; where
// the code itself matters, the request is screened earlier — see
// protectedTarget in handler.go.
var errReadOnly = errors.New("dav: read-only location")

package api

import (
	"errors"
	"net/http/httptest"
	"paylash/internal/models"
	"strings"
	"testing"
)

// stubProjectPermLookup is a fake projectPermLookup so these tests never
// need a real Postgres connection.
type stubProjectPermLookup struct {
	perm string
	err  error
}

func (s stubProjectPermLookup) GetProjectMemberPermission(projectID, userID int) (string, error) {
	return s.perm, s.err
}

func TestCanEditScopeWith(t *testing.T) {
	projectID := 3

	tests := []struct {
		name      string
		role      string
		scope     string
		projectID *int
		lookup    stubProjectPermLookup
		want      bool
	}{
		{name: "admin can edit any scope", role: "admin", scope: "project", projectID: &projectID, lookup: stubProjectPermLookup{perm: ""}, want: true},
		{name: "personal scope always allowed", role: "user", scope: "personal", want: true},
		{name: "common scope always allowed", role: "user", scope: "common", want: true},
		{name: "project scope with no project id denied", role: "user", scope: "project", projectID: nil, want: false},
		{name: "project scope: view member cannot create content", role: "user", scope: "project", projectID: &projectID, lookup: stubProjectPermLookup{perm: "view"}, want: false},
		{name: "project scope: edit member can create content", role: "user", scope: "project", projectID: &projectID, lookup: stubProjectPermLookup{perm: "edit"}, want: true},
		{name: "project scope: non-member denied", role: "user", scope: "project", projectID: &projectID, lookup: stubProjectPermLookup{perm: ""}, want: false},
		{name: "project scope: lookup error denied", role: "user", scope: "project", projectID: &projectID, lookup: stubProjectPermLookup{perm: "edit", err: errors.New("db down")}, want: false},
		{name: "unknown scope denied", role: "user", scope: "bogus", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canEditScopeWith(tt.lookup, tt.role, 1, tt.scope, tt.projectID)
			if got != tt.want {
				t.Errorf("canEditScopeWith() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntPtrEqual(t *testing.T) {
	one, oneAgain, two := 1, 1, 2

	tests := []struct {
		name string
		a, b *int
		want bool
	}{
		{name: "both nil", a: nil, b: nil, want: true},
		{name: "nil vs value", a: nil, b: &one, want: false},
		{name: "value vs nil", a: &one, b: nil, want: false},
		{name: "equal values, different pointers", a: &one, b: &oneAgain, want: true},
		{name: "different values", a: &one, b: &two, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intPtrEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("intPtrEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestInlineSafeContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp; charset=binary", true}, // DetectContentType can append a charset param
		{"audio/mpeg", true},
		{"video/mp4", true},
		{"application/pdf", true},
		// The whole point of this function: these must never be inline.
		{"image/svg+xml", false},
		{"text/html; charset=utf-8", false},
		{"text/plain; charset=utf-8", false},
		{"text/xml; charset=utf-8", false},
		{"application/octet-stream", false},
		{"application/zip", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			if got := inlineSafeContentType(tt.ct); got != tt.want {
				t.Errorf("inlineSafeContentType(%q) = %v, want %v", tt.ct, got, tt.want)
			}
		})
	}
}

func TestCanEditFolderWith(t *testing.T) {
	projectID := 3

	tests := []struct {
		name   string
		role   string
		userID int
		folder *models.Folder
		lookup stubProjectPermLookup
		want   bool
	}{
		{name: "admin can edit any folder", role: "admin", userID: 1, folder: &models.Folder{Scope: "personal", OwnerID: 99}, want: true},
		{name: "owner can edit their personal folder", role: "user", userID: 5, folder: &models.Folder{Scope: "personal", OwnerID: 5}, want: true},
		{name: "non-owner cannot edit someone else's personal folder", role: "user", userID: 6, folder: &models.Folder{Scope: "personal", OwnerID: 5}, want: false},
		{name: "common folder editable by anyone", role: "user", userID: 6, folder: &models.Folder{Scope: "common"}, want: true},
		{name: "project folder: no project id denied", role: "user", userID: 6, folder: &models.Folder{Scope: "project", ProjectID: nil}, want: false},
		{name: "project folder: view member cannot delete", role: "user", userID: 6, folder: &models.Folder{Scope: "project", ProjectID: &projectID}, lookup: stubProjectPermLookup{perm: "view"}, want: false},
		{name: "project folder: edit member can delete", role: "user", userID: 6, folder: &models.Folder{Scope: "project", ProjectID: &projectID}, lookup: stubProjectPermLookup{perm: "edit"}, want: true},
		{name: "unknown scope denied", role: "user", userID: 6, folder: &models.Folder{Scope: "bogus"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canEditFolderWith(tt.lookup, tt.role, tt.userID, tt.folder)
			if got != tt.want {
				t.Errorf("canEditFolderWith() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Renaming must never change a file's extension: it drives the icon, the
// preview, whether Collabora opens it, and the MIME type a download
// announces. The UI doesn't offer the extension for editing at all — these
// cover the server-side enforcement behind that.
func TestKeepFileExt(t *testing.T) {
	cases := []struct {
		name, oldName, newName, want string
	}{
		{"plain rename keeps extension", "report.docx", "Отчёт за август", "Отчёт за август.docx"},
		{"client sending the full name is left alone", "report.docx", "Отчёт.docx", "Отчёт.docx"},
		{"extension casing is not a change", "report.DOCX", "Отчёт.docx", "Отчёт.docx"},
		{"a different extension is appended, not swapped", "report.docx", "Отчёт.pdf", "Отчёт.pdf.docx"},
		{"a dotted name keeps its dots and gains the extension", "smeta.xlsx", "Смета 2.5", "Смета 2.5.xlsx"},
		{"extensionless file is renamed freely", "LICENSE", "ЛИЦЕНЗИЯ", "ЛИЦЕНЗИЯ"},
		{"extensionless file can gain an extension", "LICENSE", "license.txt", "license.txt"},
		{"a dotfile's leading dot is part of the name", ".gitignore", "ignore-rules", "ignore-rules"},
		{"a trailing dot is not an extension", "report.", "Отчёт", "Отчёт"},
		{"double extension keeps only the last part", "archive.tar.gz", "Архив", "Архив.gz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := keepFileExt(c.oldName, c.newName); got != c.want {
				t.Errorf("keepFileExt(%q, %q) = %q, want %q", c.oldName, c.newName, got, c.want)
			}
		})
	}
}

func TestSplitFileExt(t *testing.T) {
	cases := []struct{ in, base, ext string }{
		{"report.docx", "report", ".docx"},
		{"archive.tar.gz", "archive.tar", ".gz"},
		{"LICENSE", "LICENSE", ""},
		{".gitignore", ".gitignore", ""},
		{"report.", "report.", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		base, ext := splitFileExt(c.in)
		if base != c.base || ext != c.ext {
			t.Errorf("splitFileExt(%q) = (%q, %q), want (%q, %q)", c.in, base, ext, c.base, c.ext)
		}
	}
}

// Content-Disposition has to survive names this deployment actually uses:
// Russian and Turkmen ones, which are the majority here. A bare filename="…"
// is ISO-8859-1 by RFC 6266, so those need the RFC 5987 filename* form or the
// browser saves them as mojibake — or as "download".
func TestAsciiFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"Отчёт по проекту.xlsx", "_____ __ _______.xlsx"}, // one underscore per rune, extension preserved
		{`quote".txt`, "quote_.txt"},
		{`back\slash.txt`, "back_slash.txt"},
		{"Отчёт", "download"}, // all underscores is useless; filename* still carries the real name
		{"Смета.xlsx", "_____.xlsx"},
		{"", "download"},
		{"tab\there.txt", "tab_here.txt"},
	}
	for _, c := range cases {
		if got := asciiFilename(c.in); got != c.want {
			t.Errorf("asciiFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRFC5987Escape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"a b", "a%20b"},
		// The characters url.PathEscape would have left alone, every one of
		// which would corrupt the header parameter it sits in.
		{"a;b,c=d:e", "a%3Bb%2Cc%3Dd%3Ae"},
		{`a"b`, "a%22b"},
		{"Отчёт", "%D0%9E%D1%82%D1%87%D1%91%D1%82"},
	}
	for _, c := range cases {
		if got := rfc5987Escape(c.in); got != c.want {
			t.Errorf("rfc5987Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A name that breaks out of the quoted string would let an attacker-controlled
// filename append header parameters of their own.
func TestSetContentDispositionIsUnbreakable(t *testing.T) {
	rec := httptest.NewRecorder()
	setContentDisposition(rec, "attachment", `evil"; filename="passwd`)
	got := rec.Header().Get("Content-Disposition")
	if strings.Count(got, `filename="`) != 1 {
		t.Errorf("a quote in the name escaped the quoted string: %q", got)
	}
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Errorf("missing RFC 5987 parameter: %q", got)
	}
}

package api

import (
	"errors"
	"paylash/internal/models"
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

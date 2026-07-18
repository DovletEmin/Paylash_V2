package api

import (
	"errors"
	"paylash/internal/models"
	"testing"
)

func TestCanManageSharingWith(t *testing.T) {
	projectID := 3

	tests := []struct {
		name   string
		role   string
		userID int
		file   *models.File
		lookup stubProjectPermLookup
		want   bool
	}{
		{name: "admin can manage sharing on any file", role: "admin", userID: 1, file: &models.File{Scope: "personal", OwnerID: 99}, want: true},
		{name: "owner can manage sharing on their own personal file", role: "user", userID: 5, file: &models.File{Scope: "personal", OwnerID: 5}, want: true},
		{name: "personal-scope edit-share recipient cannot manage sharing (owner's call only)", role: "user", userID: 6, file: &models.File{Scope: "personal", OwnerID: 5}, want: false},
		{name: "common-scope file: any employee can manage sharing", role: "user", userID: 6, file: &models.File{Scope: "common", OwnerID: 5}, want: true},
		{name: "personal-scope file broadcast to common visibility: anyone can manage sharing", role: "user", userID: 6, file: &models.File{Scope: "personal", OwnerID: 5, Visibility: "common"}, want: true},
		{name: "project-scope: no project id denied", role: "user", userID: 6, file: &models.File{Scope: "project", OwnerID: 5, ProjectID: nil}, want: false},
		{name: "project-scope: view member cannot manage sharing", role: "user", userID: 6, file: &models.File{Scope: "project", OwnerID: 5, ProjectID: &projectID}, lookup: stubProjectPermLookup{perm: "view"}, want: false},
		{name: "project-scope: edit member can manage sharing", role: "user", userID: 6, file: &models.File{Scope: "project", OwnerID: 5, ProjectID: &projectID}, lookup: stubProjectPermLookup{perm: "edit"}, want: true},
		{name: "project-scope: non-member denied", role: "user", userID: 6, file: &models.File{Scope: "project", OwnerID: 5, ProjectID: &projectID}, lookup: stubProjectPermLookup{perm: ""}, want: false},
		{name: "project-scope: lookup error denied", role: "user", userID: 6, file: &models.File{Scope: "project", OwnerID: 5, ProjectID: &projectID}, lookup: stubProjectPermLookup{perm: "edit", err: errors.New("db down")}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := canManageSharingWith(tt.lookup, tt.role, tt.userID, tt.file)
			if got != tt.want {
				t.Errorf("canManageSharingWith() = %v, want %v", got, tt.want)
			}
		})
	}
}

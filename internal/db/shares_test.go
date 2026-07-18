package db

import "testing"

func TestDecideFileAccess(t *testing.T) {
	projectID := 7

	tests := []struct {
		name         string
		facts        fileAccessFacts
		userID       int
		isAdmin      bool
		requiredPerm string
		want         bool
	}{
		{
			name:         "admin bypasses everything",
			facts:        fileAccessFacts{ownerID: 99, scope: "personal"},
			userID:       1,
			isAdmin:      true,
			requiredPerm: "edit",
			want:         true,
		},
		{
			name:         "owner has full access to their own personal file",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal"},
			userID:       5,
			requiredPerm: "edit",
			want:         true,
		},
		{
			name:         "non-owner denied on a private personal file with no share",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal"},
			userID:       6,
			requiredPerm: "view",
			want:         false,
		},
		{
			name:         "common scope is open to everyone, view",
			facts:        fileAccessFacts{ownerID: 5, scope: "common"},
			userID:       6,
			requiredPerm: "view",
			want:         true,
		},
		{
			name:         "common scope is open to everyone, edit",
			facts:        fileAccessFacts{ownerID: 5, scope: "common"},
			userID:       6,
			requiredPerm: "edit",
			want:         true,
		},
		{
			name:         "personal file explicitly marked visibility=common is open too",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal", visibility: "common"},
			userID:       6,
			requiredPerm: "edit",
			want:         true,
		},
		{
			name:         "project scope: non-member denied even for view",
			facts:        fileAccessFacts{ownerID: 5, scope: "project", projectID: &projectID, projectPerm: ""},
			userID:       6,
			requiredPerm: "view",
			want:         false,
		},
		{
			name:         "project scope: view-only member can view",
			facts:        fileAccessFacts{ownerID: 5, scope: "project", projectID: &projectID, projectPerm: "view"},
			userID:       6,
			requiredPerm: "view",
			want:         true,
		},
		{
			name:         "project scope: view-only member cannot edit",
			facts:        fileAccessFacts{ownerID: 5, scope: "project", projectID: &projectID, projectPerm: "view"},
			userID:       6,
			requiredPerm: "edit",
			want:         false,
		},
		{
			name:         "project scope: edit member can edit",
			facts:        fileAccessFacts{ownerID: 5, scope: "project", projectID: &projectID, projectPerm: "edit"},
			userID:       6,
			requiredPerm: "edit",
			want:         true,
		},
		{
			name:         "direct share: view permission allows view but not edit",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal", directShare: "view"},
			userID:       6,
			requiredPerm: "edit",
			want:         false,
		},
		{
			name:         "direct share: edit permission allows edit",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal", directShare: "edit"},
			userID:       6,
			requiredPerm: "edit",
			want:         true,
		},
		{
			name:         "public share only grants view, never edit",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal", isPublicShare: true},
			userID:       6,
			requiredPerm: "edit",
			want:         false,
		},
		{
			name:         "public share grants view",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal", isPublicShare: true},
			userID:       6,
			requiredPerm: "view",
			want:         true,
		},
		{
			name:         "no owner match, no common, no project, no share: denied",
			facts:        fileAccessFacts{ownerID: 5, scope: "personal"},
			userID:       6,
			requiredPerm: "view",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideFileAccess(tt.facts, tt.userID, tt.isAdmin, tt.requiredPerm)
			if got != tt.want {
				t.Errorf("decideFileAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

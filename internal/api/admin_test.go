package api

import "testing"

func TestWouldRemoveLastAdmin(t *testing.T) {
	tests := []struct {
		name       string
		targetRole string
		newRole    string
		adminCount int
		want       bool
	}{
		{name: "demoting the sole admin", targetRole: "admin", newRole: "user", adminCount: 1, want: true},
		{name: "deleting the sole admin (newRole never admin)", targetRole: "admin", newRole: "user", adminCount: 1, want: true},
		{name: "demoting one of several admins", targetRole: "admin", newRole: "user", adminCount: 2, want: false},
		{name: "demoting a non-admin is a no-op either way", targetRole: "user", newRole: "user", adminCount: 1, want: false},
		{name: "keeping the sole admin as admin", targetRole: "admin", newRole: "admin", adminCount: 1, want: false},
		{name: "promoting a user changes nothing about admin count", targetRole: "user", newRole: "admin", adminCount: 0, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wouldRemoveLastAdmin(tt.targetRole, tt.newRole, tt.adminCount)
			if got != tt.want {
				t.Errorf("wouldRemoveLastAdmin(%q, %q, %d) = %v, want %v", tt.targetRole, tt.newRole, tt.adminCount, got, tt.want)
			}
		})
	}
}

package server

import (
	"net/http/httptest"
	"testing"
)

func TestMustChangePasswordAllowed(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"can view own profile", "GET", "/api/auth/me", true},
		{"can update own profile (change password)", "PATCH", "/api/auth/profile", true},
		{"can sign out of other sessions", "POST", "/api/auth/logout-others", true},
		{"can view any avatar (read-only, cosmetic)", "GET", "/api/avatar/42", true},
		{"can view own avatar with no id at all still matches the prefix", "GET", "/api/avatar/", true},
		{"cannot list files", "GET", "/api/files", false},
		{"cannot upload a file", "POST", "/api/files/upload", false},
		{"cannot reach admin routes", "GET", "/api/admin/users", false},
		{"cannot upload a new avatar (only viewing is allowlisted)", "POST", "/api/auth/avatar", false},
		{"wrong method on an otherwise-allowed path is blocked", "POST", "/api/auth/me", false},
		{"avatar prefix match requires GET, not POST", "POST", "/api/avatar/42", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if got := mustChangePasswordAllowed(r); got != tt.want {
				t.Errorf("mustChangePasswordAllowed(%s %s) = %v, want %v", tt.method, tt.path, got, tt.want)
			}
		})
	}
}

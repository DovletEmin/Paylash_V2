package authutil

import "testing"

func TestValidUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     bool
	}{
		{"empty", "", false},
		{"too short", "ab", false},
		{"minimum length", "abc", true},
		{"comfortably long", "architect_studio", true},
		{"whitespace-only counts as empty", "   ", false},
		{"padding trimmed before length check", "  ab  ", false},
		{"padding trimmed, still valid", "  abc  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidUsername(tt.username); got != tt.want {
				t.Errorf("ValidUsername(%q) = %v, want %v", tt.username, got, tt.want)
			}
		})
	}
}

func TestValidPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{"empty", "", false},
		{"too short", "abcde", false},
		{"minimum length", "abcdef", true},
		{"comfortably long", "correct horse battery staple", true},
		{"whitespace is significant, not trimmed", "  ab  ", true}, // 6 raw chars, even though only 2 are non-space
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidPassword(tt.password); got != tt.want {
				t.Errorf("ValidPassword(%q) = %v, want %v", tt.password, got, tt.want)
			}
		})
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword returned an error: %v", err)
	}
	if !CheckPassword("correct-horse-battery-staple", hash) {
		t.Error("CheckPassword must accept the password it was hashed from")
	}
	if CheckPassword("wrong-password", hash) {
		t.Error("CheckPassword must reject a non-matching password")
	}
}

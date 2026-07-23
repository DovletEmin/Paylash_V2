package api

import "testing"

func TestExtFromMime(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"image/jpeg", ".jpg"},
		{"application/octet-stream", ".jpg"}, // unrecognized falls back to jpg
		{"", ".jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			if got := extFromMime(tt.mime); got != tt.want {
				t.Errorf("extFromMime(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

func TestMimeFromExt(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".png", "image/png"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".jpg", "image/jpeg"},
		{".bmp", "image/jpeg"}, // unrecognized falls back to jpeg
		{"", "image/jpeg"},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := mimeFromExt(tt.ext); got != tt.want {
				t.Errorf("mimeFromExt(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

// extFromMime and mimeFromExt should round-trip for every format they both
// recognize — a mismatch here would mean an uploaded avatar gets served back
// under the wrong Content-Type.
func TestExtMimeRoundTrip(t *testing.T) {
	mimes := []string{"image/png", "image/gif", "image/webp", "image/jpeg"}
	for _, m := range mimes {
		t.Run(m, func(t *testing.T) {
			if got := mimeFromExt(extFromMime(m)); got != m {
				t.Errorf("round-trip: mimeFromExt(extFromMime(%q)) = %q, want %q", m, got, m)
			}
		})
	}
}

package wopi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDerivePostMessageOrigin(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		forwardedProto string
		tls         bool
		want        string
	}{
		{"behind Caddy over https, X-Forwarded-Proto set", "paylash.local", "https", false, "https://paylash.local"},
		{"behind a proxy terminating http", "paylash.local", "http", false, "http://paylash.local"},
		{"no proxy header, direct TLS connection", "paylash.local", "", true, "https://paylash.local"},
		{"no proxy header, no TLS -- defaults to http", "localhost:8080", "", false, "http://localhost:8080"},
		{"X-Forwarded-Proto wins even with a real TLS connection present", "paylash.local", "http", true, "http://paylash.local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/wopi/files/1", nil)
			req.Host = tt.host
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if got := derivePostMessageOrigin(req); got != tt.want {
				t.Errorf("derivePostMessageOrigin() = %q, want %q", got, tt.want)
			}
		})
	}
}

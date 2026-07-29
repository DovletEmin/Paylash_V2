package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"paylash/internal/config"
)

func TestCollaboraHealthy(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okServer.Close()

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errServer.Close()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"200 OK is healthy", okServer.URL, true},
		{"non-200 is unhealthy", errServer.URL, false},
		{"unreachable host is unhealthy", "http://127.0.0.1:1", false},
		{"empty/invalid URL is unhealthy", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{
				cfg:              &config.Config{CollaboraHealthURL: tt.url},
				healthHTTPClient: &http.Client{Timeout: 2 * time.Second},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if got := s.collaboraHealthy(ctx); got != tt.want {
				t.Errorf("collaboraHealthy(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

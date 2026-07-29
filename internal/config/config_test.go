package config

import "testing"

func TestGetEnv(t *testing.T) {
	t.Run("uses the env var when set", func(t *testing.T) {
		t.Setenv("PAYLASH_TEST_STR", "hello")
		if got := getEnv("PAYLASH_TEST_STR", "fallback"); got != "hello" {
			t.Errorf("getEnv = %q, want %q", got, "hello")
		}
	})
	t.Run("falls back when unset", func(t *testing.T) {
		if got := getEnv("PAYLASH_TEST_STR_UNSET", "fallback"); got != "fallback" {
			t.Errorf("getEnv = %q, want %q", got, "fallback")
		}
	})
	t.Run("falls back on empty string, not just unset", func(t *testing.T) {
		t.Setenv("PAYLASH_TEST_STR_EMPTY", "")
		if got := getEnv("PAYLASH_TEST_STR_EMPTY", "fallback"); got != "fallback" {
			t.Errorf("getEnv = %q, want %q", got, "fallback")
		}
	})
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name   string
		setEnv bool
		value  string
		want   int
	}{
		{"valid int", true, "9090", 9090},
		{"unset falls back", false, "", 8080},
		{"non-numeric falls back", true, "not-a-number", 8080},
		{"empty falls back", true, "", 8080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("PAYLASH_TEST_INT", tt.value)
			}
			if got := getEnvInt("PAYLASH_TEST_INT", 8080); got != tt.want {
				t.Errorf("getEnvInt = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name   string
		setEnv bool
		value  string
		want   bool
	}{
		{"true", true, "true", true},
		{"false", true, "false", false},
		{"1 parses as true", true, "1", true},
		{"0 parses as false", true, "0", false},
		{"unset falls back to default true", false, "", true},
		{"garbage falls back to default true", true, "not-a-bool", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("PAYLASH_TEST_BOOL", tt.value)
			}
			if got := getEnvBool("PAYLASH_TEST_BOOL", true); got != tt.want {
				t.Errorf("getEnvBool = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLoadDefaults locks in Load()'s fallback values when nothing is set in
// the environment -- a silent change to any of these (e.g. a typo'd env var
// name) would otherwise only surface as a confusing runtime misconfiguration
// far from its actual cause.
func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"PAYLASH_PORT", "PAYLASH_DB_URL", "PAYLASH_MINIO_ENDPOINT",
		"PAYLASH_MINIO_ACCESS_KEY", "PAYLASH_MINIO_SECRET_KEY", "PAYLASH_MINIO_USE_SSL",
		"PAYLASH_MINIO_PUBLIC_ENDPOINT", "PAYLASH_COLLABORA_URL", "PAYLASH_COLLABORA_HEALTH_URL",
		"PAYLASH_BASE_URL", "PAYLASH_ALLOW_REGISTRATION",
	} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.MinioUseSSL {
		t.Errorf("MinioUseSSL = true, want false")
	}
	if cfg.MinioPublicEndpoint != "" {
		t.Errorf("MinioPublicEndpoint = %q, want empty (disables resumable upload by default)", cfg.MinioPublicEndpoint)
	}
	if !cfg.AllowRegistration {
		t.Errorf("AllowRegistration = false, want true (self-registration on by default)")
	}
	if cfg.CollaboraHealthURL == "" {
		t.Errorf("CollaboraHealthURL should have a non-empty default")
	}
}

func TestLoadHonorsEnvOverrides(t *testing.T) {
	t.Setenv("PAYLASH_PORT", "9999")
	t.Setenv("PAYLASH_ALLOW_REGISTRATION", "false")
	cfg := Load()
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
	if cfg.AllowRegistration {
		t.Errorf("AllowRegistration = true, want false (explicitly disabled)")
	}
}

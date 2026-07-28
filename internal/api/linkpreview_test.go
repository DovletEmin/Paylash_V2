package api

import (
	"net"
	"strings"
	"testing"
)

func TestExtractFirstURL(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"check this out https://example.com/page it's great", "https://example.com/page"},
		{"no links here", ""},
		{"(see https://example.com/x)", "https://example.com/x"},
		{"https://example.com/x.", "https://example.com/x"},
		{"https://example.com/x, and more", "https://example.com/x"},
		{"first https://a.com second https://b.com", "https://a.com"},
		{"trailing paren https://example.com/wiki/Foo_(bar)", "https://example.com/wiki/Foo_(bar)"},
	}
	for _, c := range cases {
		if got := extractFirstURL(c.body); got != c.want {
			t.Errorf("extractFirstURL(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

func TestParseOpenGraph(t *testing.T) {
	html := `<!doctype html><html><head>
		<title>Fallback Title</title>
		<meta property="og:title" content="Real Title">
		<meta name="description" content="Fallback description">
		<meta property="og:description" content="Real description">
		<meta property="og:image" content="/images/preview.png">
	</head><body>ignored</body></html>`

	title, description, image := parseOpenGraph(strings.NewReader(html))
	if title != "Real Title" {
		t.Errorf("title = %q, want %q", title, "Real Title")
	}
	if description != "Real description" {
		t.Errorf("description = %q, want %q", description, "Real description")
	}
	if image != "/images/preview.png" {
		t.Errorf("image = %q, want %q", image, "/images/preview.png")
	}
}

func TestParseOpenGraphFallsBackToTitleTag(t *testing.T) {
	html := `<head><title>Just A Page</title></head><body></body>`
	title, _, _ := parseOpenGraph(strings.NewReader(html))
	if title != "Just A Page" {
		t.Errorf("title = %q, want %q", title, "Just A Page")
	}
}

func TestParseOpenGraphNoUsableTitle(t *testing.T) {
	html := `<head><meta charset="utf-8"></head><body></body>`
	title, _, _ := parseOpenGraph(strings.NewReader(html))
	if title != "" {
		t.Errorf("title = %q, want empty", title)
	}
}

func TestIsDisallowedIP(t *testing.T) {
	disallowed := []string{
		"127.0.0.1",     // loopback
		"10.0.0.1",      // RFC1918
		"172.17.0.5",    // Docker default bridge
		"192.168.1.1",   // RFC1918
		"169.254.169.254", // cloud metadata endpoint
		"0.0.0.0",
		"::1",          // IPv6 loopback
		"fc00::1",      // IPv6 unique local
		"fe80::1",      // IPv6 link-local
	}
	for _, s := range disallowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q did not parse as an IP", s)
		}
		if !isDisallowedIP(ip) {
			t.Errorf("isDisallowedIP(%s) = false, want true", s)
		}
	}

	allowed := []string{
		"93.184.216.34", // example.com (public)
		"8.8.8.8",       // public DNS
		"2606:4700:4700::1111",
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q did not parse as an IP", s)
		}
		if isDisallowedIP(ip) {
			t.Errorf("isDisallowedIP(%s) = true, want false", s)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Errorf("truncateRunes short string = %q", got)
	}
	if got := truncateRunes("hello world", 5); got != "hello" {
		t.Errorf("truncateRunes = %q, want %q", got, "hello")
	}
	// Multi-byte runes must not be split mid-character.
	multibyte := "проверка"
	if got := truncateRunes(multibyte, 4); got != "пров" {
		t.Errorf("truncateRunes multibyte = %q, want %q", got, "пров")
	}
}

package api

import "testing"

func TestIsValidSticker(t *testing.T) {
	valid := []string{"😀", "👍", "❤️", "🎉", "🏗️", "✌️"}
	for _, s := range valid {
		if !isValidSticker(s) {
			t.Errorf("isValidSticker(%q) = false, want true (curated emoji must be accepted)", s)
		}
	}

	// The whole point of the allowlist: anything that isn't a curated emoji —
	// especially markup — must be rejected so it can never reach the DB and be
	// rendered as a "sticker" without HTML-escaping (stored XSS).
	invalid := []string{
		"",
		" ",
		"hello",
		"<svg onload=alert(1)>",
		"<img src=x onerror=alert(document.cookie)>",
		"😀😀",        // two stickers, not one
		"😀 ",        // trailing space
		"&lt;b&gt;", // pre-escaped markup
		"👀",         // a real emoji, but not in the curated set
	}
	for _, s := range invalid {
		if isValidSticker(s) {
			t.Errorf("isValidSticker(%q) = true, want false (non-curated input must be rejected)", s)
		}
	}
}

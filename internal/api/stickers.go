package api

// stickerSet is the canonical allowlist of sticker emojis. A message sent
// with kind == "sticker" must have a body that is exactly one of these —
// validated server-side because the frontend renders a sticker as its raw
// body (large, bubble-less, and NOT HTML-escaped at the sticker layer for
// legacy rows). Without this check a hand-crafted client could POST
// {"kind":"sticker","body":"<svg onload=...>"} and store markup that would
// execute in every recipient's browser (stored XSS). Keep this list in sync
// with ChatPage.STICKERS in web/js/chat.js — the two are intentionally
// identical, the server being the authority on what a valid sticker is.
var stickerSet = func() map[string]bool {
	all := []string{
		// faces
		"😀", "😂", "🥰", "😎", "😢", "😡", "😮", "🥳", "😴", "🤔",
		// gestures
		"👍", "👎", "👏", "🙌", "🤝", "🙏", "💪", "✌️", "👌", "🤞",
		// hearts
		"❤️", "💙", "💚", "💛", "💜", "🧡", "💔", "💯", "✨", "🔥",
		// celebration
		"🎉", "🎊", "🎁", "🏆", "🥂", "🎈", "🌟", "⭐", "🍾",
		// studio
		"🏗️", "🏛️", "📐", "📏", "🧱", "🏠", "🖼️", "📁", "✏️", "🗺️",
	}
	m := make(map[string]bool, len(all))
	for _, s := range all {
		m[s] = true
	}
	return m
}()

// isValidSticker reports whether body is exactly one allowed sticker emoji.
func isValidSticker(body string) bool {
	return stickerSet[body]
}

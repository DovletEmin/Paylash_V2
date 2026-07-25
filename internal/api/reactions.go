package api

// reactionSet is the allowlist of emojis a message reaction may use. Like
// stickers, a reaction's emoji is rendered by the frontend, so it must come
// from a fixed server-controlled set rather than free text — otherwise a
// crafted client could store markup that executes when the reaction bar is
// rendered. Keep in sync with ChatPage.REACTIONS in web/js/chat.js.
var reactionSet = func() map[string]bool {
	all := []string{"👍", "❤️", "😂", "😮", "😢", "🙏", "🔥", "🎉"}
	m := make(map[string]bool, len(all))
	for _, e := range all {
		m[e] = true
	}
	return m
}()

// isValidReaction reports whether emoji is an allowed reaction.
func isValidReaction(emoji string) bool {
	return reactionSet[emoji]
}

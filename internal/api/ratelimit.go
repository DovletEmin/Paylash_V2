package api

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute

	// registerMaxAttempts throttles account creation per source IP — mainly
	// a brake on automated bulk-account creation, not something a real
	// employee should ever brush up against.
	registerMaxAttempts = 10
	registerWindow      = time.Hour

	// commentMaxAttempts is generous on purpose: real review-comment threads
	// during an active design discussion can be rapid-fire. It's a brake on
	// scripted spam, not on normal collaboration.
	commentMaxAttempts = 30
	commentWindow      = 10 * time.Minute

	// avatarMaxAttempts — nobody legitimately re-uploads their avatar more
	// than a handful of times an hour; mainly a cap on using the endpoint to
	// burn storage/CPU (every upload re-encodes and re-checks quota).
	avatarMaxAttempts = 10
	avatarWindow      = time.Hour

	// messageMaxAttempts is generous like commentMaxAttempts — an active
	// conversation can be rapid-fire; this brakes scripted spam, not typing.
	messageMaxAttempts = 60
	messageWindow      = time.Minute

	// chatAttachmentMaxAttempts mirrors avatarMaxAttempts' reasoning: a cap
	// on storage/CPU abuse via repeated uploads, not something a real
	// conversation legitimately brushes up against.
	chatAttachmentMaxAttempts = 20
	chatAttachmentWindow      = 10 * time.Minute
)

// keyedLimiter throttles repeated actions per key (username, IP, user id...)
// within a sliding window — backs login/registration/comment/avatar-upload
// throttling. The app runs as a single instance (see PLAN.md — no
// horizontal scaling), so in-memory state is enough; it simply resets on
// restart, which is an acceptable trade-off here.
type keyedLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	events      map[string][]time.Time
}

func newKeyedLimiter(maxAttempts int, window time.Duration) *keyedLimiter {
	return &keyedLimiter{maxAttempts: maxAttempts, window: window, events: make(map[string][]time.Time)}
}

func newLoginLimiter() *keyedLimiter { return newKeyedLimiter(loginMaxAttempts, loginWindow) }

func (l *keyedLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) >= l.maxAttempts
}

func (l *keyedLimiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events[key] = append(l.prune(key), time.Now())
}

func (l *keyedLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.events, key)
}

// prune must be called with mu held. It drops attempts outside the window,
// stores the survivors back into the map, and returns them.
func (l *keyedLimiter) prune(key string) []time.Time {
	cutoff := time.Now().Add(-l.window)
	kept := l.events[key][:0]
	for _, t := range l.events[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.events, key)
		return nil
	}
	l.events[key] = kept
	return kept
}

// clientIP extracts the caller's address, preferring X-Forwarded-For (set by
// the Caddy reverse proxy) over the raw connection address.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i >= 0 {
			fwd = fwd[:i]
		}
		return strings.TrimSpace(fwd)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

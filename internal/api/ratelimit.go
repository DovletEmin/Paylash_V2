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
)

// loginLimiter throttles repeated failed login attempts per key (username
// or source IP) to slow down password guessing. The app runs as a single
// instance (see PLAN.md — no horizontal scaling), so in-memory state is
// enough; it simply resets on restart, which is an acceptable trade-off here.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: make(map[string][]time.Time)}
}

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) >= loginMaxAttempts
}

func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.prune(key), time.Now())
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// prune must be called with mu held. It drops attempts outside the window,
// stores the survivors back into the map, and returns them.
func (l *loginLimiter) prune(key string) []time.Time {
	cutoff := time.Now().Add(-loginWindow)
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
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

package api

import (
	"testing"
	"time"
)

// The keyedLimiter still backs comment/avatar/message/attachment throttling
// (login and registration are no longer rate-limited), so its core
// block-after-N / reset behaviour is still worth pinning down.
func TestKeyedLimiter(t *testing.T) {
	const max = 3
	l := newKeyedLimiter(max, time.Minute)
	const key = "u:alice"

	if l.blocked(key) {
		t.Fatal("fresh key must not be blocked")
	}

	for i := 0; i < max-1; i++ {
		l.record(key)
	}
	if l.blocked(key) {
		t.Fatalf("must not be blocked before reaching %d attempts", max)
	}

	l.record(key)
	if !l.blocked(key) {
		t.Fatalf("must be blocked after %d attempts", max)
	}

	l.reset(key)
	if l.blocked(key) {
		t.Fatal("reset must clear the block")
	}
}

func TestKeyedLimiterKeysAreIndependent(t *testing.T) {
	const max = 3
	l := newKeyedLimiter(max, time.Minute)
	for i := 0; i < max; i++ {
		l.record("u:alice")
	}
	if !l.blocked("u:alice") {
		t.Fatal("alice should be blocked")
	}
	if l.blocked("ip:1.2.3.4") {
		t.Fatal("an unrelated key must not be affected")
	}
}

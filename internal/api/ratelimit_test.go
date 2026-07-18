package api

import "testing"

func TestLoginLimiter(t *testing.T) {
	l := newLoginLimiter()
	const key = "u:alice"

	if l.blocked(key) {
		t.Fatal("fresh key must not be blocked")
	}

	for i := 0; i < loginMaxAttempts-1; i++ {
		l.recordFailure(key)
	}
	if l.blocked(key) {
		t.Fatalf("must not be blocked before reaching %d failures", loginMaxAttempts)
	}

	l.recordFailure(key)
	if !l.blocked(key) {
		t.Fatalf("must be blocked after %d failures", loginMaxAttempts)
	}

	l.reset(key)
	if l.blocked(key) {
		t.Fatal("reset must clear the block")
	}
}

func TestLoginLimiterKeysAreIndependent(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < loginMaxAttempts; i++ {
		l.recordFailure("u:alice")
	}
	if !l.blocked("u:alice") {
		t.Fatal("alice should be blocked")
	}
	if l.blocked("ip:1.2.3.4") {
		t.Fatal("an unrelated key must not be affected")
	}
}

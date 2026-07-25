package authutil

import (
	"encoding/base32"
	"testing"
	"time"
)

// The RFC 6238 appendix B test vector for SHA1: with the ASCII seed
// "12345678901234567890", T=59s yields the 8-digit code 94287082, i.e. the
// 6-digit code 287082. Verifying against it proves the implementation is a
// correct TOTP any standard authenticator app will agree with.
func TestTOTPCodeRFC6238Vector(t *testing.T) {
	secret := base32.StdEncoding.EncodeToString([]byte("12345678901234567890"))
	got, err := TOTPCode(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatalf("TOTPCode error: %v", err)
	}
	if got != "287082" {
		t.Fatalf("TOTPCode(t=59) = %q, want 287082 (RFC 6238 vector)", got)
	}
}

func TestVerifyTOTP(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTP(secret, code, now) {
		t.Error("current code should verify")
	}
	// Skew tolerance: a code from one step ago still verifies.
	prev, _ := TOTPCode(secret, now.Add(-30*time.Second))
	if !VerifyTOTP(secret, prev, now) {
		t.Error("code from previous 30s step should verify within skew window")
	}
	// A code two steps out must NOT verify, nor should garbage.
	old, _ := TOTPCode(secret, now.Add(-90*time.Second))
	if VerifyTOTP(secret, old, now) {
		t.Error("code from 90s ago must not verify")
	}
	if VerifyTOTP(secret, "000000", now.Add(1000*time.Hour)) {
		t.Error("arbitrary code must not verify")
	}
	if VerifyTOTP(secret, "abc", now) {
		t.Error("malformed code must not verify")
	}
}

func TestRecoveryCodeHashStable(t *testing.T) {
	codes, err := GenerateRecoveryCodes(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 8 {
		t.Fatalf("want 8 codes, got %d", len(codes))
	}
	// Hashing is case/space-insensitive and deterministic.
	if HashRecoveryCode(codes[0]) != HashRecoveryCode("  "+codes[0]+"  ") {
		t.Error("recovery hash should ignore surrounding whitespace")
	}
	if HashRecoveryCode(codes[0]) == HashRecoveryCode(codes[1]) {
		t.Error("distinct codes should hash differently")
	}
}

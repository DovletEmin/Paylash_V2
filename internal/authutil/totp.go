package authutil

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238) is implemented directly on the standard library — no
// external dependency to fetch (this deployment builds offline) and the
// algorithm is small and well-specified. SHA1 / 30s / 6 digits are the
// universal defaults every authenticator app (Google Authenticator, Aegis,
// 1Password, …) uses out of the box.
const (
	totpPeriod = 30
	totpDigits = 6
)

// GenerateTOTPSecret returns a fresh random base32 secret (unpadded) to seed a
// new authenticator enrollment.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20) // 160 bits, the RFC-recommended size for SHA1
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "="), nil
}

// TOTPCode computes the 6-digit code for a base32 secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.DecodeString(padBase32(secret))
	if err != nil {
		return "", err
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(t.Unix())/totpPeriod)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, code%1_000_000), nil
}

// VerifyTOTP checks code against secret, allowing ±1 step (±30s) of clock skew
// — a constant-time comparison, so a wrong code leaks no timing signal.
func VerifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	for _, delta := range []time.Duration{0, -totpPeriod * time.Second, totpPeriod * time.Second} {
		if want, err := TOTPCode(secret, now.Add(delta)); err == nil &&
			subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPURI builds the otpauth:// URI an authenticator app imports (also what a
// QR code would encode). issuer/account appear in the app's account list.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(totpDigits))
	v.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// GenerateRecoveryCodes returns n single-use backup codes (plaintext, shown to
// the user exactly once at enrollment).
func GenerateRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, n)
	for i := range codes {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = hex.EncodeToString(b) // 10 hex chars, e.g. "a1b2c3d4e5"
	}
	return codes, nil
}

// HashRecoveryCode is the stored form of a recovery code — only hashes are
// persisted, so a database read never reveals a usable code.
func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func padBase32(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if m := len(s) % 8; m != 0 {
		s += strings.Repeat("=", 8-m)
	}
	return s
}

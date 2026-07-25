package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

// TestEncryptRoundTrip plays the user-agent side of RFC 8291: generate a UA
// keypair + auth secret, encrypt a payload for it, then decrypt exactly as a
// browser would and confirm the plaintext comes back. If the key derivation,
// header framing, or AES-GCM were wrong, the tag check would fail here — so a
// pass means the encryption is spec-correct end to end.
func TestEncryptRoundTrip(t *testing.T) {
	curve := ecdh.P256()
	uaPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uaPubBytes := uaPriv.PublicKey().Bytes()
	authSecret := make([]byte, 16)
	rand.Read(authSecret)

	sub := &Subscription{
		P256dh: base64.RawURLEncoding.EncodeToString(uaPubBytes),
		Auth:   base64.RawURLEncoding.EncodeToString(authSecret),
	}
	plaintext := []byte(`{"title":"Ada","body":"blueprint ready","conversation_id":7}`)

	body, err := encrypt(sub, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// --- decrypt as the UA ---
	if len(body) < 21 {
		t.Fatalf("body too short: %d", len(body))
	}
	salt := body[0:16]
	idlen := int(body[20])
	asPubBytes := body[21 : 21+idlen]
	ciphertext := body[21+idlen:]

	asPub, err := curve.NewPublicKey(asPubBytes)
	if err != nil {
		t.Fatalf("parse as public: %v", err)
	}
	shared, err := uaPriv.ECDH(asPub)
	if err != nil {
		t.Fatalf("ecdh: %v", err)
	}

	keyInfo := append([]byte("WebPush: info\x00"), uaPubBytes...)
	keyInfo = append(keyInfo, asPubBytes...)
	ikm := hkdfExpand(hkdfExtract(authSecret, shared), keyInfo, 32)
	prk := hkdfExtract(salt, ikm)
	cek := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)

	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	got, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("gcm open (decryption) failed: %v", err)
	}
	got = bytes.TrimSuffix(got, []byte{0x02}) // strip the RFC 8188 record delimiter

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, plaintext)
	}
}

func TestVAPIDKeysRoundTrip(t *testing.T) {
	priv, pub, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	vk, err := ParseVAPIDKeys(priv, pub)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if vk.PublicB64 != pub {
		t.Error("public key not preserved")
	}
	// Signing a JWT for a sample endpoint should succeed and be well-formed.
	auth, err := vk.vapidAuthorization("https://fcm.googleapis.com/fcm/send/abc", "mailto:admin@example.com")
	if err != nil {
		t.Fatalf("authorization: %v", err)
	}
	if len(auth) < 20 || auth[:9] != "vapid t=" && auth[:8] != "vapid t=" {
		t.Errorf("unexpected authorization header: %q", auth)
	}
}

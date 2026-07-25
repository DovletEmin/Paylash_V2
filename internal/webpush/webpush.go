// Package webpush implements the Web Push protocol (RFC 8291 message
// encryption + RFC 8292 VAPID) directly on the standard library — no external
// dependency, so the app still builds fully offline.
//
// NOTE ON DEPLOYMENT: unlike the rest of Paýlaş, delivering a web push
// requires the SERVER to make an outbound HTTPS request to the browser
// vendor's push service (Google FCM, Mozilla autopush, …). On a truly
// air-gapped LAN those hosts are unreachable and Send simply fails — callers
// treat it as best-effort (fire-and-forget), so nothing breaks; the in-app
// and native (tab-open) notifications are unaffected. Enable this only where
// the server has outbound internet.
package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Subscription is a browser Push subscription (the PushSubscription the client
// obtains from pushManager.subscribe and posts to us).
type Subscription struct {
	Endpoint string
	P256dh   string // base64url, UA public key (uncompressed EC point, 65 bytes)
	Auth     string // base64url, 16-byte auth secret
}

// VAPIDKeys is the application-server identity keypair (P-256 / ES256).
type VAPIDKeys struct {
	private   *ecdsa.PrivateKey
	PublicB64 string // base64url uncompressed point — handed to the browser
}

// GenerateVAPIDKeys creates a fresh keypair, returning both halves base64url
// encoded for persistence (private = the 32-byte scalar, public = the 65-byte
// uncompressed point).
func GenerateVAPIDKeys() (privB64, pubB64 string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	d := make([]byte, 32)
	priv.D.FillBytes(d)
	pub := elliptic.Marshal(elliptic.P256(), priv.X, priv.Y) //nolint:staticcheck // uncompressed point is exactly what the spec wants
	return base64.RawURLEncoding.EncodeToString(d), base64.RawURLEncoding.EncodeToString(pub), nil
}

// ParseVAPIDKeys reconstructs a keypair from the persisted base64url halves.
func ParseVAPIDKeys(privB64, pubB64 string) (*VAPIDKeys, error) {
	d, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		return nil, fmt.Errorf("decode vapid private: %w", err)
	}
	priv := new(ecdsa.PrivateKey)
	priv.Curve = elliptic.P256()
	priv.D = new(big.Int).SetBytes(d)
	priv.X, priv.Y = elliptic.P256().ScalarBaseMult(d)
	return &VAPIDKeys{private: priv, PublicB64: pubB64}, nil
}

func hkdfExtract(salt, ikm []byte) []byte {
	m := hmac.New(sha256.New, salt)
	m.Write(ikm)
	return m.Sum(nil)
}

func hkdfExpand(prk, info []byte, length int) []byte {
	var out, t []byte
	for counter := byte(1); len(out) < length; counter++ {
		m := hmac.New(sha256.New, prk)
		m.Write(t)
		m.Write(info)
		m.Write([]byte{counter})
		t = m.Sum(nil)
		out = append(out, t...)
	}
	return out[:length]
}

// encrypt builds the aes128gcm request body per RFC 8291 §3.4 / RFC 8188.
func encrypt(sub *Subscription, payload []byte) ([]byte, error) {
	uaPubBytes, err := base64.RawURLEncoding.DecodeString(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("decode p256dh: %w", err)
	}
	authSecret, err := base64.RawURLEncoding.DecodeString(sub.Auth)
	if err != nil {
		return nil, fmt.Errorf("decode auth: %w", err)
	}

	curve := ecdh.P256()
	uaPub, err := curve.NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, fmt.Errorf("parse ua public key: %w", err)
	}
	asPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPub := asPriv.PublicKey().Bytes() // 65-byte uncompressed
	shared, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}

	// IKM = HKDF-Expand(HKDF-Extract(auth, ecdh), "WebPush: info"||0x00||ua||as, 32)
	keyInfo := append([]byte("WebPush: info\x00"), uaPubBytes...)
	keyInfo = append(keyInfo, asPub...)
	ikm := hkdfExpand(hkdfExtract(authSecret, shared), keyInfo, 32)

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	prk := hkdfExtract(salt, ikm)
	cek := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Single record: append the 0x02 last-record delimiter (RFC 8188 §2).
	ciphertext := gcm.Seal(nil, nonce, append(payload, 0x02), nil)

	var buf bytes.Buffer
	buf.Write(salt)
	rs := make([]byte, 4)
	binary.BigEndian.PutUint32(rs, 4096) // record size
	buf.Write(rs)
	buf.WriteByte(byte(len(asPub))) // keyid length
	buf.Write(asPub)                // keyid = as public key
	buf.Write(ciphertext)
	return buf.Bytes(), nil
}

// vapidAuthorization builds the "Authorization: vapid t=..., k=..." header for
// endpoint, signing the JWT with the VAPID private key (ES256).
func (vk *VAPIDKeys) vapidAuthorization(endpoint, subscriber string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	aud := u.Scheme + "://" + u.Host
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		"aud": aud,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": subscriber,
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, vk.private, hash[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	jwt := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
	return "vapid t=" + jwt + ", k=" + vk.PublicB64, nil
}

// Send delivers one encrypted push. It returns the push service's HTTP status
// (201 on success; 404/410 means the subscription is gone and the caller
// should delete it). subscriber is a "mailto:" or "https:" contact per VAPID.
func Send(client *http.Client, vk *VAPIDKeys, subscriber string, sub *Subscription, payload []byte, ttlSeconds int) (int, error) {
	body, err := encrypt(sub, payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	auth, err := vk.vapidAuthorization(sub.Endpoint, subscriber)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(ttlSeconds))

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

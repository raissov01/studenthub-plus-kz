// Package webpush sends Web Push notifications to the PWA: RFC 8291 message encryption
// (aes128gcm), RFC 8292 VAPID authentication and RFC 8030 delivery. Standard library only.
//
// The feature is off unless FEATURE_WEBPUSH is on and VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY and
// VAPID_SUBJECT are set (generate keys with `go run ./cmd/vapid-keys`). internal/notify uses it
// as a delivery channel; docs/PUSH.md describes the routing rule.
package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// recordSize is the aes128gcm record size advertised in the header. Push messages are a
// single record, so any value larger than the message works; 4096 is what browsers use.
const recordSize = 4096

// MaxPayload is the largest plaintext that fits the 4096-byte push message limit:
// 4096 - 86 (header) - 16 (tag) - 1 (delimiter).
const MaxPayload = 3993

var b64 = base64.RawURLEncoding

// decodeB64 accepts base64url with or without padding (and standard base64, which some
// older browsers produced for subscription keys).
func decodeB64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return b64.DecodeString(s)
}

// Keys are a subscription's p256dh (65-byte uncompressed P-256 point) and auth (16 bytes).
type Keys struct {
	P256dh []byte
	Auth   []byte
}

// ParseKeys validates the base64url keys a browser's PushSubscription.toJSON() returns.
func ParseKeys(p256dh, auth string) (Keys, error) {
	pub, err := decodeB64(p256dh)
	if err != nil || len(pub) != 65 || pub[0] != 4 {
		return Keys{}, errors.New("webpush: bad p256dh key")
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return Keys{}, errors.New("webpush: p256dh is not a P-256 point")
	}
	sec, err := decodeB64(auth)
	if err != nil || len(sec) != 16 {
		return Keys{}, errors.New("webpush: bad auth secret")
	}
	return Keys{P256dh: pub, Auth: sec}, nil
}

// encrypt implements RFC 8291 §3.4 with a caller-supplied application server key pair and
// salt, so the RFC's Appendix A example is reproducible in tests.
func encrypt(plaintext []byte, k Keys, asKey *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > MaxPayload {
		return nil, fmt.Errorf("webpush: payload %d bytes > %d", len(plaintext), MaxPayload)
	}
	if len(salt) != 16 {
		return nil, errors.New("webpush: salt must be 16 bytes")
	}
	uaPub, err := ecdh.P256().NewPublicKey(k.P256dh)
	if err != nil {
		return nil, err
	}
	secret, err := asKey.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPub := asKey.PublicKey().Bytes()
	// IKM = HKDF(auth_secret, ecdh_secret, "WebPush: info" || 0x00 || ua_public || as_public, 32)
	info := append(append([]byte("WebPush: info\x00"), k.P256dh...), asPub...)
	ikm, err := hkdf.Key(sha256.New, secret, k.Auth, string(info), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// One record: plaintext followed by the 0x02 "last record" delimiter, no extra padding.
	padded := append(append([]byte{}, plaintext...), 2)
	header := make([]byte, 0, 86)
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPub)))
	header = append(header, asPub...)
	return gcm.Seal(header, nonce, padded, nil), nil
}

// Encrypt encrypts a push message body for one subscription with a fresh key pair and salt.
func Encrypt(plaintext []byte, k Keys) ([]byte, error) {
	asKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encrypt(plaintext, k, asKey, salt)
}

// VAPID is the application server identity (RFC 8292).
type VAPID struct {
	Public  string // base64url uncompressed P-256 point, given to PushManager.subscribe
	Subject string // "mailto:…" or "https://…"
	key     *ecdsa.PrivateKey
}

// GenerateVAPID makes a new key pair; cmd/vapid-keys prints it as env lines.
func GenerateVAPID() (public, private string, err error) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return b64.EncodeToString(k.PublicKey().Bytes()), b64.EncodeToString(k.Bytes()), nil
}

// ParseVAPID checks that the private key (32-byte scalar) matches the public key.
func ParseVAPID(public, private, subject string) (*VAPID, error) {
	if !strings.HasPrefix(subject, "mailto:") && !strings.HasPrefix(subject, "https://") {
		return nil, errors.New("webpush: VAPID_SUBJECT must start with mailto: or https://")
	}
	raw, err := decodeB64(private)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("webpush: VAPID_PRIVATE_KEY must be a base64url 32-byte key")
	}
	ek, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("webpush: VAPID_PRIVATE_KEY: %w", err)
	}
	pub, err := decodeB64(public)
	if err != nil || string(pub) != string(ek.PublicKey().Bytes()) {
		return nil, errors.New("webpush: VAPID_PUBLIC_KEY does not match VAPID_PRIVATE_KEY")
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, fmt.Errorf("webpush: VAPID_PRIVATE_KEY: %w", err)
	}
	return &VAPID{Public: b64.EncodeToString(pub), Subject: subject, key: key}, nil
}

// Authorization returns the RFC 8292 header value for a push service origin
// ("https://fcm.googleapis.com"): vapid t=<ES256 JWT>, k=<public key>.
func (v *VAPID) Authorization(audience string, now time.Time) (string, error) {
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": audience, "exp": now.Add(12 * time.Hour).Unix(), "sub": v.Subject})
	if err != nil {
		return "", err
	}
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, v.key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // JWS ES256: fixed-width r || s, not ASN.1
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + v.Public, nil
}

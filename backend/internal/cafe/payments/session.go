package payments

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
)

// Cashier sessions are stored encrypted with AES-256-GCM under CAFE_KASPI_SESSION_KEY (64 hex chars), so a
// database dump alone cannot drive a café's Kaspi account. Nothing here logs or returns the plaintext.

var reHexKey = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// SessionKey is the configured key, or nil when it is missing or malformed.
func SessionKey() []byte {
	h := config.Str("CAFE_KASPI_SESSION_KEY", "")
	if !reHexKey.MatchString(h) {
		return nil
	}
	k, _ := hex.DecodeString(h)
	return k
}

const sessionVersion = "v1"

func Encrypt(s Session, key []byte) (string, error) {
	if len(key) != 32 {
		return "", ErrNotConfigured
	}
	plain, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, plain, []byte(sessionVersion))
	return sessionVersion + ":" + base64.StdEncoding.EncodeToString(nonce) + ":" + base64.StdEncoding.EncodeToString(ct), nil
}

var errBadCiphertext = errors.New("payments: bad session ciphertext")

// Decrypt fails on a wrong key or a tampered row; the café then simply logs in again.
func Decrypt(ciphertext string, key []byte) (*Session, error) {
	if len(key) != 32 {
		return nil, ErrNotConfigured
	}
	parts := strings.Split(ciphertext, ":")
	if len(parts) != 3 || parts[0] != sessionVersion {
		return nil, errBadCiphertext
	}
	nonce, err1 := base64.StdEncoding.DecodeString(parts[1])
	ct, err2 := base64.StdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return nil, errBadCiphertext
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errBadCiphertext
	}
	plain, err := gcm.Open(nil, nonce, ct, []byte(sessionVersion))
	if err != nil {
		return nil, errBadCiphertext
	}
	var s Session
	if err := json.Unmarshal(plain, &s); err != nil || s.TokenSN == "" || s.VtokenSecret == "" || s.ProfileID == "" {
		return nil, errBadCiphertext
	}
	return &s, nil
}

// PhoneHash identifies a cashier number without storing it (cafés sharing one number share one login).
func PhoneHash(phone string, key []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("cafe-cashier-phone:" + NormalizePhone(phone)))
	return hex.EncodeToString(m.Sum(nil))
}

// MaskPhone keeps the operator prefix and the last two digits: 7701***45.
func MaskPhone(phone string) string {
	p := NormalizePhone(phone)
	if p == "" {
		return "***"
	}
	return p[:4] + "***" + p[len(p)-2:]
}

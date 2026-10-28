package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxAuthAge is how old a Telegram auth_date may be.
const MaxAuthAge = 24 * time.Hour

var (
	ErrBadHash  = errors.New("telegram: hash mismatch")
	ErrExpired  = errors.New("telegram: auth_date too old")
	ErrBadInput = errors.New("telegram: malformed data")
	ErrNoToken  = errors.New("telegram: BOT_TOKEN is not set")
)

// TGUser is the Telegram identity both login flows produce.
type TGUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

// VerifyWebApp checks Mini App initData: secret = HMAC_SHA256(key "WebAppData", bot token),
// hash = hex(HMAC_SHA256(key secret, data-check-string)).
func VerifyWebApp(initData, botToken string, now time.Time) (TGUser, error) {
	if botToken == "" {
		return TGUser{}, ErrNoToken
	}
	vals, err := url.ParseQuery(initData)
	if err != nil || len(initData) > 8192 {
		return TGUser{}, ErrBadInput
	}
	fields := map[string]string{}
	for k, v := range vals {
		if len(v) != 1 {
			return TGUser{}, ErrBadInput
		}
		fields[k] = v[0]
	}
	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	if err := checkFields(fields, secret, now); err != nil {
		return TGUser{}, err
	}
	var u TGUser
	if err := json.Unmarshal([]byte(fields["user"]), &u); err != nil || u.ID <= 0 {
		return TGUser{}, ErrBadInput
	}
	return u, nil
}

// VerifyWidget checks Login Widget fields: secret = SHA256(bot token),
// hash = hex(HMAC_SHA256(key secret, data-check-string)). Values may be JSON strings or numbers.
func VerifyWidget(raw map[string]any, botToken string, now time.Time) (TGUser, error) {
	if botToken == "" {
		return TGUser{}, ErrNoToken
	}
	if len(raw) > 16 {
		return TGUser{}, ErrBadInput
	}
	fields := map[string]string{}
	for k, v := range raw {
		switch x := v.(type) {
		case string:
			fields[k] = x
		case json.Number:
			fields[k] = x.String()
		default:
			return TGUser{}, ErrBadInput
		}
	}
	sum := sha256.Sum256([]byte(botToken))
	if err := checkFields(fields, sum[:], now); err != nil {
		return TGUser{}, err
	}
	id, err := strconv.ParseInt(fields["id"], 10, 64)
	if err != nil || id <= 0 {
		return TGUser{}, ErrBadInput
	}
	return TGUser{ID: id, FirstName: fields["first_name"], LastName: fields["last_name"], Username: fields["username"]}, nil
}

// checkFields verifies the hash over every field except "hash", then auth_date.
func checkFields(fields map[string]string, secret []byte, now time.Time) error {
	got, err := hex.DecodeString(fields["hash"])
	if err != nil || len(got) != sha256.Size {
		return ErrBadHash
	}
	if !hmac.Equal(got, hmacSHA256(secret, []byte(DataCheckString(fields)))) {
		return ErrBadHash
	}
	ts, err := strconv.ParseInt(fields["auth_date"], 10, 64)
	if err != nil {
		return ErrBadInput
	}
	at := time.Unix(ts, 0)
	if now.Sub(at) > MaxAuthAge || at.Sub(now) > 5*time.Minute {
		return ErrExpired
	}
	return nil
}

// DataCheckString joins "key=value" for every field except hash, sorted by key, with "\n".
func DataCheckString(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k + "=" + fields[k])
	}
	return b.String()
}

func hmacSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// SignWebApp and SignWidget build valid payloads; tests and local tooling use them.
func SignWebApp(fields map[string]string, botToken string) string {
	secret := hmacSHA256([]byte("WebAppData"), []byte(botToken))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(hmacSHA256(secret, []byte(DataCheckString(fields)))))
	return v.Encode()
}

func SignWidget(fields map[string]string, botToken string) string {
	sum := sha256.Sum256([]byte(botToken))
	return hex.EncodeToString(hmacSHA256(sum[:], []byte(DataCheckString(fields))))
}

package auth

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
)

const (
	CookieName = "sh_session"
	SessionTTL = 30 * 24 * time.Hour
)

var (
	ErrNoSecret   = errors.New("auth: SESSION_SECRET is missing or shorter than 32 bytes")
	ErrBadSession = errors.New("auth: invalid session token")
)

// Claims is the signed session payload.
type Claims struct {
	UserID  int64 `json:"u"`
	Version int   `json:"v"`
	Exp     int64 `json:"e"`
}

func secret() ([]byte, error) {
	s := config.Str("SESSION_SECRET", "")
	if len(s) < 32 {
		return nil, ErrNoSecret
	}
	return []byte(s), nil
}

// Sign returns base64url(payload) + "." + base64url(HMAC_SHA256(SESSION_SECRET, payload)).
func Sign(c Claims) (string, error) {
	key, err := secret()
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(body)
	return p + "." + base64.RawURLEncoding.EncodeToString(hmacSHA256(key, []byte(p))), nil
}

// Parse verifies the signature and expiry; the session_version check happens against the DB.
func Parse(token string, now time.Time) (Claims, error) {
	key, err := secret()
	if err != nil {
		return Claims{}, err
	}
	p, sig, ok := strings.Cut(token, ".")
	if !ok || len(token) > 512 {
		return Claims{}, ErrBadSession
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, hmacSHA256(key, []byte(p))) {
		return Claims{}, ErrBadSession
	}
	body, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return Claims{}, ErrBadSession
	}
	var c Claims
	if err := json.Unmarshal(body, &c); err != nil || c.UserID <= 0 {
		return Claims{}, ErrBadSession
	}
	if now.Unix() >= c.Exp {
		return Claims{}, ErrBadSession
	}
	return c, nil
}

// isLocal is true for plain-http development hosts, where a Secure cookie would be dropped.
func isLocal(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !isLocal(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie.
func ClearCookie(w http.ResponseWriter, r *http.Request) { setCookie(w, r, "", -1) }

// tokenFrom reads the cookie, or "Authorization: Bearer" for Mini Apps inside Telegram Web,
// whose iframe may not keep third-party cookies.
func tokenFrom(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

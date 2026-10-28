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
	// TokenHeader carries a refreshed session token back to a Mini App that authenticates
	// with "Authorization: Bearer" (it cannot see the cookie). See docs/ACCOUNTS.md.
	TokenHeader = "X-Session-Token"
)

// Session lifetimes (04-foundation): a student's session slides while they keep using the
// site, so an active student is never signed out; staff roles must sign in again every 14
// days, however active they are.
const (
	StudentSessionTTL = 90 * 24 * time.Hour // student, ambassador: sliding
	StaffSessionTTL   = 14 * 24 * time.Hour // admin, university_staff, employer: absolute, from sign-in
	// RefreshMinInterval: a sliding session is re-issued at most this often (and only once it
	// is older than half its lifetime).
	RefreshMinInterval = 24 * time.Hour
	// legacyTTL is the single lifetime tokens had before they carried "i"/"a"; used to date them.
	legacyTTL = 30 * 24 * time.Hour
)

// Sliding reports whether the role's session renews itself with activity.
func Sliding(role string) bool { return role == "student" || role == "ambassador" }

// SessionTTLFor is the lifetime of a session for role.
func SessionTTLFor(role string) time.Duration {
	if Sliding(role) {
		return StudentSessionTTL
	}
	return StaffSessionTTL
}

var (
	ErrNoSecret   = errors.New("auth: SESSION_SECRET is missing or shorter than 32 bytes")
	ErrBadSession = errors.New("auth: invalid session token")
)

// Claims is the signed session payload.
type Claims struct {
	UserID  int64 `json:"u"`
	Version int   `json:"v"`
	Exp     int64 `json:"e"`
	Issued  int64 `json:"i,omitempty"` // when this token was signed (unix)
	Auth    int64 `json:"a,omitempty"` // when the user actually signed in; kept across refreshes
}

// issued and authAt date a token; tokens from before these fields existed had a 30-day life.
func (c Claims) issued() time.Time {
	if c.Issued > 0 {
		return time.Unix(c.Issued, 0)
	}
	return time.Unix(c.Exp, 0).Add(-legacyTTL)
}

func (c Claims) authAt() time.Time {
	if c.Auth > 0 {
		return time.Unix(c.Auth, 0)
	}
	return c.issued()
}

// newClaims starts a session for a user with role at now.
func newClaims(userID int64, version int, role string, now time.Time) Claims {
	return Claims{UserID: userID, Version: version, Issued: now.Unix(), Auth: now.Unix(), Exp: now.Add(SessionTTLFor(role)).Unix()}
}

// validFor applies the role rules on top of the signature/expiry check: a staff session
// ends StaffSessionTTL after sign-in even if the token was issued while the user was a
// student (role granted later) or under the old 30-day rule.
func (c Claims) validFor(role string, now time.Time) bool {
	if Sliding(role) {
		return true
	}
	return now.Before(c.authAt().Add(StaffSessionTTL))
}

// needsRefresh: a sliding session older than half its lifetime, and at least
// RefreshMinInterval old, gets a fresh token.
func (c Claims) needsRefresh(role string, now time.Time) bool {
	if !Sliding(role) {
		return false
	}
	age := now.Sub(c.issued())
	return age >= SessionTTLFor(role)/2 && age >= RefreshMinInterval
}

func secret() ([]byte, error) {
	s := config.Str("SESSION_SECRET", "")
	if len(s) < 32 {
		return nil, ErrNoSecret
	}
	return []byte(s), nil
}

// Sign returns base64url(payload) + "." + base64url(HMAC_SHA256(SESSION_SECRET, payload)).
// A zero Issued means "now" and a zero Auth means "at Issued": every token signed from now on
// is dated, only tokens from before the field existed are dated from their expiry.
func Sign(c Claims) (string, error) {
	key, err := secret()
	if err != nil {
		return "", err
	}
	if c.Issued == 0 {
		c.Issued = time.Now().Unix()
	}
	if c.Auth == 0 {
		c.Auth = c.Issued
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
// whose iframe may not keep third-party cookies. bearer is true for the header.
func tokenFrom(r *http.Request) (tok string, bearer bool) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value, false
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")), true
	}
	return "", false
}

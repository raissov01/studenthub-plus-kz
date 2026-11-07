package auth

// «Sign in with Google» (docs/GOOGLE_LOGIN.md): the OAuth 2.0 authorization-code flow run by the server, with
// PKCE (S256), state and nonce, and the OpenID Connect ID token verified here against Google's JWKS.
//
//	GET    /api/auth/google/start?next=/path[&link=1][&lang=kk][&ref=…]  -> 302 to Google
//	GET    /api/auth/google/callback?code=…&state=…                       -> 303 back to next, session cookie set
//	DELETE /api/me/google                                                 -> unlink (not the last sign-in method)
//
// Rules:
//   - FEATURE_GOOGLE_LOGIN plus GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, or every route answers 404;
//   - state, nonce and the PKCE verifier live only in a 10-minute HMAC-signed HttpOnly cookie scoped to
//     /api/auth/google/, used once (cleared on the callback whatever happens);
//   - the ID token must be RS256-signed by a key of Google's JWKS (cached by Cache-Control), with Google's
//     iss, aud = our client id, a live exp, our nonce, and email_verified = true; an unverified Google email is
//     never used for anything;
//   - a known google_sub signs in; otherwise an account holding the same verified email signs in and gets the
//     sub linked; otherwise a new account is created like an email sign-up (no consent yet: the consent screen
//     follows), with the Google-verified email;
//   - "next" is a same-origin path only; the callback answers with a relative Location, never another host;
//   - errors come back to next as ?google=<code>, success as ?google=ok / ?google=linked.

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
)

// Google's endpoints. Variables only so that tests can point them at a fake Google (httptest); never set
// from the environment.
var (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleJWKSURL  = "https://www.googleapis.com/oauth2/v3/certs"
	googleIssuers  = []string{"https://accounts.google.com", "accounts.google.com"}
	googleHTTP     = &http.Client{Timeout: 10 * time.Second}
	googleNow      = time.Now
)

const (
	googleFlowCookie = "sh_google"
	googleFlowPath   = "/api/auth/google/"
	googleFlowTTL    = 10 * time.Minute
	googleClockSkew  = time.Minute
)

// Throttles of the two browser-navigation routes, per IP (like the email login).
var (
	googleStartPerIP    = limit{30, 15 * time.Minute}
	googleCallbackPerIP = limit{30, 15 * time.Minute}
)

var MsgGoogleLast = httpx.Msg{
	KK: "Google — аккаунтқа кірудің жалғыз жолы. Алдымен Telegram-ды байланыстырыңыз немесе email арқылы кіруге құпиясөз орнатыңыз («Құпиясөзді ұмыттым»).",
	RU: "Google — единственный способ входа в аккаунт. Сначала привяжите Telegram или задайте пароль для входа по email («Забыли пароль»).",
}

// GoogleEnabled: the switch is on and the OAuth client is configured. /api/config exposes only this boolean.
func GoogleEnabled() bool {
	return config.Bool("FEATURE_GOOGLE_LOGIN", false) &&
		config.Str("GOOGLE_CLIENT_ID", "") != "" && config.Str("GOOGLE_CLIENT_SECRET", "") != ""
}

func googleRedirectURL() string {
	return config.Str("GOOGLE_REDIRECT_URL", config.SiteURL()+"/api/auth/google/callback")
}

func googleRoutes(mux httpx.Router) {
	mux.HandleFunc("GET /api/auth/google/start", googleOnly(googleStart))
	mux.HandleFunc("GET /api/auth/google/callback", googleOnly(googleCallback))
	mux.HandleFunc("DELETE /api/me/google", googleOnly(Write(Need(googleUnlink))))
}

// googleOnly answers exactly like an unknown route while Google sign-in is off.
func googleOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !GoogleEnabled() {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
		h(w, r)
	}
}

/* ---------- the flow cookie ---------- */

// googleFlow is what the callback must find again: it never leaves the browser except back to us.
type googleFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
	Link     int64  `json:"l,omitempty"` // the signed-in user who asked to link Google; 0 = sign in
	Lang     string `json:"g,omitempty"`
	Ref      string `json:"r,omitempty"`
	Exp      int64  `json:"e"`
}

func flowKey() ([]byte, error) {
	s, err := secret()
	if err != nil {
		return nil, err
	}
	return hmacSHA256(s, []byte("studenthub google oauth flow v1")), nil
}

func signFlow(f googleFlow) (string, error) {
	key, err := flowKey()
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(f)
	p := base64.RawURLEncoding.EncodeToString(body)
	return p + "." + base64.RawURLEncoding.EncodeToString(hmacSHA256(key, []byte(p))), nil
}

func readFlow(r *http.Request, now time.Time) (googleFlow, bool) {
	c, err := r.Cookie(googleFlowCookie)
	if err != nil || c.Value == "" || len(c.Value) > 2048 {
		return googleFlow{}, false
	}
	key, err := flowKey()
	if err != nil {
		return googleFlow{}, false
	}
	p, sig, ok := strings.Cut(c.Value, ".")
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err != nil || !hmac.Equal(got, hmacSHA256(key, []byte(p))) {
		return googleFlow{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(p)
	var f googleFlow
	if err != nil || json.Unmarshal(body, &f) != nil || f.State == "" || f.Nonce == "" || f.Verifier == "" {
		return googleFlow{}, false
	}
	if now.Unix() >= f.Exp {
		return googleFlow{}, false
	}
	f.Next = SafeNext(f.Next)
	return f, true
}

func setFlowCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: googleFlowCookie, Value: value, Path: googleFlowPath, MaxAge: maxAge,
		HttpOnly: true, Secure: !isLocal(r),
		// Lax: the callback is a top-level GET navigation from accounts.google.com, which Lax cookies follow
		SameSite: http.SameSiteLaxMode,
	})
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// SafeNext returns raw when it is a path on this site (not the API), "/" otherwise: no scheme, no host, no
// "//" or "/\" (protocol-relative to browsers), no backslashes or control characters, at most 512 bytes.
func SafeNext(raw string) string {
	if raw == "" || len(raw) > 512 || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.ContainsRune(raw, '\\') ||
		strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" ||
		strings.HasPrefix(u.Path, "//") || u.Path == "/api" || strings.HasPrefix(u.Path, "/api/") {
		return "/"
	}
	u.Fragment, u.RawFragment = "", ""
	q := u.Query()
	if q.Has("google") { // a status left over from an earlier round trip
		q.Del("google")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func withGoogleStatus(next, status string) string {
	u, err := url.Parse(SafeNext(next))
	if err != nil {
		return "/?google=" + url.QueryEscape(status)
	}
	q := u.Query()
	q.Set("google", status)
	u.RawQuery = q.Encode()
	return u.String()
}

// googleBack redirects the browser to next (a same-origin path) with ?google=status.
func googleBack(w http.ResponseWriter, next, status string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", withGoogleStatus(next, status))
	w.WriteHeader(http.StatusSeeOther)
}

func ipAllowed(r *http.Request, name string, l limit) bool {
	ok, _ := ratelimit.Allow(r.Context(), "g:"+name+":ip:"+httpx.ClientIP(r), l.n, l.window)
	return ok
}

/* ---------- start ---------- */

func googleStart(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	next := SafeNext(q.Get("next"))
	if !ipAllowed(r, "start", googleStartPerIP) {
		googleBack(w, next, "rate_limited")
		return
	}
	if _, err := secret(); err != nil {
		log.Printf("auth: google: %v", err)
		googleBack(w, next, "unavailable")
		return
	}
	f := googleFlow{
		State: randomToken(32), Nonce: randomToken(32), Verifier: randomToken(48), // 64-char verifier (RFC 7636: 43–128)
		Next: next, Lang: pickLang(q.Get("lang"), r), Ref: CleanRef(q.Get("ref")),
		Exp: googleNow().Add(googleFlowTTL).Unix(),
	}
	if q.Get("link") == "1" {
		u := Current(r)
		if u == nil {
			googleBack(w, next, "session")
			return
		}
		f.Link = u.ID
	}
	val, err := signFlow(f)
	if err != nil {
		log.Printf("auth: google: %v", err)
		googleBack(w, next, "unavailable")
		return
	}
	setFlowCookie(w, r, val, int(googleFlowTTL.Seconds()))
	challenge := sha256.Sum256([]byte(f.Verifier))
	v := url.Values{
		"response_type":         {"code"},
		"client_id":             {config.Str("GOOGLE_CLIENT_ID", "")},
		"redirect_uri":          {googleRedirectURL()},
		"scope":                 {"openid email profile"},
		"state":                 {f.State},
		"nonce":                 {f.Nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
		"hl":                    {f.Lang},
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", googleAuthURL+"?"+v.Encode())
	w.WriteHeader(http.StatusFound)
}

/* ---------- callback ---------- */

var (
	errGoogleTaken      = errors.New("google: account linked to another user")
	errGoogleMismatch   = errors.New("google: user linked to another Google account")
	errGoogleEmailTaken = errors.New("google: email held by an unverified account")
	errGoogleBlocked    = errors.New("google: account deleted")
	errGoogleUnverified = errors.New("google: email not verified")
	errGoogleToken      = errors.New("google: invalid id token")
)

// googleIdentity is what a verified ID token says.
type googleIdentity struct {
	Sub, Email, Name string
}

func googleCallback(w http.ResponseWriter, r *http.Request) {
	f, ok := readFlow(r, googleNow())
	setFlowCookie(w, r, "", -1) // one use, whatever happens next
	next := "/"
	if ok {
		next = f.Next
	}
	if !ipAllowed(r, "callback", googleCallbackPerIP) {
		googleBack(w, next, "rate_limited")
		return
	}
	if !ok {
		googleBack(w, next, "expired")
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" { // access_denied: the student closed Google's screen
		googleBack(w, next, "cancelled")
		return
	}
	if st := q.Get("state"); st == "" || subtle.ConstantTimeCompare([]byte(st), []byte(f.State)) != 1 {
		googleBack(w, next, "state")
		return
	}
	code := q.Get("code")
	if code == "" || len(code) > 2048 {
		googleBack(w, next, "invalid")
		return
	}
	raw, err := googleExchange(r.Context(), code, f.Verifier)
	if err != nil {
		log.Printf("auth: google token exchange: %v", err)
		googleBack(w, next, "unavailable")
		return
	}
	id, err := VerifyGoogleIDToken(r.Context(), raw, config.Str("GOOGLE_CLIENT_ID", ""), f.Nonce, googleNow())
	switch {
	case errors.Is(err, errGoogleUnverified):
		googleBack(w, next, "email_unverified")
		return
	case err != nil:
		log.Printf("auth: google id token: %v", err)
		googleBack(w, next, "invalid")
		return
	}
	if f.Link != 0 {
		googleLink(w, r, f, id)
		return
	}
	googleSignIn(w, r, f, id)
}

// googleExchange trades the code (and the PKCE verifier) for the ID token at Google's token endpoint.
func googleExchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {googleRedirectURL()},
		"client_id":     {config.Str("GOOGLE_CLIENT_ID", "")},
		"client_secret": {config.Str("GOOGLE_CLIENT_SECRET", "")},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := googleHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var t struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&t); err != nil {
		return "", fmt.Errorf("status %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || t.IDToken == "" {
		return "", fmt.Errorf("status %d, error %q", resp.StatusCode, clip(t.Error, 64)) // Google's error code only, no body
	}
	return t.IDToken, nil
}

/* ---------- ID token ---------- */

// VerifyGoogleIDToken checks an OpenID Connect ID token from Google: RS256 signature by a JWKS key, iss, aud
// (and azp when there are several audiences), exp/iat with a minute of clock skew, the nonce, a sane sub, and
// email_verified = true with a well-formed email. A token without a verified email returns errGoogleUnverified.
func VerifyGoogleIDToken(ctx context.Context, raw, clientID, nonce string, now time.Time) (googleIdentity, error) {
	if clientID == "" || nonce == "" || len(raw) > 8192 {
		return googleIdentity{}, errGoogleToken
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return googleIdentity{}, errGoogleToken
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &hdr); err != nil || hdr.Alg != "RS256" || hdr.Kid == "" {
		return googleIdentity{}, fmt.Errorf("%w: header", errGoogleToken)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return googleIdentity{}, errGoogleToken
	}
	key, err := googleKeys.key(ctx, hdr.Kid, now)
	if err != nil {
		return googleIdentity{}, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig) != nil {
		return googleIdentity{}, fmt.Errorf("%w: signature", errGoogleToken)
	}
	var c struct {
		Iss           string          `json:"iss"`
		Aud           json.RawMessage `json:"aud"`
		Azp           string          `json:"azp"`
		Sub           string          `json:"sub"`
		Exp           json.Number     `json:"exp"`
		Iat           json.Number     `json:"iat"`
		Nonce         string          `json:"nonce"`
		Email         string          `json:"email"`
		EmailVerified any             `json:"email_verified"`
		Name          string          `json:"name"`
	}
	if err := decodeSegment(parts[1], &c); err != nil {
		return googleIdentity{}, fmt.Errorf("%w: claims", errGoogleToken)
	}
	if !slices.Contains(googleIssuers, c.Iss) {
		return googleIdentity{}, fmt.Errorf("%w: iss", errGoogleToken)
	}
	var auds []string
	var one string
	if json.Unmarshal(c.Aud, &one) == nil {
		auds = []string{one}
	} else if json.Unmarshal(c.Aud, &auds) != nil {
		return googleIdentity{}, fmt.Errorf("%w: aud", errGoogleToken)
	}
	if !slices.Contains(auds, clientID) || (len(auds) > 1 && c.Azp != clientID) {
		return googleIdentity{}, fmt.Errorf("%w: aud", errGoogleToken)
	}
	exp, err1 := c.Exp.Int64()
	iat, err2 := c.Iat.Int64()
	if err1 != nil || err2 != nil || !now.Before(time.Unix(exp, 0).Add(googleClockSkew)) || time.Unix(iat, 0).After(now.Add(googleClockSkew)) {
		return googleIdentity{}, fmt.Errorf("%w: exp/iat", errGoogleToken)
	}
	if subtle.ConstantTimeCompare([]byte(c.Nonce), []byte(nonce)) != 1 {
		return googleIdentity{}, fmt.Errorf("%w: nonce", errGoogleToken)
	}
	if c.Sub == "" || len(c.Sub) > 255 || strings.ContainsFunc(c.Sub, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return googleIdentity{}, fmt.Errorf("%w: sub", errGoogleToken)
	}
	if v, _ := c.EmailVerified.(bool); !v && c.EmailVerified != "true" {
		return googleIdentity{}, errGoogleUnverified
	}
	email, ok := NormalizeEmail(c.Email)
	if !ok {
		return googleIdentity{}, errGoogleUnverified
	}
	return googleIdentity{Sub: c.Sub, Email: email, Name: clip(c.Name, 80)}, nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	return dec.Decode(v)
}

// jwks caches Google's signing keys for as long as Google's Cache-Control says (5 min … 24 h, 1 h without
// one). An unknown kid (key rotation) triggers a refetch, at most every 30 seconds.
type jwks struct {
	mu      sync.Mutex
	url     string
	keys    map[string]*rsa.PublicKey
	expires time.Time
	fetched time.Time
}

var googleKeys = &jwks{}

func (j *jwks) key(ctx context.Context, kid string, now time.Time) (*rsa.PublicKey, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.url != googleJWKSURL { // tests swap the endpoint
		j.url, j.keys, j.expires, j.fetched = googleJWKSURL, nil, time.Time{}, time.Time{}
	}
	if k := j.keys[kid]; k != nil && now.Before(j.expires) {
		return k, nil
	}
	if now.Before(j.expires) && now.Sub(j.fetched) < 30*time.Second {
		return nil, fmt.Errorf("%w: unknown kid", errGoogleToken)
	}
	keys, maxAge, err := fetchJWKS(ctx, j.url)
	if err != nil {
		return nil, fmt.Errorf("google jwks: %w", err)
	}
	j.keys, j.fetched, j.expires = keys, now, now.Add(maxAge)
	if k := keys[kid]; k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("%w: unknown kid", errGoogleToken)
}

func fetchJWKS(ctx context.Context, u string) (map[string]*rsa.PublicKey, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := googleHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty, Kid, Use, Alg, N, E string
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&set); err != nil {
		return nil, 0, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if pub.N.BitLen() < 2048 || pub.E < 3 {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, 0, errors.New("no usable keys")
	}
	return keys, cacheMaxAge(resp.Header.Get("Cache-Control")), nil
}

func cacheMaxAge(cc string) time.Duration {
	age := time.Hour
	for _, d := range strings.Split(cc, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(strings.ToLower(d)), "max-age="); ok {
			if n, err := strconv.Atoi(v); err == nil {
				age = time.Duration(n) * time.Second
			}
		}
	}
	return min(max(age, 5*time.Minute), 24*time.Hour)
}

/* ---------- accounts ---------- */

func googleFailCode(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, errGoogleTaken), errors.As(err, &pgErr) && pgErr.Code == "23505":
		return "taken"
	case errors.Is(err, errGoogleMismatch):
		return "mismatch"
	case errors.Is(err, errGoogleEmailTaken):
		return "email_taken"
	case errors.Is(err, errGoogleBlocked):
		return "blocked"
	}
	return "unavailable"
}

func auditGoogle(ctx context.Context, tx pgx.Tx, userID int64, action string) error {
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta) values ($1, $2, $1, null, '{}')`, userID, action)
	return err
}

// googleSignIn signs in (or up) with a verified Google identity.
func googleSignIn(w http.ResponseWriter, r *http.Request, f googleFlow, id googleIdentity) {
	ctx := r.Context()
	var u *User
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		u, err = ScanUser(tx.QueryRow(ctx, `update users set last_seen_at = now() where google_sub = $1 returning `+UserColumns, id.Sub))
		switch {
		case err == nil:
			if u.DeletedAt != nil {
				return errGoogleBlocked
			}
			return bootstrapAdmin(ctx, tx, u)
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		a, err := findAccount(ctx, tx, id.Email, true)
		if err != nil {
			return err
		}
		switch {
		case a != nil && a.Deleted:
			return errGoogleBlocked
		case a != nil && a.Verified:
			// the same mailbox, proven to us before and now proven by Google: one account, Google linked to it
			u, err = ScanUser(tx.QueryRow(ctx, `update users set google_sub = $2, name = coalesce(name, $3), last_seen_at = now()
				where id = $1 and google_sub is null returning `+UserColumns, a.ID, id.Sub, nullable(id.Name)))
			if errors.Is(err, pgx.ErrNoRows) {
				return errGoogleMismatch // the account already has another Google account
			}
			if err != nil {
				return err
			}
			if err := auditGoogle(ctx, tx, u.ID, "google_link"); err != nil {
				return err
			}
			return bootstrapAdmin(ctx, tx, u)
		case a != nil && !a.pending():
			return errGoogleEmailTaken
		case a != nil:
			// an unfinished email sign-up for this address: it never had a session and holds no data, and
			// Google has just proven the mailbox, so it gives way (as in POST /api/me/email/confirm)
			if _, err := tx.Exec(ctx, `delete from users where id = $1`, a.ID); err != nil {
				return err
			}
		}
		u, err = ScanUser(tx.QueryRow(ctx, `insert into users (email, email_verified_at, google_sub, name, lang, referred_by, last_seen_at)
			values ($1, now(), $2, $3, $4, $5, now()) returning `+UserColumns,
			id.Email, id.Sub, nullable(id.Name), f.Lang, nullable(f.Ref)))
		if err != nil {
			return err
		}
		return bootstrapAdmin(ctx, tx, u)
	})
	if err != nil {
		if code := googleFailCode(err); code == "unavailable" {
			log.Printf("auth: google sign-in: %v", err)
		}
		googleBack(w, f.Next, googleFailCode(err))
		return
	}
	tok, err := Sign(newClaims(u.ID, u.SessionVersion, u.Role, time.Now()))
	if err != nil {
		log.Printf("auth: %v", err)
		googleBack(w, f.Next, "unavailable")
		return
	}
	setCookie(w, r, tok, int(SessionTTLFor(u.Role).Seconds()))
	googleBack(w, f.Next, "ok")
}

// googleLink attaches the Google identity to the signed-in account that started the flow. A Telegram-only
// account also gets the Google-verified email when no real account holds it (so ADMIN_EMAILS applies).
func googleLink(w http.ResponseWriter, r *http.Request, f googleFlow, id googleIdentity) {
	cur := Current(r)
	if cur == nil || cur.ID != f.Link {
		googleBack(w, f.Next, "session")
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var owner int64
		err := tx.QueryRow(ctx, `select id from users where google_sub = $1 for update`, id.Sub).Scan(&owner)
		switch {
		case err == nil && owner == cur.ID:
			return nil // already linked
		case err == nil:
			return errGoogleTaken
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		u, err := ScanUser(tx.QueryRow(ctx, `update users set google_sub = $2, name = coalesce(name, $3)
			where id = $1 and google_sub is null and deleted_at is null returning `+UserColumns, cur.ID, id.Sub, nullable(id.Name)))
		if errors.Is(err, pgx.ErrNoRows) {
			return errGoogleMismatch
		}
		if err != nil {
			return err
		}
		if u.Email == nil {
			a, err := findAccount(ctx, tx, id.Email, true)
			if err != nil {
				return err
			}
			if a != nil && a.pending() {
				if _, err := tx.Exec(ctx, `delete from users where id = $1`, a.ID); err != nil {
					return err
				}
				a = nil
			}
			if a == nil {
				if u, err = ScanUser(tx.QueryRow(ctx, `update users set email = $2, email_verified_at = now()
					where id = $1 returning `+UserColumns, cur.ID, id.Email)); err != nil {
					return err
				}
			}
		}
		if err := auditGoogle(ctx, tx, u.ID, "google_link"); err != nil {
			return err
		}
		return bootstrapAdmin(ctx, tx, u)
	})
	if err != nil {
		if code := googleFailCode(err); code == "unavailable" {
			log.Printf("auth: google link %d: %v", cur.ID, err)
		}
		googleBack(w, f.Next, googleFailCode(err))
		return
	}
	googleBack(w, f.Next, "linked")
}

var errLastMethod = errors.New("google: last sign-in method")

// DELETE /api/me/google: the account keeps another way in (Telegram, or a verified email with a password).
func googleUnlink(w http.ResponseWriter, r *http.Request, u *User) {
	ctx := r.Context()
	var out *User
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var linked, hasTG, emailPw bool
		err := tx.QueryRow(ctx, `select google_sub is not null, telegram_id is not null,
				email is not null and email_verified_at is not null and exists(select 1 from user_passwords p where p.user_id = users.id)
			from users where id = $1 for update`, u.ID).Scan(&linked, &hasTG, &emailPw)
		if err != nil {
			return err
		}
		if linked {
			if !hasTG && !emailPw {
				return errLastMethod
			}
			if _, err := tx.Exec(ctx, `update users set google_sub = null where id = $1`, u.ID); err != nil {
				return err
			}
			if err := auditGoogle(ctx, tx, u.ID, "google_unlink"); err != nil {
				return err
			}
		}
		out, err = ScanUser(tx.QueryRow(ctx, `select `+UserColumns+` from users where id = $1`, u.ID))
		return err
	})
	switch {
	case errors.Is(err, errLastMethod):
		httpx.Error(w, r, http.StatusConflict, "last_method", MsgGoogleLast)
	case err != nil:
		fail(w, r, "google unlink", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"user": out})
	}
}

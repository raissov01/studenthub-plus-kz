package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

const testClientID = "siva-test.apps.googleusercontent.com"

// fakeGoogle is Google's token endpoint and JWKS on httptest. A code is registered with the PKCE challenge
// the start redirect carried and the ID token to hand out; the token endpoint checks the client credentials,
// the redirect URI and the verifier exactly like Google would.
type fakeGoogle struct {
	t       *testing.T
	srv     *httptest.Server
	key     *rsa.PrivateKey
	kid     string
	mu      sync.Mutex
	codes   map[string]fakeCode
	jwksHit atomic.Int64
}

type fakeCode struct{ challenge, idToken string }

var subSeq atomic.Int64

func newGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGoogle{t: t, key: key, kid: fmt.Sprintf("kid-%d", subSeq.Add(1)), codes: map[string]fakeCode{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/certs":
			g.jwksHit.Add(1)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": g.kid,
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
		case "/token":
			_ = r.ParseForm()
			f := r.PostForm
			g.mu.Lock()
			c, ok := g.codes[f.Get("code")]
			delete(g.codes, f.Get("code"))
			g.mu.Unlock()
			sum := sha256.Sum256([]byte(f.Get("code_verifier")))
			if !ok || f.Get("grant_type") != "authorization_code" || f.Get("client_id") != testClientID ||
				f.Get("client_secret") != "test-secret" || f.Get("redirect_uri") != "https://siva.test/api/auth/google/callback" ||
				base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"id_token": c.idToken, "access_token": "ya29.fake", "token_type": "Bearer"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.srv.Close)
	oldAuth, oldToken, oldJWKS := googleAuthURL, googleTokenURL, googleJWKSURL
	googleAuthURL, googleTokenURL, googleJWKSURL = g.srv.URL+"/auth", g.srv.URL+"/token", g.srv.URL+"/certs"
	t.Cleanup(func() { googleAuthURL, googleTokenURL, googleJWKSURL = oldAuth, oldToken, oldJWKS })
	t.Setenv("FEATURE_GOOGLE_LOGIN", "true")
	t.Setenv("GOOGLE_CLIENT_ID", testClientID)
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	t.Setenv("SITE_URL", "https://siva.test")
	return g
}

// claims are a good ID token's claims for nonce; edit the map to break one.
func (g *fakeGoogle) claims(nonce, sub, email string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": "https://accounts.google.com", "aud": testClientID, "azp": testClientID, "sub": sub,
		"email": email, "email_verified": true, "name": "Айгерім Google", "nonce": nonce,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

func (g *fakeGoogle) sign(hdr, claims map[string]any, key *rsa.PrivateKey) string {
	h, _ := json.Marshal(hdr)
	c, _ := json.Marshal(claims)
	p := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	sum := sha256.Sum256([]byte(p))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		g.t.Fatal(err)
	}
	return p + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (g *fakeGoogle) idToken(claims map[string]any) string {
	return g.sign(map[string]any{"alg": "RS256", "kid": g.kid, "typ": "JWT"}, claims, g.key)
}

func get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "siva.test"
	r.Header.Set("X-Real-Ip", uniqueIP())
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	mux().ServeHTTP(w, r)
	return w
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// round is one trip through Google: the start redirect and what the browser kept from it.
type round struct {
	flow                        *http.Cookie
	state, nonce, challenge, to string
}

func (g *fakeGoogle) start(t *testing.T, query string, session *http.Cookie) round {
	t.Helper()
	w := get("/api/auth/google/start?"+query, session)
	if w.Code != http.StatusFound {
		t.Fatalf("start: %d %s (Location %s)", w.Code, w.Body, w.Header().Get("Location"))
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil || loc.Scheme+"://"+loc.Host+loc.Path != g.srv.URL+"/auth" {
		t.Fatalf("start redirect: %s", w.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("scope") != "openid email profile" || q.Get("response_type") != "code" || q.Get("client_id") != testClientID ||
		q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "https://siva.test/api/auth/google/callback" {
		t.Fatalf("start params: %v", q)
	}
	flow := cookieNamed(w, googleFlowCookie)
	if flow == nil || !flow.HttpOnly || !flow.Secure || flow.Path != googleFlowPath || flow.MaxAge != 600 || flow.SameSite != http.SameSiteLaxMode {
		t.Fatalf("flow cookie: %+v", flow)
	}
	for _, v := range []string{q.Get("state"), q.Get("nonce"), q.Get("code_challenge")} {
		if len(v) < 43 {
			t.Fatalf("weak flow value %q", v)
		}
	}
	return round{flow: flow, state: q.Get("state"), nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
}

// finish registers the code with idToken and calls the callback; it returns the response and its Location.
func (g *fakeGoogle) finish(t *testing.T, rd round, idToken string, session *http.Cookie) (*httptest.ResponseRecorder, string) {
	t.Helper()
	code := fmt.Sprintf("4/code-%d", subSeq.Add(1))
	g.mu.Lock()
	g.codes[code] = fakeCode{challenge: rd.challenge, idToken: idToken}
	g.mu.Unlock()
	w := get("/api/auth/google/callback?state="+url.QueryEscape(rd.state)+"&code="+url.QueryEscape(code), rd.flow, session)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("callback: %d %s", w.Code, w.Body)
	}
	if c := cookieNamed(w, googleFlowCookie); c == nil || c.MaxAge >= 0 {
		t.Fatal("flow cookie not cleared")
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//") {
		t.Fatalf("callback left the site: %s", loc)
	}
	return w, loc
}

// login runs a whole sign-in for sub/email and returns the session cookie (nil on failure) and Location.
func (g *fakeGoogle) login(t *testing.T, next, sub, email string) (*http.Cookie, string) {
	t.Helper()
	rd := g.start(t, "next="+url.QueryEscape(next), nil)
	w, loc := g.finish(t, rd, g.idToken(g.claims(rd.nonce, sub, email)), nil)
	return cookieNamed(w, CookieName), loc
}

func newSub() string { return fmt.Sprintf("1098765432%d", subSeq.Add(1)) }

func TestGoogleOffIs404(t *testing.T) {
	newGoogle(t)
	for _, env := range []map[string]string{
		{"FEATURE_GOOGLE_LOGIN": "false"},
		{"FEATURE_GOOGLE_LOGIN": ""},
		{"GOOGLE_CLIENT_ID": ""},
		{"GOOGLE_CLIENT_SECRET": ""},
	} {
		t.Run(fmt.Sprint(env), func(t *testing.T) {
			for k, v := range env {
				t.Setenv(k, v)
			}
			if GoogleEnabled() {
				t.Fatal("enabled")
			}
			for _, w := range []*httptest.ResponseRecorder{
				get("/api/auth/google/start?next=/"),
				get("/api/auth/google/callback?code=x&state=y"),
				post(t, "/api/me/google", nil, func(r *http.Request) { r.Method = "DELETE" }),
			} {
				if w.Code != 404 || errCode(w) != "not_found" {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
			}
		})
	}
}

func TestGoogleLoginCreatesAccount(t *testing.T) {
	testdb.Need(t)
	g := newGoogle(t)
	sub, email := newSub(), strings.ToLower(newEmail())
	sess, loc := g.login(t, "/events?tab=my", sub, strings.ToUpper(email))
	if loc != "/events?google=ok&tab=my" || sess == nil {
		t.Fatalf("location %s, cookie %v", loc, sess)
	}
	id := whoami(sess)
	if id == "nobody" {
		t.Fatal("no session")
	}
	if n := count(t, `select count(*) from users where id = $1 and google_sub = $2 and email = $3 and email_verified_at is not null
		and consent_at is null and telegram_id is null and role = 'student' and name = 'Айгерім Google'`, mustAtoi(id), sub, email); n != 1 {
		t.Fatal("account not created as expected")
	}
	// the same Google account signs in to the same row; the JWKS was fetched once (cached)
	sess2, _ := g.login(t, "/", sub, email)
	if whoami(sess2) != id {
		t.Fatal("second login is another account")
	}
	if g.jwksHit.Load() != 1 {
		t.Fatalf("jwks fetched %d times", g.jwksHit.Load())
	}
	// the sub, not the email, identifies the Google account: a changed Gmail address still reaches the row
	sess3, _ := g.login(t, "/", sub, strings.ToLower(newEmail()))
	if whoami(sess3) != id {
		t.Fatal("changed email lost the account")
	}
	// /api/me shape: googleLinked, never the sub
	r := httptest.NewRequest("GET", "/x", nil)
	r.AddCookie(sess)
	if u := Current(r); u == nil || !u.GoogleLinked {
		t.Fatal("googleLinked")
	} else if b, _ := json.Marshal(u); strings.Contains(string(b), sub) {
		t.Fatal("sub leaked")
	}
}

func TestGoogleUnverifiedEmailRejected(t *testing.T) {
	testdb.Need(t)
	g := newGoogle(t)
	for _, v := range []any{false, "false", nil} {
		email := strings.ToLower(newEmail())
		rd := g.start(t, "next=/profile", nil)
		c := g.claims(rd.nonce, newSub(), email)
		c["email_verified"] = v
		w, loc := g.finish(t, rd, g.idToken(c), nil)
		if loc != "/profile?google=email_unverified" || cookieNamed(w, CookieName) != nil {
			t.Fatalf("%v: %s", v, loc)
		}
		if n := count(t, `select count(*) from users where email = $1`, email); n != 0 {
			t.Fatal("account created from an unverified email")
		}
	}
	// and an existing account holding that address is not signed in by it
	in := fakeResend(t)
	email := newEmail()
	signUp(t, in, email, goodPw)
	rd := g.start(t, "", nil)
	c := g.claims(rd.nonce, newSub(), email)
	c["email_verified"] = false
	if w, loc := g.finish(t, rd, g.idToken(c), nil); loc != "/?google=email_unverified" || cookieNamed(w, CookieName) != nil {
		t.Fatal(loc)
	}
	if n := count(t, `select count(*) from users where lower(email) = lower($1) and google_sub is not null`, email); n != 0 {
		t.Fatal("linked through an unverified email")
	}
}

func TestGoogleBadTokensRejected(t *testing.T) {
	testdb.Need(t)
	g := newGoogle(t)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	cases := map[string]func(rd round) string{
		"nonce": func(rd round) string { c := g.claims("another-nonce", newSub(), newEmail()); return g.idToken(c) },
		"aud": func(rd round) string {
			c := g.claims(rd.nonce, newSub(), newEmail())
			c["aud"] = "someone-else.apps.googleusercontent.com"
			return g.idToken(c)
		},
		"aud list without azp": func(rd round) string {
			c := g.claims(rd.nonce, newSub(), newEmail())
			c["aud"], c["azp"] = []string{testClientID, "other"}, "other"
			return g.idToken(c)
		},
		"iss": func(rd round) string {
			c := g.claims(rd.nonce, newSub(), newEmail())
			c["iss"] = "https://accounts.evil.example"
			return g.idToken(c)
		},
		"exp": func(rd round) string {
			c := g.claims(rd.nonce, newSub(), newEmail())
			c["iat"], c["exp"] = time.Now().Add(-2*time.Hour).Unix(), time.Now().Add(-2*time.Minute).Unix()
			return g.idToken(c)
		},
		"iat in the future": func(rd round) string {
			c := g.claims(rd.nonce, newSub(), newEmail())
			c["iat"] = time.Now().Add(10 * time.Minute).Unix()
			return g.idToken(c)
		},
		"foreign key": func(rd round) string {
			return g.sign(map[string]any{"alg": "RS256", "kid": g.kid}, g.claims(rd.nonce, newSub(), newEmail()), other)
		},
		"unknown kid": func(rd round) string {
			return g.sign(map[string]any{"alg": "RS256", "kid": "nope"}, g.claims(rd.nonce, newSub(), newEmail()), g.key)
		},
		"alg none": func(rd round) string {
			tok := g.idToken(g.claims(rd.nonce, newSub(), newEmail()))
			p := strings.Split(tok, ".")
			h, _ := json.Marshal(map[string]any{"alg": "none", "kid": g.kid})
			return base64.RawURLEncoding.EncodeToString(h) + "." + p[1] + "."
		},
		"edited claims": func(rd round) string {
			tok := g.idToken(g.claims(rd.nonce, newSub(), newEmail()))
			p := strings.Split(tok, ".")
			c, _ := json.Marshal(g.claims(rd.nonce, "victim-sub", newEmail()))
			return p[0] + "." + base64.RawURLEncoding.EncodeToString(c) + "." + p[2]
		},
		"empty sub": func(rd round) string { return g.idToken(g.claims(rd.nonce, "", newEmail())) },
	}
	before := count(t, `select count(*) from users`)
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			rd := g.start(t, "next=/", nil)
			w, loc := g.finish(t, rd, mk(rd), nil)
			if loc != "/?google=invalid" || cookieNamed(w, CookieName) != nil {
				t.Fatalf("location %s", loc)
			}
		})
	}
	// state: wrong, missing; flow cookie: missing, forged, expired; Google said access_denied
	rd := g.start(t, "next=/calendar", nil)
	for name, tc := range map[string]struct {
		query  string
		cookie *http.Cookie
		want   string
	}{
		"wrong state":   {"state=x&code=c", rd.flow, "/calendar?google=state"},
		"no state":      {"code=c", rd.flow, "/calendar?google=state"},
		"no cookie":     {"state=" + rd.state + "&code=c", nil, "/?google=expired"},
		"forged cookie": {"state=" + rd.state + "&code=c", &http.Cookie{Name: googleFlowCookie, Value: rd.flow.Value[:len(rd.flow.Value)-2] + "AA"}, "/?google=expired"},
		"cancelled":     {"error=access_denied&state=" + rd.state, rd.flow, "/calendar?google=cancelled"},
	} {
		w := get("/api/auth/google/callback?"+tc.query, tc.cookie)
		if w.Code != 303 || w.Header().Get("Location") != tc.want || cookieNamed(w, CookieName) != nil {
			t.Errorf("%s: %d %s", name, w.Code, w.Header().Get("Location"))
		}
	}
	// a flow cookie older than 10 minutes
	googleNow = func() time.Time { return time.Now().Add(11 * time.Minute) }
	w := get("/api/auth/google/callback?state="+rd.state+"&code=c", rd.flow)
	googleNow = time.Now
	if w.Header().Get("Location") != "/?google=expired" {
		t.Errorf("expired flow: %s", w.Header().Get("Location"))
	}
	// a wrong PKCE verifier: Google refuses the code
	rd = g.start(t, "", nil)
	g.mu.Lock()
	g.codes["4/pkce"] = fakeCode{challenge: "not-the-challenge", idToken: g.idToken(g.claims(rd.nonce, newSub(), newEmail()))}
	g.mu.Unlock()
	if w := get("/api/auth/google/callback?state="+rd.state+"&code=4/pkce", rd.flow); w.Header().Get("Location") != "/?google=unavailable" {
		t.Errorf("pkce: %s", w.Header().Get("Location"))
	}
	if after := count(t, `select count(*) from users`); after != before {
		t.Fatalf("users %d -> %d", before, after)
	}
}

func TestGoogleLinksExistingEmailAccountAndAdminBootstrap(t *testing.T) {
	testdb.Need(t)
	g := newGoogle(t)
	in := fakeResend(t)
	email := newEmail()
	_, id := signUp(t, in, email, goodPw)
	sub := newSub()
	sess, loc := g.login(t, "/", sub, strings.ToLower(email))
	if loc != "/?google=ok" || whoami(sess) != fmt.Sprint(id) {
		t.Fatalf("not the email account: %s", loc)
	}
	if count(t, `select count(*) from users where id = $1 and google_sub = $2`, id, sub) != 1 ||
		count(t, `select count(*) from audit_log where target_id = $1 and action = 'google_link'`, id) != 1 {
		t.Fatal("google_sub not linked or not audited")
	}
	// the account already has a Google account: another Google account with the same email is refused
	if s, loc := g.login(t, "/", newSub(), email); loc != "/?google=mismatch" || s != nil {
		t.Fatal(loc)
	}
	// an unfinished email sign-up gives way to Google (as with POST /api/me/email/confirm)
	pending := newEmail()
	if w := post(t, "/api/auth/email/register", map[string]string{"email": pending, "password": goodPw, "lang": "kk"}, nil); w.Code != 200 {
		t.Fatal(w.Body)
	}
	s, _ := g.login(t, "/", newSub(), pending)
	if s == nil || count(t, `select count(*) from users where lower(email) = lower($1) and google_sub is not null`, pending) != 1 {
		t.Fatal("pending sign-up not replaced")
	}
	// ADMIN_EMAILS applies to a Google-verified email, audited once
	admin := strings.ToLower(newEmail())
	t.Setenv("ADMIN_EMAILS", "x@example.kz, "+strings.ToUpper(admin))
	asub := newSub()
	g.login(t, "/", asub, admin)
	g.login(t, "/", asub, admin)
	if count(t, `select count(*) from users where email = $1 and role = 'admin'`, admin) != 1 ||
		count(t, `select count(*) from audit_log l join users u on u.id = l.target_id where u.email = $1
			and l.action = 'role_grant' and l.reason = 'ADMIN_EMAILS bootstrap'`, admin) != 1 {
		t.Fatal("admin bootstrap")
	}
	// a deleted account is not signed in to
	exec(t, `update users set deleted_at = now() where email = $1`, admin)
	if s, loc := g.login(t, "/", asub, admin); loc != "/?google=blocked" || s != nil {
		t.Fatal(loc)
	}
}

func TestGoogleNoOpenRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "/",
		"/":                            "/",
		"/events?tab=my#top":           "/events?tab=my",
		"/plan?google=ok&x=1":          "/plan?x=1",
		"https://evil.example/":        "/",
		"//evil.example/":              "/",
		"/\\evil.example":              "/",
		"\\\\evil.example":             "/",
		"///evil.example":              "/",
		"/%2F%2Fevil.example":          "/",
		"javascript:alert(1)":          "/",
		"evil.example":                 "/",
		"/ok\r\nSet-Cookie: x=1":       "/",
		"/ok\tx":                       "/",
		"/api/auth/google/start":       "/",
		"/api":                         "/",
		"/" + strings.Repeat("a", 600): "/",
	} {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
	testdb.Need(t)
	g := newGoogle(t)
	for _, next := range []string{"https://evil.example/x", "//evil.example", "/\\evil.example"} {
		_, loc := g.login(t, next, newSub(), newEmail())
		if loc != "/?google=ok" {
			t.Fatalf("%s -> %s", next, loc)
		}
	}
	// the cookie is signed: the return path in it cannot be edited
	rd := g.start(t, "next=/events", nil)
	p, _, _ := strings.Cut(rd.flow.Value, ".")
	body, _ := base64.RawURLEncoding.DecodeString(p)
	edited := strings.Replace(string(body), `"/events"`, `"//evil.example"`, 1)
	rd.flow = &http.Cookie{Name: googleFlowCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(edited)) + rd.flow.Value[len(p):]}
	if w := get("/api/auth/google/callback?state="+rd.state+"&code=c", rd.flow); w.Header().Get("Location") != "/?google=expired" {
		t.Fatal(w.Header().Get("Location"))
	}
}

func del(t *testing.T, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return post(t, path, nil, func(r *http.Request) {
		r.Method = "DELETE"
		if cookie != nil {
			r.AddCookie(cookie)
		}
	})
}

func TestGoogleLinkAndUnlinkFromSettings(t *testing.T) {
	testdb.Need(t)
	g := newGoogle(t)
	// a Telegram account links Google; it also gets the Google-verified email
	w, _ := loginWebApp(t, 7_000_000+subSeq.Add(1))
	tgSess := sessionCookie(t, w)
	tgID := mustAtoi(whoami(tgSess))
	sub, email := newSub(), strings.ToLower(newEmail())
	rd := g.start(t, "link=1&next=/settings", tgSess)
	w2, loc := g.finish(t, rd, g.idToken(g.claims(rd.nonce, sub, email)), tgSess)
	if loc != "/settings?google=linked" || cookieNamed(w2, CookieName) != nil {
		t.Fatalf("link: %s", loc)
	}
	if count(t, `select count(*) from users where id = $1 and google_sub = $2 and email = $3 and email_verified_at is not null`, tgID, sub, email) != 1 {
		t.Fatal("not linked")
	}
	// Google now signs in to that account
	if s, _ := g.login(t, "/", sub, email); whoami(s) != fmt.Sprint(tgID) {
		t.Fatal("Google sign-in after linking")
	}
	// another account cannot link the same Google account
	w, _ = loginWebApp(t, 7_000_000+subSeq.Add(1))
	other := sessionCookie(t, w)
	rd = g.start(t, "link=1&next=/settings", other)
	if _, loc := g.finish(t, rd, g.idToken(g.claims(rd.nonce, sub, email)), other); loc != "/settings?google=taken" {
		t.Fatal(loc)
	}
	// linking needs the session that started it: without one, start refuses; a different session at the callback too
	if w := get("/api/auth/google/start?link=1&next=/settings"); w.Code != 303 || w.Header().Get("Location") != "/settings?google=session" {
		t.Fatalf("link without session: %d %s", w.Code, w.Header().Get("Location"))
	}
	rd = g.start(t, "link=1&next=/settings", other)
	if _, loc := g.finish(t, rd, g.idToken(g.claims(rd.nonce, newSub(), newEmail())), tgSess); loc != "/settings?google=session" {
		t.Fatal(loc)
	}
	// unlink: Telegram stays, so it works (and is audited)
	if w := del(t, "/api/me/google", tgSess); w.Code != 200 || body(w)["user"].(map[string]any)["googleLinked"] != false {
		t.Fatalf("unlink: %d %s", w.Code, w.Body)
	}
	if count(t, `select count(*) from users where id = $1 and google_sub is null`, tgID) != 1 ||
		count(t, `select count(*) from audit_log where target_id = $1 and action = 'google_unlink'`, tgID) != 1 {
		t.Fatal("unlink not stored")
	}
	// a Google-only account cannot remove its only way in
	gs, _ := g.login(t, "/", newSub(), newEmail())
	if w := del(t, "/api/me/google", gs); w.Code != 409 || errCode(w) != "last_method" {
		t.Fatalf("last method: %d %s", w.Code, w.Body)
	}
	// ... until it has a password for its verified email
	exec(t, `insert into user_passwords (user_id, hash) values ($1, $2)`, mustAtoi(whoami(gs)), dummyHash())
	if w := del(t, "/api/me/google", gs); w.Code != 200 {
		t.Fatalf("unlink with password: %d %s", w.Code, w.Body)
	}
	if w := del(t, "/api/me/google", nil); w.Code != 401 {
		t.Fatalf("anonymous unlink: %d", w.Code)
	}
}

func TestGoogleStartThrottled(t *testing.T) {
	testdb.Need(t)
	newGoogle(t)
	ip := uniqueIP()
	var last *httptest.ResponseRecorder
	for i := 0; i <= googleStartPerIP.n; i++ {
		r := httptest.NewRequest("GET", "/api/auth/google/start?next=/x", nil)
		r.Host = "siva.test"
		r.Header.Set("X-Real-Ip", ip)
		last = httptest.NewRecorder()
		mux().ServeHTTP(last, r)
	}
	if last.Code != 303 || last.Header().Get("Location") != "/x?google=rate_limited" {
		t.Fatalf("%d %s", last.Code, last.Header().Get("Location"))
	}
}

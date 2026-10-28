package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/mail"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// inbox is a fake Resend API: it records every POST /emails.
type inbox struct {
	mu   sync.Mutex
	msgs []sentMail
	auth []string
}

type sentMail struct {
	From, Subject, Text, HTML string
	To                        []string
}

func fakeResend(t *testing.T) *inbox {
	t.Helper()
	in := &inbox{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/emails" {
			http.Error(w, "nope", 404)
			return
		}
		var m sentMail
		_ = json.NewDecoder(r.Body).Decode(&m)
		in.mu.Lock()
		in.msgs = append(in.msgs, m)
		in.auth = append(in.auth, r.Header.Get("Authorization"))
		in.mu.Unlock()
		w.Write([]byte(`{"id":"fake"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("MAIL_FROM", "Student Hub <noreply@test.kz>")
	t.Setenv("RESEND_API_BASE", srv.URL)
	t.Setenv("MAIL_DEV_LOG", "false")
	return in
}

// to returns every message sent to addr so far (after background sends finish).
func (in *inbox) to(addr string) []sentMail {
	mail.Wait()
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []sentMail
	for _, m := range in.msgs {
		if len(m.To) == 1 && m.To[0] == addr {
			out = append(out, m)
		}
	}
	return out
}

var reCode = regexp.MustCompile(`\b\d{6}\b`)

// code is the 6-digit code of the latest message to addr; the subject must not carry it.
func (in *inbox) code(t *testing.T, addr string) string {
	t.Helper()
	msgs := in.to(addr)
	if len(msgs) == 0 {
		t.Fatalf("no email to %s", addr)
	}
	m := msgs[len(msgs)-1]
	if reCode.MatchString(m.Subject) {
		t.Fatalf("code in subject: %q", m.Subject)
	}
	c := reCode.FindString(m.Text)
	if c == "" || !strings.Contains(m.HTML, c) {
		t.Fatalf("no code in %q", m.Text)
	}
	return c
}

var mailSeq atomic.Int64

func newEmail() string { return fmt.Sprintf("Student%d@Example.KZ", mailSeq.Add(1)) }

const goodPw = "kokjiek-tau-2026"

func body(w *httptest.ResponseRecorder) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func errCode(w *httptest.ResponseRecorder) string {
	e, _ := body(w)["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func fromIP(ip string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("X-Real-Ip", ip) }
}

// signUp registers and verifies an email account; it returns the session cookie and id.
func signUp(t *testing.T, in *inbox, email, pw string) (*http.Cookie, int64) {
	t.Helper()
	if w := post(t, "/api/auth/email/register", map[string]string{"email": email, "password": pw, "lang": "kk"}, nil); w.Code != 200 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	norm := strings.ToLower(email)
	w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": in.code(t, norm), "password": pw}, nil)
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	u := body(w)["user"].(map[string]any)
	return sessionCookie(t, w), int64(u["id"].(float64))
}

func TestEmailRegisterVerifySession(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := newEmail()
	norm := strings.ToLower(email)
	w := post(t, "/api/auth/email/register", map[string]string{"email": "  " + email + " ", "password": goodPw, "lang": "ru", "ref": "amb_kz1"}, nil)
	if w.Code != 200 || body(w)["ok"] != true || body(w)["resendAfter"].(float64) != 60 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("register must not sign in before the code")
	}
	msgs := in.to(norm)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "подтвердите") || msgs[0].From != "Student Hub <noreply@test.kz>" {
		t.Fatalf("mail: %+v", msgs)
	}
	if in.auth[len(in.auth)-1] != "Bearer re_test_key" {
		t.Fatal("resend auth header")
	}
	code := in.code(t, norm)
	// stored normalized, unverified, with an argon2id hash and only an HMAC of the code
	if n := count(t, `select count(*) from users u join user_passwords p on p.user_id = u.id
		where u.email = $1 and u.email_verified_at is null and u.telegram_id is null and u.referred_by = 'amb_kz1'
		and p.hash like '$argon2id$v=19$m=19456,t=2,p=1$%'`, norm); n != 1 {
		t.Fatal("pending row not stored as expected")
	}
	if n := count(t, `select count(*) from auth_codes where email = $1 and code_hash <> convert_to($2, 'UTF8')`, norm, code); n != 1 {
		t.Fatal("code row")
	}
	// a pending account cannot sign in: right password -> 403 email_not_verified
	if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, nil); w.Code != 403 || errCode(w) != "email_not_verified" {
		t.Fatalf("login before verify: %d %s", w.Code, w.Body)
	}
	// verify needs the password too (nobody can pre-set a password for someone else's address)
	if w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": code, "password": "wrong-password-1"}, nil); w.Code != 400 || errCode(w) != "invalid_code" {
		t.Fatalf("verify with wrong password: %d %s", w.Code, w.Body)
	}
	w = post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": code, "password": goodPw}, nil)
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	out := body(w)
	u := out["user"].(map[string]any)
	if u["email"] != norm || u["emailVerified"] != true || u["telegramId"] != nil || u["lang"] != "ru" || out["needsConsent"] != true {
		t.Fatalf("user: %v", out)
	}
	if _, ok := out["token"]; ok {
		t.Fatal("email sign-in must not hand out a bearer token")
	}
	c := sessionCookie(t, w)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie: %+v", c)
	}
	id := strconv.FormatInt(int64(u["id"].(float64)), 10)
	if whoami(c) != id {
		t.Fatal("cookie does not sign in")
	}
	// the code is single-use
	if w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": code, "password": goodPw}, nil); w.Code != 400 {
		t.Fatalf("code reused: %d", w.Code)
	}
	// and now plain login works, case-insensitively
	w = post(t, "/api/auth/email/login", map[string]string{"email": strings.ToUpper(norm), "password": goodPw}, nil)
	if w.Code != 200 || whoami(sessionCookie(t, w)) != id {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	if n := count(t, "select count(*) from auth_codes a join users u on u.id = a.user_id where u.email = $1", norm); n != 0 {
		t.Fatal("codes left after verify")
	}
}

func TestEmailCodeAttemptsLockout(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	post(t, "/api/auth/email/register", map[string]string{"email": email, "password": goodPw}, nil)
	code := in.code(t, email)
	wrong := fmt.Sprintf("%06d", (mustAtoi(code)+1)%1_000_000)
	for i := 0; i < CodeMaxAttempts; i++ {
		if w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": wrong, "password": goodPw}, nil); w.Code != 400 || errCode(w) != "invalid_code" {
			t.Fatalf("attempt %d: %d %s", i, w.Code, w.Body)
		}
	}
	// the code is gone after 5 wrong tries: even the right one no longer works
	if w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": code, "password": goodPw}, nil); w.Code != 400 || errCode(w) != "invalid_code" {
		t.Fatalf("right code after lockout: %d %s", w.Code, w.Body)
	}
	if n := count(t, "select count(*) from auth_codes a join users u on u.id = a.user_id where u.email = $1", email); n != 0 {
		t.Fatal("locked code not deleted")
	}
	// resend: within 60 s of the previous code nothing new is sent
	before := len(in.to(email))
	if w := post(t, "/api/auth/email/resend", map[string]string{"email": email}, nil); w.Code != 200 {
		t.Fatalf("resend: %d", w.Code)
	}
	if len(in.to(email)) != before+1 {
		t.Fatal("resend after lockout should send a new code (old row was deleted)")
	}
	if w := post(t, "/api/auth/email/resend", map[string]string{"email": email}, nil); w.Code != 200 || len(in.to(email)) != before+1 {
		t.Fatalf("second resend inside cooldown sent mail: %d", w.Code)
	}
	exec(t, `update auth_codes set created_at = now() - interval '2 minutes' where email = $1`, email)
	post(t, "/api/auth/email/resend", map[string]string{"email": email}, nil)
	if len(in.to(email)) != before+2 {
		t.Fatal("resend after cooldown sent nothing")
	}
	w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": in.code(t, email), "password": goodPw}, nil)
	if w.Code != 200 {
		t.Fatalf("verify with fresh code: %d %s", w.Code, w.Body)
	}
}

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func TestEmailCodeExpiry(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	post(t, "/api/auth/email/register", map[string]string{"email": email, "password": goodPw}, nil)
	code := in.code(t, email)
	if n := count(t, `select count(*) from auth_codes where email = $1
		and expires_at between now() + interval '14 minutes' and now() + interval '15 minutes 5 seconds'`, email); n != 1 {
		t.Fatal("code ttl is not 15 minutes")
	}
	exec(t, `update auth_codes set expires_at = now() - interval '1 second' where email = $1`, email)
	if w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": code, "password": goodPw}, nil); w.Code != 400 || errCode(w) != "invalid_code" {
		t.Fatalf("expired code: %d %s", w.Code, w.Body)
	}
	if n := count(t, "select count(*) from auth_codes where email = $1", email); n != 0 {
		t.Fatal("expired code not cleaned up")
	}
}

func TestEmailLoginThrottled(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	signUp(t, in, email, goodPw)
	for i := 0; i < loginPerEmail.n; i++ {
		// every try from another IP: the per-address limit still applies
		if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": "not-the-password"}, nil); w.Code != 401 || errCode(w) != "invalid_credentials" {
			t.Fatalf("wrong password %d: %d %s", i, w.Code, w.Body)
		}
	}
	w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("throttle: %d %s", w.Code, w.Body)
	}
	// per IP: one address after another from the same IP
	ip := uniqueIP()
	var last int
	for i := 0; i <= loginPerIP.n; i++ {
		last = post(t, "/api/auth/email/login", map[string]string{"email": newEmail(), "password": goodPw}, fromIP(ip)).Code
	}
	if last != 429 {
		t.Fatalf("per-IP login throttle: %d", last)
	}
}

func TestEmailNoAccountEnumeration(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	taken := strings.ToLower(newEmail())
	signUp(t, in, taken, goodPw)
	free := strings.ToLower(newEmail())

	same := func(what string, a, b *httptest.ResponseRecorder) {
		t.Helper()
		if a.Code != b.Code || a.Body.String() != b.Body.String() {
			t.Fatalf("%s differs: %d %s / %d %s", what, a.Code, a.Body, b.Code, b.Body)
		}
	}
	// sign-up with a taken address answers like a new one; the owner gets a notice, no code
	n := len(in.to(taken))
	same("register", post(t, "/api/auth/email/register", map[string]string{"email": taken, "password": "another-pass-77"}, nil),
		post(t, "/api/auth/email/register", map[string]string{"email": free, "password": "another-pass-77"}, nil))
	notice := in.to(taken)
	if len(notice) != n+1 || reCode.MatchString(notice[n].Text) || !strings.Contains(notice[n].Subject, "аккаунт бар") {
		t.Fatalf("taken address should get a code-less notice: %+v", notice[n:])
	}
	if ok, _ := VerifyPassword(goodPw, passwordOf(t, taken)); !ok {
		t.Fatal("sign-up with a taken address changed its password")
	}
	// reset for unknown vs known
	same("reset", post(t, "/api/auth/email/reset/request", map[string]string{"email": taken}, nil),
		post(t, "/api/auth/email/reset/request", map[string]string{"email": "nobody-" + free}, nil))
	// resend for verified vs unknown
	same("resend", post(t, "/api/auth/email/resend", map[string]string{"email": taken}, nil),
		post(t, "/api/auth/email/resend", map[string]string{"email": "nobody2-" + free}, nil))
	// wrong password vs unknown address, and the time it takes
	var tKnown, tUnknown time.Duration
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		a := post(t, "/api/auth/email/login", map[string]string{"email": taken, "password": "wrong-pass-" + strconv.Itoa(i)}, nil)
		tKnown += time.Since(t0)
		t0 = time.Now()
		b := post(t, "/api/auth/email/login", map[string]string{"email": "ghost" + strconv.Itoa(i) + "-" + free, "password": "wrong-pass-1"}, nil)
		tUnknown += time.Since(t0)
		same("login", a, b)
	}
	if tKnown > 10*tUnknown || tUnknown > 10*tKnown {
		t.Fatalf("login timing differs by an order of magnitude: known %v, unknown %v", tKnown, tUnknown)
	}
	// verify / reset confirm with an unknown address look like a wrong code
	same("verify", post(t, "/api/auth/email/verify", map[string]string{"email": taken, "code": "000000", "password": goodPw}, nil),
		post(t, "/api/auth/email/verify", map[string]string{"email": "ghost-" + free, "code": "000000", "password": goodPw}, nil))
}

func passwordOf(t *testing.T, email string) string {
	t.Helper()
	var h string
	err := db.Tx(t.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `select p.hash from user_passwords p join users u on u.id = p.user_id where u.email = $1`, email).Scan(&h)
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestResetPasswordBumpsSessionVersion(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	old, id := signUp(t, in, email, goodPw)
	if w := post(t, "/api/auth/email/reset/request", map[string]string{"email": email}, nil); w.Code != 200 {
		t.Fatalf("reset request: %d", w.Code)
	}
	code := in.code(t, email)
	if w := post(t, "/api/auth/email/reset/confirm", map[string]string{"email": email, "code": code, "newPassword": "12345678"}, nil); w.Code != 400 || errCode(w) != "password_common" {
		t.Fatalf("weak new password: %d %s", w.Code, w.Body)
	}
	w := post(t, "/api/auth/email/reset/confirm", map[string]string{"email": email, "code": code, "newPassword": "jana-qupiya-soz-9"}, nil)
	if w.Code != 200 {
		t.Fatalf("reset confirm: %d %s", w.Code, w.Body)
	}
	fresh := sessionCookie(t, w)
	if whoami(old) != "nobody" {
		t.Fatal("old session survived the reset")
	}
	if whoami(fresh) != strconv.FormatInt(id, 10) {
		t.Fatal("reset did not sign this device in")
	}
	if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, nil); w.Code != 401 {
		t.Fatalf("old password still works: %d", w.Code)
	}
	if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": "jana-qupiya-soz-9"}, nil); w.Code != 200 {
		t.Fatalf("new password: %d %s", w.Code, w.Body)
	}
	msgs := in.to(email)
	if last := msgs[len(msgs)-1]; !strings.Contains(last.Subject, "құпиясөз өзгерді") {
		t.Fatalf("no password-changed notice: %q", last.Subject)
	}
	if n := count(t, "select count(*) from audit_log where actor_id = $1 and action = 'password_reset'", id); n != 1 {
		t.Fatal("reset not audited")
	}
	// the used code cannot be replayed
	if w := post(t, "/api/auth/email/reset/confirm", map[string]string{"email": email, "code": code, "newPassword": "yet-another-pw-1"}, nil); w.Code != 400 {
		t.Fatalf("replayed reset code: %d", w.Code)
	}
}

func TestChangePassword(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	c, id := signUp(t, in, email, goodPw)
	other := sessionCookie(t, post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, nil))
	if w := post(t, "/api/me/password", map[string]string{"currentPassword": goodPw, "newPassword": "x"}, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := post(t, "/api/me/password", map[string]string{"currentPassword": "wrong-one-123", "newPassword": "brand-new-pass-1"}, withCookie(c)); w.Code != 400 || errCode(w) != "wrong_password" {
		t.Fatalf("wrong current: %d %s", w.Code, w.Body)
	}
	if w := post(t, "/api/me/password", map[string]string{"currentPassword": goodPw, "newPassword": email}, withCookie(c)); w.Code != 400 || errCode(w) != "password_like_email" {
		t.Fatalf("password = email: %d %s", w.Code, w.Body)
	}
	w := post(t, "/api/me/password", map[string]string{"currentPassword": goodPw, "newPassword": "brand-new-pass-1"}, withCookie(c))
	if w.Code != 200 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	if whoami(sessionCookie(t, w)) != strconv.FormatInt(id, 10) || whoami(other) != "nobody" || whoami(c) != "nobody" {
		t.Fatal("change password must re-sign this device and end the others")
	}
	if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": "brand-new-pass-1"}, nil); w.Code != 200 {
		t.Fatalf("login with new password: %d", w.Code)
	}
	// a Telegram-only account has no password to change
	tw, _ := loginWebApp(t, 7_100_001)
	if w := post(t, "/api/me/password", map[string]string{"currentPassword": "whatever-123", "newPassword": "brand-new-pass-1"}, withCookie(sessionCookie(t, tw))); w.Code != 400 || errCode(w) != "no_password" {
		t.Fatalf("telegram-only: %d %s", w.Code, w.Body)
	}
}

func TestLinkEmailToTelegramAccount(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	w, out := loginWebApp(t, 7_200_001)
	tg := sessionCookie(t, w)
	id := int64(out["user"].(map[string]any)["id"].(float64))
	email := strings.ToLower(newEmail())
	if w := post(t, "/api/me/email", map[string]string{"email": email, "password": "short"}, withCookie(tg)); w.Code != 400 || errCode(w) != "password_too_short" {
		t.Fatalf("weak password: %d %s", w.Code, w.Body)
	}
	if w := post(t, "/api/me/email", map[string]string{"email": email, "password": goodPw}, withCookie(tg)); w.Code != 200 {
		t.Fatalf("link email: %d %s", w.Code, w.Body)
	}
	if n := count(t, "select count(*) from users where id = $1 and email is null", id); n != 1 {
		t.Fatal("email set before confirmation")
	}
	code := in.code(t, email)
	if w := post(t, "/api/me/email/confirm", map[string]string{"code": fmt.Sprintf("%06d", (mustAtoi(code)+7)%1_000_000)}, withCookie(tg)); w.Code != 400 {
		t.Fatalf("wrong code: %d", w.Code)
	}
	w = post(t, "/api/me/email/confirm", map[string]string{"code": code}, withCookie(tg))
	if w.Code != 200 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	u := body(w)["user"].(map[string]any)
	if u["email"] != email || u["emailVerified"] != true || u["telegramId"].(float64) != 7_200_001 {
		t.Fatalf("user after link: %v", u)
	}
	if whoami(tg) != strconv.FormatInt(id, 10) {
		t.Fatal("first email link must not sign the device out")
	}
	// email login reaches the very same account
	w = post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, nil)
	if w.Code != 200 || whoami(sessionCookie(t, w)) != strconv.FormatInt(id, 10) {
		t.Fatalf("email login after link: %d %s", w.Code, w.Body)
	}
	if n := count(t, "select count(*) from audit_log where actor_id = $1 and action = 'email_link'", id); n != 1 {
		t.Fatal("link not audited")
	}
	// with a password now, changing the address asks for the current one
	next := strings.ToLower(newEmail())
	if w := post(t, "/api/me/email", map[string]string{"email": next, "password": goodPw}, withCookie(tg)); w.Code != 400 || errCode(w) != "wrong_password" {
		t.Fatalf("change without currentPassword: %d %s", w.Code, w.Body)
	}
	if w := post(t, "/api/me/email", map[string]string{"email": email, "currentPassword": goodPw}, withCookie(tg)); w.Code != 400 || errCode(w) != "same_email" {
		t.Fatalf("same email: %d %s", w.Code, w.Body)
	}
	if w := post(t, "/api/me/email", map[string]string{"email": next, "currentPassword": goodPw}, withCookie(tg)); w.Code != 200 {
		t.Fatalf("change email: %d %s", w.Code, w.Body)
	}
	w = post(t, "/api/me/email/confirm", map[string]string{"code": in.code(t, next)}, withCookie(tg))
	if w.Code != 200 || whoami(tg) != "nobody" || whoami(sessionCookie(t, w)) != strconv.FormatInt(id, 10) {
		t.Fatalf("changing the address must end other sessions: %d %s", w.Code, w.Body)
	}
	if w := post(t, "/api/auth/email/login", map[string]string{"email": next, "password": goodPw}, nil); w.Code != 200 {
		t.Fatalf("login with new address: %d", w.Code)
	}
}

func TestLinkEmailHeldByAnotherAccount(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	taken := strings.ToLower(newEmail())
	_, owner := signUp(t, in, taken, goodPw)
	w, _ := loginWebApp(t, 7_300_001)
	tg := sessionCookie(t, w)
	n := len(in.to(taken))
	if w := post(t, "/api/me/email", map[string]string{"email": taken, "password": goodPw}, withCookie(tg)); w.Code != 200 {
		t.Fatalf("answer must not reveal the address is taken: %d %s", w.Code, w.Body)
	}
	msgs := in.to(taken)
	if len(msgs) != n+1 || reCode.MatchString(msgs[n].Text) {
		t.Fatalf("owner should get a notice without a code: %+v", msgs[n:])
	}
	for _, c := range []string{"000000", "123456"} {
		if w := post(t, "/api/me/email/confirm", map[string]string{"code": c}, withCookie(tg)); w.Code != 400 {
			t.Fatalf("confirm without a code: %d", w.Code)
		}
	}
	if n := count(t, "select count(*) from users where email = $1", taken); n != 1 {
		t.Fatal("address moved")
	}
	// an unfinished sign-up does not block a real owner: the pending row goes away
	pending := strings.ToLower(newEmail())
	post(t, "/api/auth/email/register", map[string]string{"email": pending, "password": "squatter-pass-1"}, nil)
	_ = in.code(t, pending)
	post(t, "/api/me/email", map[string]string{"email": pending, "password": goodPw}, withCookie(tg))
	w = post(t, "/api/me/email/confirm", map[string]string{"code": in.code(t, pending)}, withCookie(tg))
	if w.Code != 200 {
		t.Fatalf("confirm over a pending sign-up: %d %s", w.Code, w.Body)
	}
	if n := count(t, "select count(*) from users where email = $1 and telegram_id = 7300001", pending); n != 1 {
		t.Fatal("pending sign-up not replaced")
	}
	if n := count(t, "select count(*) from users where id = $1", owner); n != 1 {
		t.Fatal("verified owner touched")
	}
}

func TestLinkTelegramToEmailAccount(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	c, id := signUp(t, in, email, goodPw)
	sid := strconv.FormatInt(id, 10)
	const tg = 7_400_001
	// Login Widget while signed in by email: Telegram is attached, no second account
	fields := widgetFields(tg, time.Now())
	payload := map[string]any{}
	for k, v := range fields {
		payload[k] = v
	}
	payload["hash"] = SignWidget(fields, testBot)
	w := post(t, "/api/auth/telegram/widget", payload, withCookie(c))
	if w.Code != 200 {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	u := body(w)["user"].(map[string]any)
	if u["telegramId"].(float64) != tg || u["email"] != email || whoami(sessionCookie(t, w)) != sid {
		t.Fatalf("linked user: %v", u)
	}
	if n := count(t, "select count(*) from users where telegram_id = $1", tg); n != 1 {
		t.Fatal("second account created")
	}
	if n := count(t, "select count(*) from audit_log where actor_id = $1 and action = 'telegram_link'", id); n != 1 {
		t.Fatal("telegram link not audited")
	}
	// later, Telegram alone signs in to the same account
	w, out := loginWebApp(t, tg)
	if w.Code != 200 || int64(out["user"].(map[string]any)["id"].(float64)) != id {
		t.Fatalf("telegram login after link: %d %s", w.Code, w.Body)
	}
}

func widgetFields(tg int64, at time.Time) map[string]string {
	return map[string]string{"id": strconv.FormatInt(tg, 10), "first_name": "Ерлан", "username": "erlan_kz",
		"auth_date": strconv.FormatInt(at.Unix(), 10)}
}

func TestTelegramConflictRefused(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	c, id := signUp(t, in, email, goodPw)
	// Telegram 7500001 already has its own site account
	w, out := loginWebApp(t, 7_500_001)
	other := int64(out["user"].(map[string]any)["id"].(float64))
	_ = w
	w = post(t, "/api/auth/telegram/webapp", map[string]string{"initData": SignWebApp(webappFields(7_500_001, time.Now()), testBot)}, withCookie(c))
	if w.Code != 409 || errCode(w) != "telegram_taken" {
		t.Fatalf("conflict: %d %s", w.Code, w.Body)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("refused link must not switch the session")
	}
	if n := count(t, "select count(*) from users where id = $1 and telegram_id is null", id); n != 1 {
		t.Fatal("email account got the Telegram id")
	}
	if n := count(t, "select count(*) from users where id = $1 and telegram_id = 7500001 and email is null", other); n != 1 {
		t.Fatal("Telegram account changed (merged?)")
	}
	if whoami(c) != strconv.FormatInt(id, 10) {
		t.Fatal("email session lost")
	}
	// a row the bot created (no consent, no email) is refused too, with the /stop hint
	exec(t, "insert into users (telegram_id, lang, bot_started_at) values (7500002, 'kk', now())")
	w = post(t, "/api/auth/telegram/webapp", map[string]string{"initData": SignWebApp(webappFields(7_500_002, time.Now()), testBot)}, withCookie(c))
	if w.Code != 409 || errCode(w) != "telegram_taken" || !strings.Contains(w.Body.String(), "/stop") {
		t.Fatalf("bot-only conflict: %d %s", w.Code, w.Body)
	}
}

func TestEmailAuthOffFailsClosed(t *testing.T) {
	testdb.Need(t)
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("MAIL_FROM", "")
	t.Setenv("MAIL_DEV_LOG", "")
	for _, path := range []string{"/api/auth/email/register", "/api/auth/email/resend", "/api/auth/email/reset/request"} {
		w := post(t, path, map[string]string{"email": newEmail(), "password": goodPw}, nil)
		if w.Code != 503 || errCode(w) != "email_auth_off" || !strings.Contains(w.Body.String(), "Email арқылы кіру әзірге қосылмаған") {
			t.Fatalf("%s without mail: %d %s", path, w.Code, w.Body)
		}
	}
	// the developer fallback: MAIL_DEV_LOG=true logs instead of sending
	t.Setenv("MAIL_DEV_LOG", "true")
	if w := post(t, "/api/auth/email/register", map[string]string{"email": newEmail(), "password": goodPw}, nil); w.Code != 200 {
		t.Fatalf("dev log: %d %s", w.Code, w.Body)
	}
	mail.Wait()
}

func TestValidation(t *testing.T) {
	testdb.Need(t)
	fakeResend(t)
	for _, e := range []string{"", "no-at", "a@b", "Name <x@y.kz>", "x y@z.kz", "x@.kz", "x@kz.", strings.Repeat("a", 250) + "@x.kz"} {
		if w := post(t, "/api/auth/email/register", map[string]string{"email": e, "password": goodPw}, nil); w.Code != 400 || errCode(w) != "invalid_email" {
			t.Errorf("email %q: %d %s", e, w.Code, w.Body)
		}
	}
	for pw, code := range map[string]string{"short": "password_too_short", "password123": "password_common",
		"aaaaaaaaaa": "password_common", "QWERTYUIOP": "password_common", strings.Repeat("ж", 129): "password_too_long"} {
		if w := post(t, "/api/auth/email/register", map[string]string{"email": newEmail(), "password": pw}, nil); w.Code != 400 || errCode(w) != code {
			t.Errorf("password %q: %d %s", pw, w.Code, w.Body)
		}
	}
	e := strings.ToLower(newEmail())
	if w := post(t, "/api/auth/email/register", map[string]string{"email": e, "password": strings.ToUpper(e)}, nil); errCode(w) != "password_like_email" {
		t.Errorf("password = email: %s", w.Body)
	}
	// origin check applies like on every write
	w := post(t, "/api/auth/email/login", map[string]string{"email": e, "password": goodPw}, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })
	if w.Code != 403 || errCode(w) != "bad_origin" {
		t.Fatalf("foreign origin: %d", w.Code)
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("Тест-құпиясөз-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok, re := VerifyPassword("Тест-құпиясөз-1", h); !ok || re {
		t.Fatalf("verify own hash: %v %v", ok, re)
	}
	if ok, _ := VerifyPassword("Тест-құпиясөз-2", h); ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("Тест-құпиясөз-1")
	if h == h2 {
		t.Fatal("salt not random")
	}
	// a hash made with older/weaker parameters still verifies and asks for a rehash
	salt := []byte("0123456789abcdef")
	old := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", b64(salt), b64(argonKey([]byte("old-pass-123"), salt, 1, 8192, 1, 32)))
	if ok, re := VerifyPassword("old-pass-123", old); !ok || !re {
		t.Fatalf("old params: ok %v rehash %v", ok, re)
	}
	for _, bad := range []string{"", "$2a$12$abc", "$argon2id$v=19$m=x$a$b", "$argon2i$v=19$m=8192,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5"} {
		if ok, _ := VerifyPassword("x", bad); ok {
			t.Fatalf("malformed hash %q accepted", bad)
		}
	}
}

func TestRehashOnLogin(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	signUp(t, in, email, goodPw)
	salt := []byte("fedcba9876543210")
	old := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", b64(salt), b64(argonKey([]byte(goodPw), salt, 1, 8192, 1, 32)))
	exec(t, `update user_passwords set hash = $2 where user_id = (select id from users where email = $1)`, email, old)
	if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, nil); w.Code != 200 {
		t.Fatalf("login with old hash: %d", w.Code)
	}
	if h := passwordOf(t, email); !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("not rehashed: %s", h)
	}
}

func TestEmailTablesAreServerOnly(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := strings.ToLower(newEmail())
	_, id := signUp(t, in, email, goodPw)
	post(t, "/api/auth/email/reset/request", map[string]string{"email": email}, nil)
	for _, q := range []string{
		"select count(*) from user_passwords",
		"select count(*) from auth_codes",
		"update users set email = 'x@evil.kz' where id = app_user_id()",
		"update users set email_verified_at = null where id = app_user_id()",
		"insert into user_passwords (user_id, hash) values (app_user_id(), 'x')",
	} {
		err := db.WithUser(t.Context(), id, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("app_user %q: %v", q, err)
		}
	}
	if n := count(t, `select count(*) from pg_class where relname in ('user_passwords', 'auth_codes') and relrowsecurity`); n != 2 {
		t.Fatal("RLS not enabled on the new tables")
	}
	// the user's own row (as the API reads it) never carries a password hash
	err := db.WithUser(t.Context(), id, func(tx pgx.Tx) error {
		var row string
		if err := tx.QueryRow(t.Context(), "select to_jsonb(u)::text from users u where id = app_user_id()").Scan(&row); err != nil {
			return err
		}
		if strings.Contains(row, "argon2") || !strings.Contains(row, email) {
			t.Errorf("own row: %s", row)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

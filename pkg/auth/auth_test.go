package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("k", 48))
	os.Setenv("BOT_TOKEN", testBot)
	os.Setenv("RATE_WRITE_PER_MIN", "1000")
	os.Exit(testdb.Main(m, "auth"))
}

var ipSeq atomic.Int64

// uniqueIP keeps the per-IP rate limit of one test from leaking into another.
func uniqueIP() string {
	n := ipSeq.Add(1)
	return "10.0." + strconv.FormatInt(n/250, 10) + "." + strconv.FormatInt(n%250+1, 10)
}

func mux() *http.ServeMux {
	m := http.NewServeMux()
	Routes(m)
	m.HandleFunc("GET /whoami", func(w http.ResponseWriter, r *http.Request) {
		if u := Current(r); u != nil {
			w.Write([]byte(strconv.FormatInt(u.ID, 10)))
			return
		}
		w.Write([]byte("nobody"))
	})
	return m
}

func post(t *testing.T, path string, body any, mod func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	r.Header.Set("X-Real-Ip", uniqueIP())
	r.Host = "studenthub-plus-kz.vercel.app"
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	mux().ServeHTTP(w, r)
	return w
}

func whoami(cookie *http.Cookie) string {
	r := httptest.NewRequest("GET", "/whoami", nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux().ServeHTTP(w, r)
	return w.Body.String()
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie; status %d body %s", CookieName, w.Code, w.Body)
	return nil
}

func loginWebApp(t *testing.T, tgID int64) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := post(t, "/api/auth/telegram/webapp", map[string]string{"initData": SignWebApp(webappFields(tgID, time.Now()), testBot)}, nil)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	err := db.Tx(context.Background(), func(tx pgx.Tx) error { _, err := tx.Exec(context.Background(), sql, args...); return err })
	if err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	err := db.Tx(context.Background(), func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), sql, args...).Scan(&n) })
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWebAppLoginCookieAndUser(t *testing.T) {
	testdb.Need(t)
	w, out := loginWebApp(t, 1001)
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	c := sessionCookie(t, w)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*24*3600 {
		t.Fatalf("cookie flags: %+v", c)
	}
	if out["needsConsent"] != true || out["token"] == nil {
		t.Fatalf("body: %v", out)
	}
	u := out["user"].(map[string]any)
	if u["telegramId"].(float64) != 1001 || u["lang"] != "ru" || u["name"] != "Айгерім Н" || u["role"] != "student" {
		t.Fatalf("user: %v", u)
	}
	if got := whoami(c); got != strconv.FormatInt(int64(u["id"].(float64)), 10) {
		t.Fatalf("cookie does not authenticate: %s", got)
	}
	// Bearer token (Mini App inside Telegram Web) works too.
	r := httptest.NewRequest("GET", "/whoami", nil)
	r.Header.Set("Authorization", "Bearer "+out["token"].(string))
	rw := httptest.NewRecorder()
	mux().ServeHTTP(rw, r)
	if rw.Body.String() == "nobody" {
		t.Fatal("bearer token rejected")
	}
}

func TestCookieNotSecureOnLocalhost(t *testing.T) {
	testdb.Need(t)
	w := post(t, "/api/auth/telegram/webapp", map[string]string{"initData": SignWebApp(webappFields(1002, time.Now()), testBot)},
		func(r *http.Request) { r.Host = "localhost:3000"; r.Header.Set("Origin", "http://localhost:3000") })
	c := sessionCookie(t, w)
	if c.Secure || !c.HttpOnly {
		t.Fatalf("localhost cookie: %+v", c)
	}
}

func TestLoginRejectsBadInitDataAndWidget(t *testing.T) {
	testdb.Need(t)
	bad := strings.Replace(SignWebApp(webappFields(1003, time.Now()), testBot), "aigerim", "evil", 1)
	if w := post(t, "/api/auth/telegram/webapp", map[string]string{"initData": bad}, nil); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("tampered initData: %d", w.Code)
	}
	old := SignWebApp(webappFields(1003, time.Now().Add(-25*time.Hour)), testBot)
	if w := post(t, "/api/auth/telegram/webapp", map[string]string{"initData": old}, nil); w.Code != 401 || !strings.Contains(w.Body.String(), "telegram_expired") {
		t.Fatalf("expired initData: %d %s", w.Code, w.Body)
	}
	good := widgetBody(t, 1004, time.Now(), testBot)
	if w := post(t, "/api/auth/telegram/widget", good, nil); w.Code != 200 {
		t.Fatalf("widget login: %d %s", w.Code, w.Body)
	}
	if w := post(t, "/api/auth/telegram/widget", widgetBody(t, 1004, time.Now(), "9:bad"), nil); w.Code != 401 {
		t.Fatalf("bad widget hash: %d", w.Code)
	}
	if w := post(t, "/api/auth/telegram/widget", widgetBody(t, 1004, time.Now().Add(-48*time.Hour), testBot), nil); w.Code != 401 {
		t.Fatalf("old widget: %d", w.Code)
	}
	if n := count(t, "select count(*) from users where telegram_id = 1003"); n != 0 {
		t.Fatalf("rejected login created a user")
	}
}

func TestLoginLinksBotOnlyUser(t *testing.T) {
	testdb.Need(t)
	tg := 2000 + ipSeq.Add(1)
	exec(t, "insert into users (telegram_id, name, bot_started_at) values ($1, 'Бот аты', now())", tg)
	w, out := loginWebApp(t, tg)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if n := count(t, "select count(*) from users where telegram_id = $1", tg); n != 1 {
		t.Fatalf("duplicate users: %d", n)
	}
	if out["user"].(map[string]any)["name"] != "Бот аты" {
		t.Fatalf("existing name overwritten: %v", out["user"])
	}
}

func TestSessionRejectedWhenTamperedExpiredOldOrDeleted(t *testing.T) {
	testdb.Need(t)
	tg := 3000 + ipSeq.Add(1)
	w, out := loginWebApp(t, tg)
	c := sessionCookie(t, w)
	id := int64(out["user"].(map[string]any)["id"].(float64))

	tampered := *c
	tampered.Value = c.Value[:len(c.Value)-2] + "AA"
	if whoami(&tampered) != "nobody" {
		t.Fatal("tampered cookie accepted")
	}
	expired, _ := Sign(Claims{UserID: id, Version: 1, Exp: time.Now().Add(-time.Second).Unix()})
	if whoami(&http.Cookie{Name: CookieName, Value: expired}) != "nobody" {
		t.Fatal("expired cookie accepted")
	}
	other, _ := Sign(Claims{UserID: id + 1000, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
	if whoami(&http.Cookie{Name: CookieName, Value: other}) != "nobody" {
		t.Fatal("token for missing user accepted")
	}

	// "Log out everywhere" bumps session_version: the old cookie stops working.
	lw := post(t, "/api/auth/logout", map[string]bool{"all": true}, func(r *http.Request) { r.AddCookie(c) })
	if lw.Code != 200 {
		t.Fatal(lw.Body)
	}
	if cl := sessionCookie(t, lw); cl.MaxAge >= 0 || cl.Value != "" {
		t.Fatalf("logout did not clear cookie: %+v", cl)
	}
	if whoami(c) != "nobody" {
		t.Fatal("old session_version accepted")
	}

	w2, _ := loginWebApp(t, tg)
	c2 := sessionCookie(t, w2)
	if whoami(c2) == "nobody" {
		t.Fatal("fresh login rejected")
	}
	exec(t, "update users set deleted_at = now() where id = $1", id)
	if whoami(c2) != "nobody" {
		t.Fatal("deleted account accepted")
	}
	if w3, _ := loginWebApp(t, tg); w3.Code != 403 {
		t.Fatalf("blocked account could log in: %d", w3.Code)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	testdb.Need(t)
	initData := map[string]string{"initData": SignWebApp(webappFields(1007, time.Now()), testBot)}
	for name, mod := range map[string]func(*http.Request){
		"evil origin":  func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"evil referer": func(r *http.Request) { r.Header.Del("Origin"); r.Header.Set("Referer", "https://evil.example/x") },
		"no origin":    func(r *http.Request) { r.Header.Del("Origin") },
		"lookalike":    func(r *http.Request) { r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app.evil.example") },
	} {
		if w := post(t, "/api/auth/telegram/webapp", initData, mod); w.Code != 403 || !strings.Contains(w.Body.String(), "bad_origin") {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	ok := func(r *http.Request) { r.Header.Set("Origin", "https://studenthub-platform.vercel.app") }
	if w := post(t, "/api/auth/telegram/webapp", initData, ok); w.Code != 200 {
		t.Fatalf("allowed origin rejected: %d", w.Code)
	}
}

func TestWriteRateLimit(t *testing.T) {
	testdb.Need(t)
	t.Setenv("RATE_WRITE_PER_MIN", "3")
	ip := "203.0.113." + strconv.FormatInt(ipSeq.Add(1)%250+1, 10)
	codes := []int{}
	for i := 0; i < 5; i++ {
		w := post(t, "/api/auth/logout", nil, func(r *http.Request) { r.Header.Set("X-Real-Ip", ip) })
		codes = append(codes, w.Code)
	}
	if codes[2] != 200 || codes[3] != 429 || codes[4] != 429 {
		t.Fatalf("codes %v", codes)
	}
	// Per user: a signed-in student hits the limit even when the IP changes.
	t.Setenv("RATE_WRITE_PER_MIN", "1000")
	w, _ := loginWebApp(t, 1008)
	c := sessionCookie(t, w)
	t.Setenv("RATE_WRITE_PER_MIN", "2")
	var last int
	for i := 0; i < 3; i++ {
		last = post(t, "/api/auth/logout", nil, func(r *http.Request) { r.AddCookie(c) }).Code
	}
	if last != 429 {
		t.Fatalf("per-user limit not applied: %d", last)
	}
}

func TestAdminBootstrapAuditedOnce(t *testing.T) {
	testdb.Need(t)
	tg := 4000 + ipSeq.Add(1)
	t.Setenv("ADMIN_TELEGRAM_IDS", "555, "+strconv.FormatInt(tg, 10))
	_, out := loginWebApp(t, tg)
	if out["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("not admin: %v", out)
	}
	loginWebApp(t, tg)
	n := count(t, `select count(*) from audit_log a join users u on u.id = a.target_id
		where u.telegram_id = $1 and a.action = 'role_grant' and a.reason like 'ADMIN_TELEGRAM_IDS%'`, tg)
	if n != 1 {
		t.Fatalf("bootstrap audit rows: %d", n)
	}
}

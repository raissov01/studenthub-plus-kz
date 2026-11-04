package webpush

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("w", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "1000")
	os.Exit(testdb.Main(m, "webpush"))
}

var seq atomic.Int64

func newUser(t *testing.T) int64 {
	t.Helper()
	var id int64
	err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `insert into users (telegram_id, name, consent_at) values ($1, 'P', now()) returning id`, 770000+seq.Add(1)).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(context.Background(), func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), sql, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func call(t *testing.T, method, path string, uid int64, body any, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	if uid != 0 {
		var ver int
		_ = db.Tx(context.Background(), func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "select session_version from users where id = $1", uid).Scan(&ver)
		})
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: ver, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	m := http.NewServeMux()
	Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func enable(t *testing.T, f *fakePushService) {
	pub, priv, _ := GenerateVAPID()
	t.Setenv("FEATURE_WEBPUSH", "true")
	t.Setenv("VAPID_PUBLIC_KEY", pub)
	t.Setenv("VAPID_PRIVATE_KEY", priv)
	t.Setenv("VAPID_SUBJECT", "mailto:test@example.com")
	if f != nil { // trust the fake push service's TLS certificate for Default()
		old := http.DefaultTransport
		http.DefaultTransport = f.Client().Transport
		t.Cleanup(func() { http.DefaultTransport = old })
		Default().Client = &http.Client{Timeout: 5 * time.Second}
	}
}

func subJSON(endpoint string, k Keys) map[string]any {
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": b64.EncodeToString(k.P256dh), "auth": b64.EncodeToString(k.Auth)}}
}

func TestRoutesOffLookLikeUnknown(t *testing.T) {
	t.Setenv("FEATURE_WEBPUSH", "")
	for _, p := range [][2]string{{"POST", "/api/me/push"}, {"DELETE", "/api/me/push"}, {"POST", "/api/me/push/test"}} {
		if w := call(t, p[0], p[1], 0, map[string]any{}); w.Code != 404 || !strings.Contains(w.Body.String(), "not_found") {
			t.Errorf("%s %s off: %d %s", p[0], p[1], w.Code, w.Body)
		}
	}
}

func TestSubscribeMoveUnsubscribeAndTest(t *testing.T) {
	testdb.Need(t)
	f := newFakePushService(t)
	enable(t, f)
	a, b := newUser(t), newUser(t)
	sub := f.subscribe(t, "/fcm/a", 201)
	body := subJSON(sub.Endpoint, sub.Keys)

	if w := call(t, "POST", "/api/me/push", 0, body); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := call(t, "POST", "/api/me/push", a, body, "Origin", "https://evil.example"); w.Code != 403 {
		t.Fatalf("foreign origin: %d", w.Code)
	}
	for _, bad := range []map[string]any{
		subJSON("http://insecure.example/x", sub.Keys),
		subJSON("https://user:pw@push.example/x", sub.Keys),
		{"endpoint": sub.Endpoint, "keys": map[string]string{"p256dh": "AAAA", "auth": "BBBB"}},
		{"endpoint": sub.Endpoint},
	} {
		if w := call(t, "POST", "/api/me/push", a, bad); w.Code != 400 {
			t.Errorf("bad subscription accepted: %d %v", w.Code, bad)
		}
	}
	if w := call(t, "POST", "/api/me/push/test", a, nil); w.Code != 409 {
		t.Fatalf("test without subscription: %d %s", w.Code, w.Body)
	}
	if w := call(t, "POST", "/api/me/push", a, body); w.Code != 200 {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}
	if w := call(t, "POST", "/api/me/push", a, body); w.Code != 200 || count(t, `select count(*) from push_subscriptions where endpoint=$1`, sub.Endpoint) != 1 {
		t.Fatal("re-subscribe duplicated the row")
	}
	if count(t, `select count(*) from push_subscriptions where user_id=$1 and ua='Chrome 131'`, a) != 1 {
		t.Fatal("ua not shortened")
	}
	// the test push reaches the browser in the requested language
	w := call(t, "POST", "/api/me/push/test", a, nil, "Accept-Language", "en")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"delivered":1`) {
		t.Fatalf("test: %d %s", w.Code, w.Body)
	}
	var m Message
	if got := f.got["/fcm/a"]; len(got) != 1 || json.Unmarshal([]byte(got[0]), &m) != nil || !strings.Contains(m.Body, "test") || m.URL != "/settings" {
		t.Fatalf("push service got %q", f.got["/fcm/a"])
	}
	if count(t, `select count(*) from push_subscriptions where user_id=$1 and last_ok is not null`, a) != 1 {
		t.Fatal("last_ok not recorded")
	}

	// the same browser signs in to account b: the endpoint moves
	if w := call(t, "POST", "/api/me/push", b, body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if count(t, `select count(*) from push_subscriptions where user_id=$1`, a) != 0 || count(t, `select count(*) from push_subscriptions where user_id=$1`, b) != 1 {
		t.Fatal("endpoint did not move to the new account")
	}
	// a cannot remove b's subscription; b can
	if w := call(t, "DELETE", "/api/me/push", a, map[string]string{"endpoint": sub.Endpoint}); w.Code != 200 || !strings.Contains(w.Body.String(), `"removed":false`) {
		t.Fatalf("foreign unsubscribe: %d %s", w.Code, w.Body)
	}
	if w := call(t, "DELETE", "/api/me/push", b, map[string]string{"endpoint": sub.Endpoint}); w.Code != 200 || !strings.Contains(w.Body.String(), `"removed":true`) {
		t.Fatalf("unsubscribe: %d %s", w.Code, w.Body)
	}

	// a gone subscription (410) is deleted by the test push
	gone := f.subscribe(t, "/fcm/gone", 410)
	call(t, "POST", "/api/me/push", a, subJSON(gone.Endpoint, gone.Keys))
	if w := call(t, "POST", "/api/me/push/test", a, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"gone":1`) {
		t.Fatalf("test gone: %d %s", w.Code, w.Body)
	}
	if count(t, `select count(*) from push_subscriptions where endpoint=$1`, gone.Endpoint) != 0 {
		t.Fatal("410 subscription kept")
	}
}

func TestSubscriptionLimitFailuresAndRLS(t *testing.T) {
	testdb.Need(t)
	f := newFakePushService(t)
	enable(t, f)
	a, b := newUser(t), newUser(t)
	for i := 0; i < MaxPerUser+2; i++ {
		s := f.subscribe(t, "/many/"+string(rune('a'+i)), 201)
		if w := call(t, "POST", "/api/me/push", a, subJSON(s.Endpoint, s.Keys)); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if n := count(t, `select count(*) from push_subscriptions where user_id=$1`, a); n != MaxPerUser {
		t.Fatalf("kept %d subscriptions", n)
	}
	if count(t, `select count(*) from push_subscriptions where endpoint like '%/many/a' or endpoint like '%/many/b'`) != 0 {
		t.Fatal("oldest subscriptions not dropped")
	}

	// 5xx counts as a failure; MaxFailures in a row deletes the row
	bad := f.subscribe(t, "/flaky", 503)
	call(t, "POST", "/api/me/push", b, subJSON(bad.Endpoint, bad.Keys))
	pool, _ := db.Pool(context.Background())
	subs, err := List(context.Background(), pool, b)
	if err != nil || len(subs) != 1 {
		t.Fatal(subs, err)
	}
	for i := 1; i <= MaxFailures; i++ {
		res := Default().SendAll(context.Background(), pool, subs, []byte("{}"), "")
		if res.Delivered != 0 || !res.Retryable {
			t.Fatalf("%+v", res)
		}
	}
	if count(t, `select count(*) from push_subscriptions where user_id=$1`, b) != 0 {
		t.Fatal("failing subscription kept after MaxFailures")
	}

	// RLS: app_user sees and deletes only its own rows, and cannot insert
	err = db.WithUser(context.Background(), b, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), `select count(*) from push_subscriptions`).Scan(&n); err != nil || n != 0 {
			t.Errorf("b sees %d rows (%v)", n, err)
		}
		tag, err := tx.Exec(context.Background(), `delete from push_subscriptions where user_id=$1`, a)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("b deleted a's rows: %v %v", tag, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithUser(context.Background(), b, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `insert into push_subscriptions(user_id,endpoint,p256dh,auth) values ($1,'https://x.example/1',repeat('a',87),repeat('b',22))`, b)
		return err
	})
	if err == nil {
		t.Fatal("app_user inserted a subscription directly")
	}
	// account deletion removes the subscriptions
	if _, err := pool.Exec(context.Background(), `delete from users where id=$1`, a); err != nil {
		t.Fatal(err)
	}
	if count(t, `select count(*) from push_subscriptions where user_id=$1`, a) != 0 {
		t.Fatal("subscriptions outlived the account")
	}
}

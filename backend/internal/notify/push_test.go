package notify

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/webpush"
)

// fakePush is a push service: it answers each endpoint path with a configured status and
// counts the requests. Encryption itself is covered by internal/webpush tests.
type fakePush struct {
	*httptest.Server
	mu     sync.Mutex
	status map[string]int
	hits   map[string]int
}

func newFakePush(t *testing.T) *fakePush {
	f := &fakePush{status: map[string]int{}, hits: map[string]int{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits[r.URL.Path]++
		code := f.status[r.URL.Path]
		if code == 0 {
			code = 201
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePush) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// subscribe stores a browser subscription for uid whose push service answers status.
func (f *fakePush) subscribe(t *testing.T, uid int64, path string, status int) {
	t.Helper()
	f.mu.Lock()
	f.status[path] = status
	f.mu.Unlock()
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	enc := base64.RawURLEncoding.EncodeToString
	k, err := webpush.ParseKeys(enc(priv.PublicKey().Bytes()), enc(secret))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return webpush.Save(ctx(), tx, uid, f.URL+path, k, "test") }); err != nil {
		t.Fatal(err)
	}
}

func pushDispatcher(t *testing.T, f *fakePush) *Dispatcher {
	t.Helper()
	pool, err := db.Pool(ctx())
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := webpush.GenerateVAPID()
	v, err := webpush.ParseVAPID(pub, priv, "mailto:test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return &Dispatcher{Pool: pool, Token: "123:test", APIBase: os.Getenv("TELEGRAM_API_BASE"), Client: client,
		Push: &webpush.Sender{VAPID: v, Client: f.Client()}}
}

func channel(t *testing.T, uid int64) string {
	t.Helper()
	var ch *string
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select channel from notify_outbox where user_id=$1 order by id desc limit 1`, uid).Scan(&ch)
	}); err != nil {
		t.Fatal(err)
	}
	if ch == nil {
		return ""
	}
	return *ch
}

func dsend(t *testing.T, d *Dispatcher, uid int64, c Category, text string, opts Opts) Result {
	t.Helper()
	r, err := d.Send(ctx(), uid, c, text, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPushPreferredOverBot(t *testing.T) {
	testdb.Need(t)
	f := newFakePush(t)
	d := pushDispatcher(t, f)
	uid, tg := newUser(t)
	f.subscribe(t, uid, "/both", 201)
	if r := dsend(t, d, uid, CatEvents, "Іс-шара ертең", Opts{Now: at(12, 0), Link: "/event/5"}); r != Sent {
		t.Fatalf("result %s", r)
	}
	if f.count("/both") != 1 || len(sent(tg)) != 0 || channel(t, uid) != "push" {
		t.Fatalf("push=%d bot=%d channel=%q", f.count("/both"), len(sent(tg)), channel(t, uid))
	}
	// the push counts against the same daily budget as the bot
	if n := count(t, `select count(*) from notify_sends where user_id=$1 and kind='direct'`, uid); n != 1 {
		t.Fatalf("budget reservations %d", n)
	}
}

func TestPushOnlyAccountIsReachable(t *testing.T) {
	testdb.Need(t)
	f := newFakePush(t)
	d := pushDispatcher(t, f)
	var uid int64
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (email, email_verified_at, name, consent_at) values ('push-'||nextval('users_id_seq')||'@example.org', now(), 'E', now()) returning id`).Scan(&uid)
	}); err != nil {
		t.Fatal(err)
	}
	if r := dsend(t, d, uid, CatEvents, "Без подписки", Opts{Now: at(12, 0)}); r != Off {
		t.Fatalf("no channel: %s", r)
	}
	f.subscribe(t, uid, "/only", 201)
	if r := dsend(t, d, uid, CatEvents, "Есть подписка", Opts{Now: at(12, 1)}); r != Sent || f.count("/only") != 1 {
		t.Fatalf("push only: %s hits=%d", r, f.count("/only"))
	}
	// with push switched off (no sender, FEATURE_WEBPUSH unset) the account is unreachable again
	off := *d
	off.Push = nil
	t.Setenv("FEATURE_WEBPUSH", "")
	if r := dsend(t, &off, uid, CatEvents, "Флаг өшік", Opts{Now: at(12, 2)}); r != Off || f.count("/only") != 1 {
		t.Fatalf("feature off: %s hits=%d", r, f.count("/only"))
	}
}

func TestPushGoneFallsBackToBot(t *testing.T) {
	testdb.Need(t)
	f := newFakePush(t)
	d := pushDispatcher(t, f)
	uid, tg := newUser(t)
	f.subscribe(t, uid, "/gone", 410)
	if r := dsend(t, d, uid, CatEvents, "Fallback", Opts{Now: at(12, 0)}); r != Sent {
		t.Fatalf("result %s", r)
	}
	if f.count("/gone") != 1 || len(sent(tg)) != 1 || channel(t, uid) != "telegram" {
		t.Fatalf("push=%d bot=%d channel=%q", f.count("/gone"), len(sent(tg)), channel(t, uid))
	}
	if n := count(t, `select count(*) from push_subscriptions where user_id=$1`, uid); n != 0 {
		t.Fatal("410 subscription kept")
	}
	// a 5xx also falls back, but the subscription stays (one failure recorded)
	f.subscribe(t, uid, "/flaky", 503)
	if r := dsend(t, d, uid, CatEvents, "Fallback 2", Opts{Now: at(12, 5)}); r != Sent || len(sent(tg)) != 2 {
		t.Fatalf("5xx: %s bot=%d", r, len(sent(tg)))
	}
	if n := count(t, `select failures from push_subscriptions where user_id=$1`, uid); n != 1 {
		t.Fatalf("failures %d", n)
	}
}

func TestPushFailureWithoutBotRetriesThenDrops(t *testing.T) {
	testdb.Need(t)
	f := newFakePush(t)
	d := pushDispatcher(t, f)
	var uid int64
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (email, email_verified_at, name, consent_at) values ('push-'||nextval('users_id_seq')||'@example.org', now(), 'E', now()) returning id`).Scan(&uid)
	}); err != nil {
		t.Fatal(err)
	}
	f.subscribe(t, uid, "/down", 503)
	if r := dsend(t, d, uid, CatEvents, "Retry me", Opts{Now: at(12, 0)}); r != Queued {
		t.Fatalf("result %s", r)
	}
	// the push service recovers: the hourly flush delivers it
	f.mu.Lock()
	f.status["/down"] = 201
	f.mu.Unlock()
	res, err := d.Flush(ctx(), at(13, 1))
	if err != nil || res.Sent < 1 || f.count("/down") != 2 || channel(t, uid) != "push" {
		t.Fatalf("flush %+v %v hits=%d", res, err, f.count("/down"))
	}
	// all subscriptions gone and no bot: the retry is dropped (inbox row remains)
	f.subscribe(t, uid, "/dead", 410)
	exec(t, `delete from push_subscriptions where endpoint like '%/down'`)
	if r := dsend(t, d, uid, CatEvents, "Nobody home", Opts{Now: at(14, 0)}); r != Queued {
		t.Fatalf("gone: %s", r)
	}
	if res, err := d.Flush(ctx(), at(15, 1)); err != nil || count(t, `select count(*) from notify_outbox where user_id=$1 and state='dropped' and text='Nobody home'`, uid) != 1 {
		t.Fatalf("flush %+v %v", res, err)
	}
	if n := count(t, `select count(*) from notifications where user_id=$1 and text='Nobody home'`, uid); n != 1 {
		t.Fatal("inbox row missing")
	}
}

func TestPushRespectsQuietHoursCategoriesAndCap(t *testing.T) {
	testdb.Need(t)
	f := newFakePush(t)
	d := pushDispatcher(t, f)
	uid, tg := newUser(t)
	f.subscribe(t, uid, "/rules", 201)
	// quiet hours: held back, pushed at 08:00
	if r := dsend(t, d, uid, CatEvents, "Түнде", Opts{Now: at(23, 0)}); r != Queued || f.count("/rules") != 0 {
		t.Fatalf("quiet: %s hits=%d", r, f.count("/rules"))
	}
	// Flush is global: other tests' users may be due too, so check this user's push only
	if res, err := d.Flush(ctx(), at(8, 0).AddDate(0, 0, 1)); err != nil || res.Sent < 1 || f.count("/rules") != 1 {
		t.Fatalf("morning flush %+v %v", res, err)
	}
	// category off: nothing pushed
	exec(t, `insert into notify_prefs(user_id, market) values ($1, false)`, uid)
	if r := dsend(t, d, uid, CatMarket, "Маркет", Opts{Now: at(12, 0).AddDate(0, 0, 1)}); r != Off || f.count("/rules") != 1 {
		t.Fatalf("category off: %s", r)
	}
	// daily cap (3, the morning push included): the 4th direct message waits for tomorrow
	day := at(12, 0).AddDate(0, 0, 1)
	for i, want := range []Result{Sent, Sent, Queued} {
		if r := dsend(t, d, uid, CatEvents, "cap "+string(rune('a'+i)), Opts{Now: day}); r != want {
			t.Fatalf("cap %d: %s", i, r)
		}
	}
	if f.count("/rules") != 3 || len(sent(tg)) != 0 {
		t.Fatalf("hits=%d bot=%d", f.count("/rules"), len(sent(tg)))
	}
}

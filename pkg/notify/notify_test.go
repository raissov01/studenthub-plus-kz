package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// fake Telegram: records every sendMessage by chat id
var (
	tgMu   sync.Mutex
	tgSent = map[int64][]string{}
)

func sent(chat int64) []string {
	tgMu.Lock()
	defer tgMu.Unlock()
	return append([]string(nil), tgSent[chat]...)
}

func TestMain(m *testing.M) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		tgMu.Lock()
		tgSent[body.ChatID] = append(tgSent[body.ChatID], body.Text)
		tgMu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	os.Setenv("BOT_TOKEN", "123:test")
	os.Setenv("TELEGRAM_API_BASE", srv.URL)
	os.Setenv("SESSION_SECRET", strings.Repeat("n", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "1000")
	os.Setenv("CRON_SECRET", "cron-secret")
	code := testdb.Main(m, "notify")
	srv.Close()
	os.Exit(code)
}

var tgSeq atomic.Int64

func ctx() context.Context { return context.Background() }

func newUser(t *testing.T) (id, tg int64) {
	t.Helper()
	tg = 880000 + tgSeq.Add(1)
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, consent_at) values ($1, 'N', now()) returning id`, tg).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, tg
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return tx.QueryRow(ctx(), sql, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

// at is today's Almaty date at hh:mm (wall clock independent of when the tests run).
func at(hh, mm int) time.Time {
	n := time.Now().In(almaty)
	return time.Date(n.Year(), n.Month(), n.Day(), hh, mm, 0, 0, almaty)
}

func send(t *testing.T, uid int64, c Category, text string, now time.Time) Result {
	t.Helper()
	r, err := Send(ctx(), uid, c, text, Opts{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func flush(t *testing.T, now time.Time) *FlushResult {
	t.Helper()
	r, err := Flush(ctx(), now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestQuietHoursMath(t *testing.T) {
	p := Defaults()
	for _, c := range []struct {
		hh, mm int
		quiet  bool
	}{{21, 59, false}, {22, 0, true}, {23, 30, true}, {0, 0, true}, {8, 59, true}, {9, 0, false}, {12, 0, false}} {
		if got := p.Quiet(at(c.hh, c.mm)); got != c.quiet {
			t.Errorf("%02d:%02d quiet=%v, want %v", c.hh, c.mm, got, c.quiet)
		}
	}
	if end := p.QuietEnds(at(23, 0)); !end.Equal(at(9, 0).AddDate(0, 0, 1)) {
		t.Errorf("23:00 -> %v", end)
	}
	if end := p.QuietEnds(at(3, 0)); !end.Equal(at(9, 0)) {
		t.Errorf("03:00 -> %v", end)
	}
	day := Prefs{QuietEnabled: true, QuietStart: 13 * 60, QuietEnd: 15 * 60}
	if !day.Quiet(at(14, 0)) || day.Quiet(at(15, 0)) || day.Quiet(at(12, 0)) {
		t.Error("non-wrapping quiet window")
	}
	p.QuietEnabled = false
	if p.Quiet(at(23, 0)) {
		t.Error("disabled quiet hours still quiet")
	}
	if m, ok := ParseClock("07:05"); !ok || m != 425 {
		t.Error("ParseClock")
	}
	for _, bad := range []string{"7:05", "24:00", "12:60", "ab:cd", ""} {
		if _, ok := ParseClock(bad); ok {
			t.Errorf("ParseClock(%q) accepted", bad)
		}
	}
}

func TestCategoryOffSendsNothing(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	p := Defaults()
	p.On[CatEvents] = false
	if err := SavePrefs(ctx(), mustPool(t), uid, p); err != nil {
		t.Fatal(err)
	}
	if r := send(t, uid, CatEvents, "event news", at(12, 0)); r != Off {
		t.Fatalf("result %s", r)
	}
	if r := send(t, uid, CatEvents, "event news at night", at(23, 0)); r != Off {
		t.Fatalf("night result %s", r)
	}
	if len(sent(tg)) != 0 || count(t, "select count(*) from notify_outbox where user_id = $1", uid) != 0 {
		t.Fatal("switched-off category was sent or queued")
	}
	// Digest (one per day) respects its category switch and does not take the day
	p.On[CatDigest] = false
	_ = SavePrefs(ctx(), mustPool(t), uid, p)
	st, err := Digest(ctx(), uid, at(12, 0), "digest")
	if err != nil || st != DigestOff {
		t.Fatalf("digest: %s %v", st, err)
	}
	if count(t, "select count(*) from bot_daily_sends where user_id = $1", uid) != 0 || len(sent(tg)) != 0 {
		t.Fatal("digest with category off claimed the day or sent")
	}
	// another category still works
	if r := send(t, uid, CatMarket, "market", at(12, 0)); r != Sent || len(sent(tg)) != 1 {
		t.Fatalf("market: %s %v", r, sent(tg))
	}
}

func TestTransactionalAlwaysSent(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	p := Defaults()
	for _, c := range Categories {
		p.On[c] = false
	}
	p.DailyCap = 1
	_ = SavePrefs(ctx(), mustPool(t), uid, p)
	_ = RecordSend(ctx(), mustPool(t), uid, CatMarket, at(23, 0)) // cap already used
	for i, now := range []time.Time{at(12, 0), at(23, 30), at(3, 0)} {
		if r := send(t, uid, CatModeration, "decision", now); r != Sent {
			t.Fatalf("#%d: %s", i, r)
		}
	}
	if len(sent(tg)) != 3 || count(t, "select count(*) from notify_outbox where user_id = $1", uid) != 0 {
		t.Fatalf("transactional: %v", sent(tg))
	}
	// For() keeps the old func(ctx, id, text) error shape
	if err := For(CatModeration)(ctx(), uid, "via For"); err != nil || len(sent(tg)) != 4 {
		t.Fatalf("For: %v %d", err, len(sent(tg)))
	}
	if _, err := Send(ctx(), uid, Category("spam"), "x", Opts{}); err == nil {
		t.Fatal("unknown category accepted")
	}
}

func TestQuietHoursDeferThenFlush(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	night := at(23, 0)
	if r := send(t, uid, CatMentors, "matched!", night); r != Queued {
		t.Fatalf("night send: %s", r)
	}
	if len(sent(tg)) != 0 {
		t.Fatal("sent during quiet hours")
	}
	var notBefore time.Time
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), "select not_before from notify_outbox where user_id = $1", uid).Scan(&notBefore)
	})
	if want := at(9, 0).AddDate(0, 0, 1); !notBefore.Equal(want) {
		t.Fatalf("not_before %v, want %v", notBefore, want)
	}
	// still night: the hourly run leaves it alone
	if r := flush(t, night.Add(2*time.Hour)); r.Sent != 0 || len(sent(tg)) != 0 {
		t.Fatalf("flushed at night: %+v", r)
	}
	morning := at(9, 5).AddDate(0, 0, 1)
	r := flush(t, morning)
	if r.Sent < 1 || len(sent(tg)) != 1 || sent(tg)[0] != "matched!" {
		t.Fatalf("morning flush: %+v %v", r, sent(tg))
	}
	if count(t, "select count(*) from notify_sends where user_id = $1", uid) != 1 {
		t.Fatal("flushed message not counted against the cap")
	}
}

func TestDailyCap(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	p := Defaults()
	p.DailyCap = 2
	_ = SavePrefs(ctx(), mustPool(t), uid, p)
	noon := at(12, 0)
	got := []Result{
		send(t, uid, CatEvents, "1", noon),
		send(t, uid, CatMarket, "2", noon.Add(time.Minute)),
		send(t, uid, CatEvents, "3", noon.Add(2*time.Minute)),
	}
	if got[0] != Sent || got[1] != Sent || got[2] != Queued || len(sent(tg)) != 2 {
		t.Fatalf("cap 2: %v %v", got, sent(tg))
	}
	// transactional messages do not count and are not capped
	if send(t, uid, CatModeration, "decision", noon.Add(3*time.Minute)) != Sent {
		t.Fatal("transactional capped")
	}
	// a digest-style message is also held back by the cap
	if st, _ := DigestAs(ctx(), uid, CatMarket, noon, "digest"); st != DigestDeferred {
		t.Fatalf("digest over cap: %s", st)
	}
	// same day: still capped
	flush(t, noon.Add(time.Hour))
	if len(sent(tg)) != 3 {
		t.Fatalf("cap broken same day: %v", sent(tg))
	}
	// next day after quiet hours
	flush(t, noon.AddDate(0, 0, 1))
	if s := sent(tg); len(s) != 4 || s[3] != "3" {
		t.Fatalf("next day: %v", s)
	}
}

func TestOutboxFlushIdempotent(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	send(t, uid, CatEvents, "once", at(23, 0))
	morning := at(10, 0).AddDate(0, 0, 1)
	flush(t, morning)
	flush(t, morning)
	flush(t, morning.Add(time.Hour))
	if len(sent(tg)) != 1 {
		t.Fatalf("sequential flushes: %v", sent(tg))
	}
	// overlapping runs: skip locked + claim before send
	send(t, uid, CatEvents, "parallel", at(23, 0))
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = Flush(ctx(), morning.Add(2*time.Hour)) }()
	}
	wg.Wait()
	if s := sent(tg); len(s) != 2 || s[1] != "parallel" {
		t.Fatalf("parallel flushes: %v", s)
	}
	if n := count(t, "select count(*) from notify_outbox where user_id = $1 and state = 'sent'", uid); n != 2 {
		t.Fatalf("sent rows %d", n)
	}
}

func TestFlushDropsSwitchedOffAndStale(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	send(t, uid, CatMarket, "market at night", at(23, 0))
	p := Defaults()
	p.On[CatMarket] = false
	_ = SavePrefs(ctx(), mustPool(t), uid, p)
	r := flush(t, at(10, 0).AddDate(0, 0, 1))
	if r.Dropped < 1 || len(sent(tg)) != 0 {
		t.Fatalf("switched off meanwhile: %+v %v", r, sent(tg))
	}
	// a message that waited longer than the TTL is not worth sending
	uid2, tg2 := newUser(t)
	send(t, uid2, CatEvents, "old", at(23, 0))
	flush(t, at(10, 0).AddDate(0, 0, 4))
	if len(sent(tg2)) != 0 {
		t.Fatal("stale message sent")
	}
}

func mustPool(t *testing.T) Querier {
	t.Helper()
	p, err := db.Pool(ctx())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- HTTP ----

type resp struct{ *httptest.ResponseRecorder }

func (r resp) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Fatalf("json: %v %s", err, r.Body)
	}
	return m
}

func (r resp) code() string {
	var m struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &m)
	return m.Error.Code
}

func do(t *testing.T, method, path string, uid int64, body any, header ...string) resp {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	if uid != 0 {
		var ver int
		_ = db.Tx(ctx(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx(), "select session_version from users where id = $1", uid).Scan(&ver)
		})
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: ver, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	m := http.NewServeMux()
	Routes(m)
	me.Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return resp{w}
}

func TestPrefsAPI(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t)
	if w := do(t, "GET", "/api/me/notifications", 0, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	w := do(t, "GET", "/api/me/notifications", uid, nil)
	if w.Code != 200 {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	prefs := w.json(t)["prefs"].(map[string]any)
	cats := prefs["categories"].(map[string]any)
	q := prefs["quietHours"].(map[string]any)
	if len(cats) != 7 || cats["events"] != true || cats["moderation"] != true || q["start"] != "22:00" || q["end"] != "09:00" ||
		q["enabled"] != true || prefs["dailyCap"] != float64(3) {
		t.Fatalf("defaults: %s", w.Body)
	}

	w = do(t, "PUT", "/api/me/notifications", uid, map[string]any{
		"categories": map[string]bool{"market": false, "jobs": false},
		"quietHours": map[string]any{"start": "23:30", "end": "07:00"},
		"dailyCap":   5,
	})
	if w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	w = do(t, "GET", "/api/me/notifications", uid, nil)
	prefs = w.json(t)["prefs"].(map[string]any)
	cats = prefs["categories"].(map[string]any)
	q = prefs["quietHours"].(map[string]any)
	if cats["market"] != false || cats["jobs"] != false || cats["events"] != true || q["start"] != "23:30" || q["end"] != "07:00" || prefs["dailyCap"] != float64(5) {
		t.Fatalf("after put: %s", w.Body)
	}
	// partial update keeps the rest
	if w := do(t, "PUT", "/api/me/notifications", uid, map[string]any{"quietHours": map[string]any{"enabled": false}}); w.Code != 200 {
		t.Fatalf("quiet off: %d", w.Code)
	}
	p, _ := LoadPrefs(ctx(), mustPool(t), uid)
	if p.QuietEnabled || p.Allows(CatMarket) || p.DailyCap != 5 || p.QuietStart != 23*60+30 {
		t.Fatalf("stored: %+v", p)
	}

	for _, c := range []struct {
		body any
		code string
	}{
		{map[string]any{"categories": map[string]bool{"moderation": false}}, "moderation_locked"},
		{map[string]any{"categories": map[string]bool{"spam": false}}, "unknown_category"},
		{map[string]any{"quietHours": map[string]any{"start": "25:00"}}, "invalid_quiet_hours"},
		{map[string]any{"quietHours": map[string]any{"start": "07:00", "end": "07:00"}}, "invalid_quiet_hours"},
		{map[string]any{"dailyCap": 0}, "invalid_daily_cap"},
		{map[string]any{"dailyCap": 21}, "invalid_daily_cap"},
	} {
		if w := do(t, "PUT", "/api/me/notifications", uid, c.body); w.Code != 400 || w.code() != c.code {
			t.Errorf("%v: %d %s", c.body, w.Code, w.Body)
		}
	}
	if w := do(t, "PUT", "/api/me/notifications", uid, map[string]any{"categories": map[string]bool{"moderation": true}}); w.Code != 200 {
		t.Errorf("moderation true is a no-op: %d", w.Code)
	}
	// auth.Write: foreign origin refused
	if w := do(t, "PUT", "/api/me/notifications", uid, map[string]any{"dailyCap": 4}, "Origin", "https://evil.example"); w.Code != 403 {
		t.Errorf("foreign origin: %d", w.Code)
	}
}

func TestPrefsRLS(t *testing.T) {
	testdb.Need(t)
	a, _ := newUser(t)
	b, _ := newUser(t)
	p := Defaults()
	p.On[CatEvents] = false
	_ = SavePrefs(ctx(), mustPool(t), b, p)
	err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx(), "select count(*) from notify_prefs").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("a sees %d foreign pref rows", n)
		}
		// a reading b's prefs through LoadPrefs gets defaults, not b's row
		got, err := LoadPrefs(ctx(), tx, b)
		if err != nil {
			return err
		}
		if !got.Allows(CatEvents) {
			t.Error("a read b's preferences")
		}
		tag, err := tx.Exec(ctx(), "update notify_prefs set events = true where user_id = $1", b)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			t.Error("a updated b's preferences")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithUser(ctx(), a, func(tx pgx.Tx) error { return SavePrefs(ctx(), tx, b, Defaults()) })
	if err == nil {
		t.Fatal("a inserted a row for b")
	}
	err = db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), "insert into notify_outbox (user_id, category, text, not_before) values ($1, 'events', 'x', now())", a)
		return err
	})
	if err == nil {
		t.Fatal("students must not write the outbox")
	}
	if got, _ := LoadPrefs(ctx(), mustPool(t), b); got.Allows(CatEvents) {
		t.Fatal("b's prefs changed")
	}
}

func TestCronEndpoint(t *testing.T) {
	testdb.Need(t)
	if w := do(t, "GET", "/api/cron/notify", 0, nil); w.Code != 401 {
		t.Fatalf("no secret: %d", w.Code)
	}
	if w := do(t, "GET", "/api/cron/notify", 0, nil, "Authorization", "Bearer wrong"); w.Code != 401 {
		t.Fatalf("wrong secret: %d", w.Code)
	}
	uid, tg := newUser(t)
	exec(t, "insert into notify_outbox (user_id, category, text, not_before) values ($1, 'events', 'due', now() - interval '1 minute')", uid)
	exec(t, "insert into notify_prefs (user_id, quiet_enabled) values ($1, false)", uid)
	w := do(t, "GET", "/api/cron/notify", 0, nil, "Authorization", "Bearer cron-secret")
	if w.Code != 200 || w.json(t)["ok"] != true {
		t.Fatalf("cron: %d %s", w.Code, w.Body)
	}
	if s := sent(tg); len(s) != 1 || s[0] != "due" {
		t.Fatalf("cron sent %v", s)
	}
}

func TestAccountDeletionCascades(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t)
	_ = SavePrefs(ctx(), mustPool(t), uid, Defaults())
	send(t, uid, CatEvents, "queued", at(23, 0))
	_ = RecordSend(ctx(), mustPool(t), uid, CatEvents, at(12, 0))
	w := do(t, "GET", "/api/me/export", uid, nil)
	if w.Code != 200 {
		t.Fatalf("export: %d", w.Code)
	}
	ex := w.json(t)
	if len(ex["notificationPrefs"].([]any)) != 1 || len(ex["notificationOutbox"].([]any)) != 1 {
		t.Fatalf("export sections: %v %v", ex["notificationPrefs"], ex["notificationOutbox"])
	}
	if err := me.Delete(ctx(), uid); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"notify_prefs", "notify_outbox", "notify_sends"} {
		if n := count(t, "select count(*) from "+tbl+" where user_id = $1", uid); n != 0 {
			t.Errorf("%s keeps %d rows", tbl, n)
		}
	}
}

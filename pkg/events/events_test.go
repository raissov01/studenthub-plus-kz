package events

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("e", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("FEATURE_EVENTS", "true")
	os.Setenv("DB_MAX_CONNS", "8")
	os.Setenv("CRON_SECRET", "cron-secret")
	Notify = func(ctx context.Context, userID int64, text string) error { sent.add(userID, text); return nil }
	NotifyUrgent = Notify
	os.Exit(testdb.Main(m, "events"))
}

type outbox struct {
	mu  sync.Mutex
	msg map[int64][]string
}

func (o *outbox) add(id int64, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.msg == nil {
		o.msg = map[int64][]string{}
	}
	o.msg[id] = append(o.msg[id], text)
}

func (o *outbox) of(id int64) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.msg[id]...)
}

var sent outbox

var seq atomic.Int64

func ctx() context.Context { return context.Background() }

func newUser(t *testing.T, role, univ, faculty string) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, role, university, faculty, consent_at)
			values ($1, $2, $3::user_role, nullif($4, ''), nullif($5, ''), now()) returning id`,
			90000+seq.Add(1), "User "+role, role, univ, faculty).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
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

var mux = func() *http.ServeMux { m := http.NewServeMux(); Routes(m); return m }()

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func errCode(w *httptest.ResponseRecorder) string {
	var out struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out.Error.Code
}

type evOpts struct {
	univ                string
	capacity            *int
	walkIn, cert, teams bool
	startsIn, dur       time.Duration
	status              string
	createdBy           int64
}

func newEvent(t *testing.T, o evOpts) int64 {
	t.Helper()
	if o.univ == "" {
		o.univ = "ENU"
	}
	if o.status == "" {
		o.status = "published"
	}
	if o.dur == 0 {
		o.dur = 2 * time.Hour
	}
	var cb *int64
	if o.createdBy != 0 {
		cb = &o.createdBy
	}
	start := time.Now().Add(o.startsIn)
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into events (title_kk, cat, starts_at, ends_at, university, capacity, walk_in_allowed,
			gives_cert, teams_enabled, status, created_by) values ('Тест іс-шара', 'career', $1, $2, $3, $4, $5, $6, $7, $8::event_status, $9)
			returning id`, start, start.Add(o.dur), o.univ, o.capacity, o.walkIn, o.cert, o.teams, o.status, cb).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func intp(n int) *int { return &n }

func path(id int64, rest string) string { return fmt.Sprintf("/api/events/%d%s", id, rest) }

// startQR starts check-in as the organizer and returns the current ci_ token.
func startQR(t *testing.T, ev, org int64) string {
	t.Helper()
	w := do(t, "POST", path(ev, "/checkin/start"), org, nil)
	if w.Code != 200 {
		t.Fatalf("start check-in: %d %s", w.Code, w.Body)
	}
	return decode(t, w)["token"].(string)
}

func TestFeatureOffIs404(t *testing.T) {
	t.Setenv("FEATURE_EVENTS", "false")
	for _, c := range [][2]string{{"GET", "/api/events"}, {"GET", "/api/events/1"}, {"POST", "/api/events/1/register"},
		{"POST", "/api/events/checkin"}, {"GET", "/api/cron/events"}, {"POST", "/api/events/create"}} {
		if w := do(t, c[0], c[1], 0, map[string]any{}); w.Code != 404 || errCode(w) != "not_found" {
			t.Errorf("%s %s with events off: %d %s", c[0], c[1], w.Code, w.Body)
		}
	}
}

func TestCreatePermissions(t *testing.T) {
	testdb.Need(t)
	student := newUser(t, "student", "ENU", "")
	ambA := newUser(t, "ambassador", "ENU", "")
	ambB := newUser(t, "ambassador", "KazNU", "")
	staffA := newUser(t, "university_staff", "ENU", "")
	admin := newUser(t, "admin", "", "")
	start := time.Now().Add(48 * time.Hour).UTC()
	body := func(univ string) map[string]any {
		b := map[string]any{"title_kk": "Мансап күні", "cat": "career", "starts_at": start, "ends_at": start.Add(2 * time.Hour),
			"capacity": 50, "status": "published"}
		if univ != "" {
			b["university"] = univ
		}
		return b
	}
	if w := do(t, "POST", "/api/events/create", 0, body("")); w.Code != 401 {
		t.Fatalf("anonymous create: %d", w.Code)
	}
	if w := do(t, "POST", "/api/events/create", student, body("")); w.Code != 403 {
		t.Fatalf("student create: %d", w.Code)
	}
	if w := do(t, "POST", "/api/events/create", ambA, body("KazNU")); w.Code != 403 || errCode(w) != "foreign_university" {
		t.Fatalf("ambassador creates for another university: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/events/create", ambA, map[string]any{"title_kk": "x", "cat": "career"}); w.Code != 400 {
		t.Fatalf("invalid body: %d", w.Code)
	}
	w := do(t, "POST", "/api/events/create", ambA, body(""))
	if w.Code != 201 {
		t.Fatalf("ambassador create: %d %s", w.Code, w.Body)
	}
	ev := decode(t, w)["event"].(map[string]any)
	id := int64(ev["id"].(float64))
	if ev["university"] != "ENU" || ev["created_by"].(float64) != float64(ambA) {
		t.Fatalf("created: %v", ev)
	}
	// another university's ambassador cannot edit; same-university staff and admin can
	if w := do(t, "PATCH", path(id, ""), ambB, map[string]any{"title_kk": "Бөтен"}); w.Code != 403 {
		t.Fatalf("foreign ambassador patch: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", path(id, ""), staffA, map[string]any{"title_kk": "Жаңа атау"}); w.Code != 200 {
		t.Fatalf("staff patch: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", path(id, ""), admin, map[string]any{"capacity": 60}); w.Code != 200 {
		t.Fatalf("admin patch: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", path(id, ""), ambA, map[string]any{"university": "KazNU"}); w.Code != 400 {
		t.Fatalf("moving university: %d", w.Code)
	}
	if w := do(t, "PATCH", path(id, ""), ambA, map[string]any{"status": "finished"}); w.Code != 400 {
		t.Fatalf("finish via patch: %d", w.Code)
	}
	if w := do(t, "POST", "/api/events/create", admin, body("KazNU")); w.Code != 201 {
		t.Fatalf("admin create: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", path(id, "/checkin/start"), ambB, nil); w.Code != 403 {
		t.Fatalf("foreign check-in start: %d", w.Code)
	}
	// RLS backs the Go check: a foreign ambassador's direct update touches nothing
	err := db.WithUser(ctx(), ambB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx(), `update events set title_kk = 'hack' where id = $1`, id)
		if err == nil && tag.RowsAffected() != 0 {
			t.Errorf("RLS let a foreign ambassador update")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// drafts are invisible to students
	draft := newEvent(t, evOpts{status: "draft", startsIn: 24 * time.Hour})
	if w := do(t, "GET", path(draft, ""), student, nil); w.Code != 404 {
		t.Fatalf("student sees draft: %d", w.Code)
	}
	if w := do(t, "GET", path(draft, ""), ambA, nil); w.Code != 200 {
		t.Fatalf("organizer draft: %d", w.Code)
	}
}

func TestCapacityRaceAndWaitlist(t *testing.T) {
	testdb.Need(t)
	ev := newEvent(t, evOpts{capacity: intp(5), startsIn: 24 * time.Hour})
	const n = 30
	users := make([]int64, n)
	for i := range users {
		users[i] = newUser(t, "student", "ENU", "")
	}
	var wg sync.WaitGroup
	codes := make([]int, n)
	statuses := make([]string, n)
	for i, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := do(t, "POST", path(ev, "/register"), u, nil)
			codes[i] = w.Code
			var out map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			statuses[i], _ = out["status"].(string)
		}()
	}
	wg.Wait()
	reg, wait := 0, 0
	for i := range codes {
		if codes[i] != 200 {
			t.Fatalf("register %d: %d", i, codes[i])
		}
		switch statuses[i] {
		case "registered":
			reg++
		case "waitlist":
			wait++
		}
	}
	if reg != 5 || wait != n-5 {
		t.Fatalf("registered %d waitlist %d", reg, wait)
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1 and status = 'registered'`, ev); got != 5 {
		t.Fatalf("db registered = %d", got)
	}
	// registering twice changes nothing
	w := do(t, "POST", path(ev, "/register"), users[0], nil)
	if w.Code != 200 || decode(t, w)["already"] != true {
		t.Fatalf("double register: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1`, ev); got != n {
		t.Fatalf("rows after double register = %d", got)
	}
	// a registered person cancels -> the oldest waitlisted gets the seat and a direct message
	var leaver, first int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx(), `select user_id from event_regs where event_id = $1 and status = 'registered' limit 1`, ev).Scan(&leaver); err != nil {
			return err
		}
		return tx.QueryRow(ctx(), `select user_id from event_regs where event_id = $1 and status = 'waitlist'
			order by registered_at, user_id limit 1`, ev).Scan(&first)
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, "DELETE", path(ev, "/register"), leaver, nil); w.Code != 200 {
		t.Fatalf("unregister: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1 and user_id = $2 and status = 'registered'`, ev, first); got != 1 {
		t.Fatal("first waitlisted was not promoted")
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1 and status = 'registered'`, ev); got != 5 {
		t.Fatalf("registered after promotion = %d", got)
	}
	waitFor(t, func() bool { return len(sent.of(first)) == 1 })
	if w := do(t, "DELETE", path(ev, "/register"), leaver, nil); w.Code != 404 {
		t.Fatalf("second unregister: %d", w.Code)
	}
	// concurrent cancellations + registrations still never exceed capacity
	more := make([]int64, 10)
	for i := range more {
		more[i] = newUser(t, "student", "ENU", "")
	}
	regs := []int64{}
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx(), `select user_id from event_regs where event_id = $1 and status = 'registered'`, ev)
		regs, _ = pgx.CollectRows(rows, pgx.RowTo[int64])
		return nil
	})
	for _, u := range regs {
		wg.Add(1)
		go func() { defer wg.Done(); do(t, "DELETE", path(ev, "/register"), u, nil) }()
	}
	for _, u := range more {
		wg.Add(1)
		go func() { defer wg.Done(); do(t, "POST", path(ev, "/register"), u, nil) }()
	}
	wg.Wait()
	if got := count(t, `select count(*) from event_regs where event_id = $1 and status = 'registered'`, ev); got != 5 {
		t.Fatalf("registered after churn = %d (capacity 5)", got)
	}
	// capacity raised -> waitlist moves up
	admin := newUser(t, "admin", "", "")
	if w := do(t, "PATCH", path(ev, ""), admin, map[string]any{"capacity": 8}); w.Code != 200 {
		t.Fatalf("raise capacity: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1 and status = 'registered'`, ev); got != 8 {
		t.Fatalf("registered after raising capacity = %d", got)
	}
	if w := do(t, "PATCH", path(ev, ""), admin, map[string]any{"capacity": 2}); w.Code != 409 {
		t.Fatalf("capacity below registered: %d", w.Code)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestRLSRegistrationsArePrivate(t *testing.T) {
	testdb.Need(t)
	ev := newEvent(t, evOpts{startsIn: 24 * time.Hour})
	a := newUser(t, "student", "ENU", "Экономика")
	b := newUser(t, "student", "ENU", "Физика")
	outsider := newUser(t, "student", "ENU", "")
	org := newUser(t, "ambassador", "ENU", "")
	for _, u := range []int64{a, b} {
		if w := do(t, "POST", path(ev, "/register"), u, nil); w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	seen := func(uid int64) int {
		var n int
		if err := db.WithUser(ctx(), uid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx(), `select count(*) from event_regs where event_id = $1`, ev).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if seen(a) != 1 || seen(outsider) != 0 {
		t.Fatalf("student sees %d rows, outsider %d", seen(a), seen(outsider))
	}
	if seen(org) != 2 {
		t.Fatalf("organizer sees %d", seen(org))
	}
	// the student cannot write their own registration around the RPC
	err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update event_regs set checked_in_at = now() where user_id = app_user_id()`)
		return err
	})
	if err == nil {
		t.Fatal("student could set checked_in_at directly")
	}
	err = db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `insert into event_regs (event_id, user_id, ticket) values ($1, app_user_id(), 'x')`, ev)
		return err
	})
	if err == nil {
		t.Fatal("student inserted a registration directly")
	}
	err = db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `select checkin_key from events limit 1`)
		return err
	})
	if err == nil {
		t.Fatal("app_user can read checkin_key")
	}
	// "who is going": count for everyone, names only for registered people
	w := do(t, "GET", path(ev, "/attendees"), outsider, nil)
	if out := decode(t, w); out["count"].(float64) != 2 || out["attendees"] != nil {
		t.Fatalf("outsider attendees: %s", w.Body)
	}
	w = do(t, "GET", path(ev, "/attendees"), 0, nil)
	if out := decode(t, w); out["count"].(float64) != 2 || out["attendees"] != nil {
		t.Fatalf("anonymous attendees: %s", w.Body)
	}
	w = do(t, "GET", path(ev, "/attendees"), a, nil)
	if out := decode(t, w); len(out["attendees"].([]any)) != 2 {
		t.Fatalf("registered attendees: %s", w.Body)
	}
	// my registrations
	w = do(t, "GET", "/api/events/my", a, nil)
	if regs := decode(t, w)["registrations"].([]any); len(regs) != 1 {
		t.Fatalf("my: %s", w.Body)
	}
	if w := do(t, "GET", path(ev, "/regs"), a, nil); w.Code != 403 {
		t.Fatalf("student organizer list: %d", w.Code)
	}
}

func TestQRCheckin(t *testing.T) {
	testdb.Need(t)
	org := newUser(t, "ambassador", "ENU", "")
	ev := newEvent(t, evOpts{startsIn: 10 * time.Minute, createdBy: org})
	reg := newUser(t, "student", "ENU", "")
	stranger := newUser(t, "student", "ENU", "")
	if w := do(t, "POST", path(ev, "/register"), reg, nil); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := do(t, "GET", path(ev, "/checkin/live"), org, nil); w.Code != 409 || errCode(w) != "checkin_not_started" {
		t.Fatalf("live before start: %d %s", w.Code, w.Body)
	}
	tok := startQR(t, ev, org)
	if !strings.HasPrefix(tok, "ci_") || len(tok) > 64 || strings.Trim(tok, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
		t.Fatalf("token not startapp-safe: %q", tok)
	}
	live := decode(t, do(t, "GET", path(ev, "/checkin/live"), org, nil))
	if live["registered"].(float64) != 1 || live["checked_in"].(float64) != 0 || live["notice_kk"] == "" {
		t.Fatalf("live: %v", live)
	}
	// not registered and walk-in off
	if w := do(t, "POST", "/api/events/checkin", stranger, map[string]string{"token": tok}); w.Code != 403 || errCode(w) != "not_registered" {
		t.Fatalf("stranger: %d %s", w.Code, w.Body)
	}
	// forged: flipped MAC byte, foreign event id, garbage
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tok, "ci_"))
	bad := append([]byte(nil), raw...)
	bad[len(bad)-1] ^= 1
	for _, f := range []string{"ci_" + base64.RawURLEncoding.EncodeToString(bad), "ci_garbage", "", tok + "x"} {
		if w := do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": f}); w.Code != 400 || errCode(w) != "token_invalid" {
			t.Fatalf("forged %q: %d %s", f, w.Code, w.Body)
		}
	}
	other := newEvent(t, evOpts{startsIn: 10 * time.Minute})
	otherTok := startQR(t, other, newUser(t, "admin", "", ""))
	oraw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(otherTok, "ci_"))
	copy(oraw[:8], raw[:8]) // our event id, the other event's signature
	if w := do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": "ci_" + base64.RawURLEncoding.EncodeToString(oraw)}); w.Code != 400 {
		t.Fatalf("cross-event token: %d", w.Code)
	}
	// screenshot: a token from two minutes ago is refused
	var nonce []byte
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select checkin_key from events where id = $1`, ev).Scan(&nonce)
	})
	old := "ci_" + MakeToken(ev, nonce, time.Now().Add(-2*time.Minute))
	if w := do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": old}); w.Code != 400 || errCode(w) != "token_expired" {
		t.Fatalf("old token: %d %s", w.Code, w.Body)
	}
	// the previous minute's token is still fine during the 30 s grace, not after
	base := time.Now().Truncate(time.Minute)
	prev := "ci_" + MakeToken(ev, nonce, base.Add(-time.Second))
	Now = func() time.Time { return base.Add(10 * time.Second) }
	if err := verifyToken(mustRaw(prev), nonce, Now()); err != nil {
		t.Fatalf("grace: %v", err)
	}
	Now = func() time.Time { return base.Add(40 * time.Second) }
	if err := verifyToken(mustRaw(prev), nonce, Now()); err != errTokenExpired {
		t.Fatalf("after grace: %v", err)
	}
	Now = time.Now
	// fresh token works; a replay by the same student is idempotent
	w := do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": tok})
	if w.Code != 200 || decode(t, w)["status"] != "checked_in" {
		t.Fatalf("check-in: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "✅") {
		t.Fatalf("welcome: %s", w.Body)
	}
	w = do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": tok})
	if w.Code != 200 || decode(t, w)["status"] != "already" {
		t.Fatalf("replay: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1 and checked_in_at is not null`, ev); got != 1 {
		t.Fatalf("checked in rows = %d", got)
	}
	// restarting check-in rotates the key: the old QR dies
	tok2 := startQR(t, ev, org)
	if w := do(t, "POST", "/api/events/checkin", stranger, map[string]string{"token": tok}); w.Code != 400 {
		t.Fatalf("token after restart: %d", w.Code)
	}
	// organizer "not confirmed" -> QR refused afterwards, manual brings them back
	if w := do(t, "POST", path(ev, "/checkin/revoke"), org, map[string]any{"user_id": reg}); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": tok2}); w.Code != 403 || errCode(w) != "checkin_revoked" {
		t.Fatalf("after revoke: %d %s", w.Code, w.Body)
	}
	var ticket string
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select ticket from event_regs where event_id = $1 and user_id = $2`, ev, reg).Scan(&ticket)
	})
	if w := do(t, "POST", path(ev, "/checkin/manual"), org, map[string]any{"ticket": ticket}); w.Code != 200 {
		t.Fatalf("manual by ticket: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", path(ev, "/checkin/manual"), org, map[string]any{"ticket": "nope"}); w.Code != 404 {
		t.Fatalf("unknown ticket: %d", w.Code)
	}
	if w := do(t, "POST", path(ev, "/checkin/manual"), reg, map[string]any{"user_id": reg}); w.Code != 403 {
		t.Fatalf("student manual: %d", w.Code)
	}
	// signed-out scan -> claim, completed after login, not reusable
	late := newUser(t, "student", "ENU", "")
	do(t, "POST", path(ev, "/register"), late, nil)
	w = do(t, "POST", "/api/events/checkin", 0, map[string]string{"token": tok2})
	if w.Code != 202 {
		t.Fatalf("anonymous scan: %d %s", w.Code, w.Body)
	}
	claimID := decode(t, w)["claim"].(string)
	if w := do(t, "POST", "/api/events/checkin/claim", late, map[string]string{"claim": claimID}); w.Code != 200 {
		t.Fatalf("claim: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/events/checkin/claim", stranger, map[string]string{"claim": claimID}); w.Code != 410 {
		t.Fatalf("claim reuse: %d", w.Code)
	}
	exec(t, `insert into event_checkin_claims (id, event_id, created_at, expires_at) values ('expired-claim', $1, now() - interval '11 minutes', now() - interval '1 minute')`, ev)
	if w := do(t, "POST", "/api/events/checkin/claim", stranger, map[string]string{"claim": "expired-claim"}); w.Code != 410 {
		t.Fatalf("expired claim: %d", w.Code)
	}
	live = decode(t, do(t, "GET", path(ev, "/checkin/live"), org, nil))
	if live["checked_in"].(float64) != 2 {
		t.Fatalf("live count: %v", live)
	}
	// outside the window: an event starting in 2 hours
	later := newEvent(t, evOpts{startsIn: 2 * time.Hour, createdBy: org})
	do(t, "POST", path(later, "/register"), reg, nil)
	ltok := startQR(t, later, org)
	if w := do(t, "POST", "/api/events/checkin", reg, map[string]string{"token": ltok}); w.Code != 409 || errCode(w) != "checkin_closed" {
		t.Fatalf("too early: %d %s", w.Code, w.Body)
	}
}

func mustRaw(tok string) []byte {
	_, raw, _ := tokenEvent(tok)
	return raw
}

func TestWalkInAndTeams(t *testing.T) {
	testdb.Need(t)
	org := newUser(t, "admin", "", "")
	ev := newEvent(t, evOpts{startsIn: -5 * time.Minute, walkIn: true, teams: true})
	tok := startQR(t, ev, org)
	facs := []string{"A", "A", "A", "A", "B", "B", "B", "C", "C", "D", "D", "E"}
	var users []int64
	for _, f := range facs {
		u := newUser(t, "student", "ENU", f)
		users = append(users, u)
		w := do(t, "POST", "/api/events/checkin", u, map[string]string{"token": tok})
		if w.Code != 200 || decode(t, w)["team_no"] == nil {
			t.Fatalf("walk-in: %d %s", w.Code, w.Body)
		}
	}
	if got := count(t, `select count(*) from event_regs where event_id = $1 and checkin_method = 'walk_in'`, ev); got != len(facs) {
		t.Fatalf("walk-ins = %d", got)
	}
	if got := count(t, `select max(c) from (select count(*) c from event_regs where event_id = $1 group by team_no) x`, ev); got > TeamMax {
		t.Fatalf("team over %d: %d", TeamMax, got)
	}
	// re-split: 12 people -> 2 teams of 6, faculty A spread over both
	w := do(t, "POST", path(ev, "/teams"), org, nil)
	if w.Code != 200 || decode(t, w)["teams"].(float64) != 2 {
		t.Fatalf("split: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(distinct team_no) from event_regs r join users u on u.id = r.user_id where event_id = $1 and u.faculty = 'A'`, ev); got != 2 {
		t.Fatalf("faculty A in %d teams", got)
	}
	if got := count(t, `select min(c) from (select count(*) c from event_regs where event_id = $1 group by team_no) x`, ev); got < 4 {
		t.Fatalf("team smaller than 4: %d", got)
	}
	// "with my previous team": at the next event a returning person joins their old team-mates
	exec(t, `update events set status = 'finished' where id = $1`, ev)
	ev2 := newEvent(t, evOpts{startsIn: -5 * time.Minute, walkIn: true, teams: true})
	tok2 := startQR(t, ev2, org)
	var mates []int64
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx(), `select user_id from event_regs where event_id = $1 and team_no = 2 order by user_id`, ev)
		mates, _ = pgx.CollectRows(rows, pgx.RowTo[int64])
		return nil
	})
	// fill team 1 with strangers first
	for range 5 {
		do(t, "POST", "/api/events/checkin", newUser(t, "student", "ENU", "Z"), map[string]string{"token": tok2})
	}
	do(t, "POST", "/api/events/checkin", mates[0], map[string]string{"token": tok2})
	if w := do(t, "POST", path(ev2, "/register"), mates[1], map[string]any{"with_previous_team": true}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	do(t, "POST", "/api/events/checkin", mates[1], map[string]string{"token": tok2})
	if got := count(t, `select count(distinct team_no) from event_regs where event_id = $1 and user_id = any($2)`, ev2, mates[:2]); got != 1 {
		t.Fatal("previous team-mates were not put together")
	}
}

func TestFinishFeedbackCertificatesAndExport(t *testing.T) {
	testdb.Need(t)
	org := newUser(t, "ambassador", "ENU", "")
	ev := newEvent(t, evOpts{startsIn: -10 * time.Minute, cert: true, createdBy: org})
	came := newUser(t, "student", "ENU", "")
	noshow := newUser(t, "student", "ENU", "")
	for _, u := range []int64{came, noshow} {
		do(t, "POST", path(ev, "/register"), u, nil)
	}
	tok := startQR(t, ev, org)
	do(t, "POST", "/api/events/checkin", came, map[string]string{"token": tok})
	if w := do(t, "POST", path(ev, "/feedback"), came, map[string]any{"score": 5}); w.Code != 409 {
		t.Fatalf("feedback before end: %d", w.Code)
	}
	if w := do(t, "POST", path(ev, "/finish"), came, nil); w.Code != 403 {
		t.Fatalf("student finish: %d", w.Code)
	}
	w := do(t, "POST", path(ev, "/finish"), org, nil)
	if w.Code != 200 || decode(t, w)["certificates"].(float64) != 1 {
		t.Fatalf("finish: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", path(ev, "/finish"), org, nil); w.Code != 409 {
		t.Fatalf("finish twice: %d", w.Code)
	}
	if w := do(t, "POST", path(ev, "/feedback"), noshow, map[string]any{"score": 1}); w.Code != 403 {
		t.Fatalf("no-show feedback: %d", w.Code)
	}
	if w := do(t, "POST", path(ev, "/feedback"), came, map[string]any{"score": 6}); w.Code != 400 {
		t.Fatalf("score 6: %d", w.Code)
	}
	if w := do(t, "POST", path(ev, "/feedback"), came, map[string]any{"score": 4, "text": "Жақсы өтті"}); w.Code != 200 {
		t.Fatalf("feedback: %d %s", w.Code, w.Body)
	}
	sum := decode(t, do(t, "GET", path(ev, "/feedback"), org, nil))
	if sum["count"].(float64) != 1 || sum["average"].(float64) != 4 {
		t.Fatalf("summary: %v", sum)
	}
	// verified attendance: only checked_in_at counts
	att := decode(t, do(t, "GET", "/api/events/my/attendance", came, nil))["attendance"].([]any)
	if len(att) != 1 || att[0].(map[string]any)["has_certificate"] != true || att[0].(map[string]any)["verified"] != true {
		t.Fatalf("attendance: %v", att)
	}
	if att := decode(t, do(t, "GET", "/api/events/my/attendance", noshow, nil))["attendance"].([]any); len(att) != 0 {
		t.Fatalf("no-show attendance: %v", att)
	}
	// CSV only for organizers
	if w := do(t, "GET", path(ev, "/attendees.csv"), came, nil); w.Code != 403 {
		t.Fatalf("student csv: %d", w.Code)
	}
	w = do(t, "GET", path(ev, "/attendees.csv"), org, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "checked_in_at") || strings.Count(w.Body.String(), "\n") != 3 {
		t.Fatalf("csv: %d %q", w.Code, w.Body)
	}
	if w := do(t, "GET", path(ev, "/ics"), 0, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "BEGIN:VEVENT") {
		t.Fatalf("ics: %d", w.Code)
	}
	// account deletion removes the person's registrations and certificates, keeps the event
	if err := me.Delete(ctx(), came); err != nil {
		t.Fatal(err)
	}
	if err := me.Delete(ctx(), org); err != nil {
		t.Fatal(err)
	}
	if got := count(t, `select (select count(*) from event_regs where user_id = $1) + (select count(*) from event_certificates where user_id = $1)`, came); got != 0 {
		t.Fatalf("rows left after deletion: %d", got)
	}
	if got := count(t, `select count(*) from events where id = $1 and created_by is null`, ev); got != 1 {
		t.Fatal("event lost or created_by kept after organizer deletion")
	}
}

func TestDailyRemindersRespectOnePerDay(t *testing.T) {
	testdb.Need(t)
	u := newUser(t, "student", "ENU", "")
	busy := newUser(t, "student", "ENU", "")
	now := time.Now().In(time.FixedZone("ALA", 5*3600))
	tomorrowNoon := time.Date(now.Year(), now.Month(), now.Day()+1, 12, 0, 0, 0, now.Location())
	e1 := newEvent(t, evOpts{startsIn: time.Until(tomorrowNoon)})
	e2 := newEvent(t, evOpts{startsIn: time.Until(tomorrowNoon.Add(2 * time.Hour))})
	for _, ev := range []int64{e1, e2} {
		do(t, "POST", path(ev, "/register"), u, nil)
		do(t, "POST", path(ev, "/register"), busy, nil)
	}
	// "busy" already got a bot message today
	exec(t, `insert into bot_daily_sends (user_id, day) values ($1, (now() at time zone 'Asia/Almaty')::date)`, busy)
	st, err := Daily(ctx(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{u, busy} {
		if n := count(t, `select count(*) from notifications where user_id=$1 and category='events'`, uid); n != 2 {
			t.Fatalf("inbox for %d: %d, stats=%+v", uid, n, st)
		}
		if n := count(t, `select count(*) from notify_outbox where user_id=$1 and kind='digest'`, uid); n != 2 {
			t.Fatalf("digest items for %d: %d", uid, n)
		}
	}
	if _, err := Daily(ctx(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, `select count(*) from notifications where user_id=$1`, u); n != 2 {
		t.Fatal("retry duplicated inbox")
	}
	// cron endpoint needs the secret
	if w := do(t, "GET", "/api/cron/events", 0, nil); w.Code != 401 {
		t.Fatalf("cron without secret: %d", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/cron/events", nil)
	r.Header.Set("Authorization", "Bearer cron-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("cron: %d %s", w.Code, w.Body)
	}
}

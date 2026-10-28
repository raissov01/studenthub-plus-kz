package notify

import (
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func inboxTexts(t *testing.T, uid int64) []string {
	t.Helper()
	var out []string
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx(), `select category || ':' || text from notifications where user_id = $1 order by id`, uid)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func emailUser(t *testing.T) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (email, email_verified_at, name, consent_at)
			values ('inbox'||nextval('users_id_seq')||'@example.kz', now(), 'E', now()) returning id`).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Every path writes the inbox, whatever happens to the bot message.
func TestInboxWrittenForEveryPath(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	exec(t, `insert into notify_prefs (user_id, market) values ($1, false)`, uid)

	send(t, uid, CatEvents, "<b>sent</b> &amp; now", at(12, 0))            // sent
	send(t, uid, CatEvents, "quiet", at(23, 0))                            // queued (quiet hours)
	if r := send(t, uid, CatMarket, "off category", at(12, 0)); r != Off { // dropped for Telegram
		t.Fatalf("market off: %v", r)
	}
	if err := Direct(ctx(), uid, "decision"); err != nil {
		t.Fatal(err)
	}
	if err := DirectAs(ctx(), uid, CatJobs, "invited"); err != nil {
		t.Fatal(err)
	}
	got := inboxTexts(t, uid)
	want := []string{"events:sent & now", "events:quiet", "market:off category", "moderation:decision", "jobs:invited"}
	if len(got) != len(want) {
		t.Fatalf("inbox %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("inbox[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Flushing the queued message later does not add a second inbox row.
	before := len(sent(tg))
	flush(t, at(10, 0).Add(24*time.Hour))
	if len(sent(tg)) == before || len(inboxTexts(t, uid)) != len(want) {
		t.Fatalf("flush: sent %d->%d inbox %v", before, len(sent(tg)), inboxTexts(t, uid))
	}
}

// No Telegram (email-only), blocked bot, bot_inactive: the bot is silent, the inbox is not.
func TestInboxWithoutBot(t *testing.T) {
	testdb.Need(t)
	email := emailUser(t)
	inactive, _ := newUser(t)
	exec(t, `update users set bot_inactive = true where id = $1`, inactive)
	for _, uid := range []int64{email, inactive} {
		send(t, uid, CatMentors, "match", at(12, 0))
		if _, err := DigestAs(ctx(), uid, CatMarket, at(12, 0), "listing expires"); err != nil {
			t.Fatal(err)
		}
		if err := Direct(ctx(), uid, "moderated"); err != nil {
			t.Fatal(err)
		}
		if got := inboxTexts(t, uid); len(got) != 3 {
			t.Fatalf("user %d inbox %v", uid, got)
		}
	}
}

// Crons retry digests that were not sent; the inbox stores the text once.
func TestDigestInboxDedupe(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t)
	now := at(12, 0)
	if st, _ := DigestAs(ctx(), uid, CatMentors, now, "check-in"); st != DigestSent {
		t.Fatalf("first: %s", st)
	}
	for i := 0; i < 3; i++ { // same day: claimed, retried on later runs
		if st, _ := DigestAs(ctx(), uid, CatMentors, now.Add(time.Duration(i)*time.Hour), "check-in"); st != DigestClaimed {
			t.Fatalf("retry %d: %s", i, st)
		}
	}
	_, _ = DigestAs(ctx(), uid, CatMentors, now, "another text")
	if got := inboxTexts(t, uid); len(got) != 2 {
		t.Fatalf("dedupe: %v", got)
	}
	// Send (an event, not a retry) is never deduplicated.
	send(t, uid, CatEvents, "same", now)
	send(t, uid, CatEvents, "same", now)
	if got := inboxTexts(t, uid); len(got) != 4 {
		t.Fatalf("send dedupe: %v", got)
	}
}

func TestInboxLinks(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t)
	link := func() *string {
		var l *string
		_ = db.Tx(ctx(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx(), `select link from notifications where user_id = $1 order by id desc limit 1`, uid).Scan(&l)
		})
		return l
	}
	send(t, uid, CatEvents, "Орын босады: https://studenthub-plus-kz.vercel.app/event/42.", at(12, 0))
	if l := link(); l == nil || *l != "/event/42" {
		t.Fatalf("auto link %v", l)
	}
	if _, err := Send(ctx(), uid, CatEvents, "explicit", Opts{Now: at(12, 0), Link: "/mentors/me"}); err != nil {
		t.Fatal(err)
	}
	if l := link(); l == nil || *l != "/mentors/me" {
		t.Fatalf("explicit link %v", l)
	}
	_ = Direct(WithLink(ctx(), "//evil.example/x"), uid, "bad link")
	if l := link(); l != nil {
		t.Fatalf("protocol-relative link stored: %v", *l)
	}
	_ = Direct(ctx(), uid, "foreign https://evil.example/event/1")
	if l := link(); l != nil {
		t.Fatalf("foreign link stored: %v", *l)
	}
}

func TestInboxAPI(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t)
	other, _ := newUser(t)
	for i := 0; i < 5; i++ {
		_ = Direct(ctx(), uid, "msg "+strconv.Itoa(i))
	}
	_ = Direct(ctx(), other, "not yours")

	if w := do(t, "GET", "/api/me/inbox", 0, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	w := do(t, "GET", "/api/me/inbox?limit=2", uid, nil)
	if w.Code != 200 {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	body := w.json(t)
	items := body["items"].([]any)
	if len(items) != 2 || body["unread"] != float64(5) || body["nextBefore"] == nil {
		t.Fatalf("page 1: %s", w.Body)
	}
	first := items[0].(map[string]any)
	if first["text"] != "msg 4" || first["read"] != false || first["category"] != "moderation" {
		t.Fatalf("newest first: %v", first)
	}
	next := int64(body["nextBefore"].(float64))
	w = do(t, "GET", "/api/me/inbox?limit=50&before="+strconv.FormatInt(next, 10), uid, nil)
	if n := len(w.json(t)["items"].([]any)); n != 3 {
		t.Fatalf("page 2: %d", n)
	}
	for _, bad := range []string{"?limit=0", "?limit=51", "?before=x", "?before=-1"} {
		if w := do(t, "GET", "/api/me/inbox"+bad, uid, nil); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}

	// GET /api/me carries the unread count for the bell badge.
	if w := do(t, "GET", "/api/me", uid, nil); w.json(t)["inboxUnread"] != float64(5) {
		t.Fatalf("me: %s", w.Body)
	}

	// Mark one read; ids of another user are ignored.
	var otherID int64
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select id from notifications where user_id = $1`, other).Scan(&otherID)
	})
	firstID := int64(first["id"].(float64))
	w = do(t, "POST", "/api/me/inbox/read", uid, map[string]any{"ids": []int64{firstID, otherID}})
	if w.Code != 200 || w.json(t)["unread"] != float64(4) || w.json(t)["marked"] != float64(1) {
		t.Fatalf("read ids: %d %s", w.Code, w.Body)
	}
	if count(t, `select count(*) from notifications where id = $1 and read_at is null`, otherID) != 1 {
		t.Fatal("marked another user's notification")
	}
	if w := do(t, "GET", "/api/me/inbox?unread=1", uid, nil); len(w.json(t)["items"].([]any)) != 4 {
		t.Fatalf("unread filter: %s", w.Body)
	}
	for _, bad := range []any{map[string]any{}, map[string]any{"all": true, "ids": []int64{1}}, map[string]any{"ids": []int64{0}}} {
		if w := do(t, "POST", "/api/me/inbox/read", uid, bad); w.Code != 400 {
			t.Errorf("%v: %d", bad, w.Code)
		}
	}
	w = do(t, "POST", "/api/me/inbox/read", uid, map[string]any{"all": true})
	if w.Code != 200 || w.json(t)["unread"] != float64(0) {
		t.Fatalf("read all: %s", w.Body)
	}
	if w := do(t, "GET", "/api/me", uid, nil); w.json(t)["inboxUnread"] != float64(0) {
		t.Fatalf("me after read: %s", w.Body)
	}
	if count(t, `select count(*) from notifications where user_id = $1 and read_at is null`, other) != 1 {
		t.Fatal("read all touched another user")
	}
	// The export includes the inbox.
	if w := do(t, "GET", "/api/me/export", uid, nil); w.Code != 200 || len(w.json(t)["notifications"].([]any)) != 5 {
		t.Fatalf("export: %d", w.Code)
	}
}

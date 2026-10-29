package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// All producer categories, including the former moderation bypass, share three
// direct slots. Digest is a separate single slot, regardless of module ordering.
func TestGlobalBudgetsAcrossModules(t *testing.T) {
	testdb.Need(t)
	modules := []struct {
		name     string
		category Category
	}{
		{"events", CatEvents}, {"ambassador", CatModeration}, {"mentors", CatMentors},
		{"market", CatMarket}, {"b2b", CatJobs}, {"moderation", CatModeration},
		{"reminders", CatDeadlines}, {"community", CatCommunity},
	}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
			uid, tg := newUser(t)
			now := at(12, 0)
			for i := range modules {
				index := i
				if reverse {
					index = len(modules) - 1 - i
				}
				m := modules[index]
				direct := WithKey(ctx(), m.name+":direct")
				got, err := Send(direct, uid, m.category, m.name+" direct", Opts{Now: now})
				if err != nil {
					t.Fatal(err)
				}
				want := Queued
				if i < 3 {
					want = Sent
				}
				if got != want {
					t.Fatalf("%s: %s want %s", m.name, got, want)
				}
				for range 2 {
					if _, err := Send(direct, uid, m.category, m.name+" direct", Opts{Now: now}); err != nil {
						t.Fatal(err)
					}
					if _, err := DigestAs(WithKey(ctx(), m.name+":digest"), uid, m.category, now, m.name+" digest"); err != nil {
						t.Fatal(err)
					}
				}
			}
			flush(t, now)
			flush(t, now)
			messages := sent(tg)
			if len(messages) != 4 {
				t.Fatalf("global direct<=3 + digest<=1: %v", messages)
			}
			digest := messages[3]
			for _, m := range modules {
				if !strings.Contains(digest, m.name+" digest") {
					t.Fatalf("missing %s: %s", m.name, digest)
				}
			}
			if strings.Index(digest, "reminders digest") > strings.Index(digest, "events digest") || strings.Index(digest, "events digest") > strings.Index(digest, "market digest") {
				t.Fatalf("priority: %s", digest)
			}
			if n := len(inboxTexts(t, uid)); n != 2*len(modules) {
				t.Fatalf("duplicate/missing inbox entries: %d", n)
			}
			if n := count(t, `select count(*) from notify_sends where user_id=$1 and kind='direct'`, uid); n != 3 {
				t.Fatal(n)
			}
			if n := count(t, `select count(*) from notify_sends where user_id=$1 and kind='digest'`, uid); n != 1 {
				t.Fatal(n)
			}
			// Retries on a new day must not recreate yesterday's delivered notices.
			for _, m := range modules {
				_, _ = DigestAs(WithKey(ctx(), m.name+":digest"), uid, m.category, now.AddDate(0, 0, 1), m.name+" digest")
			}
			flush(t, now.AddDate(0, 0, 1))
			if len(sent(tg)) != 7 || len(inboxTexts(t, uid)) != 16 {
				t.Fatalf("next-day retry duplicated a delivery: %v", sent(tg))
			}
		})
	}
}

func TestEveryCategoryQuietAndDisabled(t *testing.T) {
	testdb.Need(t)
	for _, category := range Categories {
		for _, kind := range []string{"direct", "digest"} {
			for _, disabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/off=%v", category, kind, disabled), func(t *testing.T) {
					uid, tg := newUser(t)
					p := Defaults()
					p.On[category] = !disabled
					if err := SavePrefs(ctx(), mustPool(t), uid, p); err != nil {
						t.Fatal(err)
					}
					now := at(22, 0)
					for range 2 {
						if kind == "direct" {
							_, err := Send(ctx(), uid, category, "night", Opts{Now: now, Key: "night"})
							if err != nil {
								t.Fatal(err)
							}
						} else {
							_, err := DigestAs(WithKey(ctx(), "night"), uid, category, now, "night")
							if err != nil {
								t.Fatal(err)
							}
						}
					}
					flush(t, at(7, 59).AddDate(0, 0, 1))
					if len(sent(tg)) != 0 {
						t.Fatal("quiet hours violated")
					}
					flush(t, at(8, 0).AddDate(0, 0, 1))
					flush(t, at(8, 0).AddDate(0, 0, 1))
					want := 1
					if disabled {
						want = 0
					}
					if len(sent(tg)) != want || len(inboxTexts(t, uid)) != 1 {
						t.Fatalf("sent=%v inbox=%v", sent(tg), inboxTexts(t, uid))
					}
				})
			}
		}
	}
}

func TestConcurrentSendAndFlushShareAtomicBudget(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	now := at(12, 0)
	var wg sync.WaitGroup
	errs := make(chan error, 80)
	for i := 0; i < 24; i++ {
		for range 2 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := Send(ctx(), uid, Categories[i%len(Categories)], fmt.Sprintf("message %d", i), Opts{Now: now, Key: fmt.Sprint(i)})
				errs <- err
			}(i)
		}
	}
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := Flush(ctx(), now); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(sent(tg)) != 3 || len(inboxTexts(t, uid)) != 24 {
		t.Fatalf("sent=%d inbox=%d", len(sent(tg)), len(inboxTexts(t, uid)))
	}
	flush(t, now)
	if len(sent(tg)) != 3 {
		t.Fatal("same-day flush exceeded cap")
	}
}

func TestBudgetResetsAtAlmatyMidnight(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	p := Defaults()
	p.QuietEnabled = false
	if err := SavePrefs(ctx(), mustPool(t), uid, p); err != nil {
		t.Fatal(err)
	}
	// 18:59 UTC is 23:59 Almaty, 19:00 UTC starts the next day.
	before := at(23, 59).UTC()
	for i := 0; i < 3; i++ {
		send(t, uid, CatModeration, fmt.Sprint(i), before)
	}
	if got := send(t, uid, CatEvents, "after", before.Add(time.Minute)); got != Sent {
		t.Fatal(got)
	}
	if len(sent(tg)) != 4 {
		t.Fatal(sent(tg))
	}
	_, _ = DigestAs(ctx(), uid, CatDeadlines, before, "today")
	flush(t, before)
	_, _ = DigestAs(ctx(), uid, CatEvents, before.Add(time.Minute), "tomorrow")
	flush(t, before.Add(time.Minute))
	if len(sent(tg)) != 6 {
		t.Fatal(sent(tg))
	}
}

func TestInboxFailurePreventsTelegramAndRollsBackOutbox(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	exec(t, fmt.Sprintf(`alter table notifications add constraint integrity_inbox_failure check(user_id<>%d)`, uid))
	t.Cleanup(func() { exec(t, `alter table notifications drop constraint integrity_inbox_failure`) })
	if _, err := Send(ctx(), uid, CatModeration, "must persist first", Opts{Now: at(12, 0)}); err == nil {
		t.Fatal("inbox failure ignored")
	}
	if len(sent(tg)) != 0 || count(t, `select count(*) from notify_outbox where user_id=$1`, uid) != 0 {
		t.Fatal("delivered without an inbox")
	}
}

func TestKnownRejectionRetryAndAmbiguousOutcome(t *testing.T) {
	testdb.Need(t)
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprintf("ambiguous=%v", ambiguous), func(t *testing.T) {
			uid, _ := newUser(t)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n == 1 && ambiguous {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
					return
				}
				if n == 1 {
					w.WriteHeader(429)
					_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"rate limited"}`))
					return
				}
				_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
			}))
			defer srv.Close()
			pool, err := db.Pool(ctx())
			if err != nil {
				t.Fatal(err)
			}
			d := &Dispatcher{Pool: pool, Token: "test", APIBase: srv.URL}
			now := at(12, 0)
			opts := Opts{Now: now, Key: "retry-me"}
			got, err := d.Send(ctx(), uid, CatJobs, "invited", opts)
			if err != nil {
				t.Fatal(err)
			}
			if ambiguous && got != Uncertain {
				t.Fatal(got)
			}
			if !ambiguous && got != Queued {
				t.Fatal(got)
			}
			for range 3 {
				if _, err := d.Send(ctx(), uid, CatJobs, "invited", opts); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if _, _, err := d.dispatch(ctx(), uid, "direct", 0, now.Add(2*time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			want := int32(2)
			if ambiguous {
				want = 1
			}
			if calls.Load() != want || len(inboxTexts(t, uid)) != 1 {
				t.Fatalf("calls=%d inbox=%v", calls.Load(), inboxTexts(t, uid))
			}
			if n := count(t, `select count(*) from notify_sends where user_id=$1`, uid); n != 1 {
				t.Fatalf("reservation count %d", n)
			}
		})
	}
}

func TestDigestLengthAndKeyboardPreserved(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	pool, err := db.Pool(ctx())
	if err != nil {
		t.Fatal(err)
	}
	var markup json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text   string          `json:"text"`
			Markup json.RawMessage `json:"reply_markup"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if textSize(body.Text) > 4096 {
			t.Error("Telegram length exceeded")
		}
		markup = body.Markup
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	d := &Dispatcher{Pool: pool, Token: "test", APIBase: srv.URL}
	now := at(12, 0)
	_, err = d.Digest(ctx(), uid, CatDeadlines, strings.Repeat("😀", 2500), Opts{Now: now, Key: "long", ReplyMarkup: json.RawMessage(`{"inline_keyboard":[[{"text":"Open","url":"https://example.org"}]]}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = d.dispatch(ctx(), uid, "digest", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markup), "Open") || len(inboxTexts(t, uid)) != 1 {
		t.Fatal("keyboard/inbox missing")
	}
	if len(sent(tg)) != 0 {
		t.Fatal("wrong transport used")
	}
}

func TestLegacyOutboxRetryReusesInbox(t *testing.T) {
	testdb.Need(t)
	for _, hasInbox := range []bool{false, true} {
		t.Run(fmt.Sprintf("inbox=%v", hasInbox), func(t *testing.T) {
			uid, tg := newUser(t)
			now := at(12, 0)
			raw := "<b>legacy</b> &amp; notice"
			if hasInbox {
				exec(t, `insert into notifications(user_id,category,text) values($1,'events','legacy & notice')`, uid)
			}
			exec(t, `insert into notify_outbox(user_id,category,text,not_before,created_at) values($1,'events',$2,$3,$3)`, uid, raw, now)
			for range 2 {
				if got, err := Send(ctx(), uid, CatEvents, raw, Opts{Now: now, Key: "legacy-event"}); err != nil || got != Sent {
					t.Fatalf("%s %v", got, err)
				}
			}
			if len(sent(tg)) != 1 || len(inboxTexts(t, uid)) != 1 || count(t, `select count(*) from notify_outbox where user_id=$1`, uid) != 1 {
				t.Fatal("upgrade retry duplicated/lost a receipt")
			}
			if n := count(t, `select count(*) from notify_outbox where user_id=$1 and notification_id is not null and delivery_key is not null`, uid); n != 1 {
				t.Fatal("legacy receipt not repaired")
			}
		})
	}
}

func TestStaleReservationNeverBlindlyResent(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	now := at(12, 0)
	// Model a process crash after committing its budget reservation and before recording
	// Telegram's result. Whether Telegram accepted the message cannot be determined.
	send(t, uid, CatModeration, "crashed", at(23, 0))
	exec(t, `update notify_outbox set state='sending',claimed_at=$2 where user_id=$1`, uid, now.Add(-time.Hour))
	exec(t, `insert into notify_sends(user_id,category,sent_at,kind,outbox_id) select user_id,category,$2,kind,id from notify_outbox where user_id=$1`, uid, now)
	flush(t, now)
	flush(t, now.Add(time.Hour))
	if len(sent(tg)) != 0 {
		t.Fatal("ambiguous claim was resent")
	}
	if n := count(t, `select count(*) from notify_sends where user_id=$1`, uid); n != 1 {
		t.Fatal("ambiguous attempt lost its budget reservation")
	}
	if got, err := Send(ctx(), uid, CatModeration, "crashed", Opts{Now: now}); err != nil || got != Uncertain {
		t.Fatalf("retry %s %v", got, err)
	}
	if len(inboxTexts(t, uid)) != 1 {
		t.Fatal("crash recovery duplicated the inbox")
	}
}

func TestDistinctSourceKeysKeepIdenticalMessages(t *testing.T) {
	testdb.Need(t)
	uid, tg := newUser(t)
	for _, key := range []string{"pair:1:closed", "pair:2:closed", "pair:2:closed"} {
		if _, err := Send(ctx(), uid, CatMentors, "pair closed", Opts{Now: at(12, 0), Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent(tg)) != 2 || len(inboxTexts(t, uid)) != 2 {
		t.Fatal("distinct source events were incorrectly deduplicated")
	}
}

func TestMissingTokenDoesNotConsumeBudget(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t)
	t.Setenv("BOT_TOKEN", "")
	if got := send(t, uid, CatJobs, "queued until configured", at(12, 0)); got != Queued {
		t.Fatal(got)
	}
	if n := count(t, `select count(*) from notify_sends where user_id=$1`, uid); n != 0 {
		t.Fatal("no-op delivery consumed budget")
	}
	if len(inboxTexts(t, uid)) != 1 {
		t.Fatal("missing inbox")
	}
}

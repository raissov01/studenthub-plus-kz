package notify_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raissov01/studenthub-plus-kz/pkg/ambassador"
	"github.com/raissov01/studenthub-plus-kz/pkg/b2b"
	"github.com/raissov01/studenthub-plus-kz/pkg/bot"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/events"
	"github.com/raissov01/studenthub-plus-kz/pkg/market"
	"github.com/raissov01/studenthub-plus-kz/pkg/mentors"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// Exercise the real producers, not replaced Notify variables: each module creates
// an inbox receipt even with categories off, and all compete for the same budgets.
func TestRealModuleProducersShareDispatcher(t *testing.T) {
	testdb.Need(t)
	ctx := context.Background()
	pool, err := db.Pool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Almaty")
	n := time.Now().In(loc)
	now := time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, loc)
	originalNow := notify.Now
	notify.Now = func() time.Time { return now }
	t.Cleanup(func() { notify.Now = originalNow })
	t.Setenv("MARKET_SEASON", "2035-01-01..2035-01-02")
	var mu sync.Mutex
	messages := map[int64][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/content" {
			_, _ = w.Write([]byte(`{"opportunities":[],"jobs":[],"discounts":[]}`))
			return
		}
		var body struct {
			Chat int64  `json:"chat_id"`
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var inbox int
		if err := pool.QueryRow(ctx, `select count(*) from notifications n join users u on u.id=n.user_id where u.telegram_id=$1`, body.Chat).Scan(&inbox); err != nil || inbox == 0 {
			t.Errorf("Telegram called before inbox commit: %d %v", inbox, err)
		}
		mu.Lock()
		messages[body.Chat] = append(messages[body.Chat], body.Text)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	t.Setenv("BOT_TOKEN", "module-test")
	t.Setenv("TELEGRAM_API_BASE", srv.URL)
	for _, off := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%v", off), func(t *testing.T) {
			uid, tg := moduleFixtures(t, pool, now)
			p := notify.Defaults()
			if off {
				for _, c := range notify.Categories {
					p.On[c] = false
				}
			}
			if err := notify.SavePrefs(ctx, pool, uid, p); err != nil {
				t.Fatal(err)
			}
			b := &bot.Bot{Pool: pool, Cfg: bot.Config{Token: "module-test", APIBase: srv.URL, SiteURL: srv.URL}, Now: func() time.Time { return now }}
			producers := []struct {
				name string
				run  func() error
			}{
				{"events direct", func() error { return events.Notify(notify.WithKey(ctx, "module:event"), uid, "event direct") }},
				{"moderation", func() error {
					return moderation.Notify(notify.WithKey(ctx, "module:moderation"), uid, "moderation direct")
				}},
				{"b2b direct", func() error { return b2b.Notify(notify.WithKey(ctx, "module:job"), uid, "job direct") }},
				{"ambassador", func() error { _, err := ambassador.Daily(ctx, now); return err }},
				{"bot reply", func() error {
					return b.HandleUpdate(ctx, &bot.Update{UpdateID: 9876, Message: &bot.Message{From: &bot.TgUser{ID: tg}, Chat: bot.Chat{ID: tg, Type: "private"}, Text: "/help"}})
				}},
				{"events digest", func() error { _, err := events.Daily(ctx, now); return err }},
				{"mentors", func() error { _, err := mentors.RunCron(ctx, now); return err }},
				{"market", func() error { _, err := market.RunCron(ctx, now); return err }},
				{"b2b digest", func() error { _, err := b2b.Daily(ctx, now); return err }},
				{"bot reminders", func() error { _, err := b.RunCron(ctx, now); return err }},
			}
			for _, producer := range producers {
				before := moduleCount(t, pool, `select count(*) from notifications where user_id=$1`, uid)
				if err := producer.run(); err != nil {
					t.Fatalf("%s: %v", producer.name, err)
				}
				if got := moduleCount(t, pool, `select count(*) from notifications where user_id=$1`, uid); got != before+1 {
					t.Fatalf("%s inbox before=%d after=%d", producer.name, before, got)
				}
				if err := producer.run(); err != nil {
					t.Fatalf("%s retry: %v", producer.name, err)
				}
				if got := moduleCount(t, pool, `select count(*) from notifications where user_id=$1`, uid); got != before+1 {
					t.Fatalf("%s retry duplicated inbox", producer.name)
				}
			}
			for range 2 {
				if _, err := notify.Flush(ctx, now); err != nil {
					t.Fatal(err)
				}
			}
			mu.Lock()
			got := append([]string(nil), messages[tg]...)
			mu.Unlock()
			want := 4
			if off {
				want = 0
			}
			if len(got) != want {
				t.Fatalf("all modules: %d messages want %d: %q", len(got), want, got)
			}
			if !off {
				digest := got[3]
				for _, part := range []string{"Module deadline", "Module event", "🎓", "🎁", "Module Company"} {
					if !strings.Contains(digest, part) {
						t.Fatalf("combined digest missing %s: %s", part, digest)
					}
				}
				if strings.Index(digest, "Module deadline") > strings.Index(digest, "Module event") {
					t.Fatal("deadline priority lost")
				}
			}
		})
	}
}

func moduleCount(t *testing.T, p *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := p.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func moduleFixtures(t *testing.T, p *pgxpool.Pool, now time.Time) (uid, tg int64) {
	t.Helper()
	ctx := context.Background()
	row := func(sql string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := p.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := p.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	uid = row(`insert into users(telegram_id,name,consent_at,bot_started_at,safety_note_sent,is_adult,university)
  values(9000000+nextval('users_id_seq'),'Module Student',now(),now(),true,true,'Module Uni') returning id`)
	if err := p.QueryRow(ctx, `select telegram_id from users where id=$1`, uid).Scan(&tg); err != nil {
		t.Fatal(err)
	}
	other := row(`insert into users(name,email,consent_at,is_adult,university) values('Module Mentor','module-'||nextval('users_id_seq')||'@example.org',now(),true,'Module Uni') returning id`)
	event := row(`insert into events(title_kk,cat,starts_at,ends_at,university,status) values('Module event','other',$1,$2,'Module Uni','published') returning id`, now.Add(24*time.Hour), now.Add(25*time.Hour))
	exec(`insert into event_regs(event_id,user_id,ticket) values($1,$2,'module-ticket-'||$1::text)`, event, uid)
	exec(`insert into mentor_pairs(mentor_id,mentee_id,university,need,started_at) values($1,$2,'Module Uni','study',$3)`, other, uid, now.AddDate(0, 0, -22))
	exec(`insert into market_items(user_id,title,category,condition,university,expires_at) values($1,'Module book','books','good','Module Uni',$2)`, uid, now.AddDate(0, 0, 2))
	employer := row(`insert into employers(owner_user_id,name,bin,website,contact_name,status) values($1,'Module Company',lpad($1::text,12,'0'),'https://example.org','Module Contact','verified') returning id`, other)
	job := row(`insert into jobs(employer_id,title_kk,desc_kk,city,remote,pay,per,hours,sched_kk,apply_kk,minor_ok,status,expires_at)
  values($1,'Module job','Module job description with enough text','astana',true,1000,'hour',2,'Flexible','Apply here',false,'published',$2) returning id`, employer, now.AddDate(0, 0, 10))
	exec(`insert into applications(job_id,employer_id,user_id,shared_fields,status,created_at) values($1,$2,$3,'{}','no_response',$4)`, job, employer, uid, now.AddDate(0, 0, -15))
	exec(`insert into ambassador_terms(user_id,university,term_start,term_end,hours_per_week,ref_code) values($1,'Module Uni',$2::date-30,$2::date+7,3,'module'||$1::text)`, uid, notify.Day(now))
	exec(`insert into reminders(user_id,opportunity_id,title,deadline) values($1,'module-deadline','Module deadline',$2::date+3)`, uid, notify.Day(now))
	return uid, tg
}

// Quiz currently uses WebSocket/HTTP replies only; it has no Telegram notifier. A
// production sendMessage added to any Go module must go through notify's transport.
func TestOnlySharedDispatcherCallsSendMessage(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), `"sendMessage"`) || strings.Contains(string(raw), `"/sendMessage"`) {
			if filepath.Clean(path) != filepath.Join("..", "notify", "notify.go") {
				t.Errorf("Telegram bypass: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

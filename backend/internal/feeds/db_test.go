package feeds

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/content"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Main(m, "feeds")) }

// answers maps a post URL to the AI's answer; the fake server picks it from the user message.
func aiServer(t *testing.T, answers map[string]Result) (*AI, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.Unmarshal(b, &body)
		user := body.Messages[1].Content
		var ans Result
		for url, a := range answers {
			if strings.Contains(user, "Post URL: "+url+"\n") {
				ans = a
			}
		}
		content, _ := json.Marshal(ans)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}},
			"usage":   map[string]any{"prompt_tokens": 800, "completion_tokens": 200},
		})
	}))
	t.Cleanup(srv.Close)
	ai := NewAI(func(k string) string {
		return map[string]string{"OPENAI_API_KEY": "test-key", "OPENAI_BASE_URL": srv.URL}[k]
	})
	ai.Sleep = func(context.Context, time.Duration) error { return nil }
	return ai, &calls
}

func ctxb() context.Context { return context.Background() }

func q1[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error { return tx.QueryRow(ctxb(), sql, args...).Scan(&v) }); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return v
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error { _, err := tx.Exec(ctxb(), sql, args...); return err }); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestCollectorPipeline(t *testing.T) {
	testdb.Need(t)
	t.Setenv("FEATURE_FEEDS", "true")
	recent := now.Add(-2 * time.Hour)
	hackText := "Хакатон Astana Code для студентов: команды до 4 человек, призовой фонд. Приём заявок до 20 ноября. Регистрация https://hack.example.kz/apply/astana-code"
	posts := map[string][]Post{
		"src-high": {
			{ExternalID: "10", URL: "https://t.me/high/10", Text: hackText, Links: []string{"https://hack.example.kz/apply/astana-code"}, PostedAt: &recent},
			{ExternalID: "11", URL: "https://t.me/high/11", Text: "Всем хорошего дня и продуктивной недели, друзья! Не забывайте пить воду.", PostedAt: &recent},
			{ExternalID: "12", URL: "https://t.me/high/12", Text: "Конкурс эссе: обзор итогов прошлогоднего конкурса и фото победителей с церемонии.", PostedAt: &recent},
		},
		"src-med": {
			// the same hackathon reposted by another channel
			{ExternalID: "77", URL: "https://t.me/med/77", Text: "Репост: " + hackText, Links: []string{"https://hack.example.kz/apply/astana-code"}, PostedAt: &recent},
			{ExternalID: "78", URL: "https://t.me/med/78", Text: "Erasmus+ обмен для бакалавров: семестр в Польше, стипендия покрывает проживание. Подробности на сайте вуза.", PostedAt: &recent},
		},
	}
	hack := Result{IsRelevant: true, Category: "hackathon", TitleKK: "Astana Code хакатоны", TitleRU: "Хакатон Astana Code",
		SummaryKK: "Студенттерге арналған хакатон.", SummaryRU: "Хакатон для студентов.", Organizer: "Example", City: "Astana",
		Deadline: "2026-11-20", Funding: "none", ApplyURL: "https://hack.example.kz/apply/astana-code", Confidence: 0.92}
	hackRepost := hack
	hackRepost.Confidence = 0.8
	erasmus := Result{IsRelevant: true, Category: "exchange", TitleKK: "Erasmus+ алмасу", TitleRU: "Обмен Erasmus+",
		SummaryKK: "Польшада семестр.", SummaryRU: "Семестр в Польше.", Deadline: "2026-12-01", Funding: "partly", Confidence: 0.95}
	ai, calls := aiServer(t, map[string]Result{
		"https://t.me/high/10": hack,
		"https://t.me/high/12": {IsRelevant: false, Category: "other", Reason: "report about a past contest"},
		"https://t.me/med/77":  hackRepost,
		"https://t.me/med/78":  erasmus,
	})
	sources := []Source{
		{ID: "src-high", Kind: KindTGPublic, Handle: "high_chan", Name: "High", Lang: "ru", Category: "hackathon", Scope: "KZ", Trust: "high", Enabled: true},
		{ID: "src-med", Kind: KindTGPublic, Handle: "med_chan", Name: "Medium", Lang: "ru", Category: "other", Scope: "KZ", Trust: "medium", Enabled: true},
		{ID: "src-off", Kind: KindRSS, URL: "https://off.example/feed", Name: "Off", Lang: "en", Category: "other", Scope: "international", Trust: "low", Enabled: false},
	}
	fetch := FetcherFunc(func(_ context.Context, s Source) ([]Post, error) {
		if s.ID == "src-off" {
			t.Error("a disabled source was fetched")
		}
		return posts[s.ID], nil
	})
	c := &Collector{Sources: sources, Fetchers: map[string]Fetcher{KindTGPublic: fetch, KindRSS: fetch}, AI: ai,
		Now: func() time.Time { return now }, DailyTokens: 100000, AutoMin: 0.85, UndatedTTL: 45, MaxAgeDays: 30, MaxAIPerRun: 50}
	rep, err := c.Run(ctxb())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Fetched != 5 || rep.New != 5 || rep.Skipped != 1 || rep.AICalls != 4 || rep.Published != 1 || rep.Pending != 1 ||
		rep.Duplicates != 1 || rep.Irrelevant != 1 || rep.Tokens != 4000 {
		t.Fatalf("report %+v", rep)
	}
	if *calls != 4 {
		t.Errorf("ai calls %d", *calls)
	}

	// trusted + confident: published without review; the repost is linked to it
	hackID := q1[int64](t, `select i.id from feed_items i join feed_posts p on p.id = i.post_id where p.external_id = '10'`)
	if st := q1[string](t, `select status from feed_items where id = $1`, hackID); st != StatusPublished {
		t.Errorf("hack status %s", st)
	}
	if ex := q1[string](t, `select extra_sources::text from feed_items where id = $1`, hackID); !strings.Contains(ex, `"source_id": "src-med"`) {
		t.Errorf("extra sources %s", ex)
	}
	if st := q1[string](t, `select status || ':' || coalesce(note, '') from feed_posts where external_id = '77'`); !strings.HasPrefix(st, "duplicate:") {
		t.Errorf("repost %s", st)
	}
	// a medium source never publishes itself: the Erasmus item waits in the moderation queue
	var subID int64
	var erasmusID int64
	_ = db.Tx(ctxb(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctxb(), `select i.id, i.submission_id from feed_items i join feed_posts p on p.id = i.post_id where p.external_id = '78'`).Scan(&erasmusID, &subID)
	})
	if subID == 0 || q1[string](t, `select kind || ':' || status from submissions where id = $1`, subID) != SubmissionKind+":pending" {
		t.Fatalf("submission %d", subID)
	}
	// the guessed deadline (not in the post text) was dropped before anything was stored
	if d := q1[*string](t, `select deadline::text from feed_items where id = $1`, erasmusID); d != nil {
		t.Errorf("guessed deadline stored: %v", *d)
	}
	if conf := q1[float32](t, `select confidence from feed_items where id = $1`, erasmusID); conf > float32(GuessedCap)+0.001 {
		t.Errorf("confidence %v", conf)
	}

	// /api/content serves the published one only, labelled
	recs, err := Published(ctxb())
	if err != nil || len(recs) != 1 {
		t.Fatalf("published %v %v", recs, err)
	}
	raw, _ := json.Marshal(recs[0])
	var rec map[string]any
	_ = json.Unmarshal(raw, &rec)
	feed := rec["feed"].(map[string]any)
	if rec["id"] != IDPrefix+itoa(hackID) || rec["source"] != ContentSource || rec["type"] != "grant" || rec["deadline"] != "2026-11-20" ||
		feed["source_name"] != "High" || feed["reviewed"] != false || !strings.Contains(raw2s(feed["note"]), "ИИ қорытындысы") ||
		feed["original_url"] != "https://t.me/high/10" || rec["city"] != "astana" {
		t.Errorf("record %s", raw)
	}

	// the admin edits and approves the pending item through the shared queue
	admin := q1[int64](t, `insert into users (telegram_id, name, role, consent_at) values (990001, 'Admin', 'admin', now()) returning id`)
	if _, err := edit(map[string]any{"title_kk": "x"}, map[string]any{"deadline": "2020-01-01"}, AlmatyToday(now)); err == nil {
		t.Error("a past deadline must not be accepted in the editor")
	}
	exec(t, `update submissions set payload = payload || '{"title_kk": "Erasmus+ семестр Польшада", "deadline": "2026-12-01"}'::jsonb where id = $1`, subID)
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		_, err := moderation.Review(ctxb(), tx, subID, admin, true, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if st := q1[string](t, `select status || ':' || (data->>'title_kk') || ':' || deadline::text from feed_items where id = $1`, erasmusID); st != "published:Erasmus+ семестр Польшада:2026-12-01" {
		t.Errorf("approved %s", st)
	}
	recs, _ = Published(ctxb())
	if len(recs) != 2 {
		t.Fatalf("after approval %d records", len(recs))
	}
	// the content loader merges them as opportunities (FEATURE_FEEDS on)
	l := &content.Loader{DataDir: t.TempDir(), Warn: func(string) {}}
	got := l.Load(ctxb(), now)
	n := 0
	for _, o := range got.Opportunities {
		b, _ := json.Marshal(o)
		if strings.Contains(string(b), `"source":"feed"`) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("content has %d feed opportunities", n)
	}
	t.Setenv("FEATURE_FEEDS", "false")
	got = l.Load(ctxb(), now)
	for _, o := range got.Opportunities {
		if b, _ := json.Marshal(o); strings.Contains(string(b), `"source":"feed"`) {
			t.Error("feed records served while the switch is off")
		}
	}
	t.Setenv("FEATURE_FEEDS", "true")

	// a second run sees nothing new and makes no AI calls
	rep, err = c.Run(ctxb())
	if err != nil || rep.New != 0 || rep.AICalls != 0 {
		t.Errorf("second run %+v %v", rep, err)
	}

	// expiry: the deadline passes -> hidden from /api/content
	later := time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error { _, err := ExpireItems(ctxb(), tx, later, 45); return err }); err != nil {
		t.Fatal(err)
	}
	if st := q1[string](t, `select status from feed_items where id = $1`, hackID); st != StatusExpired {
		t.Errorf("after the deadline: %s", st)
	}
	recs, _ = Published(ctxb())
	if len(recs) != 1 {
		t.Errorf("expired item still served: %d", len(recs))
	}

	// unpublish by the admin
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error { return Unpublish(ctxb(), tx, erasmusID, admin) }); err != nil {
		t.Fatal(err)
	}
	if recs, _ = Published(ctxb()); len(recs) != 0 {
		t.Errorf("unpublished item served")
	}
}

func TestExpiringPendingLeavesQueue(t *testing.T) {
	testdb.Need(t)
	exec(t, `insert into feed_sources (id, kind, handle, name) values ('src-exp', 'tg_public', 'exp_chan', 'Exp') on conflict do nothing`)
	var item, sub int64
	err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		pid, _, err := InsertPost(ctxb(), tx, Post{SourceID: "src-exp", ExternalID: "1", URL: "https://t.me/exp_chan/1", Text: "Грант, дедлайн 6 ноября"}, "processed", "")
		if err != nil {
			return err
		}
		r := Result{IsRelevant: true, Category: "grant", TitleKK: "Грант", Deadline: "2026-11-06", Funding: "unknown", SourceURL: "https://t.me/exp_chan/1"}
		item, err = InsertItem(ctxb(), tx, NewItem{PostID: pid, Source: Source{ID: "src-exp", Name: "Exp", Trust: "low"}, Result: r, Status: StatusPending})
		if err != nil {
			return err
		}
		return tx.QueryRow(ctxb(), `select submission_id from feed_items where id = $1`, item).Scan(&sub)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		_, err := ExpireItems(ctxb(), tx, time.Date(2026, 11, 7, 6, 0, 0, 0, time.UTC), 45)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if st := q1[string](t, `select status from submissions where id = $1`, sub); st != "rejected" {
		t.Errorf("submission %s", st)
	}
	if st := q1[string](t, `select status from feed_items where id = $1`, item); st != StatusExpired {
		t.Errorf("item %s", st)
	}
}

func TestDailyBudgetStopsAI(t *testing.T) {
	testdb.Need(t)
	day := time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)
	exec(t, `insert into feed_ai_usage (day, calls, prompt_tokens, completion_tokens) values ($1::date, 10, 9000, 1000)`, AlmatyToday(day))
	ai, calls := aiServer(t, nil)
	src := Source{ID: "src-budget", Kind: KindTGPublic, Handle: "budget_chan", Name: "B", Lang: "ru", Category: "grant", Scope: "KZ", Trust: "high", Enabled: true}
	p := Post{ExternalID: "1", URL: "https://t.me/budget_chan/1", Text: "Стипендия для студентов магистратуры, подача заявок открыта на сайте фонда."}
	c := &Collector{Sources: []Source{src}, Fetchers: map[string]Fetcher{KindTGPublic: FetcherFunc(func(context.Context, Source) ([]Post, error) { return []Post{p}, nil })},
		AI: ai, Now: func() time.Time { return day }, DailyTokens: 10000, MaxAIPerRun: 10}
	rep, err := c.Run(ctxb())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.BudgetHit || *calls != 0 || rep.New != 1 {
		t.Errorf("%+v calls %d", rep, *calls)
	}
	if st := q1[string](t, `select status from feed_posts where source_id = 'src-budget'`); st != "new" {
		t.Errorf("post must wait for tomorrow: %s", st)
	}
}

func TestSourceSwitchSurvivesSync(t *testing.T) {
	testdb.Need(t)
	s := Source{ID: "src-sync", Kind: KindRSS, URL: "https://a.example/feed", Name: "A", Lang: "en", Category: "other", Scope: "KZ", Trust: "low", Enabled: true}
	var got []Source
	sync := func() {
		if err := db.Tx(ctxb(), func(tx pgx.Tx) error { var err error; got, err = SyncSources(ctxb(), tx, []Source{s}); return err }); err != nil {
			t.Fatal(err)
		}
	}
	sync()
	if !got[0].Enabled {
		t.Fatal("first sync takes the file's value")
	}
	exec(t, `update feed_sources set enabled = false where id = 'src-sync'`)
	s.Name = "A renamed"
	sync()
	if got[0].Enabled || q1[string](t, `select name from feed_sources where id = 'src-sync'`) != "A renamed" {
		t.Errorf("admin switch lost or name not synced: %+v", got[0])
	}
}

func TestRLSHidesFeedTablesFromStudents(t *testing.T) {
	testdb.Need(t)
	uid := q1[int64](t, `insert into users (telegram_id, name, consent_at) values (990002, 'Student', now()) returning id`)
	for _, table := range []string{"feed_sources", "feed_posts", "feed_items", "feed_ai_usage"} {
		err := db.WithUser(ctxb(), uid, func(tx pgx.Tx) error {
			var n int
			return tx.QueryRow(ctxb(), "select count(*) from "+table).Scan(&n)
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s readable by app_user: %v", table, err)
		}
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func raw2s(v any) string { b, _ := json.Marshal(v); return string(b) }

package bot

// Port of legacy/js/tests/bot.test.mjs. Telegram and the site are httptest fakes; storage is
// a throwaway Postgres database created from supabase/migrations for this test run.
//
// TEST_DATABASE_URL (default postgres://postgres:dev@localhost:54329/studenthub) points at
// any database on the server; the tests create studenthub_bot_test_<pid> next to it and drop
// it at the end. Without a reachable server the DB tests are skipped.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raissov01/studenthub-plus-kz/pkg/db/q"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

var (
	testPool  *pgxpool.Pool
	skipDB    string
	adminConn string
)

func TestMain(m *testing.M) {
	code := func() int {
		cleanup, err := setupDB()
		if err != nil {
			skipDB = err.Error()
		}
		if cleanup != nil {
			defer cleanup()
		}
		return m.Run()
	}()
	os.Exit(code)
}

func setupDB() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		base = "postgres://postgres:dev@localhost:54329/studenthub"
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("postgres unreachable: %v", err)
	}
	name := fmt.Sprintf("studenthub_bot_test_%d", os.Getpid())
	_, _ = admin.Exec(ctx, "drop database if exists "+name)
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		admin.Close(ctx)
		return nil, err
	}
	drop := func() {
		if testPool != nil {
			testPool.Close()
		}
		ctx := context.Background()
		if _, err := admin.Exec(ctx, "drop database if exists "+name+" with (force)"); err != nil {
			fmt.Fprintln(os.Stderr, "drop test database:", err)
		}
		admin.Close(ctx)
	}
	u, _ := url.Parse(base)
	u.Path = "/" + name
	cfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return drop, err
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol // same as pkg/db
	if testPool, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
		return drop, err
	}
	files, _ := filepath.Glob("../../supabase/migrations/*.sql")
	sort.Strings(files)
	if len(files) == 0 {
		return drop, fmt.Errorf("no migrations found")
	}
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return drop, err
		}
		if _, err := testPool.Exec(ctx, string(sql)); err != nil {
			return drop, fmt.Errorf("%s: %v", filepath.Base(f), err)
		}
	}
	return drop, nil
}

// ---- harness ----

type call struct {
	Method string
	Body   map[string]any
}

type harness struct {
	t          *testing.T
	b          *Bot
	mux        *http.ServeMux
	site       *httptest.Server
	tgSrv      *httptest.Server
	mu         sync.Mutex
	sent       []call
	opps       []map[string]any
	contentAPI bool
	tgFail     func(method string, body map[string]any) map[string]any
	today      string
	updateID   int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if skipDB != "" {
		t.Skip("skipping DB test: " + skipDB)
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, "truncate users, reminders, bot_daily_sends, bot_events restart identity cascade"); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, contentAPI: true, today: localDate(time.Now())}
	h.opps = []map[string]any{h.opp("o7", 7), h.opp("o3", 3), h.opp("o1", 1), h.opp("o20", 20), h.opp("old", -2), h.opp("nodl", nil), h.opp("o10", 10)}

	h.tgSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/botTEST_TOKEN/") {
			http.NotFound(w, r)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/botTEST_TOKEN/")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		h.mu.Lock()
		h.sent = append(h.sent, call{method, body})
		n := len(h.sent)
		fail := h.tgFail
		h.mu.Unlock()
		if fail != nil {
			if f := fail(method, body); f != nil {
				w.WriteHeader(int(f["error_code"].(float64)))
				_ = json.NewEncoder(w).Encode(f)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": n}})
	}))
	h.site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/api/content":
			if !h.contentAPI {
				w.WriteHeader(404)
				_, _ = w.Write([]byte("{}"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"opportunities": h.opps, "jobs": []any{}, "discounts": []any{}})
		case "/data/opportunities.json":
			_ = json.NewEncoder(w).Encode(h.opps)
		default:
			h.t.Errorf("unexpected site fetch %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { h.tgSrv.Close(); h.site.Close() })

	h.b = &Bot{
		Cfg: Config{
			Token: "TEST_TOKEN", WebhookSecret: "hook-secret", CronSecret: "cron-secret",
			SiteURL: h.site.URL, APIBase: h.tgSrv.URL,
			Admins: map[string]bool{"900": true, "901": true},
		},
		Pool: testPool,
	}
	h.mux = http.NewServeMux()
	h.b.Register(h.mux)
	return h
}

func (h *harness) opp(id string, inDays any, extra ...map[string]any) map[string]any {
	var dl any
	if n, ok := inDays.(int); ok {
		dl = addDays(h.today, n)
	}
	o := map[string]any{
		"id": id, "type": "grant", "city": "astana", "spec": []string{"all"},
		"title":     map[string]string{"kk": "Мүмкіндік " + id, "ru": "Возможность " + id},
		"deadline":  dl,
		"docs":      []map[string]string{{"kk": "Транскрипт", "ru": "Транскрипт"}},
		"posted_at": addDays(h.today, -1),
	}
	for _, e := range extra {
		for k, v := range e {
			o[k] = v
		}
	}
	return o
}

func (h *harness) reset() {
	h.mu.Lock()
	h.sent = nil
	h.mu.Unlock()
}

func (h *harness) calls() []call {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]call{}, h.sent...)
}

// hook posts an update; secret "" sends no secret header at all.
func (h *harness) hook(update map[string]any, secret string) int {
	h.updateID++
	update["update_id"] = h.updateID
	body, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPost, "/api/telegram", bytes.NewReader(body))
	if secret != "" {
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec.Code
}

func from(id int64, lang string) map[string]any {
	return map[string]any{"id": id, "is_bot": false, "first_name": "S", "language_code": lang}
}

func (h *harness) say(id int64, text string, lang ...string) {
	l := "kk"
	if len(lang) > 0 {
		l = lang[0]
	}
	h.hook(map[string]any{"message": map[string]any{
		"message_id": 1, "from": from(id, l), "chat": map[string]any{"id": id, "type": "private"}, "date": 0, "text": text,
	}}, "hook-secret")
}

func (h *harness) press(id int64, data string, message map[string]any) {
	if message == nil {
		message = map[string]any{"message_id": 5, "chat": map[string]any{"id": id}, "reply_markup": map[string]any{"inline_keyboard": []any{}}}
	}
	h.hook(map[string]any{"callback_query": map[string]any{
		"id": fmt.Sprintf("cb%d", h.updateID), "from": from(id, "kk"), "data": data, "message": message,
	}}, "hook-secret")
}

func (h *harness) messages(chatID int64) []call {
	var out []call
	for _, c := range h.calls() {
		if c.Method == "sendMessage" && (chatID == 0 || int64(c.Body["chat_id"].(float64)) == chatID) {
			out = append(out, c)
		}
	}
	return out
}

func (h *harness) cron(auth string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec.Code
}

func (h *harness) runCron(now time.Time) *CronResult {
	h.t.Helper()
	r, err := h.b.RunCron(context.Background(), now)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func (h *harness) qs() *q.Queries { return q.New(testPool) }

// user creates a bot user directly (like store.saveUser in the JS tests).
func (h *harness) user(tgID int64, lang string) q.User {
	h.t.Helper()
	u, err := h.qs().BotTouchUser(context.Background(), q.BotTouchUserParams{TelegramID: tgID, Lang: lang})
	if err != nil {
		h.t.Fatal(err)
	}
	_ = h.qs().BotMarkSafetyNote(context.Background(), u.ID)
	return u
}

func (h *harness) getUser(tgID int64) *q.User {
	u, err := h.qs().BotUserByTelegram(context.Background(), tgID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return &u
}

func (h *harness) putReminder(userID int64, oppID, deadline, title string) {
	h.t.Helper()
	err := h.qs().BotUpsertReminder(context.Background(), q.BotUpsertReminderParams{
		UserID: userID, OpportunityID: oppID, Title: title, TitleRu: pgText(title), Deadline: pgDate(deadline),
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

// reminders lists the ACTIVE reminder ids of a Telegram user.
func (h *harness) reminders(tgID int64) []string {
	u := h.getUser(tgID)
	if u == nil {
		return nil
	}
	list, err := h.qs().BotUserReminders(context.Background(), u.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	var ids []string
	for _, r := range list {
		ids = append(ids, r.OpportunityID)
	}
	return ids
}

func (h *harness) count(sql string, args ...any) int {
	h.t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func keyboard(c call) [][]map[string]any {
	rm, _ := c.Body["reply_markup"].(map[string]any)
	rows, _ := rm["inline_keyboard"].([]any)
	var out [][]map[string]any
	for _, row := range rows {
		var r []map[string]any
		for _, b := range row.([]any) {
			r = append(r, b.(map[string]any))
		}
		out = append(out, r)
	}
	return out
}

func flat(kb [][]map[string]any) []map[string]any {
	var out []map[string]any
	for _, r := range kb {
		out = append(out, r...)
	}
	return out
}

func textOf(c call) string { s, _ := c.Body["text"].(string); return s }

func mustMatch(t *testing.T, s, re string) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(s) {
		t.Fatalf("%q does not match %s", s, re)
	}
}

func eq[T comparable](t *testing.T, got, want T, msg ...string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", strings.Join(msg, " "), got, want)
	}
}

func nextWeekday(target int) time.Time {
	for i := 1; i <= 7; i++ {
		d := time.Now().Add(time.Duration(i) * 24 * time.Hour)
		if weekday(localDate(d)) == target {
			return d
		}
	}
	panic("unreachable")
}

const day = 24 * time.Hour

// ---- tests (same order as the JS suite) ----

func TestRegistrationKazakh(t *testing.T) {
	h := newHarness(t)
	h.say(100, "/start remind_o20")
	msgs := h.messages(100)
	eq(t, len(msgs), 2, "messages")
	mustMatch(t, textOf(msgs[0]), `«Мүмкіндік o20» мерзімі — \d+ \S+.* 7, 3 және 1 күн қалғанда еске саламын ✅`)
	eq(t, keyboard(msgs[0])[0][0]["url"].(string), h.site.URL+"/opportunity/o20")
	mustMatch(t, textOf(msgs[1]), `Бот ешқашан ақша сұрамайды`)
	eq(t, len(h.reminders(100)), 1)

	h.say(100, "/start remind_o10")
	eq(t, len(h.messages(100)), 3, "money warning is not repeated")
	eq(t, len(h.reminders(100)), 2)
	eq(t, h.count("select count(*) from users where telegram_id = 100 and consent_at is null and bot_started_at is not null"), 1, "bot-only user row")
}

func TestRegistrationRussian(t *testing.T) {
	h := newHarness(t)
	h.say(101, "/start remind_o20", "ru")
	mustMatch(t, textOf(h.messages(101)[0]), `Дедлайн «Возможность o20» — \d+ [а-я]+.*Напомню за 7, 3 и 1 день ✅`)
	mustMatch(t, textOf(h.messages(101)[1]), `Бот никогда не просит денег`)
}

func TestPlainStartAndLanguage(t *testing.T) {
	h := newHarness(t)
	h.say(102, "/start")
	kb := keyboard(h.messages(102)[0])
	eq(t, kb[0][0]["callback_data"].(string), "lang:kk")
	eq(t, kb[0][1]["callback_data"].(string), "lang:ru")
	eq(t, kb[1][0]["url"].(string), h.site.URL+"/")
	h.press(102, "lang:ru", nil)
	eq(t, h.getUser(102).Lang, "ru")
}

func TestContentFallback(t *testing.T) {
	h := newHarness(t)
	h.contentAPI = false
	h.say(103, "/start remind_o20")
	eq(t, len(h.reminders(103)), 1)
}

func TestNotFoundExpiredNoDeadline(t *testing.T) {
	h := newHarness(t)
	cases := []struct{ id, re string }{
		{"nope", `таба алмадым`}, {"old", `аяқталып қойыпты`}, {"nodl", `нақты мерзім жоқ`},
		{"o1", `уақыт қалмады`}, {"bad$id", `таба алмадым`},
	}
	for _, c := range cases {
		h.reset()
		h.say(104, "/start remind_"+c.id)
		m := h.messages(104)[0]
		mustMatch(t, textOf(m), c.re)
		eq(t, keyboard(m)[0][0]["url"].(string), h.site.URL+"/", c.id)
	}
	eq(t, len(h.reminders(104)), 0)
}

func TestCronMergesIntoOneMessage(t *testing.T) {
	h := newHarness(t)
	u := h.user(200, "kk")
	for _, o := range h.opps {
		switch o["id"] {
		case "o7", "o3", "o1", "o20":
			h.putReminder(u.ID, o["id"].(string), o["deadline"].(string), "Мүмкіндік "+o["id"].(string))
		}
	}
	eq(t, h.cron("Bearer cron-secret"), 200)
	msgs := h.messages(200)
	eq(t, len(msgs), 1, "one message")
	txt := textOf(msgs[0])
	i1, i3, i7 := strings.Index(txt, "Мүмкіндік o1"), strings.Index(txt, "Мүмкіндік o3"), strings.Index(txt, "Мүмкіндік o7")
	if !(i1 >= 0 && i1 < i3 && i3 < i7) {
		t.Fatalf("order wrong: %s", txt)
	}
	if strings.Contains(txt, "Мүмкіндік o20") {
		t.Fatal("o20 is not due")
	}
	mustMatch(t, txt, `Ертең — соңғы күн`)
	mustMatch(t, txt, `Дайындайтын құжаттар: Транскрипт`)
	var open, done, skip bool
	for _, b := range flat(keyboard(msgs[0])) {
		if b["url"] == h.site.URL+"/opportunity/o1" {
			open = true
		}
		if b["callback_data"] == "done:o3" && strings.Contains(b["text"].(string), "Тапсырдым ✅") {
			done = true
		}
		if b["callback_data"] == "skip:o7" && strings.Contains(b["text"].(string), "Қажет емес") {
			skip = true
		}
	}
	if !open || !done || !skip {
		t.Fatalf("buttons missing: open=%v done=%v skip=%v", open, done, skip)
	}
}

func TestCronSecondRunSameDaySendsNothing(t *testing.T) {
	h := newHarness(t)
	u := h.user(201, "ru")
	h.putReminder(u.ID, "o3", addDays(h.today, 3), "Возможность o3")
	h.runCron(time.Now())
	eq(t, len(h.messages(201)), 1)
	second := h.runCron(time.Now())
	eq(t, len(h.messages(201)), 1)
	eq(t, second.AlreadySent, 1)
	eq(t, h.count("select count(*) from bot_daily_sends where user_id = $1", u.ID), 1)
}

func TestCron403MarksInactive(t *testing.T) {
	h := newHarness(t)
	u := h.user(202, "kk")
	h.putReminder(u.ID, "o7", addDays(h.today, 7), "Мүмкіндік o7")
	h.tgFail = func(method string, body map[string]any) map[string]any {
		if body["chat_id"] == float64(202) {
			return map[string]any{"ok": false, "error_code": float64(403), "description": "Forbidden: bot was blocked by the user"}
		}
		return nil
	}
	r := h.runCron(time.Now())
	eq(t, r.Blocked, 1)
	eq(t, h.getUser(202).BotInactive, true)
	eq(t, h.count("select count(*) from bot_daily_sends where user_id = $1", u.ID), 1, "403 keeps the day claimed")
	h.reset()
	h.runCron(time.Now().Add(4 * day)) // o7 now has 3 days left
	eq(t, len(h.messages(202)), 0)
}

func TestCronPausedAndResume(t *testing.T) {
	h := newHarness(t)
	h.say(203, "/start remind_o20")
	h.say(203, "/pause")
	if p := pauseISO(h.getUser(203).PausedUntil); !(p > h.today) {
		t.Fatalf("pausedUntil %q", p)
	}
	h.putReminder(h.getUser(203).ID, "o3", addDays(h.today, 3), "Мүмкіндік o3")
	h.reset()
	h.runCron(time.Now())
	eq(t, len(h.messages(203)), 0)
	h.say(203, "/resume")
	h.reset()
	h.runCron(time.Now())
	eq(t, len(h.messages(203)), 1)
}

func TestWebhookSecret(t *testing.T) {
	h := newHarness(t)
	upd := func() map[string]any {
		return map[string]any{"message": map[string]any{"from": from(1, "kk"), "chat": map[string]any{"id": 1, "type": "private"}, "text": "/start"}}
	}
	eq(t, h.hook(upd(), ""), 401, "missing secret")
	eq(t, h.hook(upd(), "wrong"), 401, "wrong secret")
	eq(t, len(h.calls()), 0)
	if h.getUser(1) != nil {
		t.Fatal("user created by a rejected update")
	}
	eq(t, h.hook(upd(), "hook-secret"), 200)

	// the production route checks the secret before touching the database
	t.Setenv("WEBHOOK_SECRET", "env-secret")
	mux := http.NewServeMux()
	Routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/telegram", strings.NewReader("{}"))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "hook-secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	eq(t, rec.Code, 401, "Routes: wrong secret")
}

func TestCronSecret(t *testing.T) {
	h := newHarness(t)
	eq(t, h.cron("Bearer nope"), 401)
	eq(t, h.cron(""), 401)
	eq(t, h.cron("cron-secret"), 401)
	eq(t, len(h.calls()), 0)

	t.Setenv("CRON_SECRET", "env-cron")
	mux := http.NewServeMux()
	Routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.Header.Set("Authorization", "Bearer cron-secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	eq(t, rec.Code, 401, "Routes: wrong cron secret")
}

func TestDoneAndSkipStopReminders(t *testing.T) {
	h := newHarness(t)
	h.say(300, "/start remind_o20")
	h.say(300, "/start remind_o10")
	s := h.site.URL
	message := map[string]any{"message_id": 9, "chat": map[string]any{"id": 300}, "reply_markup": map[string]any{"inline_keyboard": []any{
		[]any{map[string]any{"text": "1 · Ашу", "url": s + "/opportunity/o20"}},
		[]any{map[string]any{"text": "1 · Тапсырдым ✅", "callback_data": "done:o20"}, map[string]any{"text": "1 · Қажет емес", "callback_data": "skip:o20"}},
		[]any{map[string]any{"text": "2 · Ашу", "url": s + "/opportunity/o10"}},
		[]any{map[string]any{"text": "2 · Тапсырдым ✅", "callback_data": "done:o10"}, map[string]any{"text": "2 · Қажет емес", "callback_data": "skip:o10"}},
	}}}
	h.press(300, "done:o20", message)
	eq(t, strings.Join(h.reminders(300), ","), "o10")
	eq(t, h.count("select count(*) from bot_events where kind = 'submitted'"), 1)
	eq(t, h.count("select count(*) from reminders where opportunity_id = 'o20' and status = 'submitted'"), 1)
	var kbEdit *call
	for _, c := range h.calls() {
		if c.Method == "editMessageReplyMarkup" {
			c := c
			kbEdit = &c
			break
		}
	}
	if kbEdit == nil {
		t.Fatal("no keyboard edit")
	}
	raw, _ := json.Marshal(kbEdit.Body["reply_markup"])
	if strings.Contains(string(raw), "o20") || !strings.Contains(string(raw), "o10") {
		t.Fatalf("keyboard after done: %s", raw)
	}
	h.press(300, "skip:o10", message)
	eq(t, len(h.reminders(300)), 0)
	eq(t, h.count("select count(*) from bot_events where kind = 'submitted'"), 1)
	eq(t, h.count("select count(*) from reminders where opportunity_id = 'o10' and status = 'dismissed'"), 1)
	// stopped reminders never come back in cron
	h.reset()
	h.runCron(time.Now().Add(13 * day))
	eq(t, len(h.messages(300)), 0)
}

func TestMyListAndDelete(t *testing.T) {
	h := newHarness(t)
	h.say(301, "/start remind_o20")
	h.say(301, "/start remind_o10")
	h.reset()
	h.say(301, "/my")
	m := h.messages(301)[0]
	if strings.Index(textOf(m), "o10") > strings.Index(textOf(m), "o20") {
		t.Fatal("nearest deadline first")
	}
	var dels []string
	for _, b := range flat(keyboard(m)) {
		if cd, _ := b["callback_data"].(string); strings.HasPrefix(cd, "del:") {
			dels = append(dels, cd)
		}
	}
	eq(t, strings.Join(dels, ","), "del:o10,del:o20")
	h.press(301, "del:o10", nil)
	eq(t, strings.Join(h.reminders(301), ","), "o20")
}

func TestStopReallyDeletes(t *testing.T) {
	h := newHarness(t)
	h.say(400, "/start remind_o20")
	u := h.getUser(400)
	if _, err := h.qs().BotClaimDay(context.Background(), q.BotClaimDayParams{UserID: u.ID, Day: pgDate(h.today)}); err != nil {
		t.Fatal(err)
	}
	h.press(400, "done:o20", nil)
	h.say(400, "/stop")
	msgs := h.messages(400)
	last := keyboard(msgs[len(msgs)-1])
	if last[0][0]["callback_data"] != "stop:yes" {
		t.Fatal("no confirm button")
	}
	if h.getUser(400) == nil {
		t.Fatal("nothing deleted before confirm")
	}
	h.press(400, "stop:no", nil)
	if h.getUser(400) == nil {
		t.Fatal("stop:no deleted the user")
	}
	h.press(400, "stop:yes", nil)
	if h.getUser(400) != nil {
		t.Fatal("user row still there")
	}
	eq(t, h.count("select count(*) from users"), 0, "users")
	eq(t, h.count("select count(*) from reminders"), 0, "reminders")
	eq(t, h.count("select count(*) from bot_daily_sends"), 0, "daily sends")
	eq(t, h.count("select count(*) from bot_events where user_id is not null"), 0, "events keep no user link")
	eq(t, h.count("select count(*) from bot_events where kind = 'submitted'"), 1, "anonymous stats survive")
}

func TestStopKeepsSiteAccount(t *testing.T) {
	h := newHarness(t)
	if _, err := testPool.Exec(context.Background(),
		"insert into users (telegram_id, name, lang, consent_at) values (410, 'Site Student', 'ru', now())"); err != nil {
		t.Fatal(err)
	}
	h.say(410, "/start remind_o20")
	eq(t, h.getUser(410).Lang, "ru", "site language kept")
	h.say(410, "/pause")
	h.press(410, "stop:yes", nil)
	u := h.getUser(410)
	if u == nil {
		t.Fatal("site account must not be deleted by /stop")
	}
	eq(t, u.BotStartedAt.Valid, false, "bot_started_at cleared")
	eq(t, u.PausedUntil.Valid, false, "paused_until cleared")
	eq(t, u.SafetyNoteSent, false, "safety note reset")
	eq(t, u.Name.String, "Site Student")
	eq(t, h.count("select count(*) from reminders where user_id = $1", u.ID), 0)
}

func TestStatsAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.say(500, "/start remind_o20")
	h.reset()
	h.say(500, "/stats")
	if strings.Contains(textOf(h.messages(500)[0]), "Студенттер") {
		t.Fatal("non-admin got stats")
	}
	h.say(900, "/start")
	h.say(900, "/stats")
	var st string
	for _, m := range h.messages(900) {
		if strings.Contains(textOf(m), "Студенттер") {
			st = textOf(m)
		}
	}
	mustMatch(t, st, `Студенттер: 2`)
	mustMatch(t, st, `Белсенді еске салулар: 1`)
	mustMatch(t, st, `«Тапсырдым»: 0`)
}

func TestDigestOffByDefault(t *testing.T) {
	h := newHarness(t)
	h.say(600, "/start remind_o20")
	for _, m := range h.messages(600) {
		if strings.Contains(textOf(m), "Қай қаладасың") {
			t.Fatal("city question with digest off")
		}
	}
	u := h.getUser(600)
	_ = h.qs().BotSetDigestCity(context.Background(), q.BotSetDigestCityParams{ID: u.ID, DigestCity: pgText("astana")})
	_ = h.qs().BotSetDigestDirection(context.Background(), q.BotSetDigestDirectionParams{ID: u.ID, DigestDirection: pgText("it")})
	h.reset()
	r := h.runCron(nextWeekday(4))
	eq(t, r.Digests, 0)
	eq(t, len(h.messages(600)), 0)
	t.Setenv("DIGEST_ENABLED", "")
	eq(t, ConfigFromEnv().Digest, false, "DIGEST_ENABLED defaults to false")
}

func TestDigestEnabled(t *testing.T) {
	h := newHarness(t)
	h.b.Cfg.Digest = true
	asked := func() bool {
		for _, m := range h.messages(700) {
			if strings.Contains(textOf(m), "Қай қаладасың") {
				return true
			}
		}
		return false
	}
	h.say(700, "/start remind_o20")
	if !asked() {
		t.Fatal("city not asked")
	}
	h.press(700, "city:astana", nil)
	h.press(700, "dir:it", nil)
	u := h.getUser(700)
	eq(t, u.DigestCity.String, "astana")
	eq(t, u.DigestDirection.String, "it")
	eq(t, u.DigestAsked, true)
	h.reset()
	h.say(700, "/start remind_o20")
	if asked() {
		t.Fatal("asked only once")
	}

	thu := nextWeekday(4)
	thuISO := localDate(thu)
	h.mu.Lock()
	h.opps = append(h.opps,
		h.opp("new1", 30, map[string]any{"posted_at": thuISO, "spec": []string{"it"}}),
		h.opp("new2", 30, map[string]any{"posted_at": thuISO, "city": "almaty"}))
	h.mu.Unlock()
	// reminder due on that Thursday -> digest must wait
	h.putReminder(u.ID, "dueThu", addDays(thuISO, 3), "D")
	h.reset()
	r1 := h.runCron(thu)
	eq(t, r1.Reminders, 1)
	eq(t, r1.Digests, 0)
	eq(t, len(h.messages(700)), 1)
	h.reset()
	fri := thu.Add(day)
	r2 := h.runCron(fri)
	eq(t, r2.Digests, 1)
	d := textOf(h.messages(700)[0])
	mustMatch(t, d, `new1`)
	if strings.Contains(d, "new2") {
		t.Fatal("other city filtered out")
	}
	h.reset()
	r3 := h.runCron(fri.Add(day))
	eq(t, r3.Digests, 0, "one digest per week")
}

// Replaces the JS "Upstash store speaks the REST protocol" test: the Postgres primitives.
func TestPostgresStorePrimitives(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	qs := h.qs()
	u := h.user(1, "kk")
	claim := func(d string) bool {
		_, err := qs.BotClaimDay(ctx, q.BotClaimDayParams{UserID: u.ID, Day: pgDate(d)})
		if err != nil && err != pgx.ErrNoRows {
			t.Fatal(err)
		}
		return err == nil
	}
	eq(t, claim("2030-01-01"), true)
	eq(t, claim("2030-01-01"), false)
	eq(t, claim("2030-01-02"), true)
	_ = qs.BotReleaseDay(ctx, q.BotReleaseDayParams{UserID: u.ID, Day: pgDate("2030-01-01")})
	eq(t, claim("2030-01-01"), true, "released day can be claimed again")

	h.putReminder(u.ID, "o1", "2030-01-01", "a")
	all, _ := qs.BotActiveReminders(ctx)
	eq(t, len(all), 1)
	n, _ := qs.BotFinishReminder(ctx, q.BotFinishReminderParams{UserID: u.ID, OpportunityID: "o1", Status: "submitted"})
	eq(t, n, int64(1))
	n, _ = qs.BotFinishReminder(ctx, q.BotFinishReminderParams{UserID: u.ID, OpportunityID: "o1", Status: "submitted"})
	eq(t, n, int64(0), "already finished")
	h.putReminder(u.ID, "o1", "2030-01-01", "a")
	eq(t, strings.Join(h.reminders(1), ","), "o1", "asking again reactivates")

	_ = qs.BotLogEvent(ctx, q.BotLogEventParams{Kind: "submitted", UserID: pgInt8(u.ID)})
	c, _ := qs.BotCountEvents(ctx, q.BotCountEventsParams{Kind: "submitted", At: pgTime(time.Now().Add(-time.Hour))})
	eq(t, c, int64(1))

	if err := h.b.DeleteUserData(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	eq(t, h.count("select count(*) from users"), 0)
	eq(t, h.count("select count(*) from reminders"), 0)
	eq(t, h.count("select count(*) from bot_daily_sends"), 0)
	if h.getUser(1) != nil {
		t.Fatal("user still there")
	}
}

func TestPureHelpers(t *testing.T) {
	eq(t, fmtDate("2026-12-07", "kk", "2026-10-28"), "7 желтоқсан")
	eq(t, fmtDate("2027-01-05", "ru", "2026-10-28"), "5 января 2027")
	eq(t, strings.Join([]string{ruDays(1), ruDays(3), ruDays(11), ruDays(22), ruDays(5)}, " "), "день дня дней дня дней")
	eq(t, daysBetween("2026-10-28", "2026-11-04"), 7)
	eq(t, weekday("2026-10-29"), 4) // Thursday
	eq(t, weekStart("2026-11-01"), "2026-10-26")
	eq(t, esc(`<a href="x">&`), `&lt;a href="x"&gt;&amp;`)
	eq(t, safeEqual("x", ""), false)
	eq(t, safeEqual("abc", "abc"), true)
	eq(t, localDate(time.Date(2026, 10, 28, 19, 30, 0, 0, time.UTC)), "2026-10-29") // UTC+5
}

// F8: /settings toggles persist in notify_prefs and the daily job follows the switches.
func TestSettingsTogglesPersist(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.say(800, "/settings")
	var settings *call
	for _, m := range h.messages(800) {
		if strings.Contains(textOf(m), "Хабарлама баптаулары") {
			settings = &m
		}
	}
	if settings == nil {
		t.Fatal("no settings message")
	}
	mustMatch(t, textOf(*settings), `22:00–09:00`)
	mustMatch(t, textOf(*settings), `Модерация шешімдері`)
	var data []string
	for _, b := range flat(keyboard(*settings)) {
		data = append(data, b["callback_data"].(string))
	}
	eq(t, strings.Join(data, ","), "set:deadlines,set:digest,set:events,set:mentors,set:market,set:jobs,set:quiet")

	h.reset()
	h.press(800, "set:deadlines", nil)
	h.press(800, "set:quiet", nil)
	h.press(800, "set:moderation", nil) // not a switch: ignored
	h.press(800, "set:bogus", nil)
	u := h.getUser(800)
	p, err := notify.LoadPrefs(ctx, testPool, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, p.Allows(notify.CatDeadlines), false, "deadlines off")
	eq(t, p.QuietEnabled, false, "quiet off")
	eq(t, p.Allows(notify.CatEvents), true, "events untouched")
	var redraw *call
	for _, c := range h.calls() {
		if c.Method == "editMessageText" {
			redraw = &c
		}
	}
	if redraw == nil {
		t.Fatal("settings message not redrawn")
	}
	for _, b := range flat(keyboard(*redraw)) {
		if b["callback_data"] == "set:deadlines" && !strings.HasPrefix(b["text"].(string), "✗") {
			t.Fatalf("deadlines button: %v", b["text"])
		}
	}

	// the daily job skips deadline reminders while the switch is off
	h.putReminder(u.ID, "o3", addDays(h.today, 3), "Мүмкіндік o3")
	h.reset()
	h.runCron(time.Now())
	eq(t, len(h.messages(800)), 0, "deadlines off")
	h.press(800, "set:deadlines", nil)
	h.reset()
	h.runCron(time.Now())
	eq(t, len(h.messages(800)), 1, "deadlines back on")
	eq(t, h.count("select count(*) from notify_sends where user_id = $1 and category = 'deadlines'", u.ID), 1, "counted for the daily cap")

	// help lists the command in both languages; /stop clears the settings
	h.reset()
	h.say(800, "/help")
	mustMatch(t, textOf(h.messages(800)[0]), `/settings`)
	mustMatch(t, txt("ru").Help, `/settings — настройки уведомлений`)
	h.press(800, "stop:yes", nil)
	eq(t, h.count("select count(*) from notify_prefs where user_id = $1", u.ID), 0, "prefs gone after /stop")
}

func TestDigestRespectsSwitch(t *testing.T) {
	h := newHarness(t)
	h.b.Cfg.Digest = true
	u := h.user(810, "kk")
	_ = h.qs().BotSetDigestCity(context.Background(), q.BotSetDigestCityParams{ID: u.ID, DigestCity: pgText("astana")})
	_ = h.qs().BotSetDigestDirection(context.Background(), q.BotSetDigestDirectionParams{ID: u.ID, DigestDirection: pgText("it")})
	thu := nextWeekday(4)
	h.mu.Lock()
	h.opps = append(h.opps, h.opp("fresh", 30, map[string]any{"posted_at": localDate(thu)}))
	h.mu.Unlock()
	h.press(810, "set:digest", nil)
	h.reset()
	eq(t, h.runCron(thu).Digests, 0, "digest off")
	h.press(810, "set:digest", nil)
	h.reset()
	eq(t, h.runCron(thu).Digests, 1, "digest on")
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/content"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cronrun"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func launchExec(t *testing.T, query string, args ...any) {
	t.Helper()
	err := db.Tx(context.Background(), func(tx pgx.Tx) error { _, err := tx.Exec(context.Background(), query, args...); return err })
	if err != nil {
		t.Fatal(err)
	}
}
func launchCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	err := db.Tx(context.Background(), func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), query, args...).Scan(&n) })
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func TestContentNeverServesDemoRecords(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	// Exercise every content kind with both bundled data and an extra-source contribution.
	t.Setenv("FEATURE_EVENTS", "false")
	t.Setenv("FEATURE_B2B", "false")
	t.Setenv("FEATURE_AMBASSADOR", "false")
	admin := launchUser(t, "admin")
	// the old «show demo» switch is gone
	for _, method := range []string{"GET", "PUT"} {
		if w := launchRequest(t, method, "/api/admin/settings", `{"showDemo":true}`, admin); w.Code != 404 && w.Code != 405 {
			t.Errorf("%s /api/admin/settings: %d", method, w.Code)
		}
	}
	dir := t.TempDir()
	for _, kind := range []string{"jobs", "opportunities", "discounts"} {
		rows := []map[string]any{{"id": "demo", "is_demo": true}}
		for i := 0; i < 2; i++ {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("real-%d", i)})
		}
		b, _ := json.Marshal(rows)
		if err := os.WriteFile(filepath.Join(dir, kind+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	content.RegisterExtra("launch-test", func(context.Context) (map[string][]content.Record, error) {
		return map[string][]content.Record{"events": {
			content.RawRecord(json.RawMessage(`{"id":"demo","is_demo":true}`), ""),
			content.RawRecord(json.RawMessage(`{"id":"real-0"}`), ""),
			content.RawRecord(json.RawMessage(`{"id":"real-1","is_demo":false}`), ""),
		}}, nil
	})
	t.Cleanup(func() { content.RegisterExtra("launch-test", nil) })
	l := &content.Loader{DataDir: dir, Env: func(string) string { return "" }}
	for range 2 { // the second pass reads the cached source slices: they were not mutated
		w := httptest.NewRecorder()
		content.Handler(l)(w, httptest.NewRequest("GET", "/api/content", nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Header())
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"jobs", "opportunities", "discounts", "events"} {
			var recs []struct {
				ID   string `json:"id"`
				Demo bool   `json:"is_demo"`
			}
			if err := json.Unmarshal(got[kind], &recs); err != nil {
				t.Fatal(err)
			}
			if len(recs) != 2 || recs[0].Demo || recs[1].Demo || recs[0].ID != "real-0" {
				t.Errorf("%s: %+v", kind, recs)
			}
		}
	}
}

func TestRealEventsContentSource(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	var real, demo int64
	err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		const q = `insert into events (title_kk,cat,starts_at,ends_at,university,status,is_demo,checkin_key)
  values ($1,'other',now()+interval '1 day',now()+interval '2 days','LAUNCH','published',$2,'secret-checkin') returning id`
		if err := tx.QueryRow(context.Background(), q, "Launch real", false).Scan(&real); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), q, "Launch demo", true).Scan(&demo)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { launchExec(t, `delete from events where id = any($1)`, []int64{real, demo}) })
	l := &content.Loader{DataDir: t.TempDir(), Env: func(string) string { return "" }, Warn: func(string) {}}
	b, _ := json.Marshal(l.Load(context.Background(), time.Now()))
	if !strings.Contains(string(b), "Launch real") || strings.Contains(string(b), "Launch demo") || strings.Contains(string(b), "secret-checkin") {
		t.Fatalf("event feed: %s", b)
	}
}

func TestMonitoringPrivacyRateLimitAndRLS(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	launchExec(t, `delete from client_errors`)
	body := `{"message":"TypeError: Alice alice@example.com +7 (777) 123-45-67 123456789012","screen":"/profile/alice?token=secret#private","browser":"Mozilla/5.0 Chrome/131.0 Windows Alice"}`
	send := func(body, ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/client-errors", strings.NewReader(body))
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-Real-IP", ip)
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		return w
	}
	// This must remain anonymous and never resolve/store the browser identity.
	for i := 0; i < 5; i++ {
		if w := send(body, "198.51.100.8"); w.Code != 204 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if w := send(body, "198.51.100.8"); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code, w.Body)
	}
	if w := send(body, "198.51.100.9"); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	for i, bad := range []string{`{"message":"error","userId":1}`, `{"message":"error","ip":"1.2.3.4"}`, `{"message":""}`, `{"message":"error"} {}`, strings.Repeat("x", 9000)} {
		if w := send(bad, fmt.Sprintf("203.0.113.%d", i)); w.Code != 400 {
			t.Errorf("bad input: %d %s", w.Code, w.Body)
		}
	}
	if n := launchCount(t, `select count(*) from client_errors where message='TypeError: [details redacted]' and screen='/profile' and browser='Chrome/131'`); n != 6 {
		t.Fatalf("sanitized rows=%d", n)
	}
	if n := launchCount(t, `select count(*) from information_schema.columns where table_schema='public' and table_name='client_errors' and column_name not in ('id','message','screen','browser','created_at')`); n != 0 {
		t.Fatal("personal/extra columns added to client_errors")
	}
	uid := launchUser(t, "admin")
	for _, table := range []string{"client_errors", "app_settings", "cron_runs"} {
		if launchCount(t, `select count(*) from pg_class where oid=$1::regclass and relrowsecurity`, table) != 1 {
			t.Fatal("RLS disabled", table)
		}
		for _, query := range []string{"select * from " + table, "delete from " + table} {
			err := db.WithUser(context.Background(), uid, func(tx pgx.Tx) error { _, err := tx.Exec(context.Background(), query); return err })
			if err == nil {
				t.Fatal("app_user accessed server-only table", query)
			}
		}
	}
}

func TestCronMonitoringHealthAndRetention(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	launchExec(t, `delete from cron_runs`)
	launchExec(t, `delete from client_errors`)
	launchExec(t, `insert into client_errors (message,screen,browser,created_at) values
 ('Failed to fetch','/jobs','Chrome/131',now()),('Failed to fetch','/jobs','Chrome/131',now()),
 ('Script error.','/','Unknown',now()-interval '25 hours'),('Load failed','/','Unknown',now()-interval '31 days')`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/botTEST_ONLY/getWebhookInfo" {
			fmt.Fprint(w, `{"ok":true,"result":{"url":"https://secret.example/webhook?token=private","pending_update_count":3,"last_error_date":1700000000,"last_error_message":"private error"}}`)
			return
		}
		if r.URL.Path == "/api/content" {
			fmt.Fprint(w, `{"opportunities":[]}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{}}`)
	}))
	defer server.Close()
	t.Setenv("TELEGRAM_API_BASE", server.URL)
	t.Setenv("CONTENT_URL", server.URL)
	t.Setenv("BOT_TOKEN", "TEST_ONLY")
	t.Setenv("CRON_SECRET", "launch-cron-secret")
	t.Setenv("WEBHOOK_SECRET", "launch-webhook-secret")
	t.Setenv("MARKET_SEASON", "")
	cron := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		return w
	}
	for _, path := range cronrun.Endpoints {
		for _, tok := range []string{"", "wrong"} {
			if w := cron(path, tok); w.Code != 401 {
				t.Fatalf("%s unauthorized: %d", path, w.Code)
			}
		}
	}
	if launchCount(t, `select count(*) from cron_runs`) != 0 {
		t.Fatal("unauthorized cron recorded")
	}
	for _, tok := range []string{"", "wrong"} {
		r := httptest.NewRequest("POST", "/api/telegram", strings.NewReader(`{}`))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", tok)
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("webhook accepted wrong secret: %d", w.Code)
		}
	}
	admin := launchUser(t, "admin")
	w := launchRequest(t, "GET", "/api/admin/health", "", admin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var health struct {
		DB     bool `json:"dbReachable"`
		Errors []struct {
			Message string `json:"message"`
			Count   int    `json:"count"`
		} `json:"errors"`
		Cron []struct {
			Last *time.Time `json:"lastSuccessAt"`
		} `json:"cronRuns"`
		Queue    *int `json:"moderationQueue"`
		Telegram struct {
			Configured, Reachable, Installed bool
			Pending                          int `json:"pendingUpdates"`
		} `json:"telegram"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if !health.DB || len(health.Errors) != 1 || health.Errors[0].Count != 2 || health.Queue == nil || len(health.Cron) != len(cronrun.Endpoints) || !health.Telegram.Reachable || !health.Telegram.Installed || health.Telegram.Pending != 3 {
		t.Fatal(w.Body)
	}
	for _, c := range health.Cron {
		if c.Last != nil {
			t.Fatal("unrun cron has timestamp")
		}
	}
	if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret.example") || strings.Contains(w.Body.String(), "TEST_ONLY") {
		t.Fatal("webhook secrets exposed")
	}
	for _, path := range cronrun.Endpoints {
		w := cron(path, "launch-cron-secret")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if launchCount(t, `select count(*) from cron_runs where endpoint=$1 and last_success_at>now()-interval '1 minute'`, path) != 1 {
			t.Fatal("cron not recorded", path)
		}
	}
	if launchCount(t, `select count(*) from client_errors`) != 3 {
		t.Fatal("30-day retention did not keep only recent errors")
	}
	// A failed job itself must retain its last successful timestamp.
	var before string
	err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `select last_success_at::text from cron_runs where endpoint='/api/cron/events'`).Scan(&before)
	})
	if err != nil {
		t.Fatal(err)
	}
	launchExec(t, `alter table events rename to launch_events_unavailable`)
	func() {
		defer launchExec(t, `alter table launch_events_unavailable rename to events`)
		if w := cron("/api/cron/events", "launch-cron-secret"); w.Code != 500 {
			t.Fatal("failed job counted as success", w.Code)
		}
	}()
	if launchCount(t, `select count(*) from cron_runs where endpoint='/api/cron/events' and last_success_at=$1::timestamptz`, before) != 1 {
		t.Fatal("failed cron advanced timestamp")
	}
	// Real storage failure: the work may finish, but the run must not claim success.
	launchExec(t, `alter table cron_runs rename to launch_cron_runs_unavailable`)
	func() {
		defer launchExec(t, `alter table launch_cron_runs_unavailable rename to cron_runs`)
		if w := cron("/api/cron/ambassador", "launch-cron-secret"); w.Code != 500 {
			t.Fatal("failed storage counted as success", w.Code)
		}
	}()
	t.Setenv("BOT_TOKEN", "")
	w = launchRequest(t, "GET", "/api/admin/health", "", admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatal(w.Code, w.Body)
	}
	t.Setenv("BOT_TOKEN", "TEST_ONLY")
	server.Close()
	w = launchRequest(t, "GET", "/api/admin/health", "", admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"reachable":false`) {
		t.Fatal("Telegram outage broke DB health", w.Code, w.Body)
	}
}

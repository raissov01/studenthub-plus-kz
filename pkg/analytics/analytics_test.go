package analytics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

func TestCleanDropsPersonalData(t *testing.T) {
	got := Clean([]event{
		{Name: "onboarding_complete", Props: map[string]any{"faculty": "econ", "course": float64(2), "city": "astana", "name": "Құралай", "gpa": 3.5}},
		{Name: "ai_message_sent", Props: map[string]any{"text": "менің телефоным 8707..."}},
		{Name: "job_view", Props: map[string]any{"id": "j1", "phone": "+77001234567"}},
		{Name: "unknown_event", Props: map[string]any{"id": "x"}},
		{Name: "onboarding_complete", Props: map[string]any{"course": float64(9), "city": "Astana City!"}},
	})
	if len(got) != 4 {
		t.Fatalf("unknown event must be dropped, got %d events", len(got))
	}
	if p := got[0].Props; len(p) != 3 || p["name"] != nil || p["gpa"] != nil {
		t.Errorf("onboarding props = %v", p)
	}
	if len(got[1].Props) != 0 {
		t.Errorf("chat text leaked: %v", got[1].Props)
	}
	if p := got[2].Props; p["phone"] != nil || p["id"] != "j1" {
		t.Errorf("job_view props = %v", p)
	}
	if len(got[3].Props) != 0 {
		t.Errorf("out-of-range course / free-form city must be dropped: %v", got[3].Props)
	}
}

func TestCleanRef(t *testing.T) {
	for in, want := range map[string]string{"share": "share", "amb_Aru7": "amb_aru7", "amb_": "", "google": "", "amb_x;drop": ""} {
		if got := CleanRef(in); got != want {
			t.Errorf("CleanRef(%q) = %q, want %q", in, got, want)
		}
	}
}

// testDB creates a throwaway database with all migrations and points DATABASE_URL at it.
func testDB(t *testing.T) {
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		base = "postgres://postgres:dev@localhost:54329/studenthub"
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Skip("postgres not reachable:", err)
	}
	name := fmt.Sprintf("studenthub_an_test_%d", os.Getpid())
	_, _ = admin.Exec(ctx, "drop database if exists "+name)
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "drop database if exists "+name+" with (force)"); admin.Close(ctx) })
	url := strings.Replace(base, "/studenthub", "/"+name, 1)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../supabase/migrations/*.sql")
	sort.Strings(files)
	for _, f := range files {
		sql, _ := os.ReadFile(f)
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	conn.Close(ctx)
	t.Setenv("DATABASE_URL", url)
}

func TestIngestAndReport(t *testing.T) {
	testDB(t)
	ctx := context.Background()
	post := func(body string) int {
		r := httptest.NewRequest("POST", "/api/events", strings.NewReader(body))
		w := httptest.NewRecorder()
		ingest(w, r)
		return w.Code
	}
	a, b, c := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	if code := post(`{"anon_id":"` + a + `","ref":"share","events":[{"name":"visit"},{"name":"deadline_saved","props":{"id":"o1"}}]}`); code != http.StatusNoContent {
		t.Fatalf("ingest = %d", code)
	}
	post(`{"anon_id":"` + b + `","ref":"amb_aru","events":[{"name":"visit"},{"name":"feedback","props":{"score":4,"missing":"Жатақхана туралы"}}]}`)
	post(`{"anon_id":"` + c + `","ref":"amb_aru","events":[{"name":"visit"}]}`)
	if code := post(`{"anon_id":"not-a-uuid","events":[]}`); code != http.StatusBadRequest {
		t.Errorf("bad anon id = %d", code)
	}
	// make device a look like it arrived 8 days ago and came back on day 1 and day 7
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update analytics_devices set first_day = first_day - 8 where anon_id = $1`, a); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into analytics_events (anon_id, name, day) values ($1, 'visit', (now() at time zone 'Asia/Almaty')::date - 7), ($1, 'visit', (now() at time zone 'Asia/Almaty')::date - 1)`, a)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Report(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if s.Devices != 3 || s.FromShare != 1 || s.SavedFirst != 1 || s.Ambassadors["aru"] != 2 {
		t.Errorf("summary = %+v", s)
	}
	if s.EligibleDay7 != 1 || s.ReturnedDay7 != 1 || s.ReturnedDay1 != 1 {
		t.Errorf("retention = %+v", s)
	}
	if s.FeedbackCount != 1 || s.FeedbackAvg != 4 || len(s.FeedbackRecent) != 1 {
		t.Errorf("feedback = %+v", s)
	}
}

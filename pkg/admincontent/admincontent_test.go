package admincontent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("c", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Exit(testdb.Main(m, "admincontent"))
}

var seq atomic.Int64

func ctx() context.Context { return context.Background() }

func newUser(t *testing.T, role string) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, role, consent_at) values ($1, 'U', $2::user_role, now()) returning id`,
			70000+seq.Add(1), role).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
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
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

type apiErr struct {
	Code, Message, Field, Path string
}

func errOf(w *httptest.ResponseRecorder) apiErr {
	var out struct{ Error apiErr }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out.Error
}

func item(t *testing.T, w *httptest.ResponseRecorder) Item {
	t.Helper()
	var out struct{ Item Item }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Item.ID == "" {
		t.Fatalf("item %d %s", w.Code, w.Body)
	}
	return out.Item
}

// published /api/content, with the real data/*.json as the base lists
func published(t *testing.T) map[string][]map[string]any {
	t.Helper()
	l := &content.Loader{Env: func(string) string { return "" }, DataDir: "../../data", Warn: func(string) {}}
	b, _ := json.Marshal(l.Load(ctx(), Now()))
	var out map[string][]map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func find(list []map[string]any, id string) map[string]any {
	for _, x := range list {
		if x["id"] == id {
			return x
		}
	}
	return nil
}

func daysFromNow(n int) string { return content.AlmatyToday(Now().AddDate(0, 0, n)) }

func opportunity() map[string]any {
	return map[string]any{
		"id": "o1", "type": "intl", "funded": true,
		"title":      map[string]any{"kk": "Жазғы мектеп", "ru": "Летняя школа"},
		"org":        map[string]any{"kk": "Халықаралық бөлім", "ru": "Международный отдел"},
		"req":        map[string]any{"course": 2, "gpa": 3.2, "en": "B1", "skills": []any{"research"}},
		"spec":       []any{"it", "eng"},
		"desc":       map[string]any{"kk": "Екі апталық жазғы мектеп", "ru": "Двухнедельная летняя школа"},
		"docs":       []any{map[string]any{"kk": "Транскрипт", "ru": "Транскрипт"}, map[string]any{"kk": "Хат"}},
		"steps":      []any{map[string]any{"kk": "Өтінім беру", "ru": "Подать заявку"}},
		"source_url": "https://uni.kz/summer", "posted_at": "2026-10-20", "verified_at": "2026-10-21", "verified_by": "Admin",
		"contact": map[string]any{"phone": "+7 700 000 00 00", "email": "intl@uni.kz", "site": "https://uni.kz"},
		"is_demo": true, "deadline": daysFromNow(30), "city": "almaty",
	}
}

func TestAccessAdminOnly(t *testing.T) {
	testdb.Need(t)
	student := newUser(t, "student")
	staff := newUser(t, "university_staff")
	body := map[string]any{"kind": "opportunities", "record": opportunity()}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/admin/content"}, {"POST", "/api/admin/content"}, {"POST", "/api/admin/content/validate"},
		{"GET", "/api/admin/content/adm-1"}, {"PUT", "/api/admin/content/adm-1"}, {"POST", "/api/admin/content/adm-1/hide"},
		{"POST", "/api/admin/content/adm-1/unhide"}, {"DELETE", "/api/admin/content/adm-1"},
	} {
		if w := do(t, c.method, c.path, 0, body); w.Code != 401 {
			t.Errorf("anonymous %s %s: %d", c.method, c.path, w.Code)
		}
		for _, uid := range []int64{student, staff} {
			if w := do(t, c.method, c.path, uid, body); w.Code != 403 {
				t.Errorf("non-admin %s %s: %d", c.method, c.path, w.Code)
			}
		}
	}
	if n := count(t, `select count(*) from admin_content`); n != 0 {
		t.Fatalf("rows written by non-admins: %d", n)
	}
}

func TestCreateAppearsInContentExactShape(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin")
	w := do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "opportunities", "record": opportunity()})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	it := item(t, w)
	if !strings.HasPrefix(it.ID, "adm-") || it.ID == "adm-o1" || it.Status != "published" || it.NoDeadline || it.Expired {
		t.Fatalf("item: %+v", it)
	}
	got := find(published(t)["opportunities"], it.ID)
	if got == nil {
		t.Fatal("not in /api/content")
	}
	// exactly the data/opportunities.json keys plus source
	var sample []map[string]any
	b, _ := os.ReadFile("../../data/opportunities.json")
	_ = json.Unmarshal(b, &sample)
	for k := range sample[0] {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if len(got) != len(sample[0])+1 || got["source"] != "admin" || got["is_demo"] != false {
		t.Fatalf("record: %v", got)
	}
	want := opportunity()
	want["id"], want["is_demo"], want["source"] = it.ID, false, "admin"
	want["docs"] = []any{map[string]any{"kk": "Транскрипт", "ru": "Транскрипт"}, map[string]any{"kk": "Хат", "ru": "Хат"}}
	wb, _ := json.Marshal(want)
	var wantN map[string]any
	_ = json.Unmarshal(wb, &wantN)
	gb, _ := json.Marshal(got)
	wb, _ = json.Marshal(wantN)
	if string(gb) != string(wb) {
		t.Fatalf("shape:\n got %s\nwant %s", gb, wb)
	}
	// the data/*.json o1 is untouched
	if o1 := find(published(t)["opportunities"], "o1"); o1 == nil || o1["source"] != nil {
		t.Fatalf("o1: %v", o1)
	}
	if n := count(t, `select count(*) from audit_log where action = 'content_create' and actor_id = $1 and meta->>'id' = $2`, admin, it.ID); n != 1 {
		t.Fatalf("audit rows: %d", n)
	}

	// jobs and discounts, without a deadline
	job := map[string]any{"today": true, "title": map[string]any{"kk": "Курьер"}, "org": map[string]any{"kk": "Дүкен"}, "city": "astana",
		"pay": 6000, "per": "shift", "hours": 4, "sched": map[string]any{"kk": "Кешке"}, "tags": []any{"daily"}, "skills": []any{},
		"spec": []any{}, "desc": map[string]any{"kk": "Жеткізу"}, "req": []any{map[string]any{"kk": "Велосипед", "ru": "Велосипед"}},
		"source_url": "https://shop.kz/jobs", "contact": map[string]any{}}
	w = do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "jobs", "no_deadline": true, "record": job})
	jit := item(t, w)
	disc := map[string]any{"cat": "food", "pct": "−10%", "name": map[string]any{"kk": "Кофе", "ru": "Кофе"}, "city": "astana",
		"source_url": "https://coffee.kz"}
	w = do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "discounts", "no_deadline": true, "record": disc})
	dit := item(t, w)
	c := published(t)
	j, d := find(c["jobs"], jit.ID), find(c["discounts"], dit.ID)
	if j == nil || d == nil || j["source"] != "admin" || d["source"] != "admin" || j["deadline"] != nil || j["today"] != true ||
		j["posted_at"] != content.AlmatyToday(Now()) || d["what"].(map[string]any)["kk"] != "" || j["spec"].([]any)[0] != "all" {
		t.Fatalf("job %v\ndiscount %v", j, d)
	}
	if !jit.NoDeadline {
		t.Fatal("no_deadline flag")
	}
}

func TestValidationMirrorsSheets(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin")
	before := count(t, `select count(*) from admin_content`)
	for _, c := range []struct {
		name       string
		edit       func(m map[string]any)
		noDeadline bool
		field      string
		path       string
	}{
		{"title", func(m map[string]any) { m["title"] = map[string]any{"ru": "Школа"} }, false, "title_kk", "title.kk"},
		{"type", func(m map[string]any) { m["type"] = "trip" }, false, "type", "type"},
		{"city", func(m map[string]any) { m["city"] = "" }, false, "city", "city"},
		{"https", func(m map[string]any) { m["source_url"] = "http://uni.kz" }, false, "source_url", "source_url"},
		{"date", func(m map[string]any) { m["deadline"] = "31.02.2026" }, false, "deadline", "deadline"},
		{"deadline or none", func(m map[string]any) { m["deadline"] = nil }, false, "deadline", "deadline"},
		{"both", func(m map[string]any) {}, true, "deadline", "deadline"},
		{"email", func(m map[string]any) { m["contact"] = map[string]any{"email": "nope"} }, false, "contact_email", "contact.email"},
		{"gpa", func(m map[string]any) { m["req"] = map[string]any{"gpa": 4.5} }, false, "req_gpa", "req.gpa"},
		{"docs", func(m map[string]any) { m["docs"] = []any{map[string]any{"ru": "Паспорт"}} }, false, "docs_ru", "docs[].ru"},
	} {
		rec := opportunity()
		c.edit(rec)
		w := do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "opportunities", "no_deadline": c.noDeadline, "record": rec})
		e := errOf(w)
		if w.Code != 400 || e.Code != "invalid_record" || e.Field != c.field || e.Path != c.path || e.Message == "" {
			t.Errorf("%s: %d %+v", c.name, w.Code, e)
		}
		// same answer from the dry run, which stores nothing
		if w := do(t, "POST", "/api/admin/content/validate", admin, map[string]any{"kind": "opportunities", "no_deadline": c.noDeadline, "record": rec}); errOf(w).Field != c.field {
			t.Errorf("%s validate: %s", c.name, w.Body)
		}
	}
	if w := do(t, "POST", "/api/admin/content/validate", admin, map[string]any{"kind": "opportunities", "record": opportunity()}); w.Code != 200 {
		t.Fatalf("valid dry run: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "places", "record": opportunity()}); w.Code != 400 || errOf(w).Code != "invalid_kind" {
		t.Fatalf("kind: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "opportunities", "status": "draft", "record": opportunity()}); w.Code != 400 || errOf(w).Code != "invalid_status" {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "opportunities"}); w.Code != 400 {
		t.Fatalf("no record: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/api/admin/content?lang=ru", strings.NewReader(`{"kind":"jobs","no_deadline":true,"record":{"title":{"kk":"Курьер"},"city":"astana","pay":1,"per":"shift","source_url":"https://x.kz"}}`))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	tok, _ := auth.Sign(auth.Claims{UserID: admin, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if e := errOf(w); e.Field != "hours" || !strings.HasPrefix(e.Message, "Проверьте поле «hours»") {
		t.Fatalf("ru message: %+v", e)
	}
	if n := count(t, `select count(*) from admin_content`); n != before {
		t.Fatalf("rows after invalid writes: %d, before %d", n, before)
	}
}

func TestEditHideUnhideDeleteAudited(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin")
	it := item(t, do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "opportunities", "record": opportunity()}))
	path := "/api/admin/content/" + it.ID

	rec := opportunity()
	rec["title"] = map[string]any{"kk": "Күзгі мектеп", "ru": "Осенняя школа"}
	if w := do(t, "PUT", path, admin, map[string]any{"kind": "jobs", "record": rec}); w.Code != 400 || errOf(w).Field != "kind" {
		t.Fatalf("kind change: %d %s", w.Code, w.Body)
	}
	bad := opportunity()
	bad["city"] = "paris"
	if w := do(t, "PUT", path, admin, map[string]any{"record": bad}); w.Code != 400 || errOf(w).Field != "city" {
		t.Fatalf("invalid edit: %d %s", w.Code, w.Body)
	}
	w := do(t, "PUT", path, admin, map[string]any{"record": rec})
	if w.Code != 200 {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	if got := find(published(t)["opportunities"], it.ID); got == nil || got["title"].(map[string]any)["kk"] != "Күзгі мектеп" {
		t.Fatalf("edited record: %v", got)
	}
	if n := count(t, `select count(*) from audit_log where action = 'content_update' and meta->>'id' = $1 and meta->'changed' = '["title"]'::jsonb`, it.ID); n != 1 {
		t.Fatalf("update audit: %d", n)
	}

	if w := do(t, "POST", path+"/hide", admin, nil); w.Code != 200 || item(t, w).Status != "hidden" {
		t.Fatalf("hide: %d %s", w.Code, w.Body)
	}
	if find(published(t)["opportunities"], it.ID) != nil {
		t.Fatal("hidden record served")
	}
	// an edit keeps the status unless given
	if w := do(t, "PUT", path, admin, map[string]any{"record": rec}); item(t, w).Status != "hidden" {
		t.Fatal("edit unhid the record")
	}
	var list struct {
		Items  []Item
		Counts map[string]map[string]int
	}
	_ = json.Unmarshal(do(t, "GET", "/api/admin/content?kind=opportunities&status=hidden&q=күзгі", admin, nil).Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].ID != it.ID || list.Counts["opportunities"]["hidden"] != 1 {
		t.Fatalf("list: %+v", list)
	}
	if w := do(t, "POST", path+"/unhide", admin, nil); w.Code != 200 || item(t, w).Status != "published" {
		t.Fatalf("unhide: %d %s", w.Code, w.Body)
	}
	if find(published(t)["opportunities"], it.ID) == nil {
		t.Fatal("unhidden record not served")
	}
	if w := do(t, "GET", path, admin, nil); w.Code != 200 || item(t, w).ID != it.ID {
		t.Fatalf("get: %d", w.Code)
	}

	if w := do(t, "DELETE", path, admin, nil); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if find(published(t)["opportunities"], it.ID) != nil {
		t.Fatal("deleted record served")
	}
	for _, c := range []struct{ method, p string }{{"GET", path}, {"PUT", path}, {"POST", path + "/hide"}, {"DELETE", path}, {"GET", "/api/admin/content/o1"}} {
		if w := do(t, c.method, c.p, admin, map[string]any{"record": rec}); w.Code != 404 {
			t.Errorf("%s %s after delete: %d", c.method, c.p, w.Code)
		}
	}
	for _, a := range []string{"content_create", "content_update", "content_hide", "content_unhide", "content_delete"} {
		if n := count(t, `select count(*) from audit_log where action = $1 and actor_id = $2 and meta->>'id' = $3`, a, admin, it.ID); n < 1 {
			t.Errorf("no %s audit row", a)
		}
	}
	if n := count(t, `select count(*) from audit_log where action = 'content_delete' and meta->'record'->'title'->>'kk' = 'Күзгі мектеп'`); n != 1 {
		t.Fatal("deleted record not kept in the audit row")
	}
}

func TestExpiredHidden(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin")
	it := item(t, do(t, "POST", "/api/admin/content", admin, map[string]any{"kind": "opportunities", "record": opportunity()}))
	if find(published(t)["opportunities"], it.ID) == nil {
		t.Fatal("live record missing")
	}
	defer func() { Now = time.Now }()
	Now = func() time.Time { return time.Now().AddDate(0, 0, 31) }
	if find(published(t)["opportunities"], it.ID) != nil {
		t.Fatal("expired record served")
	}
	var list struct{ Items []Item }
	_ = json.Unmarshal(do(t, "GET", "/api/admin/content?kind=opportunities", admin, nil).Body.Bytes(), &list)
	for _, x := range list.Items {
		if x.ID == it.ID && !x.Expired {
			t.Fatal("admin list should flag the record as expired")
		}
	}
}

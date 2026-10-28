package b2b

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

var sent = struct {
	sync.Mutex
	direct map[int64][]string
	digest map[int64][]string
}{direct: map[int64][]string{}, digest: map[int64][]string{}}

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("b", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("FEATURE_B2B", "on")
	os.Setenv("CRON_SECRET", "cron-secret")
	fake := func(_ context.Context, id int64, text string) error {
		sent.Lock()
		sent.direct[id] = append(sent.direct[id], text)
		sent.Unlock()
		return nil
	}
	Notify, moderation.Notify = fake, fake
	Digest = func(_ context.Context, id int64, _ time.Time, text string) (string, error) {
		sent.Lock()
		defer sent.Unlock()
		sent.digest[id] = append(sent.digest[id], text)
		return notify.DigestSent, nil
	}
	os.Exit(testdb.Main(m, "b2b"))
}

func directTo(id int64) []string {
	sent.Lock()
	defer sent.Unlock()
	return slices.Clone(sent.direct[id])
}
func digestTo(id int64) []string {
	sent.Lock()
	defer sent.Unlock()
	return slices.Clone(sent.digest[id])
}

func ctx() context.Context { return context.Background() }

var seq atomic.Int64

type opt struct {
	role, univ string
	minor      bool
	noConsent  bool
}

func newUser(t *testing.T, o opt) int64 {
	t.Helper()
	if o.role == "" {
		o.role = "student"
	}
	if o.univ == "" {
		o.univ = "ENU"
	}
	n := seq.Add(1)
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, tg_username, name, university, faculty, course, role, consent_at, is_adult, created_at)
			values ($1, $2, $3, $4, 'IT', 2, $5::user_role, case when $6 then null else now() end, $7, now() - interval '30 days') returning id`,
			820000+n, "u"+strconv.FormatInt(n, 10), "Name "+strconv.FormatInt(n, 10), o.univ, o.role, o.noConsent, !o.minor).Scan(&id)
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

func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return tx.QueryRow(ctx(), sql, args...).Scan(&v) }); err != nil {
		t.Fatal(err)
	}
	return v
}

type resp struct{ *httptest.ResponseRecorder }

func (r resp) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Fatalf("json %d: %s", r.Code, r.Body)
	}
	return m
}

func do(t *testing.T, method, path string, uid int64, body any) resp {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		ver := scalar[int](t, "select session_version from users where id = $1", uid)
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: ver, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	if strings.HasPrefix(path, "/api/cron/") {
		r.Header.Set("Authorization", "Bearer cron-secret")
	}
	m := http.NewServeMux()
	Routes(m)
	me.Routes(m)
	moderation.Routes(m, func(h http.Handler) http.Handler { return h })
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return resp{w}
}

func want(t *testing.T, r resp, code int, errCode string) {
	t.Helper()
	if r.Code != code || (errCode != "" && !strings.Contains(r.Body.String(), `"code":"`+errCode+`"`)) {
		t.Fatalf("want %d %s, got %d %s", code, errCode, r.Code, r.Body)
	}
}

func approve(t *testing.T, admin int64, kind string, key string, id int64) {
	t.Helper()
	sid := scalar[int64](t, `select id from submissions where kind = $1 and status = 'pending' and (payload->>$2)::bigint = $3`, kind, key, id)
	want(t, do(t, "POST", "/api/admin/queue/"+strconv.FormatInt(sid, 10)+"/approve", admin, map[string]string{}), 200, "")
}

var binSeq atomic.Int64

// company creates a verified company owned by a new user; returns owner id and employer id.
func company(t *testing.T, admin int64) (int64, int64) {
	t.Helper()
	owner := newUser(t, opt{})
	bin := strconv.FormatInt(100000000000+binSeq.Add(1), 10)
	r := do(t, "POST", "/api/employer/apply", owner, map[string]string{"name": "ТОО Кофе", "bin": bin,
		"website": "https://2gis.kz/astana/firm/1", "contactName": "Асқар", "contactPhone": "+7 701 000 00 00"})
	want(t, r, 201, "")
	eid := int64(r.obj(t)["employer"].(map[string]any)["id"].(float64))
	approve(t, admin, KindVerification, "employer_id", eid)
	return owner, eid
}

func jobBody(minorOK bool) map[string]any {
	return map[string]any{
		"titleKk": "Бариста", "titleRu": "Бариста", "descKk": "Кофе дайындау, клиенттерге қызмет көрсету, тазалық.",
		"city": "astana", "address": "Мәңгілік Ел 10", "pay": 9000, "per": "shift", "hours": 6,
		"schedKk": "Сенбі-жексенбі", "tags": []string{"noexp"}, "skills": []string{"comm"},
		"req":     []map[string]string{{"kk": "Ұқыптылық", "ru": "Аккуратность"}},
		"applyKk": "Өтінім жіберіңіз, 2 күнде хабарласамыз", "minorOk": minorOK, "acceptRules": true,
	}
}

// publishedJob creates a job and approves it through the queue if needed.
func publishedJob(t *testing.T, admin, owner int64, minorOK bool) int64 {
	t.Helper()
	r := do(t, "POST", "/api/employer/jobs", owner, jobBody(minorOK))
	want(t, r, 201, "")
	o := r.obj(t)
	id := int64(o["job"].(map[string]any)["id"].(float64))
	if o["pending"].(bool) {
		approve(t, admin, KindJob, "job_id", id)
	}
	return id
}

func loadContent(t *testing.T) string {
	t.Helper()
	l := &content.Loader{Env: func(string) string { return "" }, DataDir: t.TempDir(), Warn: func(string) {}}
	b, _ := json.Marshal(l.Load(ctx(), time.Now()))
	return string(b)
}

func TestEmployerVerificationThroughQueue(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	owner := newUser(t, opt{})
	body := map[string]string{"name": "ТОО Тест", "bin": "990000000001", "website": "https://example.kz", "contactName": "Бек"}
	want(t, do(t, "POST", "/api/employer/apply", owner, map[string]string{"name": "X", "bin": "12", "website": "http://x", "contactName": "B"}), 400, "invalid")
	r := do(t, "POST", "/api/employer/apply", owner, body)
	want(t, r, 201, "")
	eid := int64(r.obj(t)["employer"].(map[string]any)["id"].(float64))
	want(t, do(t, "POST", "/api/employer/apply", owner, body), 409, "already_member")
	want(t, do(t, "POST", "/api/employer/apply", newUser(t, opt{}), body), 409, "bin_taken")
	// unverified company cannot publish
	want(t, do(t, "POST", "/api/employer/jobs", owner, jobBody(true)), 403, "not_verified")
	if s := do(t, "GET", "/api/employer/me", owner, nil).obj(t); s["employer"].(map[string]any)["status"] != "pending" {
		t.Fatalf("me: %v", s)
	}
	q := do(t, "GET", "/api/admin/queue?kind="+KindVerification, admin, nil)
	if !strings.Contains(q.Body.String(), "990000000001") {
		t.Fatalf("queue: %s", q.Body)
	}
	approve(t, admin, KindVerification, "employer_id", eid)
	if st := scalar[string](t, `select status from employers where id = $1`, eid); st != "verified" {
		t.Fatal(st)
	}
	if role := scalar[string](t, `select role::text from users where id = $1`, owner); role != "employer" {
		t.Fatalf("role %s", role)
	}
	if msgs := directTo(owner); len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1], "кепілдік ЕМЕС") {
		t.Fatalf("notice: %v", msgs)
	}
	// non-admins cannot decide
	want(t, do(t, "POST", "/api/admin/queue/1/approve", owner, nil), 403, "")
	// rejection path
	o2 := newUser(t, opt{})
	r = do(t, "POST", "/api/employer/apply", o2, map[string]string{"name": "ТОО Жоқ", "bin": "990000000002", "website": "https://e.kz", "contactName": "Бек"})
	e2 := int64(r.obj(t)["employer"].(map[string]any)["id"].(float64))
	sid := scalar[int64](t, `select id from submissions where kind = $1 and (payload->>'employer_id')::bigint = $2`, KindVerification, e2)
	want(t, do(t, "POST", "/api/admin/queue/"+strconv.FormatInt(sid, 10)+"/reject", admin, map[string]string{"reason": "БСН табылмады"}), 200, "")
	if st := scalar[string](t, `select status from employers where id = $1`, e2); st != "rejected" {
		t.Fatal(st)
	}
	// a rejected applicant may apply again
	want(t, do(t, "POST", "/api/employer/apply", o2, map[string]string{"name": "ТОО Жоқ", "bin": "990000000002", "website": "https://e.kz", "contactName": "Бек"}), 201, "")
}

func TestJobPublishingAndContent(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	owner, eid := company(t, admin)
	b := jobBody(true)
	delete(b, "acceptRules")
	want(t, do(t, "POST", "/api/employer/jobs", owner, b), 403, "rules_required")
	bad := jobBody(true)
	bad["descKk"] = "Жұмысқа кіру үшін депозит 5000 тг төлеу керек, сосын бастаймыз."
	want(t, do(t, "POST", "/api/employer/jobs", owner, bad), 400, "forbidden_terms")
	noMinor := jobBody(true)
	delete(noMinor, "minorOk")
	want(t, do(t, "POST", "/api/employer/jobs", owner, noMinor), 400, "invalid")
	httpSite := jobBody(true)
	httpSite["per"] = "week"
	want(t, do(t, "POST", "/api/employer/jobs", owner, httpSite), 400, "invalid")

	r := do(t, "POST", "/api/employer/jobs", owner, jobBody(true))
	want(t, r, 201, "")
	if !r.obj(t)["pending"].(bool) {
		t.Fatal("first job must wait in the queue")
	}
	j1 := int64(r.obj(t)["job"].(map[string]any)["id"].(float64))
	key := `"id":"job-` + strconv.FormatInt(j1, 10) + `"`
	if strings.Contains(loadContent(t), key) {
		t.Fatal("pending job in /api/content")
	}
	approve(t, admin, KindJob, "job_id", j1)
	c := loadContent(t)
	if !strings.Contains(c, key) || !strings.Contains(c, `"source":"employer"`) {
		t.Fatalf("approved job not in content: %s", c)
	}
	// exact data/jobs.json shape (+ extras)
	var parsed struct{ Jobs []map[string]any }
	json.Unmarshal([]byte(c), &parsed)
	var rec map[string]any
	for _, j := range parsed.Jobs {
		if j["id"] == "job-"+strconv.FormatInt(j1, 10) {
			rec = j
		}
	}
	for _, k := range []string{"id", "title", "org", "city", "pay", "per", "hours", "sched", "tags", "skills", "spec", "desc", "req",
		"source_url", "posted_at", "verified_at", "verified_by", "contact", "is_demo", "deadline", "employer", "adult_only", "apply"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("content job misses %q: %v", k, rec)
		}
	}
	// 2nd and 3rd through the queue, the 4th publishes at once
	publishedJob(t, admin, owner, true)
	publishedJob(t, admin, owner, true)
	r = do(t, "POST", "/api/employer/jobs", owner, jobBody(true))
	if r.obj(t)["pending"].(bool) {
		t.Fatal("4th job should publish directly")
	}
	// close / extend through the guarded app_user path
	want(t, do(t, "POST", "/api/employer/jobs/"+strconv.FormatInt(j1, 10)+"/close", owner, nil), 200, "")
	if strings.Contains(loadContent(t), key) {
		t.Fatal("closed job in content")
	}
	want(t, do(t, "POST", "/api/employer/jobs/"+strconv.FormatInt(j1, 10)+"/extend", owner, nil), 200, "")
	// the app_user path cannot fake an approval
	err := db.WithUser(ctx(), owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update jobs set approved_at = now() where employer_id = $1`, eid)
		return err
	})
	if err == nil {
		t.Fatal("approved_at writable by the employer")
	}
	// 30 days later the cron expires it
	exec(t, `update jobs set expires_at = now() - interval '1 minute' where id = $1`, j1)
	want(t, do(t, "GET", "/api/cron/b2b", 0, nil), 200, "")
	if st := scalar[string](t, `select status from jobs where id = $1`, j1); st != "expired" {
		t.Fatal(st)
	}
	// suspension hides every job at once
	want(t, do(t, "POST", "/api/admin/employers/"+strconv.FormatInt(eid, 10)+"/suspend", admin, map[string]string{"reason": "шағымдар"}), 200, "")
	if strings.Contains(loadContent(t), `"org":{"kk":"ТОО Кофе"`) && strings.Contains(loadContent(t), "job-"+strconv.FormatInt(eid, 10)) {
		t.Fatal("suspended company still listed")
	}
	want(t, do(t, "POST", "/api/employer/jobs", owner, jobBody(true)), 403, "not_verified")
}

func TestApplicationFlowAndWhatEmployerSees(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	owner, _ := company(t, admin)
	job := publishedJob(t, admin, owner, true)
	adultJob := publishedJob(t, admin, owner, false)
	st := newUser(t, opt{})
	jp := "/api/jobs/" + strconv.FormatInt(job, 10)

	want(t, do(t, "POST", jp+"/apply", 0, nil), 401, "")
	want(t, do(t, "POST", jp+"/apply", newUser(t, opt{noConsent: true}), map[string]any{"share": map[string]any{"name": true}, "confirm": true}), 403, "consent_required")
	want(t, do(t, "POST", jp+"/apply", st, map[string]any{"share": map[string]any{"name": true}}), 400, "confirm_required")
	want(t, do(t, "POST", jp+"/apply", st, map[string]any{"share": map[string]any{}, "confirm": true}), 400, "nothing_shared")
	want(t, do(t, "POST", jp+"/apply", st, map[string]any{"share": map[string]any{"gpa": 3.9}, "confirm": true}), 400, "invalid")
	want(t, do(t, "POST", jp+"/apply", owner, map[string]any{"share": map[string]any{"name": true}, "confirm": true}), 400, "own_company")
	r := do(t, "POST", jp+"/apply", st, map[string]any{
		"share": map[string]any{"name": true, "skills": []string{"Excel"}}, "letter": "Сәлем! Демалыс күндері жұмыс істей аламын.", "confirm": true,
	})
	want(t, r, 201, "")
	if r.obj(t)["company"] != "ТОО Кофе" {
		t.Fatalf("company in confirmation: %v", r.obj(t))
	}
	want(t, do(t, "POST", jp+"/apply", st, map[string]any{"share": map[string]any{"name": true}, "confirm": true}), 409, "already_applied")
	if msgs := directTo(owner); len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1], "Жаңа өтінім") {
		t.Fatalf("employer not told: %v", msgs)
	}

	// the employer sees exactly the chosen fields: no course, no Telegram, no phone, no ids
	list := do(t, "GET", "/api/employer/jobs/"+strconv.FormatInt(job, 10)+"/applications", owner, nil)
	want(t, list, 200, "")
	body := list.Body.String()
	for _, leak := range []string{"telegram", "phone", "course", "specialty", "userId", "user_id", "isAdult", strconv.FormatInt(st, 10) + ","} {
		if strings.Contains(body, leak) {
			t.Fatalf("employer sees %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "Excel") || !strings.Contains(body, "Name ") {
		t.Fatalf("shared fields missing: %s", body)
	}
	appID := int64(list.obj(t)["applications"].([]any)[0].(map[string]any)["id"].(float64))
	ap := "/api/employer/applications/" + strconv.FormatInt(appID, 10) + "/status"
	want(t, do(t, "POST", ap, st, map[string]string{"status": "invited"}), 403, "not_member")
	want(t, do(t, "POST", ap, owner, map[string]string{"status": "no_response"}), 400, "")
	want(t, do(t, "POST", ap, owner, map[string]string{"status": "viewed"}), 200, "")
	want(t, do(t, "POST", ap, owner, map[string]string{"status": "invited", "message": "Сенбі 10:00-де келіңіз <b>"}), 200, "")
	if msgs := directTo(st); len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1], "шақырды") || strings.Contains(msgs[len(msgs)-1], "<b>") {
		t.Fatalf("student invite message: %v", msgs)
	}
	my := do(t, "GET", "/api/applications/my", st, nil)
	if !strings.Contains(my.Body.String(), `"status":"invited"`) || !strings.Contains(my.Body.String(), "Сенбі 10:00") {
		t.Fatalf("my: %s", my.Body)
	}
	want(t, do(t, "POST", "/api/applications/"+strconv.FormatInt(appID, 10)+"/withdraw", newUser(t, opt{}), nil), 404, "application_not_found")
	want(t, do(t, "POST", "/api/applications/"+strconv.FormatInt(appID, 10)+"/withdraw", st, nil), 200, "")
	list = do(t, "GET", "/api/employer/jobs/"+strconv.FormatInt(job, 10)+"/applications", owner, nil)
	if strings.Contains(list.Body.String(), "Excel") || !strings.Contains(list.Body.String(), `"withdrawn"`) {
		t.Fatalf("withdrawn application still shows data: %s", list.Body)
	}
	want(t, do(t, "POST", ap, owner, map[string]string{"status": "rejected"}), 409, "application_closed")

	// minors: adult-only job hidden and refused
	minor := newUser(t, opt{minor: true})
	ajp := "/api/jobs/" + strconv.FormatInt(adultJob, 10)
	want(t, do(t, "GET", ajp, minor, nil), 404, "job_not_found")
	want(t, do(t, "POST", ajp+"/apply", minor, map[string]any{"share": map[string]any{"name": true}, "confirm": true}), 403, "minor")
	if l := do(t, "GET", "/api/jobs", minor, nil).Body.String(); strings.Contains(l, "job-"+strconv.FormatInt(adultJob, 10)) ||
		!strings.Contains(l, "job-"+strconv.FormatInt(job, 10)) {
		t.Fatalf("minor list: %s", l)
	}
	// RLS too: job_apply refuses even when called directly
	var res string
	db.WithUser(ctx(), minor, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select result from job_apply($1, '{"name":"x"}')`, adultJob).Scan(&res)
	})
	if res != "minor" {
		t.Fatalf("job_apply for a minor: %s", res)
	}
	want(t, do(t, "GET", ajp, st, nil), 200, "")
	if !strings.Contains(do(t, "GET", ajp, 0, nil).Body.String(), `"adult_only":true`) {
		t.Fatal("adult_only flag missing")
	}
	// a student cannot write an application directly or forge a status
	err := db.WithUser(ctx(), st, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `insert into applications (job_id, employer_id, user_id, shared_fields) select id, employer_id, app_user_id(), '{}' from jobs limit 1`)
		return err
	})
	if err == nil {
		t.Fatal("direct insert allowed")
	}
}

func TestCrossCompanyIsolation(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	ownerA, eA := company(t, admin)
	ownerB, eB := company(t, admin)
	jobA := publishedJob(t, admin, ownerA, true)
	st := newUser(t, opt{})
	want(t, do(t, "POST", "/api/jobs/"+strconv.FormatInt(jobA, 10)+"/apply", st,
		map[string]any{"share": map[string]any{"name": true}, "confirm": true}), 201, "")
	appA := scalar[int64](t, `select id from applications where job_id = $1`, jobA)

	// Go checks
	want(t, do(t, "GET", "/api/employer/jobs/"+strconv.FormatInt(jobA, 10)+"/applications", ownerB, nil), 404, "job_not_found")
	want(t, do(t, "POST", "/api/employer/applications/"+strconv.FormatInt(appA, 10)+"/status", ownerB, map[string]string{"status": "rejected"}), 404, "application_not_found")
	want(t, do(t, "PATCH", "/api/employer/jobs/"+strconv.FormatInt(jobA, 10), ownerB, jobBody(true)), 404, "job_not_found")
	want(t, do(t, "POST", "/api/employer/jobs/"+strconv.FormatInt(jobA, 10)+"/close", ownerB, nil), 404, "job_not_found")
	if l := do(t, "GET", "/api/employer/jobs", ownerB, nil).Body.String(); strings.Contains(l, `"id":`+strconv.FormatInt(jobA, 10)+`,`) {
		t.Fatalf("B lists A's job: %s", l)
	}
	// RLS: crafted SQL as B's member sees and changes nothing of A
	err := db.WithUser(ctx(), ownerB, func(tx pgx.Tx) error {
		for q, wantN := range map[string]int{
			"select count(*) from applications where employer_id = " + strconv.FormatInt(eA, 10):     0,
			"select count(*) from jobs where employer_id = " + strconv.FormatInt(eA, 10):             0,
			"select count(*) from employers where id = " + strconv.FormatInt(eA, 10):                 0,
			"select count(*) from employer_members where employer_id = " + strconv.FormatInt(eA, 10): 0,
			"select count(*) from employers": 1,
		} {
			var n int
			if err := tx.QueryRow(ctx(), q).Scan(&n); err != nil {
				return err
			}
			if n != wantN {
				t.Errorf("%s: %d", q, n)
			}
		}
		for _, q := range []string{
			"update applications set status = 'rejected' where employer_id = " + strconv.FormatInt(eA, 10),
			"update jobs set status = 'closed' where employer_id = " + strconv.FormatInt(eA, 10),
			"update employers set about = 'x' where id = " + strconv.FormatInt(eA, 10),
		} {
			tag, err := tx.Exec(ctx(), q)
			if err != nil || tag.RowsAffected() != 0 {
				t.Errorf("%s: %v %d", q, err, tag.RowsAffected())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// the employer cannot rewrite what the student sent
	db.WithUser(ctx(), ownerA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update applications set status = 'viewed', shared_fields = '{"name":"forged"}' where id = $1`, appA)
		return err
	})
	if v := scalar[string](t, `select shared_fields->>'name' from applications where id = $1`, appA); v == "forged" {
		t.Fatal("employer rewrote shared_fields")
	}
	// the employer cannot change its own name / БСН / status
	if err := db.WithUser(ctx(), ownerA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update employers set status = 'verified', bin = '000000000000' where id = $1`, eA)
		return err
	}); err == nil {
		t.Fatal("employer changed status / bin")
	}
	_ = eB
}

func TestGhostingRules(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	owner, eid := company(t, admin)
	member := newUser(t, opt{})
	inv := do(t, "POST", "/api/employer/invites", owner, nil)
	want(t, inv, 201, "")
	want(t, do(t, "POST", "/api/employer/join", member, map[string]string{"code": inv.obj(t)["code"].(string)}), 200, "")
	job := publishedJob(t, admin, owner, true)
	var students []int64
	for i := 0; i < 6; i++ {
		s := newUser(t, opt{})
		students = append(students, s)
		want(t, do(t, "POST", "/api/jobs/"+strconv.FormatInt(job, 10)+"/apply", s,
			map[string]any{"share": map[string]any{"name": true}, "confirm": true}), 201, "")
	}
	// one answered quickly; one 6 days old (reminder); four 15 days old (no_response)
	exec(t, `update applications set status = 'invited', responded_at = created_at + interval '2 days' where user_id = $1`, students[0])
	exec(t, `update applications set created_at = now() - interval '6 days' where user_id = $1`, students[1])
	exec(t, `update applications set created_at = now() - interval '15 days' where user_id = any($1)`, students[2:])
	beforeOwner, beforeMember := len(directTo(owner)), len(directTo(member))
	r := do(t, "GET", "/api/cron/b2b", 0, nil)
	want(t, r, 200, "")
	o := r.obj(t)
	if o["noResponse"].(float64) != 4 || o["studentsTold"].(float64) != 4 || o["employerReminders"].(float64) < 1 {
		t.Fatalf("cron: %v", o)
	}
	if len(directTo(owner)) == beforeOwner || len(directTo(member)) == beforeMember {
		t.Fatal("company members not reminded")
	}
	if d := digestTo(students[2]); len(d) != 1 || !strings.Contains(d[0], "14 күнде жауап бермеді") {
		t.Fatalf("student message: %v", d)
	}
	if st := scalar[string](t, `select status from applications where user_id = $1`, students[1]); st != "sent" {
		t.Fatal(st)
	}
	// second run: nothing twice
	o = do(t, "GET", "/api/cron/b2b", 0, nil).obj(t)
	if o["noResponse"].(float64) != 0 || o["studentsTold"].(float64) != 0 {
		t.Fatalf("second run: %v", o)
	}
	if len(digestTo(students[2])) != 1 {
		t.Fatal("student told twice")
	}
	// response stats: 6 applications, 1 answered of 5 decided = 20% -> low, listed last, admin told
	stats, _ := ResponseStatsFor(ctx(), []int64{eid})
	s := stats[eid]
	if s == nil || *s.Rate != 20 || *s.Days != 2 || !s.Low() {
		t.Fatalf("stats: %+v", s)
	}
	if !strings.Contains(do(t, "GET", "/api/jobs/"+strconv.FormatInt(job, 10), 0, nil).Body.String(), `"low_response":true`) {
		t.Fatal("low_response flag")
	}
	found := false
	for _, m := range directTo(admin) {
		found = found || strings.Contains(m, "20%")
	}
	if !found {
		t.Fatalf("admin not warned: %v", directTo(admin))
	}
	// cron needs the secret
	req := httptest.NewRequest("GET", "/api/cron/b2b", nil)
	m := http.NewServeMux()
	Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("cron without secret: %d", w.Code)
	}
}

func TestJobReportsHideAfterThree(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	owner, _ := company(t, admin)
	job := publishedJob(t, admin, owner, true)
	jp := "/api/jobs/" + strconv.FormatInt(job, 10) + "/report"
	want(t, do(t, "POST", jp, owner, map[string]string{"reason": "scam"}), 400, "own_job")
	for i := 0; i < 3; i++ {
		r := do(t, "POST", jp, newUser(t, opt{}), map[string]string{"reason": "fee", "text": "ақы сұрады"})
		want(t, r, 200, "")
		if hidden := r.obj(t)["hidden"].(bool); hidden != (i == 2) {
			t.Fatalf("report %d hidden=%v", i, hidden)
		}
	}
	if strings.Contains(loadContent(t), "job-"+strconv.FormatInt(job, 10)) {
		t.Fatal("hidden job in content")
	}
	if n := scalar[int](t, `select count(*) from submissions where kind = $1 and (payload->>'job_id')::bigint = $2`, KindJobReport, job); n != 1 {
		t.Fatalf("queue items: %d", n)
	}
	// admin finds the reports groundless -> visible again
	sid := scalar[int64](t, `select id from submissions where kind = $1 and (payload->>'job_id')::bigint = $2`, KindJobReport, job)
	want(t, do(t, "POST", "/api/admin/queue/"+strconv.FormatInt(sid, 10)+"/reject", admin, map[string]string{"reason": "негізсіз"}), 200, "")
	if st := scalar[string](t, `select status from jobs where id = $1`, job); st != "published" {
		t.Fatal(st)
	}
}

func TestUniversityCabinet(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	staff := newUser(t, opt{univ: "ENU"})
	other := newUser(t, opt{univ: "KazNU"})
	want(t, do(t, "GET", "/api/university/stats", staff, nil), 403, "not_staff")
	// the role alone (without an admin binding) is not enough
	exec(t, `update users set role = 'university_staff' where id = $1`, other)
	want(t, do(t, "GET", "/api/university/stats", other, nil), 403, "not_staff")
	want(t, do(t, "POST", "/api/admin/university-staff", staff, map[string]any{"userId": staff, "university": "ENU"}), 403, "")
	want(t, do(t, "POST", "/api/admin/university-staff", admin, map[string]any{"userId": staff, "university": "ENU"}), 200, "")
	want(t, do(t, "POST", "/api/admin/university-staff", admin, map[string]any{"userId": other, "university": "KazNU"}), 200, "")

	opp := map[string]any{"kind": "opportunity", "payload": map[string]any{
		"type": "grant", "title_kk": "Ректор гранты", "org_kk": "ЕҰУ", "desc_kk": "Үздік студенттерге арналған грант.",
		"source_url": "https://enu.kz/grant", "city": "astana", "deadline": time.Now().AddDate(0, 1, 0).Format("2006-01-02"),
		"docs": []map[string]string{{"kk": "Транскрипт"}}, "steps": []map[string]string{{"kk": "Өтінім толтыру"}},
	}}
	r := do(t, "POST", "/api/university/posts", staff, opp)
	want(t, r, 201, "")
	pid := int64(r.obj(t)["post"].(map[string]any)["id"].(float64))
	c := loadContent(t)
	if !strings.Contains(c, `"id":"uni-`+strconv.FormatInt(pid, 10)+`"`) || !strings.Contains(c, `"official":true`) || !strings.Contains(c, `"source":"university"`) {
		t.Fatalf("official opportunity not in content: %s", c)
	}
	want(t, do(t, "POST", "/api/university/posts", staff, map[string]any{"kind": "announcement", "payload": map[string]any{
		"title_kk": "Сессия кестесі", "body_kk": "Қысқы сессия кестесі жарияланды."}}), 201, "")
	if a := do(t, "GET", "/api/announcements?university=ENU", 0, nil).Body.String(); !strings.Contains(a, "Сессия кестесі") {
		t.Fatalf("announcements: %s", a)
	}
	// another university's staff can neither edit nor delete it (Go + RLS)
	want(t, do(t, "PATCH", "/api/university/posts/"+strconv.FormatInt(pid, 10), other, map[string]any{"hidden": true}), 404, "post_not_found")
	want(t, do(t, "DELETE", "/api/university/posts/"+strconv.FormatInt(pid, 10), other, nil), 404, "post_not_found")
	if err := db.WithUser(ctx(), other, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `insert into university_posts (university, author_id, kind, payload) values ('ENU', app_user_id(), 'announcement', '{}')`)
		return err
	}); err == nil {
		t.Fatal("KazNU staff wrote an ENU post")
	}
	// moving to another university through the profile does not work
	do(t, "PATCH", "/api/me", other, map[string]string{"university": "ENU"})
	if u := scalar[string](t, `select university from users where id = $1`, other); u != "KazNU" {
		t.Fatalf("staff changed university: %s", u)
	}

	// stats: small groups are "< 5", no personal data
	for i := 0; i < 6; i++ {
		newUser(t, opt{univ: "ENU"})
	}
	exec(t, `update users set faculty = 'Physics' where id = $1`, newUser(t, opt{univ: "ENU"}))
	s := do(t, "GET", "/api/university/stats", staff, nil)
	want(t, s, 200, "")
	body := s.Body.String()
	if !strings.Contains(body, `"Physics":"\u003c 5"`) {
		t.Fatalf("small group shown: %s", body)
	}
	if strings.Contains(body, "Name ") || strings.Contains(body, "telegram") || strings.Contains(body, `"u1`) {
		t.Fatalf("personal data in stats: %s", body)
	}
	if !strings.Contains(body, `"avgRating":null`) {
		t.Fatalf("average with < 5 ratings: %s", body)
	}
	csv := do(t, "GET", "/api/university/stats.csv", staff, nil)
	if !strings.Contains(csv.Body.String(), "faculty:Physics,< 5") {
		t.Fatalf("csv: %s", csv.Body)
	}
	// staff reach no student-level data: employer cabinets and admin endpoints are closed
	for _, p := range []string{"/api/employer/jobs", "/api/admin/employers", "/api/admin/university-staff", "/api/admin/queue"} {
		if w := do(t, "GET", p, staff, nil); w.Code != 403 {
			t.Errorf("staff GET %s: %d", p, w.Code)
		}
	}
	want(t, do(t, "DELETE", "/api/admin/university-staff/"+strconv.FormatInt(staff, 10), admin, nil), 200, "")
	want(t, do(t, "GET", "/api/university/stats", staff, nil), 403, "not_staff")
}

func TestAccountDeletion(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})

	// student: applications (and the shared copy) go with the account
	owner, _ := company(t, admin)
	job := publishedJob(t, admin, owner, true)
	st := newUser(t, opt{})
	want(t, do(t, "POST", "/api/jobs/"+strconv.FormatInt(job, 10)+"/apply", st,
		map[string]any{"share": map[string]any{"name": true, "phone": "+77010000000"}, "confirm": true}), 201, "")
	if w := do(t, "GET", "/api/me/export", st, nil); !strings.Contains(w.Body.String(), "jobApplications") || !strings.Contains(w.Body.String(), "+77010000000") {
		t.Fatalf("export: %s", w.Body)
	}
	want(t, do(t, "DELETE", "/api/me", st, map[string]string{"confirm": "DELETE"}), 200, "")
	if n := scalar[int](t, `select count(*) from applications where job_id = $1`, job); n != 0 {
		t.Fatal("application survived account deletion")
	}
	if n := scalar[int](t, `select count(*) from applications where shared_fields::text like '%77010000000%'`); n != 0 {
		t.Fatal("phone survived")
	}

	// owner with a colleague: the colleague takes over, pending queue items follow
	owner2, e2 := company(t, admin)
	colleague := newUser(t, opt{})
	inv := do(t, "POST", "/api/employer/invites", owner2, nil)
	want(t, do(t, "POST", "/api/employer/join", colleague, map[string]string{"code": inv.obj(t)["code"].(string)}), 200, "")
	pending := do(t, "POST", "/api/employer/jobs", owner2, jobBody(true))
	pj := int64(pending.obj(t)["job"].(map[string]any)["id"].(float64))
	want(t, do(t, "POST", "/api/employer/leave", owner2, nil), 409, "owner_must_transfer")
	want(t, do(t, "DELETE", "/api/me", owner2, map[string]string{"confirm": "DELETE"}), 200, "")
	if o := scalar[int64](t, `select owner_user_id from employers where id = $1`, e2); o != colleague {
		t.Fatalf("owner after deletion: %d", o)
	}
	if st := scalar[string](t, `select status from employers where id = $1`, e2); st != "verified" {
		t.Fatal(st)
	}
	approve(t, admin, KindJob, "job_id", pj) // the queue item survived
	if r := do(t, "GET", "/api/employer/members", colleague, nil).Body.String(); !strings.Contains(r, `"role":"owner"`) {
		t.Fatalf("members: %s", r)
	}

	// explicit hand-over and stop
	m2 := newUser(t, opt{})
	inv = do(t, "POST", "/api/employer/invites", colleague, nil)
	want(t, do(t, "POST", "/api/employer/join", m2, map[string]string{"code": inv.obj(t)["code"].(string)}), 200, "")
	want(t, do(t, "POST", "/api/employer/join", m2, map[string]string{"code": inv.obj(t)["code"].(string)}), 409, "already_member")
	want(t, do(t, "POST", "/api/employer/transfer", m2, map[string]any{"userId": colleague}), 403, "not_owner")
	want(t, do(t, "POST", "/api/employer/transfer", colleague, map[string]any{"userId": m2}), 200, "")
	want(t, do(t, "POST", "/api/employer/close", m2, map[string]string{"confirm": "nope"}), 400, "confirm_required")
	want(t, do(t, "POST", "/api/employer/close", m2, map[string]string{"confirm": "CLOSE"}), 200, "")
	if n := scalar[int](t, `select count(*) from jobs where employer_id = $1 and status = 'published'`, e2); n != 0 {
		t.Fatal("jobs of a closed company still published")
	}
	if r := scalar[string](t, `select role::text from users where id = $1`, colleague); r != "student" {
		t.Fatalf("role after close: %s", r)
	}

	// owner alone: the company is stopped
	owner3, e3 := company(t, admin)
	publishedJob(t, admin, owner3, true)
	want(t, do(t, "DELETE", "/api/me", owner3, map[string]string{"confirm": "DELETE"}), 200, "")
	if st := scalar[string](t, `select status from employers where id = $1`, e3); st != "closed" {
		t.Fatal(st)
	}
	if n := scalar[int](t, `select count(*) from jobs where employer_id = $1 and status = 'published'`, e3); n != 0 {
		t.Fatal("jobs of an ownerless company still published")
	}
}

func TestSwitchOff(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, opt{role: "admin"})
	owner, _ := company(t, admin)
	job := publishedJob(t, admin, owner, true)
	t.Setenv("FEATURE_B2B", "off")
	for _, p := range []string{"/api/jobs", "/api/jobs/" + strconv.FormatInt(job, 10), "/api/employer/me", "/api/university/stats",
		"/api/announcements", "/api/cron/b2b", "/api/admin/employers"} {
		if w := do(t, "GET", p, admin, nil); w.Code != 404 {
			t.Errorf("GET %s with b2b off: %d", p, w.Code)
		}
	}
	if strings.Contains(loadContent(t), "job-"+strconv.FormatInt(job, 10)) {
		t.Fatal("content lists jobs with b2b off")
	}
}

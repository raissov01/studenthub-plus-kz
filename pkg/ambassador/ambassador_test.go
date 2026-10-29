package ambassador

import (
	"bytes"
	"context"
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
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("m", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("FEATURE_AMBASSADOR", "true")
	os.Setenv("CRON_SECRET", "cron-secret")
	send := func(ctx context.Context, userID int64, text string) error { box.add(userID, text); return nil }
	Notify, moderation.Notify = send, send
	os.Exit(testdb.Main(m, "ambassador"))
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

var box outbox

var seq atomic.Int64

func ctx() context.Context { return context.Background() }

func newUser(t *testing.T, role, univ string) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, role, university, consent_at, created_at)
			values ($1, $2, $3::user_role, nullif($4, ''), now(), now() - interval '30 days') returning id`,
			80000+seq.Add(1), "User", role, univ).Scan(&id)
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

// the shared moderation queue is wired by pkg/server outside this feature's switch; mount it here too
var mux = func() *http.ServeMux {
	m := http.NewServeMux()
	Routes(m)
	SubmissionRoutes(m)
	moderation.Routes(m, func(h http.Handler) http.Handler { return h })
	return m
}()

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	if body == nil {
		b = nil
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

// pendingID returns the newest pending submission of kind by user.
func pendingID(t *testing.T, kind string, by int64) int64 {
	t.Helper()
	return int64(count(t, `select coalesce(max(id), 0) from submissions where kind = $1 and submitted_by = $2 and status = 'pending'`, kind, by))
}

// makeAmbassador runs the real flow: apply -> admin approves.
func makeAmbassador(t *testing.T, admin int64, univ string) int64 {
	t.Helper()
	u := newUser(t, "student", univ)
	if w := do(t, "POST", "/api/ambassador/apply", u, map[string]any{"university": univ, "motivation": "Университетке пайдалы болғым келеді", "hours_per_week": "2-3"}); w.Code != 201 {
		t.Fatalf("apply: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", pendingID(t, KindApplication, u)), admin, map[string]any{}); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	return u
}

func TestFeatureOffIs404(t *testing.T) {
	t.Setenv("FEATURE_AMBASSADOR", "false")
	for _, c := range [][2]string{{"POST", "/api/ambassador/apply"}, {"GET", "/api/ambassador/me"},
		{"GET", "/api/ambassador/rating"}, {"GET", "/api/certificates/abcdefgh"}, {"GET", "/api/cron/ambassador"}} {
		if w := do(t, c[0], c[1], 0, map[string]any{}); w.Code != 404 || errCode(w) != "not_found" {
			t.Errorf("%s %s with ambassador off: %d", c[0], c[1], w.Code)
		}
	}
}

func TestApplicationReviewFlow(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin", "")
	student := newUser(t, "student", "ENU")
	apply := map[string]any{"university": "ENU", "motivation": "Мен көмектескім келеді!", "hours_per_week": "3-5"}
	if w := do(t, "POST", "/api/ambassador/apply", student, map[string]any{"university": "ENU", "motivation": "қысқа", "hours_per_week": "3-5"}); w.Code != 400 {
		t.Fatalf("short motivation: %d", w.Code)
	}
	if w := do(t, "POST", "/api/ambassador/apply", student, map[string]any{"university": "ENU", "motivation": strings.Repeat("я", 301), "hours_per_week": "3-5"}); w.Code != 400 {
		t.Fatalf("301 chars: %d", w.Code)
	}
	if w := do(t, "POST", "/api/ambassador/apply", student, apply); w.Code != 201 {
		t.Fatalf("apply: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/ambassador/apply", student, apply); w.Code != 409 || errCode(w) != "already_pending" {
		t.Fatalf("second apply: %d %s", w.Code, w.Body)
	}
	// queue: admin only, kind filter, oldest first
	if w := do(t, "GET", "/api/admin/queue", student, nil); w.Code != 403 {
		t.Fatalf("student queue: %d", w.Code)
	}
	other := newUser(t, "student", "ENU")
	do(t, "POST", "/api/submissions", other, map[string]any{"kind": "place", "payload": map[string]any{"cat": "print", "name_kk": "Баспахана", "address": "Сәтпаев 2"}})
	q := decode(t, do(t, "GET", "/api/admin/queue?kind="+KindApplication, admin, nil))
	items := q["items"].([]any)
	if len(items) == 0 || q["pendingByKind"].(map[string]any)["place"] == nil {
		t.Fatalf("queue: %v", q)
	}
	var prev float64
	for _, it := range items {
		m := it.(map[string]any)
		if m["kind"] != KindApplication {
			t.Fatalf("kind filter leaked %v", m["kind"])
		}
		if id := m["id"].(float64); id < prev {
			t.Fatal("queue not oldest first")
		} else {
			prev = id
		}
	}
	sid := pendingID(t, KindApplication, student)
	// reject needs a reason
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/reject", sid), admin, map[string]any{}); w.Code != 400 || errCode(w) != "reason_required" {
		t.Fatalf("reject without reason: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/reject", sid), admin, map[string]any{"reason": "Мотивация <b>толық емес</b>"}); w.Code != 200 {
		t.Fatalf("reject: %d %s", w.Code, w.Body)
	}
	msgs := box.of(student)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "&lt;b&gt;толық емес") {
		t.Fatalf("rejection message: %v", msgs)
	}
	if got := count(t, `select count(*) from ambassador_applications where user_id = $1 and status = 'rejected' and reason like 'Мотивация%'`, student); got != 1 {
		t.Fatal("application not marked rejected")
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", sid), admin, nil); w.Code != 409 || errCode(w) != "not_pending" {
		t.Fatalf("decide twice: %d", w.Code)
	}
	if got := count(t, `select count(*) from audit_log where action = 'moderation_reject' and target_id = $1`, student); got != 1 {
		t.Fatal("no audit row")
	}
	// second application approved -> lead, role ambassador, direct message with the cabinet link
	if w := do(t, "POST", "/api/ambassador/apply", student, apply); w.Code != 201 {
		t.Fatalf("re-apply: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", pendingID(t, KindApplication, student)), admin, nil); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from users where id = $1 and role = 'ambassador'`, student); got != 1 {
		t.Fatal("role not granted")
	}
	if msgs := box.of(student); !strings.HasSuffix(msgs[len(msgs)-1], "/ambassador") {
		t.Fatalf("approval message: %v", msgs)
	}
	cab := decode(t, do(t, "GET", "/api/ambassador/me", student, nil))
	term := cab["term"].(map[string]any)
	if term["role_in_team"] != "lead" || term["hours_per_week"].(float64) != 5 || term["university"] != "ENU" {
		t.Fatalf("term: %v", term)
	}
	if !strings.Contains(cab["referral"].(map[string]any)["link"].(string), "?ref=amb_") {
		t.Fatalf("referral: %v", cab["referral"])
	}
	if w := do(t, "POST", "/api/ambassador/apply", student, apply); w.Code != 409 || errCode(w) != "already_ambassador" {
		t.Fatalf("apply as ambassador: %d", w.Code)
	}
	// team: 1 lead + 3 members, the 5th approval is refused and stays pending
	for range 3 {
		makeAmbassador(t, admin, "ENU")
	}
	fifth := newUser(t, "student", "ENU")
	do(t, "POST", "/api/ambassador/apply", fifth, apply)
	fid := pendingID(t, KindApplication, fifth)
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", fid), admin, nil); w.Code != 409 || errCode(w) != "team_full" {
		t.Fatalf("5th: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from submissions where id = $1 and status = 'pending'`, fid); got != 1 {
		t.Fatal("refused approval was not rolled back")
	}
	if got := count(t, `select count(*) from ambassador_terms where university = 'ENU' and status = 'active' and role_in_team = 'lead'`); got != 1 {
		t.Fatalf("leads = %d", got)
	}
}

func TestCabinetStatsOwnUniversityOnly(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin", "")
	amb := makeAmbassador(t, admin, "SDU")
	for range 3 {
		newUser(t, "student", "SDU")
	}
	exec(t, `update users set faculty = 'IT', last_seen_at = now() where university = 'SDU'`)
	w := do(t, "GET", "/api/ambassador/stats", amb, nil)
	if w.Code != 200 {
		t.Fatalf("stats: %d %s", w.Code, w.Body)
	}
	s := decode(t, w)
	if s["university"] != "SDU" || s["students"] != "< 5" || s["by_faculty"].(map[string]any)["other"] != "< 5" || s["by_faculty"].(map[string]any)["IT"] != nil {
		t.Fatalf("stats: %v", s)
	}
	// 10-032: a faculty with 5+ students is named, the small ones are only "other"
	for range 5 {
		newUser(t, "student", "SDU")
	}
	exec(t, `update users set faculty = 'Law' where university = 'SDU' and faculty is null and role = 'student'`)
	if f := decode(t, do(t, "GET", "/api/ambassador/stats", amb, nil))["by_faculty"].(map[string]any); f["Law"] != float64(5) || f["other"] != "< 5" || f["IT"] != nil {
		t.Fatalf("by_faculty: %v", f)
	}
	if w := do(t, "GET", "/api/ambassador/stats?university=ENU", amb, nil); w.Code != 403 {
		t.Fatalf("foreign stats: %d", w.Code)
	}
	if w := do(t, "GET", "/api/ambassador/stats", newUser(t, "student", "SDU"), nil); w.Code != 403 {
		t.Fatalf("student stats: %d", w.Code)
	}
	if w := do(t, "GET", "/api/ambassador/stats?university=SDU", admin, nil); w.Code != 200 {
		t.Fatalf("admin stats: %d", w.Code)
	}
	for range 3 {
		newUser(t, "student", "SDU")
	}
	if s := decode(t, do(t, "GET", "/api/ambassador/stats", amb, nil)); s["students"].(float64) < 5 {
		t.Fatalf("unmasked count: %v", s["students"])
	}
	// referrals come from users.referred_by
	code := decode(t, do(t, "GET", "/api/ambassador/me", amb, nil))["referral"].(map[string]any)["code"].(string)
	for range 2 {
		u := newUser(t, "student", "")
		exec(t, `update users set referred_by = $2 where id = $1`, u, "amb_"+code)
	}
	if j := decode(t, do(t, "GET", "/api/ambassador/me", amb, nil))["referral"].(map[string]any)["joined"].(float64); j != 2 {
		t.Fatalf("joined = %v", j)
	}
	// pause
	w = do(t, "POST", "/api/ambassador/pause", amb, map[string]any{})
	if w.Code != 200 {
		t.Fatalf("pause: %d", w.Code)
	}
	wl := decode(t, do(t, "GET", "/api/ambassador/me", amb, nil))["workload"].(map[string]any)
	if wl["paused"] != true || wl["available"] != false {
		t.Fatalf("workload: %v", wl)
	}
	if ok, _ := Available(ctx(), amb); ok {
		t.Fatal("paused ambassador available")
	}
	do(t, "DELETE", "/api/ambassador/pause", amb, nil)
	if ok, _ := Available(ctx(), amb); !ok {
		t.Fatal("unpaused ambassador not available")
	}
	// rating is public and lists the team university
	r := decode(t, do(t, "GET", "/api/ambassador/rating", 0, nil))["rating"].([]any)
	found := false
	for _, x := range r {
		if x.(map[string]any)["university"] == "SDU" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rating: %v", r)
	}
}

func loader(t *testing.T) *content.Loader {
	return &content.Loader{Env: func(string) string { return "" }, DataDir: t.TempDir(), Warn: func(string) {}}
}

func contentHas(t *testing.T, list, id string) (map[string]any, bool) {
	t.Helper()
	c := loader(t).Load(ctx(), time.Now())
	b, _ := json.Marshal(c)
	var out map[string][]map[string]any
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(b, &raw)
	out = map[string][]map[string]any{}
	for _, k := range []string{"opportunities", "places"} {
		var xs []map[string]any
		_ = json.Unmarshal(raw[k], &xs)
		out[k] = xs
	}
	for _, x := range out[list] {
		if x["id"] == id {
			return x, true
		}
	}
	return nil, false
}

func TestSubmissionsPublishAndReports(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin", "")
	amb := makeAmbassador(t, admin, "KBTU")
	opp := map[string]any{"type": "grant", "title_kk": "Жас ғалым гранты", "org_kk": "Қор", "desc_kk": "Жас зерттеушілерге арналған грант",
		"source_url": "https://fund.kz/grant", "deadline": time.Now().AddDate(0, 1, 0).Format("2006-01-02"), "free": true, "city": "almaty"}
	if w := do(t, "POST", "/api/submissions", amb, map[string]any{"kind": "opportunity", "university": "ENU", "payload": opp}); w.Code != 403 || errCode(w) != "foreign_university" {
		t.Fatalf("foreign university: %d %s", w.Code, w.Body)
	}
	bad := map[string]any{"type": "grant", "title_kk": "Грант", "org_kk": "Қор", "desc_kk": "Сипаттама мәтіні", "source_url": "http://x.kz"}
	if w := do(t, "POST", "/api/submissions", amb, map[string]any{"kind": "opportunity", "payload": bad}); w.Code != 400 || errCode(w) != "invalid_source_url" {
		t.Fatalf("http source: %d %s", w.Code, w.Body)
	}
	w := do(t, "POST", "/api/submissions", amb, map[string]any{"kind": "opportunity", "payload": opp})
	if w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}
	sid := int64(decode(t, w)["id"].(float64))
	if got := count(t, `select count(*) from submissions where id = $1 and university = 'KBTU'`, sid); got != 1 {
		t.Fatal("university not forced to the ambassador's")
	}
	// not approved -> not on the site
	if _, ok := contentHas(t, "opportunities", fmt.Sprintf("amb-%d", sid)); ok {
		t.Fatal("pending submission visible")
	}
	if c := count(t, `select count(*) from community_content`); c != 0 {
		t.Fatalf("community rows before approval: %d", c)
	}
	q := decode(t, do(t, "GET", "/api/admin/queue?kind=opportunity", admin, nil))["items"].([]any)
	cl := q[0].(map[string]any)["checklist"].(map[string]any)
	if cl["hasOfficialSource"] != true || cl["noFee"] != true || cl["deadlineOk"] != true {
		t.Fatalf("checklist: %v", cl)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", sid), admin, nil); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	var cid int64
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select id from community_content where submission_id = $1`, sid).Scan(&cid)
	})
	rid := fmt.Sprintf("amb-%d", cid)
	rec, ok := contentHas(t, "opportunities", rid)
	if !ok || rec["source"] != "ambassador" || rec["university"] != "KBTU" || rec["title"].(map[string]any)["kk"] != "Жас ғалым гранты" {
		t.Fatalf("published record: %v %v", ok, rec)
	}
	// feature off -> not merged
	t.Setenv("FEATURE_AMBASSADOR", "false")
	if _, ok := contentHas(t, "opportunities", rid); ok {
		t.Fatal("merged while ambassador is off")
	}
	t.Setenv("FEATURE_AMBASSADOR", "true")
	// student suggestion rejected -> author told the reason; "my submissions" shows it
	student := newUser(t, "student", "KBTU")
	w = do(t, "POST", "/api/submissions", student, map[string]any{"kind": "place", "payload": map[string]any{"cat": "canteen", "name_kk": "Асхана", "address": "Төле би 59"}})
	psid := int64(decode(t, w)["id"].(float64))
	do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/reject", psid), admin, map[string]any{"reason": "Мекенжай расталмады"})
	if msgs := box.of(student); len(msgs) != 1 || !strings.Contains(msgs[0], "Мекенжай расталмады") {
		t.Fatalf("student message: %v", msgs)
	}
	mine := decode(t, do(t, "GET", "/api/submissions/my", student, nil))["submissions"].([]any)
	if len(mine) != 1 || mine[0].(map[string]any)["reason"] != "Мекенжай расталмады" {
		t.Fatalf("my submissions: %v", mine)
	}
	// RLS: only own submissions (the application + the opportunity), never the student's
	if n := len(decode(t, do(t, "GET", "/api/submissions/my", amb, nil))["submissions"].([]any)); n != 2 {
		t.Fatalf("ambassador sees %d submissions", n)
	}

	// report rule: hide after 3 distinct reporters with 7+ day old accounts
	fresh := make([]int64, 3)
	for i := range fresh {
		fresh[i] = newUser(t, "student", "")
	}
	exec(t, `update users set created_at = now() - interval '2 days' where id = any($1)`, fresh)
	report := func(uid int64) *httptest.ResponseRecorder {
		return do(t, "POST", "/api/reports", uid, map[string]any{"kind": "opportunity", "id": rid, "reason": "Ақылы, алаяқтық"})
	}
	adminMsgs := len(box.of(admin))
	for _, u := range fresh {
		if w := report(u); w.Code != 200 || decode(t, w)["hidden"] != false {
			t.Fatalf("fresh report: %d %s", w.Code, w.Body)
		}
	}
	waitFor(t, func() bool { return len(box.of(admin)) == adminMsgs+1 }) // first report only
	if _, ok := contentHas(t, "opportunities", rid); !ok {
		t.Fatal("hidden by new accounts")
	}
	old := []int64{newUser(t, "student", ""), newUser(t, "student", ""), newUser(t, "student", "")}
	report(old[0])
	report(old[0]) // same reporter twice counts once
	if w := report(old[1]); decode(t, w)["hidden"] != false {
		t.Fatal("hidden after 2 counted reports")
	}
	if w := report(old[2]); decode(t, w)["hidden"] != true {
		t.Fatalf("not hidden after 3: %s", w.Body)
	}
	if _, ok := contentHas(t, "opportunities", rid); ok {
		t.Fatal("still visible after 3 counted reports")
	}
	time.Sleep(50 * time.Millisecond)
	if len(box.of(admin)) != adminMsgs+1 {
		t.Fatalf("admin got %d report messages", len(box.of(admin))-adminMsgs)
	}
	if w := do(t, "POST", "/api/reports", old[0], map[string]any{"kind": "opportunity", "id": "amb-999999", "reason": "жоқ жазба"}); w.Code != 404 {
		t.Fatalf("missing target: %d", w.Code)
	}
	if w := do(t, "POST", "/api/reports", old[0], map[string]any{"kind": "spaceship", "id": "1", "reason": "белгісіз"}); w.Code != 400 {
		t.Fatalf("unknown kind: %d", w.Code)
	}
	rep := decode(t, do(t, "GET", "/api/admin/reports", admin, nil))["reports"].([]any)
	if len(rep) == 0 || rep[0].(map[string]any)["counted"].(float64) != 3 {
		t.Fatalf("admin reports: %v", rep)
	}
	if w := do(t, "POST", "/api/admin/reports/dismiss", admin, map[string]any{"kind": "opportunity", "id": rid, "reason": "негізсіз"}); w.Code != 200 {
		t.Fatalf("dismiss: %d %s", w.Code, w.Body)
	}
	if _, ok := contentHas(t, "opportunities", rid); !ok {
		t.Fatal("not visible again after dismissal")
	}

	// ambassador deletes the account: the published record stays, author nulled; own rows gone
	if err := me.Delete(ctx(), amb); err != nil {
		t.Fatal(err)
	}
	if got := count(t, `select count(*) from community_content where id = $1 and author_id is null`, cid); got != 1 {
		t.Fatal("published content lost on author deletion")
	}
	if got := count(t, `select (select count(*) from ambassador_terms where user_id = $1) + (select count(*) from ambassador_applications where user_id = $1)`, amb); got != 0 {
		t.Fatalf("ambassador rows left: %d", got)
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

func TestTermExpiryRenewalAndCertificate(t *testing.T) {
	testdb.Need(t)
	admin := newUser(t, "admin", "")
	amb := makeAmbassador(t, admin, "AITU")
	var termID int64
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select id from ambassador_terms where user_id = $1`, amb).Scan(&termID)
	})
	// renewal asked 14 days before the end; "yes" goes to the queue, approval extends the term
	exec(t, `update ambassador_terms set term_end = current_date + 10 where id = $1`, termID)
	st, err := Daily(ctx(), time.Now())
	if err != nil || st.RenewAsks < 1 {
		t.Fatalf("daily: %+v %v", st, err)
	}
	cab := decode(t, do(t, "GET", "/api/ambassador/me", amb, nil))
	if cab["renewal"].(map[string]any)["ask"] != true {
		t.Fatalf("renewal: %v", cab["renewal"])
	}
	if w := do(t, "POST", "/api/ambassador/renew", amb, map[string]any{"continue": true}); w.Code != 200 {
		t.Fatalf("renew: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/ambassador/renew", amb, map[string]any{"continue": true}); w.Code != 409 {
		t.Fatalf("renew twice: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", pendingID(t, KindRenewal, amb)), admin, nil); w.Code != 200 {
		t.Fatalf("approve renewal: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from ambassador_terms where id = $1 and term_end > current_date + 30`, termID); got != 1 {
		t.Fatal("term not extended")
	}
	// certificate: issued by admin, public data, unknown code 404
	w := do(t, "POST", fmt.Sprintf("/api/admin/ambassadors/%d/certificate", termID), admin, nil)
	if w.Code != 200 {
		t.Fatalf("issue: %d %s", w.Code, w.Body)
	}
	code := decode(t, w)["cert_code"].(string)
	if w := do(t, "POST", fmt.Sprintf("/api/admin/ambassadors/%d/certificate", termID), amb, nil); w.Code != 403 {
		t.Fatalf("ambassador issues: %d", w.Code)
	}
	c := decode(t, do(t, "GET", "/api/certificates/"+code, 0, nil))
	if c["university"] != "AITU" || !strings.HasSuffix(c["verify_url"].(string), code) {
		t.Fatalf("certificate: %v", c)
	}
	if w := do(t, "GET", "/api/certificates/nonexistent123", 0, nil); w.Code != 404 {
		t.Fatalf("unknown certificate: %d", w.Code)
	}
	// a published contribution survives the end of the term
	w = do(t, "POST", "/api/submissions", amb, map[string]any{"kind": "place", "payload": map[string]any{"cat": "print", "name_kk": "Көшірме", "address": "Мәңгілік ел 55"}})
	sid := int64(decode(t, w)["id"].(float64))
	do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", sid), admin, nil)
	// term ended yesterday: the cabinet closes at once, before the daily job runs
	exec(t, `update ambassador_terms set term_end = current_date - 1, term_start = current_date - 100 where id = $1`, termID)
	if w := do(t, "GET", "/api/ambassador/me", amb, nil); w.Code != 403 || errCode(w) != "not_ambassador" {
		t.Fatalf("expired cabinet: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/submissions", amb, map[string]any{"kind": "place", "university": "ENU", "payload": map[string]any{"cat": "print", "name_kk": "Көшірме", "address": "Мәңгілік ел 55"}}); w.Code != 201 {
		t.Fatalf("expired ambassador submits as a student: %d", w.Code) // no longer bound to AITU, but still only a suggestion
	}
	r := httptest.NewRequest("GET", "/api/cron/ambassador", nil)
	r.Header.Set("Authorization", "Bearer cron-secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != 200 || decode(t, rec)["ended"].(float64) < 1 {
		t.Fatalf("cron: %d %s", rec.Code, rec.Body)
	}
	if got := count(t, `select count(*) from users where id = $1 and role = 'student'`, amb); got != 1 {
		t.Fatal("role not returned to student")
	}
	if got := count(t, `select count(*) from audit_log where action = 'role_revoke' and target_id = $1`, amb); got != 1 {
		t.Fatal("no role_revoke audit")
	}
	if _, ok := contentHas(t, "places", fmt.Sprintf("amb-%d", count(t, `select id from community_content where submission_id = $1`, sid))); !ok {
		t.Fatal("content removed when the term ended")
	}
	// the certificate still verifies after the term ended
	if w := do(t, "GET", "/api/certificates/"+code, 0, nil); w.Code != 200 {
		t.Fatalf("certificate after term: %d", w.Code)
	}
	// admin ends a term early with a reason
	amb2 := makeAmbassador(t, admin, "AITU")
	var t2 int64
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select id from ambassador_terms where user_id = $1 and status = 'active'`, amb2).Scan(&t2)
	})
	if w := do(t, "POST", fmt.Sprintf("/api/admin/ambassadors/%d/end", t2), admin, map[string]any{}); w.Code != 400 {
		t.Fatalf("end without reason: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/ambassadors/%d/end", t2), admin, map[string]any{"reason": "өз өтініші бойынша"}); w.Code != 200 {
		t.Fatalf("end: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from users where id = $1 and role = 'student'`, amb2); got != 1 {
		t.Fatal("ended early but still ambassador")
	}
	// the same person comes back later: same referral code
	var oldCode string
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select ref_code from ambassador_terms where id = $1`, t2).Scan(&oldCode)
	})
	do(t, "POST", "/api/ambassador/apply", amb2, map[string]any{"university": "AITU", "motivation": "Қайта оралғым келеді", "hours_per_week": "1-2"})
	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", pendingID(t, KindApplication, amb2)), admin, nil); w.Code != 200 {
		t.Fatalf("re-approve: %d %s", w.Code, w.Body)
	}
	if got := count(t, `select count(*) from ambassador_terms where user_id = $1 and status = 'active' and ref_code = $2`, amb2, oldCode); got != 1 {
		t.Fatal("ref code not reused")
	}
	if list := decode(t, do(t, "GET", "/api/admin/ambassadors?status=all", admin, nil))["terms"].([]any); len(list) < 3 {
		t.Fatalf("admin list: %d", len(list))
	}
	if w := do(t, "PATCH", fmt.Sprintf("/api/admin/ambassadors/%d", termID), admin, map[string]any{"hours_per_week": 3}); w.Code != 404 {
		t.Fatalf("patch ended term: %d", w.Code)
	}
}

// «Мүмкіндік ұсыну»: a student suggests with the ambassador switch off, the admin corrects the
// pending payload, approves it; it is published as source = "student" and the author is told.
func TestStudentSuggestionWithoutAmbassadorSwitch(t *testing.T) {
	testdb.Need(t)
	t.Setenv("FEATURE_AMBASSADOR", "false")
	admin := newUser(t, "admin", "")
	student := newUser(t, "student", "ENU")
	other := newUser(t, "student", "ENU")
	exec(t, `update users set consent_at = null where id = $1`, other)
	deadline := time.Now().AddDate(0, 2, 0).Format("2006-01-02")
	opp := map[string]any{"type": "grant", "title_kk": "Жас зерттеуші гарнты", "org_kk": "Ғылым қоры",
		"desc_kk": "Студенттерге арналған зерттеу гранты", "source_url": "https://science.kz/grant",
		"deadline": deadline, "free": true, "city": "astana"}

	if w := do(t, "POST", "/api/submissions", 0, map[string]any{"kind": "opportunity", "payload": opp}); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := do(t, "POST", "/api/submissions", other, map[string]any{"kind": "opportunity", "payload": opp}); w.Code != 403 || errCode(w) != "consent_required" {
		t.Fatalf("no consent: %d %s", w.Code, w.Body)
	}
	w := do(t, "POST", "/api/submissions", student, map[string]any{"kind": "opportunity", "payload": opp})
	if w.Code != 201 {
		t.Fatalf("suggest with the switch off: %d %s", w.Code, w.Body)
	}
	sid := int64(decode(t, w)["id"].(float64))

	// the admin fixes the typo and adds the ru title; same validation as the submission
	path := fmt.Sprintf("/api/admin/queue/%d", sid)
	fixed := map[string]any{}
	for k, v := range opp {
		fixed[k] = v
	}
	fixed["title_kk"], fixed["title_ru"] = "Жас зерттеуші гранты", "Грант молодого исследователя"
	if w := do(t, "PATCH", path, student, map[string]any{"payload": fixed}); w.Code != 403 {
		t.Fatalf("student edits the queue: %d", w.Code)
	}
	if w := do(t, "PATCH", path, 0, map[string]any{"payload": fixed}); w.Code != 401 {
		t.Fatalf("anonymous edits the queue: %d", w.Code)
	}
	bad := map[string]any{}
	for k, v := range fixed {
		bad[k] = v
	}
	bad["source_url"] = "http://science.kz"
	if w := do(t, "PATCH", path, admin, map[string]any{"payload": bad}); w.Code != 400 || errCode(w) != "invalid_source_url" {
		t.Fatalf("invalid edit: %d %s", w.Code, w.Body)
	}
	w = do(t, "PATCH", path, admin, map[string]any{"payload": fixed})
	if w.Code != 200 {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	var stored map[string]any
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select payload from submissions where id = $1`, sid).Scan(&stored)
	})
	if stored["title_kk"] != "Жас зерттеуші гранты" || stored["author_role"] != "student" {
		t.Fatalf("stored payload: %v", stored)
	}
	if n := count(t, `select count(*) from audit_log where action = 'moderation_edit' and actor_id = $1
		and meta->>'submission' = $2 and meta->'changed' ? 'title_kk'`, admin, fmt.Sprint(sid)); n != 1 {
		t.Fatalf("edit audit rows: %d", n)
	}

	if w := do(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", sid), admin, map[string]any{}); w.Code != 200 || decode(t, w)["notified"] != true {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", path, admin, map[string]any{"payload": fixed}); w.Code != 409 || errCode(w) != "not_pending" {
		t.Fatalf("edit after approval: %d %s", w.Code, w.Body)
	}
	var cid int64
	_ = db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select id from community_content where submission_id = $1`, sid).Scan(&cid)
	})
	rec, ok := contentHas(t, "opportunities", fmt.Sprintf("amb-%d", cid))
	if !ok || rec["source"] != "student" || rec["title"].(map[string]any)["ru"] != "Грант молодого исследователя" || rec["deadline"] != deadline {
		t.Fatalf("published: %v %v", ok, rec)
	}
	if msgs := box.of(student); len(msgs) != 1 || !strings.Contains(msgs[0], fmt.Sprintf("№%d", sid)) {
		t.Fatalf("author notice: %v", msgs)
	}
	// non-editable kinds
	exec(t, `insert into submissions (kind, payload, submitted_by) values ('market_report', '{}', $1)`, student)
	mid := pendingID(t, "market_report", student)
	if w := do(t, "PATCH", fmt.Sprintf("/api/admin/queue/%d", mid), admin, map[string]any{"payload": map[string]any{}}); w.Code != 400 || errCode(w) != "not_editable" {
		t.Fatalf("market_report edit: %d %s", w.Code, w.Body)
	}
}

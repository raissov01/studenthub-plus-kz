package mentors

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// tg records what the fake Telegram API received: chat id -> texts.
var tg = struct {
	sync.Mutex
	msgs map[int64][]string
}{msgs: map[int64][]string{}}

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("s", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "10000")
	os.Setenv("FEATURE_MENTORS", "on")
	os.Setenv("CRON_SECRET", "cron-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		tg.Lock()
		tg.msgs[body.ChatID] = append(tg.msgs[body.ChatID], body.Text)
		tg.Unlock()
		io.WriteString(w, `{"ok":true}`)
	}))
	os.Setenv("BOT_TOKEN", "123:test")
	os.Setenv("TELEGRAM_API_BASE", srv.URL)
	code := testdb.Main(m, "mentors")
	srv.Close()
	os.Exit(code)
}

func ctx() context.Context { return context.Background() }

var seq atomic.Int64

type userOpt struct {
	role       string
	university string
	faculty    string
	course     int
	adult      *bool
	username   string
	ageDays    int
}

var yes, no = true, false

// newUser creates a consented user; returns id and telegram id.
func newUser(t *testing.T, o userOpt) (int64, int64) {
	t.Helper()
	if o.role == "" {
		o.role = "student"
	}
	if o.university == "" {
		o.university = "ENU"
	}
	if o.adult == nil {
		o.adult = &yes
	}
	n := seq.Add(1)
	tgID := 900000 + n
	if o.username == "" {
		o.username = "user" + strconv.FormatInt(n, 10)
	}
	var course *int
	if o.course > 0 {
		course = &o.course
	}
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, tg_username, name, university, faculty, course, role, consent_at, is_adult, created_at)
			values ($1, nullif($2, '-'), $3, $4, nullif($5, ''), $6, $7::user_role, now(), $8, now() - make_interval(days => $9)) returning id`,
			tgID, o.username, "User "+strconv.FormatInt(n, 10), o.university, o.faculty, course, o.role, *o.adult, o.ageDays).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, tgID
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

type resp struct {
	*httptest.ResponseRecorder
}

func (r resp) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Fatalf("json: %v %s", err, r.Body)
	}
	return m
}

func (r resp) code() string {
	var m struct{ Error struct{ Code string } }
	json.Unmarshal(r.Body.Bytes(), &m)
	return m.Error.Code
}

func do(t *testing.T, method, path string, uid int64, body any) resp {
	t.Helper()
	var rd io.Reader = bytes.NewReader(nil)
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	if strings.HasPrefix(path, "/api/cron/") {
		r.Header.Set("Authorization", "Bearer cron-secret")
	}
	m := http.NewServeMux()
	Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return resp{w}
}

func sent(tgID int64) []string {
	tg.Lock()
	defer tg.Unlock()
	return append([]string(nil), tg.msgs[tgID]...)
}

// approvedMentor creates a user, applies through the API and approves through the queue.
func approvedMentor(t *testing.T, o userOpt, capacity int) (int64, int64) {
	t.Helper()
	if o.course == 0 {
		o.course = 3
	}
	id, tgID := newUser(t, o)
	w := do(t, "POST", "/api/mentors/apply", id, map[string]any{"areas": []string{"adaptation", "study"}, "format": "both",
		"about": "3-курс, жатақханада тұрамын", "capacity": capacity})
	if w.Code != 201 {
		t.Fatalf("apply: %d %s", w.Code, w.Body)
	}
	sub := int64(w.json(t)["submissionId"].(float64))
	admin, _ := newUser(t, userOpt{role: "admin"})
	err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := moderation.Review(ctx(), tx, sub, admin, true, ""); return err })
	if err != nil {
		t.Fatal(err)
	}
	return id, tgID
}

func request(t *testing.T, uid int64, need string) int64 {
	t.Helper()
	w := do(t, "POST", "/api/mentors/requests", uid, map[string]any{"need": need, "goal": "Жатақхана құжаттары"})
	if w.Code != 201 {
		t.Fatalf("request: %d %s", w.Code, w.Body)
	}
	return int64(w.json(t)["request"].(map[string]any)["id"].(float64))
}

func TestApplicationEligibilityAndQueue(t *testing.T) {
	testdb.Need(t)
	first, _ := newUser(t, userOpt{course: 1})
	minor, _ := newUser(t, userOpt{course: 3, adult: &no})
	for _, id := range []int64{first, minor} {
		if w := do(t, "POST", "/api/mentors/apply", id, map[string]any{"areas": []string{"study"}}); w.Code != 403 || w.code() != "not_eligible" {
			t.Fatalf("ineligible apply: %d %s", w.Code, w.Body)
		}
	}
	alumni, _ := newUser(t, userOpt{course: 1})
	if w := do(t, "POST", "/api/mentors/apply", alumni, map[string]any{"areas": []string{"career"}, "alumni": true}); w.Code != 201 {
		t.Fatalf("alumni apply: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/mentors/apply", alumni, map[string]any{"areas": []string{"career"}, "alumni": true}); w.Code != 409 || w.code() != "already_applied" {
		t.Fatalf("double apply: %d %s", w.Code, w.Body)
	}
	if n := scalar[int](t, `select count(*) from submissions where kind = 'mentor_application' and submitted_by = $1 and status = 'pending'`, alumni); n != 1 {
		t.Fatalf("queue rows: %d", n)
	}
	// pending mentor cannot approve themselves through the profile endpoint (RLS)
	if w := do(t, "PATCH", "/api/mentors/me", alumni, map[string]any{"status": "approved"}); w.Code != 409 {
		t.Fatalf("self-approve: %d %s", w.Code, w.Body)
	}
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		if IsApprovedMentor(ctx(), tx, alumni) {
			t.Error("pending mentor reported approved")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// rejected through the queue, may re-apply
	admin, _ := newUser(t, userOpt{role: "admin"})
	sub := scalar[int64](t, `select id from submissions where kind = 'mentor_application' and submitted_by = $1`, alumni)
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		_, err := moderation.Review(ctx(), tx, sub, admin, false, "әңгімеге келмеді")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if s := scalar[string](t, `select status from mentors where user_id = $1`, alumni); s != "rejected" {
		t.Fatalf("status after reject: %s", s)
	}
	if w := do(t, "POST", "/api/mentors/apply", alumni, map[string]any{"areas": []string{"career"}, "alumni": true}); w.Code != 201 {
		t.Fatalf("re-apply: %d %s", w.Code, w.Body)
	}

	m, _ := approvedMentor(t, userOpt{}, 2)
	err = db.WithUser(ctx(), m, func(tx pgx.Tx) error {
		if !IsApprovedMentor(ctx(), tx, m) {
			t.Error("approved mentor not recognised (as app_user)")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// pause / resume
	if w := do(t, "PATCH", "/api/mentors/me", m, map[string]any{"status": "paused"}); w.Code != 200 {
		t.Fatalf("pause: %d %s", w.Code, w.Body)
	}
	err = db.Tx(ctx(), func(tx pgx.Tx) error {
		if IsApprovedMentor(ctx(), tx, m) {
			t.Error("paused mentor still approved")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if w := do(t, "PATCH", "/api/mentors/me", m, map[string]any{"status": "approved", "capacity": 3}); w.Code != 200 {
		t.Fatalf("resume: %d %s", w.Code, w.Body)
	}
}

func TestCapacityAndHonestSummary(t *testing.T) {
	testdb.Need(t)
	uni := "Capacity Uni"
	admin, _ := newUser(t, userOpt{role: "admin", university: uni})
	mentor, _ := approvedMentor(t, userOpt{university: uni}, 2)
	var reqs []int64
	for i := 0; i < 3; i++ {
		s, _ := newUser(t, userOpt{university: uni, course: 1})
		reqs = append(reqs, request(t, s, "adaptation"))
	}
	w := do(t, "GET", "/api/mentors/summary?university="+strings.ReplaceAll(uni, " ", "+"), 0, nil)
	st := w.json(t)["stats"].(map[string]any)
	if st["mentors"].(float64) != 1 || st["waiting"].(float64) != 3 || st["freeMentors"].(float64) != 1 || st["typicalDays"] != nil {
		t.Fatalf("summary: %s", w.Body)
	}
	for i, req := range reqs {
		w := do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": req, "mentorId": mentor})
		if i < 2 && w.Code != 201 {
			t.Fatalf("match %d: %d %s", i, w.Code, w.Body)
		}
		if i == 2 && (w.Code != 409 || w.code() != "mentor_full") {
			t.Fatalf("over capacity: %d %s", w.Code, w.Body)
		}
	}
	if n := scalar[int](t, `select count(*) from mentor_pairs where mentor_id = $1 and ended_at is null`, mentor); n != 2 {
		t.Fatalf("active pairs %d", n)
	}
	// a full mentor disappears from the candidate list
	w = do(t, "GET", "/api/admin/mentors/requests/"+strconv.FormatInt(reqs[2], 10)+"/candidates", admin, nil)
	if w.Code != 200 || len(w.json(t)["candidates"].([]any)) != 0 {
		t.Fatalf("candidates of full mentor: %s", w.Body)
	}
}

func TestCapacityUnderConcurrency(t *testing.T) {
	testdb.Need(t)
	uni := "Race Uni"
	admin, _ := newUser(t, userOpt{role: "admin", university: uni})
	mentor, _ := approvedMentor(t, userOpt{university: uni}, 1)
	var reqs []int64
	for i := 0; i < 6; i++ {
		s, _ := newUser(t, userOpt{university: uni})
		reqs = append(reqs, request(t, s, "study"))
	}
	var ok atomic.Int32
	var wg sync.WaitGroup
	for _, req := range reqs {
		wg.Add(1)
		go func(req int64) {
			defer wg.Done()
			if w := do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": req, "mentorId": mentor}); w.Code == 201 {
				ok.Add(1)
			}
		}(req)
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d concurrent matches succeeded with capacity 1", ok.Load())
	}
}

func TestMinorGetsNoPairAndNoUsernames(t *testing.T) {
	testdb.Need(t)
	uni := "Minor Uni"
	admin, adminTG := newUser(t, userOpt{role: "admin", university: uni})
	mentor, mentorTG := approvedMentor(t, userOpt{university: uni, username: "mentor_secret"}, 2)
	minor, minorTG := newUser(t, userOpt{university: uni, adult: &no, username: "minor_secret"})
	w := do(t, "POST", "/api/mentors/requests", minor, map[string]any{"need": "adaptation", "goal": "", "expect": "one_on_one"})
	if w.Code != 201 {
		t.Fatalf("minor request: %d %s", w.Code, w.Body)
	}
	body := w.json(t)
	req := int64(body["request"].(map[string]any)["id"].(float64))
	if body["request"].(map[string]any)["expect"] != "group" || body["groupOnly"] != true || body["offerGroup"] != true {
		t.Fatalf("minor request not group-only: %s", w.Body)
	}
	w = do(t, "GET", "/api/admin/mentors/requests/"+strconv.FormatInt(req, 10)+"/candidates", admin, nil)
	if c := w.json(t); c["groupOnly"] != true || len(c["candidates"].([]any)) != 0 {
		t.Fatalf("minor candidates: %s", w.Body)
	}
	// even if the request row is tampered to expect one_on_one, the database refuses
	exec(t, `update mentor_requests set expect = 'one_on_one' where id = $1`, req)
	w = do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": req, "mentorId": mentor})
	if w.Code != 409 || w.code() != "mentee_minor" {
		t.Fatalf("minor match: %d %s", w.Code, w.Body)
	}
	if n := scalar[int](t, `select count(*) from mentor_pairs where mentee_id = $1`, minor); n != 0 {
		t.Fatal("pair created for a minor")
	}
	// board shows no usernames at all
	w = do(t, "GET", "/api/admin/mentors?university="+strings.ReplaceAll(uni, " ", "+"), admin, nil)
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("board leaks usernames: %s", w.Body)
	}
	for _, id := range []int64{minorTG, mentorTG, adminTG} {
		for _, m := range sent(id) {
			if strings.Contains(m, "minor_secret") || (id == minorTG && strings.Contains(m, "mentor_secret")) {
				t.Fatalf("username leaked to %d: %q", id, m)
			}
		}
	}
}

func TestAdultPairIntroAndLifecycle(t *testing.T) {
	testdb.Need(t)
	uni := "Life Uni"
	admin, _ := newUser(t, userOpt{role: "admin", university: uni})
	mentor, mentorTG := approvedMentor(t, userOpt{university: uni, username: "good_mentor"}, 2)
	mentee, menteeTG := newUser(t, userOpt{university: uni, username: "new_student"})
	req := request(t, mentee, "adaptation")
	w := do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": req, "mentorId": mentor})
	if w.Code != 201 {
		t.Fatalf("match: %d %s", w.Code, w.Body)
	}
	pair := int64(w.json(t)["pairId"].(float64))
	toMentor, toMentee := sent(mentorTG), sent(menteeTG)
	if len(toMentee) == 0 || !strings.Contains(toMentee[len(toMentee)-1], "@good_mentor") || !strings.Contains(toMentee[len(toMentee)-1], "15") {
		t.Fatalf("mentee intro: %q", toMentee)
	}
	if !strings.Contains(toMentor[len(toMentor)-1], "@new_student") || !strings.Contains(toMentor[len(toMentor)-1], "университет аумағында") {
		t.Fatalf("mentor intro: %q", toMentor)
	}
	if n := scalar[int](t, `select count(*) from audit_log where action = 'mentor_match' and actor_id = $1`, admin); n != 1 {
		t.Fatal("match not audited")
	}
	// the mentee sees the partner's username; a third student sees nothing of the pair
	w = do(t, "GET", "/api/mentors/pairs", mentee, nil)
	if !strings.Contains(w.Body.String(), "good_mentor") {
		t.Fatalf("pairs: %s", w.Body)
	}
	other, _ := newUser(t, userOpt{university: uni})
	if w := do(t, "GET", "/api/mentors/pairs", other, nil); strings.Contains(w.Body.String(), "good_mentor") {
		t.Fatal("pair visible to a stranger")
	}
	ps := "/api/mentors/pairs/" + strconv.FormatInt(pair, 10)
	if w := do(t, "POST", ps+"/end", other, map[string]any{}); w.Code != 404 {
		t.Fatalf("stranger ends pair: %d", w.Code)
	}
	if w := do(t, "POST", ps+"/checkin", mentee, map[string]any{"kind": "check3", "met": true, "score": 4}); w.Code != 409 || w.code() != "too_early" {
		t.Fatalf("early check-in: %d %s", w.Code, w.Body)
	}
	exec(t, `update mentor_pairs set started_at = now() - interval '29 days' where id = $1`, pair)
	if w := do(t, "POST", ps+"/checkin", mentee, map[string]any{"kind": "check3", "met": false}); w.Code != 200 || w.json(t)["offerRematch"] != true {
		t.Fatalf("check3 no: %d %s", w.Code, w.Body)
	}
	for _, uid := range []int64{mentor, mentee} {
		if w := do(t, "POST", ps+"/checkin", uid, map[string]any{"kind": "month", "continue": true}); w.Code != 200 {
			t.Fatalf("month: %d %s", w.Code, w.Body)
		}
	}
	if scalar[*time.Time](t, `select continued_at from mentor_pairs where id = $1`, pair) == nil {
		t.Fatal("both said yes but the pair did not continue")
	}
	if w := do(t, "POST", ps+"/end", mentee, map[string]any{"reason": "", "score": 5}); w.Code != 200 {
		t.Fatalf("end: %d %s", w.Code, w.Body)
	}
	if s := scalar[string](t, `select status from mentor_requests where id = $1`, req); s != "closed" {
		t.Fatalf("request after end: %s", s)
	}
	if n := scalar[int](t, `select count(*) from user_badges where user_id = $1 and badge = 'mentor' and verified`, mentor); n != 1 {
		t.Fatal("mentor badge not awarded for score 5")
	}
	// after the end the username is no longer shown
	if w := do(t, "GET", "/api/mentors/pairs", mentee, nil); strings.Contains(w.Body.String(), "good_mentor") {
		t.Fatalf("username shown for an ended pair: %s", w.Body)
	}
}

func TestUnapprovedMentorIsNotMatched(t *testing.T) {
	testdb.Need(t)
	uni := "Pending Uni"
	admin, _ := newUser(t, userOpt{role: "admin", university: uni})
	pending, _ := newUser(t, userOpt{university: uni, course: 2})
	do(t, "POST", "/api/mentors/apply", pending, map[string]any{"areas": []string{"study"}})
	paused, _ := approvedMentor(t, userOpt{university: uni}, 2)
	do(t, "PATCH", "/api/mentors/me", paused, map[string]any{"status": "paused"})
	plain, _ := newUser(t, userOpt{university: uni})
	s, _ := newUser(t, userOpt{university: uni})
	req := request(t, s, "study")
	for _, m := range []int64{pending, paused, plain} {
		w := do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": req, "mentorId": m})
		if w.Code != 409 || w.code() != "mentor_not_approved" {
			t.Fatalf("match with %d: %d %s", m, w.Code, w.Body)
		}
	}
}

func TestAmbassadorOnlyOwnUniversity(t *testing.T) {
	testdb.Need(t)
	amb, _ := newUser(t, userOpt{role: "ambassador", university: "Amb A"})
	mentorA, _ := approvedMentor(t, userOpt{university: "Amb A"}, 2)
	mentorB, _ := approvedMentor(t, userOpt{university: "Amb B"}, 2)
	sa, _ := newUser(t, userOpt{university: "Amb A"})
	sb, _ := newUser(t, userOpt{university: "Amb B"})
	reqA, reqB := request(t, sa, "career"), request(t, sb, "career")

	w := do(t, "GET", "/api/ambassador/mentors?university=Amb+B", amb, nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"Amb A"`) || strings.Contains(body, `"Amb B"`) {
		t.Fatalf("ambassador board: %d %s", w.Code, body)
	}
	if w := do(t, "GET", "/api/ambassador/mentors/requests/"+strconv.FormatInt(reqB, 10)+"/candidates", amb, nil); w.Code != 404 {
		t.Fatalf("foreign candidates: %d", w.Code)
	}
	for _, c := range []struct{ req, mentor int64 }{{reqB, mentorB}, {reqA, mentorB}, {reqB, mentorA}} {
		w := do(t, "POST", "/api/ambassador/mentors/match", amb, map[string]any{"requestId": c.req, "mentorId": c.mentor})
		if w.Code != 403 || w.code() != "wrong_university" {
			t.Fatalf("foreign match %v: %d %s", c, w.Code, w.Body)
		}
	}
	if n := scalar[int](t, `select count(*) from mentor_pairs where mentee_id in ($1, $2)`, sa, sb); n != 0 {
		t.Fatal("cross-university pair created")
	}
	if w := do(t, "POST", "/api/ambassador/mentors/match", amb, map[string]any{"requestId": reqA, "mentorId": mentorA}); w.Code != 201 {
		t.Fatalf("own match: %d %s", w.Code, w.Body)
	}
	// roles: ambassadors stay out of /api/admin, students out of both
	if w := do(t, "GET", "/api/admin/mentors", amb, nil); w.Code != 403 {
		t.Fatalf("ambassador on admin route: %d", w.Code)
	}
	if w := do(t, "GET", "/api/ambassador/mentors", sa, nil); w.Code != 403 {
		t.Fatalf("student on ambassador route: %d", w.Code)
	}
	if w := do(t, "GET", "/api/ambassador/mentors", 0, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	// the lead-only hook (06) is honoured
	AmbassadorCanMatch = func(context.Context, int64, string) (bool, error) { return false, nil }
	t.Cleanup(func() { AmbassadorCanMatch = nil })
	if w := do(t, "GET", "/api/ambassador/mentors", amb, nil); w.Code != 403 || w.code() != "not_lead" {
		t.Fatalf("non-lead: %d %s", w.Code, w.Body)
	}
}

func TestWeek3QuestionRespectsOnePerDay(t *testing.T) {
	testdb.Need(t)
	uni := "Cron Uni"
	admin, _ := newUser(t, userOpt{role: "admin", university: uni})
	mentor, mentorTG := approvedMentor(t, userOpt{university: uni}, 2)
	mentee, menteeTG := newUser(t, userOpt{university: uni})
	w := do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": request(t, mentee, "life"), "mentorId": mentor})
	pair := int64(w.json(t)["pairId"].(float64))
	exec(t, `update mentor_pairs set started_at = now() - interval '22 days' where id = $1`, pair)
	now := time.Now()
	// the mentee already got a bot message today (e.g. a deadline reminder)
	exec(t, `insert into bot_daily_sends (user_id, day) values ($1, $2::date)`, mentee, notify.Day(now))
	before := len(sent(menteeTG))
	mentorBefore := len(sent(mentorTG))

	res, err := RunCron(ctx(), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Check3Sent != 1 || res.Postponed != 1 {
		t.Fatalf("first run: %+v", res)
	}
	if len(sent(menteeTG)) != before || len(sent(mentorTG)) != mentorBefore+1 {
		t.Fatal("day budget broken on the first run")
	}
	// same day again: nothing new for anyone
	if res, _ := RunCron(ctx(), now); res.Check3Sent != 0 {
		t.Fatalf("second run same day: %+v", res)
	}
	if len(sent(mentorTG)) != mentorBefore+1 {
		t.Fatal("mentor asked twice")
	}
	// tomorrow the mentee gets it, exactly once
	res, err = RunCron(ctx(), now.Add(24*time.Hour))
	if err != nil || res.Check3Sent != 1 {
		t.Fatalf("next day: %+v %v", res, err)
	}
	if len(sent(menteeTG)) != before+1 {
		t.Fatal("mentee not asked the next day")
	}
	if n := scalar[int](t, `select count(*) from bot_daily_sends where user_id = $1 group by day order by 1 desc limit 1`, mentee); n != 1 {
		t.Fatal("more than one claim per day")
	}
	if scalar[*time.Time](t, `select check3_at from mentor_pairs where id = $1`, pair) == nil {
		t.Fatal("check3_at not set")
	}
	// HTTP entry point needs the cron secret
	r := httptest.NewRequest("GET", "/api/cron/mentors", nil)
	m := http.NewServeMux()
	Routes(m)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, r)
	if rec.Code != 401 {
		t.Fatalf("cron without secret: %d", rec.Code)
	}
	if w := do(t, "GET", "/api/cron/mentors", 0, nil); w.Code != 200 {
		t.Fatalf("cron with secret: %d %s", w.Code, w.Body)
	}
}

func TestReportFlow(t *testing.T) {
	testdb.Need(t)
	uni := "Report Uni"
	admin, adminTG := newUser(t, userOpt{role: "admin", university: uni})
	mentor, _ := approvedMentor(t, userOpt{university: uni}, 2)
	mentee, menteeTG := newUser(t, userOpt{university: uni})
	w := do(t, "POST", "/api/admin/mentors/match", admin, map[string]any{"requestId": request(t, mentee, "life"), "mentorId": mentor})
	pair := int64(w.json(t)["pairId"].(float64))
	ps := "/api/mentors/pairs/" + strconv.FormatInt(pair, 10)
	adminBefore := len(sent(adminTG))

	stranger, _ := newUser(t, userOpt{university: uni})
	if w := do(t, "POST", ps+"/report", stranger, map[string]any{"reason": "money"}); w.Code != 404 {
		t.Fatalf("stranger report: %d", w.Code)
	}
	w = do(t, "POST", ps+"/report", mentee, map[string]any{"reason": "money", "text": "қарыз сұрады", "endPair": true})
	if w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body)
	}
	sub := int64(w.json(t)["submissionId"].(float64))
	if n := scalar[int](t, `select count(*) from submissions where id = $1 and kind = 'mentor_report' and status = 'pending'
		and (payload->>'againstId')::bigint = $2`, sub, mentor); n != 1 {
		t.Fatal("report not queued")
	}
	if scalar[*time.Time](t, `select ended_at from mentor_pairs where id = $1`, pair) == nil {
		t.Fatal("endPair did not end the pair")
	}
	if len(sent(adminTG)) <= adminBefore {
		t.Fatal("admin not notified of the report")
	}
	menteeBefore := len(sent(menteeTG))
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := moderation.Review(ctx(), tx, sub, admin, true, ""); return err }); err != nil {
		t.Fatal(err)
	}
	if len(sent(menteeTG)) != menteeBefore+1 {
		t.Fatal("reporter not told about the decision")
	}
	// the admin takes the mentor role away (audited); the mentor cannot undo it
	w = do(t, "POST", "/api/admin/mentors/"+strconv.FormatInt(mentor, 10)+"/status", admin, map[string]any{"status": "removed", "reason": "шағым расталды"})
	if w.Code != 200 {
		t.Fatalf("remove mentor: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", "/api/mentors/me", mentor, map[string]any{"status": "approved"}); w.Code != 409 {
		t.Fatalf("removed mentor re-approved self: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/mentors/apply", mentor, map[string]any{"areas": []string{"study"}}); w.Code != 409 || w.code() != "mentor_removed" {
		t.Fatalf("removed mentor re-applies: %d %s", w.Code, w.Body)
	}
	if n := scalar[int](t, `select count(*) from audit_log where action = 'mentor_status' and target_id = $1`, mentor); n != 1 {
		t.Fatal("status change not audited")
	}
	if w := do(t, "POST", "/api/admin/mentors/"+strconv.FormatInt(mentor, 10)+"/status", admin, map[string]any{"status": "removed"}); w.Code != 400 {
		t.Fatalf("status without reason: %d", w.Code)
	}
}

func TestFeatureOff(t *testing.T) {
	testdb.Need(t)
	t.Setenv("FEATURE_MENTORS", "off")
	admin, _ := newUser(t, userOpt{role: "admin"})
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/mentors/summary"}, {"POST", "/api/mentors/requests"}, {"GET", "/api/admin/mentors"},
		{"GET", "/api/ambassador/mentors"}, {"GET", "/api/cron/mentors"},
	} {
		if w := do(t, c.method, c.path, admin, map[string]any{}); w.Code != 404 {
			t.Errorf("%s %s with feature off: %d", c.method, c.path, w.Code)
		}
	}
}

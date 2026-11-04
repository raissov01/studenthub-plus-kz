package surveys_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/server"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/surveys"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Main(m, "surveys")) }

var bg = context.Background()

func env(t *testing.T) {
	t.Helper()
	testdb.Need(t)
	t.Setenv("FEATURE_SURVEYS", "true")
	t.Setenv("FEATURE_POINTS", "true")
	t.Setenv("ALLOWED_HOSTS", "localhost")
	t.Setenv("SESSION_SECRET", strings.Repeat("survey-test-secret", 3))
	t.Setenv("RATE_WRITE_PER_MIN", "10000")
}

var seq int64

func user(t *testing.T, role, univ, faculty string, course int) int64 {
	t.Helper()
	seq++
	var id int64
	err := db.Tx(bg, func(tx pgx.Tx) error {
		return tx.QueryRow(bg, `insert into users (telegram_id, name, role, university, faculty, course, consent_at, is_adult, bot_inactive)
			values ((select coalesce(max(telegram_id), 50000) + 1 from users), $1, $2, $3, nullif($4, ''), nullif($5, 0), now(), true, true) returning id`,
			fmt.Sprintf("Survey user %d", seq), role, univ, faculty, course).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	if role == "university_staff" && univ != "" {
		exec(t, `insert into university_staff (user_id, university, granted_by) values ($1, $2, $1)`, id, univ)
	}
	return id
}

func exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if err := db.Tx(bg, func(tx pgx.Tx) error { _, err := tx.Exec(bg, q, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(bg, func(tx pgx.Tx) error { return tx.QueryRow(bg, q, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func call(t *testing.T, method, path string, body any, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	var s string
	if body != nil {
		b, _ := json.Marshal(body)
		s = string(b)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(s))
	r.Header.Set("Origin", "http://localhost")
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}
	return v
}

var questions = []map[string]any{
	{"id": "q1", "type": "single", "text": "Асхана сапасы қалай?", "required": true, "options": []string{"Жақсы", "Орташа", "Нашар"}},
	{"id": "q2", "type": "scale", "text": "Бағалаңыз", "required": true, "max": 5},
	{"id": "q3", "type": "text", "text": "Ұсыныс", "required": false},
	{"id": "q4", "type": "matrix", "text": "Жатақхана", "required": false, "max": 5, "rows": []string{"Тазалық", "Жылу"}},
	{"id": "q5", "type": "multiple", "text": "Не ұнайды?", "required": false, "options": []string{"Ас", "Баға", "Орын"}},
}

func newSurvey(t *testing.T, staff int64, anonymous bool, audience map[string]any) int64 {
	t.Helper()
	w := call(t, "POST", "/api/staff/surveys", map[string]any{"title": "Асхана сапасы", "questions": questions, "anonymous": anonymous, "audience": audience}, staff)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	id := decode[struct{ Survey struct{ ID int64 } }](t, w).Survey.ID
	if w := call(t, "POST", fmt.Sprintf("/api/staff/surveys/%d/publish", id), nil, staff); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	return id
}

func answer(t *testing.T, id, uid int64, choice int) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, "POST", fmt.Sprintf("/api/surveys/%d/responses", id), map[string]any{"answers": map[string]any{
		"q1": choice, "q2": 4, "q3": fmt.Sprintf("secret text of %d", uid), "q4": []int{5, 3}, "q5": []int{0, 2},
	}}, uid)
}

// The anonymity schema: the responses table has no way to reach a person.
func TestAnonymousResponsesHaveNoUserLink(t *testing.T) {
	env(t)
	for _, table := range []string{"survey_responses", "survey_markers"} {
		if n := count(t, `select count(*) from pg_constraint where conrelid = $1::regclass and contype = 'f' and confrelid = 'users'::regclass`, table); n != 0 {
			t.Fatalf("%s references users", table)
		}
		if n := count(t, `select count(*) from information_schema.columns where table_name = $1
			and (column_name like '%user%' or data_type like 'timestamp%' or column_name like '%_at')`, table); n != 0 {
			t.Fatalf("%s has a user or time column", table)
		}
	}
	staff := user(t, "university_staff", "ANON-U", "", 0)
	stud := user(t, "student", "ANON-U", "it", 2)
	id := newSurvey(t, staff, true, nil)
	if w := answer(t, id, stud, 0); w.Code != 201 {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	if count(t, `select count(*) from survey_response_owners o join survey_responses r on r.id = o.response_id where r.survey_id = $1`, id) != 0 {
		t.Fatal("anonymous survey stored an owner row")
	}
	// nothing stored equals or contains the user id, and the marker is not a plain hash of it
	var answers, marker string
	err := db.Tx(bg, func(tx pgx.Tx) error {
		if err := tx.QueryRow(bg, `select to_jsonb(r)::text from survey_responses r where survey_id = $1`, id).Scan(&answers); err != nil {
			return err
		}
		return tx.QueryRow(bg, `select encode(marker, 'hex') from survey_markers where survey_id = $1`, id).Scan(&marker)
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answers, `"`+strconv.FormatInt(stud, 10)+`"`) || strings.Contains(answers, "user") {
		t.Fatalf("response leaks the user: %s", answers)
	}
	plain := sha256.Sum256([]byte(strconv.FormatInt(stud, 10)))
	if marker == fmt.Sprintf("%x", plain) {
		t.Fatal("marker is an unkeyed hash")
	}
	// the marker depends on the server key: a different SESSION_SECRET gives a different marker
	var salt []byte
	_ = db.Tx(bg, func(tx pgx.Tx) error { return tx.QueryRow(bg, `select marker_salt from surveys where id = $1`, id).Scan(&salt) })
	m1, _ := surveys.Marker(id, salt, stud)
	t.Setenv("SESSION_SECRET", strings.Repeat("another-secret-xx", 3))
	m2, _ := surveys.Marker(id, salt, stud)
	if fmt.Sprintf("%x", m1) != marker || string(m1) == string(m2) {
		t.Fatal("marker is not HMAC(server key, survey, salt)(user)")
	}
	// the points ledger names neither the survey nor the response
	var ref string
	_ = db.Tx(bg, func(tx pgx.Tx) error {
		return tx.QueryRow(bg, `select ref_id from xp_events where user_id = $1 and reason = 'survey'`, stud).Scan(&ref)
	})
	if ref == "" || ref == strconv.FormatInt(id, 10) || strings.Contains(answers, ref) {
		t.Fatalf("points ref %q", ref)
	}
}

func TestDoubleVoteAndAudience(t *testing.T) {
	env(t)
	staff := user(t, "university_staff", "DV-U", "", 0)
	stud := user(t, "student", "DV-U", "it", 2)
	outsider := user(t, "student", "DV-U", "econ", 2)
	foreign := user(t, "student", "OTHER-U", "it", 2)
	id := newSurvey(t, staff, true, map[string]any{"faculties": []string{"it"}, "courses": []int{2}})

	list := decode[struct{ Surveys []surveys.Survey }](t, call(t, "GET", "/api/surveys", nil, stud))
	if len(list.Surveys) != 1 || list.Surveys[0].Done {
		t.Fatalf("eligible list: %+v", list)
	}
	for _, other := range []int64{outsider, foreign} {
		if l := decode[struct{ Surveys []surveys.Survey }](t, call(t, "GET", "/api/surveys", nil, other)); len(l.Surveys) != 0 {
			t.Fatal("survey shown outside its audience")
		}
		if w := answer(t, id, other, 0); w.Code != 404 {
			t.Fatalf("outsider answered: %d", w.Code)
		}
	}
	if w := call(t, "POST", fmt.Sprintf("/api/surveys/%d/responses", id), map[string]any{"answers": map[string]any{"q1": 0}}, stud); w.Code != 400 {
		t.Fatalf("missing required answer accepted: %d", w.Code)
	}
	if w := answer(t, id, stud, 0); w.Code != 201 || !strings.Contains(w.Body.String(), `"xp":15`) {
		t.Fatalf("first: %d %s", w.Code, w.Body)
	}
	if w := answer(t, id, stud, 1); w.Code != 409 {
		t.Fatalf("second vote: %d %s", w.Code, w.Body)
	}
	if count(t, `select count(*) from survey_responses where survey_id = $1`, id) != 1 || count(t, `select count(*) from xp_events where user_id = $1`, stud) != 1 {
		t.Fatal("double vote stored or paid twice")
	}
	if l := decode[struct{ Surveys []surveys.Survey }](t, call(t, "GET", "/api/surveys", nil, stud)); !l.Surveys[0].Done {
		t.Fatal("done flag")
	}
	// published questions are frozen
	if w := call(t, "PUT", fmt.Sprintf("/api/staff/surveys/%d", id), map[string]any{"title": "Changed", "questions": questions}, staff); w.Code != 409 {
		t.Fatalf("frozen: %d", w.Code)
	}
	// closed: no more answers
	call(t, "POST", fmt.Sprintf("/api/staff/surveys/%d/close", id), nil, staff)
	if w := answer(t, user(t, "student", "DV-U", "it", 2), id, 0); w.Code != 404 {
		t.Fatalf("closed survey answered: %d", w.Code)
	}
}

func TestStaffScopeAndRoles(t *testing.T) {
	env(t)
	staff := user(t, "university_staff", "SC-A", "", 0)
	other := user(t, "university_staff", "SC-B", "", 0)
	stud := user(t, "student", "SC-A", "it", 1)
	admin := user(t, "admin", "", "", 0)
	unbound := user(t, "university_staff", "", "", 0)
	exec(t, `delete from university_staff where user_id = $1`, unbound)
	id := newSurvey(t, staff, true, nil)
	for _, path := range []string{"", "/results", "/results.csv"} {
		if w := call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d%s", id, path), nil, other); w.Code != 404 {
			t.Fatalf("other university %s: %d", path, w.Code)
		}
		if w := call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d%s", id, path), nil, admin); w.Code != 200 {
			t.Fatalf("admin %s: %d", path, w.Code)
		}
	}
	if w := call(t, "GET", "/api/staff/surveys", nil, stud); w.Code != 403 {
		t.Fatalf("student staff list: %d", w.Code)
	}
	if w := call(t, "GET", "/api/staff/surveys", nil, unbound); w.Code != 403 {
		t.Fatalf("unbound staff: %d", w.Code)
	}
	if w := call(t, "GET", "/api/surveys", nil, staff); w.Code != 403 {
		t.Fatalf("staff answering: %d", w.Code)
	}
	// admin must name the university; staff cannot pick another one
	if w := call(t, "POST", "/api/staff/surveys", map[string]any{"title": "X survey", "questions": questions}, admin); w.Code != 400 {
		t.Fatalf("admin without university: %d", w.Code)
	}
	w := call(t, "POST", "/api/staff/surveys", map[string]any{"university": "SC-B", "title": "X survey", "questions": questions}, staff)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"university":"SC-A"`) {
		t.Fatalf("staff university override: %d %s", w.Code, w.Body)
	}
}

func TestResultsKAnonymityAndCSV(t *testing.T) {
	env(t)
	staff := user(t, "university_staff", "K-U", "", 0)
	id := newSurvey(t, staff, true, nil)
	var studs []int64
	for i := 0; i < 4; i++ {
		studs = append(studs, user(t, "student", "K-U", "it", 1))
	}
	for _, s := range studs {
		if w := answer(t, id, s, 0); w.Code != 201 {
			t.Fatal(w.Code, w.Body)
		}
	}
	res := decode[surveys.Results](t, call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d/results", id), nil, staff))
	if !res.Hidden || res.Responses != 4 || res.Questions[0].Counts != nil || res.Questions[2].Texts != nil {
		t.Fatalf("4 responses must stay hidden: %+v", res)
	}
	csv := call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d/results.csv", id), nil, staff).Body.String()
	if strings.Contains(csv, "secret text") || !strings.Contains(csv, "hidden (<5)") {
		t.Fatalf("csv under k: %s", csv)
	}
	// 5th answer from another faculty (and 1 more for a 6-response breakdown test)
	if w := answer(t, id, user(t, "student", "K-U", "it", 1), 1); w.Code != 201 {
		t.Fatal(w.Code)
	}
	if w := answer(t, id, user(t, "student", "K-U", "econ", 3), 2); w.Code != 201 {
		t.Fatal(w.Code)
	}
	res = decode[surveys.Results](t, call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d/results", id), nil, staff))
	if res.Hidden || res.Questions[0].Counts[0] != 4 || len(res.Questions[2].Texts) != 6 || *res.Questions[1].Mean != 4 {
		t.Fatalf("results: %+v", res.Questions)
	}
	// breakdown: econ has 1 → it alone would be exposed as total − it, so nothing is broken down
	res = decode[surveys.Results](t, call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d/results?by=faculty", id), nil, staff))
	if !res.BreakdownHidden || len(res.Segments) != 0 {
		t.Fatalf("breakdown leaked a small group: %+v", res.Segments)
	}
	csv = call(t, "GET", fmt.Sprintf("/api/staff/surveys/%d/results.csv", id), nil, staff).Body.String()
	if strings.Contains(csv, "secret text") || !strings.Contains(csv, "q1,") || strings.Contains(csv, "hidden") {
		t.Fatalf("csv: %s", csv)
	}
}

func TestSuppressComplementary(t *testing.T) {
	to := surveys.Suppress(map[string]int{"it": 9, "econ": 6, "agro": 2})
	// agro (2) alone in "other" would be total − it − econ, so the smallest visible (econ) joins it
	if to["agro"] != "other" || to["econ"] != "other" || to["it"] != "it" {
		t.Fatalf("%v", to)
	}
	to = surveys.Suppress(map[string]int{"it": 9, "econ": 6, "agro": 2, "vet": 3})
	if to["it"] != "it" || to["econ"] != "econ" || to["agro"] != "other" || to["vet"] != "other" {
		t.Fatalf("%v", to)
	}
	to = surveys.Suppress(map[string]int{"it": 3, "econ": 1})
	for _, v := range to {
		if v != "other" {
			t.Fatalf("%v", to)
		}
	}
}

func TestBreakdownHidesQuestionWhenAnyGroupIsSmall(t *testing.T) {
	qs, err := surveys.ValidateQuestions([]surveys.Question{{ID: "a", Type: "scale", Text: "Scale question", Max: 5}, {ID: "b", Type: "single", Text: "Optional one", Options: []string{"x", "y"}}})
	if err != nil {
		t.Fatal(err)
	}
	var rs []surveys.Stored
	for i := 0; i < 12; i++ {
		ans := map[string]json.RawMessage{"a": json.RawMessage("3")}
		fac := "it"
		if i >= 6 {
			fac = "econ"
		}
		if i < 6 || i < 8 { // econ has only two answers to "b"
			ans["b"] = json.RawMessage("1")
		}
		rs = append(rs, surveys.Stored{Answers: ans, Faculty: fac})
	}
	res := surveys.BuildResults(&surveys.Survey{Questions: qs}, rs, "faculty")
	if len(res.Segments) != 2 || res.Segments[0].Questions[0].Hidden || !res.Segments[0].Questions[1].Hidden || !res.Segments[1].Questions[1].Hidden {
		t.Fatalf("%+v", res.Segments)
	}
	if res.Questions[1].Hidden || res.Questions[1].Answered != 8 {
		t.Fatalf("overall %+v", res.Questions[1])
	}
}

// RLS: app_user (a student's own session, e.g. the data export) can never read anonymous answers,
// markers or surveys; an identified answer is readable only by its author.
func TestRLS(t *testing.T) {
	env(t)
	staff := user(t, "university_staff", "RLS-U", "", 0)
	a, b := user(t, "student", "RLS-U", "it", 1), user(t, "student", "RLS-U", "it", 1)
	anon := newSurvey(t, staff, true, nil)
	named := newSurvey(t, staff, false, nil)
	for _, s := range []int64{a, b} {
		answer(t, anon, s, 0)
		answer(t, named, s, 0)
	}
	var mine, all int
	err := db.WithUser(bg, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(bg, `select count(*) from survey_responses where survey_id = $1`, named).Scan(&mine); err != nil {
			return err
		}
		return tx.QueryRow(bg, `select count(*) from survey_responses`).Scan(&all)
	})
	if err != nil || mine != 1 || all != 1 {
		t.Fatalf("app_user sees %d/%d responses: %v", mine, all, err)
	}
	for _, q := range []string{`select count(*) from survey_markers`, `select count(*) from surveys`} {
		err := db.WithUser(bg, a, func(tx pgx.Tx) error { var n int; return tx.QueryRow(bg, q).Scan(&n) })
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exp := call(t, "GET", "/api/me/export", nil, a).Body.String()
	if !strings.Contains(exp, `"surveyAnswers"`) || strings.Count(exp, `"survey_id"`) != 1 {
		t.Fatalf("export: %s", exp)
	}
	// account deletion takes the identified answer, the anonymous one stays (it is nobody's)
	if w := call(t, "DELETE", "/api/me", map[string]string{"confirm": "DELETE"}, a); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if count(t, `select count(*) from survey_responses where survey_id = $1`, named) != 1 || count(t, `select count(*) from survey_responses where survey_id = $1`, anon) != 2 {
		t.Fatal("deletion")
	}
}

func TestValidateAnswers(t *testing.T) {
	qs, err := surveys.ValidateQuestions([]surveys.Question{
		{Type: "single", Text: "One?", Required: true, Options: []string{"a", "b"}},
		{Type: "matrix", Text: "Grid", Required: true, Max: 10, Rows: []string{"r1", "r2"}},
		{Type: "text", Text: "Text"},
	})
	if err != nil || qs[0].ID != "q1" || qs[2].ID != "q3" {
		t.Fatal(err, qs)
	}
	ok := surveys.Answers{"q1": json.RawMessage("1"), "q2": json.RawMessage("[10,1]"), "q3": json.RawMessage(`"  hi  "`)}
	out, err := surveys.ValidateAnswers(qs, ok)
	if err != nil || out["q3"] != "hi" {
		t.Fatal(err, out)
	}
	for _, bad := range []surveys.Answers{
		{"q1": json.RawMessage("2"), "q2": json.RawMessage("[1,1]")},
		{"q1": json.RawMessage("0"), "q2": json.RawMessage("[1,0]")},
		{"q1": json.RawMessage("0"), "q2": json.RawMessage("[11,1]")},
		{"q1": json.RawMessage("0"), "q2": json.RawMessage("[1,1]"), "zz": json.RawMessage("1")},
		{"q1": json.RawMessage("0"), "q2": json.RawMessage("[1,1]"), "q3": json.RawMessage(`"` + strings.Repeat("x", 501) + `"`)},
	} {
		if _, err := surveys.ValidateAnswers(qs, bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	if _, err := surveys.ValidateQuestions([]surveys.Question{{Type: "scale", Text: "Scale", Max: 7}}); err == nil {
		t.Fatal("1–7 scale accepted")
	}
}

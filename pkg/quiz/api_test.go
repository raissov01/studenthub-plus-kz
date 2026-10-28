package quiz

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
)

func TestCRUDAndRLS(t *testing.T) {
	a, b := newUser(t), newUser(t)
	id, q := createQuizT(t, a, sampleQuiz("private"))
	if q["isMine"] != true || q["questionCount"].(float64) != 5 || q["visibility"] != "private" {
		t.Fatalf("created: %v", q)
	}
	// owner gets the editor view with keys
	w := do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d", id), a, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"correctIndex":1`) {
		t.Fatalf("owner get: %d %s", w.Code, w.Body)
	}
	ed := decode[struct {
		Quiz      Meta       `json:"quiz"`
		Questions []Question `json:"questions"`
	}](t, w)
	// someone else: not found everywhere
	for _, c := range []struct{ m, p string }{
		{"GET", "/api/quiz/quizzes/%d"}, {"PUT", "/api/quiz/quizzes/%d"}, {"DELETE", "/api/quiz/quizzes/%d"},
		{"GET", "/api/quiz/quizzes/%d/play"}, {"GET", "/api/quiz/quizzes/%d/stats"}, {"POST", "/api/quiz/quizzes/%d/clone"},
		{"POST", "/api/quiz/quizzes/%d/share/reset"},
	} {
		var body any
		if c.m == "PUT" {
			body = sampleQuiz("public")
		}
		if w := do(t, c.m, fmt.Sprintf(c.p, id), b, body); w.Code != 404 {
			t.Errorf("%s %s by other: %d %s", c.m, c.p, w.Code, w.Body)
		}
	}
	if n := count(t, "select count(*) from quizzes where id = $1 and visibility = 'private' and title = 'Биология негіздері'", id); n != 1 {
		t.Fatal("other student changed the quiz")
	}
	// RLS directly: crafted SQL as B sees and changes nothing
	err := db.WithUser(ctxb(), b, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctxb(), "update quizzes set title = 'hacked' where id = $1", id)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("rls update: %v %d", err, tag.RowsAffected())
		}
		tag, err = tx.Exec(ctxb(), "delete from quiz_questions where quiz_id = $1", id)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("rls delete questions: %v %d", err, tag.RowsAffected())
		}
		var n int
		tx.QueryRow(ctxb(), "select count(*) from quizzes where id = $1", id).Scan(&n)
		if n != 0 {
			t.Error("private quiz visible to other student")
		}
		sp, err := tx.Begin(ctxb())
		if err != nil {
			return err
		}
		if _, err := sp.Exec(ctxb(), "insert into quizzes (user_id, title, share_code) values ($1, 'x', 'AAAAAAAAAA')", a); err == nil {
			t.Error("inserted a quiz for someone else")
		}
		sp.Rollback(ctxb())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// owner edits: question ids kept, version checked
	ed.Questions[0].Prompt = "3+3?"
	ed.Questions[0].Options = []string{"5", "6"}
	ed.Questions = ed.Questions[:2]
	w = do(t, "PUT", fmt.Sprintf("/api/quiz/quizzes/%d", id), a, map[string]any{
		"title": "Жаңа атау", "visibility": "public", "version": ed.Quiz.Version, "questions": ed.Questions})
	if w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	if count(t, "select count(*) from quiz_questions where quiz_id = $1", id) != 2 ||
		count(t, "select count(*) from quiz_questions where id = $1 and prompt = '3+3?'", ed.Questions[0].ID) != 1 {
		t.Fatal("questions not replaced in place")
	}
	w = do(t, "PUT", fmt.Sprintf("/api/quiz/quizzes/%d", id), a, map[string]any{"title": "x", "version": ed.Quiz.Version, "questions": ed.Questions})
	if w.Code != 409 {
		t.Fatalf("stale version: %d", w.Code)
	}
	// public now: B reads the quiz but never the keys
	err = db.WithUser(ctxb(), b, func(tx pgx.Tx) error {
		var n, k int
		tx.QueryRow(ctxb(), "select count(*) from quizzes where id = $1", id).Scan(&n)
		tx.QueryRow(ctxb(), "select count(*) from quiz_answer_keys where quiz_id = $1", id).Scan(&k)
		if n != 1 || k != 0 {
			t.Errorf("public read %d, keys %d", n, k)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w = do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/play", id), 0, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "correct") || strings.Contains(w.Body.String(), "answers") {
		t.Fatalf("guest play: %d %s", w.Code, w.Body)
	}
	if w := do(t, "GET", "/api/quiz/quizzes?q=атау", 0, nil); !strings.Contains(w.Body.String(), "Жаңа атау") {
		t.Fatalf("browse: %s", w.Body)
	}
	// validation errors name the question
	bad := sampleQuiz("private")
	bad["questions"].([]map[string]any)[1]["correctBool"] = nil
	w = do(t, "POST", "/api/quiz/quizzes", a, bad)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"question":2`) {
		t.Fatalf("invalid: %d %s", w.Code, w.Body)
	}
	// delete
	if w := do(t, "DELETE", fmt.Sprintf("/api/quiz/quizzes/%d", id), a, nil); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if count(t, "select count(*) from quiz_questions where quiz_id = $1", id) != 0 {
		t.Fatal("questions survived")
	}
}

func TestAttemptCannotBeForgedAndRetryWrong(t *testing.T) {
	owner, player := newUser(t), newUser(t)
	id, _ := createQuizT(t, owner, sampleQuiz("public"))
	pr := decode[playResp](t, do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/play", id), player, nil))
	if len(pr.Questions) != 5 {
		t.Fatal(pr)
	}
	q := pr.Questions
	// everything wrong, but the client claims a perfect score in every way it can
	forged := fmt.Sprintf(`{"score": 100, "percentage": 100, "answers": [
		{"questionId": %d, "answer": 0, "isCorrect": true, "points": 1000, "credit": 1},
		{"questionId": %d, "answer": false, "isCorrect": true},
		{"questionId": %d, "answer": "мұз", "correct": true},
		{"questionId": %d, "answer": [0], "isCorrect": true},
		{"questionId": %d, "answer": 1}]}`, q[0].ID, q[1].ID, q[2].ID, q[3].ID, q[4].ID)
	w := do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), player, forged)
	if w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}
	res := decode[Result](t, w)
	if res.Score != 0 || res.Percentage != 0 || res.CorrectCount != 0 || res.MaxScore != 4 || res.WrongCount != 4 || res.AttemptID == nil {
		t.Fatalf("forged attempt scored: %+v", res)
	}
	if count(t, "select count(*) from quiz_attempts where id = $1 and score = 0 and percentage = 0", *res.AttemptID) != 1 {
		t.Fatal("stored score differs")
	}
	// retry wrong: only the 4 graded wrong questions
	w = do(t, "GET", fmt.Sprintf("/api/quiz/attempts/%d/retry", *res.AttemptID), player, nil)
	rr := decode[playResp](t, w)
	if w.Code != 200 || len(rr.Questions) != 4 {
		t.Fatalf("retry view: %d %s", w.Code, w.Body)
	}
	right := fmt.Sprintf(`{"retryOf": %d, "answers": [
		{"questionId": %d, "answer": 1}, {"questionId": %d, "answer": true},
		{"questionId": %d, "answer": " Water "}, {"questionId": %d, "answer": [2, 0]}]}`,
		*res.AttemptID, q[0].ID, q[1].ID, q[2].ID, q[3].ID)
	w = do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), player, right)
	res2 := decode[Result](t, w)
	if w.Code != 201 || res2.Mode != "retry" || res2.Percentage != 100 || res2.Total != 4 {
		t.Fatalf("retry: %d %s", w.Code, w.Body)
	}
	// someone else's attempt is invisible
	if w := do(t, "GET", fmt.Sprintf("/api/quiz/attempts/%d", *res.AttemptID), owner, nil); w.Code != 404 {
		t.Fatalf("foreign attempt: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), owner, fmt.Sprintf(`{"retryOf": %d, "answers": []}`, *res.AttemptID)); w.Code != 400 {
		t.Fatalf("retry of foreign attempt: %d", w.Code)
	}
	// history and result detail
	h := decode[struct {
		Attempts []map[string]any `json:"attempts"`
	}](t, do(t, "GET", "/api/quiz/attempts", player, nil))
	if len(h.Attempts) != 2 {
		t.Fatalf("history %v", h)
	}
	d := decode[Result](t, do(t, "GET", fmt.Sprintf("/api/quiz/attempts/%d", *res.AttemptID), player, nil))
	if len(d.Answers) != 5 || d.Answers[0].Correct == nil || d.Answers[0].Explanation != "Арифметика" || d.WrongCount != 4 {
		t.Fatalf("detail: %+v", d)
	}
	// guest: graded, not stored
	before := count(t, "select count(*) from quiz_attempts")
	w = do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), 0, fmt.Sprintf(`{"answers":[{"questionId":%d,"answer":1}]}`, q[0].ID))
	g := decode[Result](t, w)
	if w.Code != 201 || g.AttemptID != nil || g.CorrectCount != 1 || count(t, "select count(*) from quiz_attempts") != before {
		t.Fatalf("guest: %d %s", w.Code, w.Body)
	}
	// unknown question ids are refused
	if w := do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), player, `{"questionIds":[999999],"answers":[]}`); w.Code != 400 {
		t.Fatalf("foreign question: %d", w.Code)
	}
	// per-question stats for the owner
	st := decode[struct {
		Attempts  int `json:"attempts"`
		Questions []struct {
			Answered, Correct int
			Distribution      map[string]int
		} `json:"questions"`
	}](t, do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/stats", id), owner, nil))
	if st.Attempts != 1 || st.Questions[0].Answered != 2 || st.Questions[0].Correct != 1 || st.Questions[0].Distribution["0"] != 1 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestSharingAndClone(t *testing.T) {
	owner, other := newUser(t), newUser(t)
	id, q := createQuizT(t, owner, sampleQuiz("unlisted"))
	code := q["shareCode"].(string)
	if w := do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/play", id), other, nil); w.Code != 404 {
		t.Fatalf("unlisted by id: %d", w.Code)
	}
	if w := do(t, "GET", "/api/quiz/s/"+code, 0, nil); w.Code != 200 {
		t.Fatalf("unlisted by code: %d", w.Code)
	}
	w := do(t, "GET", "/api/quiz/s/"+code+"/meta", 0, nil)
	meta := decode[map[string]any](t, w)
	if w.Code != 200 || meta["title"] != "Биология негіздері" || meta["questionCount"].(float64) != 5 ||
		!strings.HasSuffix(meta["url"].(string), "/quiz/s/"+code) || strings.Contains(w.Body.String(), "Студентова") {
		t.Fatalf("meta: %d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatal("meta cache")
	}
	// attempts on unlisted need the code
	pr := decode[playResp](t, do(t, "GET", "/api/quiz/s/"+code, other, nil))
	body := fmt.Sprintf(`{"answers":[{"questionId":%d,"answer":1}]%%s}`, pr.Questions[0].ID)
	if w := do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), other, fmt.Sprintf(body, "")); w.Code != 404 {
		t.Fatalf("attempt without code: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), other, fmt.Sprintf(body, `,"share":"`+code+`"`)); w.Code != 201 {
		t.Fatalf("attempt with code: %d %s", w.Code, w.Body)
	}
	// clone via the link: a private copy with its own answer keys
	w = do(t, "POST", "/api/quiz/s/"+code+"/clone", other, nil)
	if w.Code != 201 {
		t.Fatalf("clone: %d %s", w.Code, w.Body)
	}
	c := decode[struct {
		ID   int64 `json:"id"`
		Quiz Meta  `json:"quiz"`
	}](t, w)
	if c.Quiz.Visibility != "private" || !c.Quiz.IsMine || c.Quiz.Source != "clone" || c.Quiz.QuestionCount != 5 {
		t.Fatalf("clone meta: %+v", c.Quiz)
	}
	if count(t, "select count(*) from quiz_answer_keys where quiz_id = $1", c.ID) != 5 {
		t.Fatal("clone keys")
	}
	// reset the link: the old one dies
	if w := do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/share/reset", id), owner, nil); w.Code != 200 {
		t.Fatalf("reset: %d", w.Code)
	}
	if w := do(t, "GET", "/api/quiz/s/"+code, 0, nil); w.Code != 404 {
		t.Fatalf("old link alive: %d", w.Code)
	}
	// private: no link access, no meta
	pid, pq := createQuizT(t, owner, sampleQuiz("private"))
	_ = pid
	if pq["shareCode"] == "" {
		t.Fatal("owner sees no code")
	}
	pcode := pq["shareCode"].(string)
	for _, p := range []string{"/api/quiz/s/" + pcode, "/api/quiz/s/" + pcode + "/meta"} {
		if w := do(t, "GET", p, other, nil); w.Code != 404 {
			t.Fatalf("private %s: %d", p, w.Code)
		}
	}
	// admin hide takes a public quiz out of browsing
	pubID, _ := createQuizT(t, owner, sampleQuiz("public"))
	admin := newUser(t)
	exec(t, "update users set role = 'admin' where id = $1", admin)
	if w := do(t, "POST", fmt.Sprintf("/api/admin/quiz/quizzes/%d/hide", pubID), other, nil); w.Code != 403 {
		t.Fatalf("non-admin hide: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/quiz/quizzes/%d/hide", pubID), admin, nil); w.Code != 200 {
		t.Fatalf("hide: %d", w.Code)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/play", pubID), other, nil); w.Code != 404 {
		t.Fatalf("hidden playable: %d", w.Code)
	}
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error { _, err := tx.Exec(ctxb(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureOff(t *testing.T) {
	t.Setenv("FEATURE_QUIZ", "false")
	for _, p := range []string{"/api/quiz/quizzes", "/api/quiz/s/abc", "/api/quiz/live/ws", "/api/quiz/import/template.xlsx"} {
		if w := do(t, "GET", p, 0, nil); w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := do(t, "POST", "/api/quiz/live/join", 0, `{"code":"123456","nickname":"aa"}`); w.Code != 404 {
		t.Errorf("join: %d", w.Code)
	}
}

func TestAccountDeletionRemovesQuizData(t *testing.T) {
	uid, other := newUser(t), newUser(t)
	name := uploadJPEG(t, uid)
	body := sampleQuiz("public")
	body["questions"].([]map[string]any)[0]["image"] = name
	id, _ := createQuizT(t, uid, body)
	// someone else's attempt on the deleted user's quiz goes with the quiz
	pr := decode[playResp](t, do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/play", id), other, nil))
	do(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", id), other, fmt.Sprintf(`{"answers":[{"questionId":%d,"answer":1}]}`, pr.Questions[0].ID))
	if err := me.Delete(ctxb(), uid); err != nil {
		t.Fatal(err)
	}
	if count(t, "select count(*) from quizzes where user_id = $1", uid) != 0 || count(t, "select count(*) from quiz_media where name = $1", name) != 0 ||
		count(t, "select count(*) from quiz_attempts where quiz_id = $1", id) != 0 {
		t.Fatal("quiz rows survived deletion")
	}
	if fileExists(name) {
		t.Fatal("media file survived deletion")
	}
}

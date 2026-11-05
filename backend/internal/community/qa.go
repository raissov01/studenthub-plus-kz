package community

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/points"
)

// Course Q&A («Пән бойынша сұрақ-жауап», FEATURE_COURSEQA).
//
// Subjects live on the device (frontend lib/subjects.ts), so a board is the normalised subject
// name (NormKey) within a university, optionally narrowed to the asker's faculty. Reading is
// open to everyone; writing needs an account. «Anonymous» hides the author from classmates
// only: the server keeps user_id and admins see it (and the shared queue shows it). Admins can
// lock a thread (no new answers), e.g. while an exam is on.

var tagRe = regexp.MustCompile(`^[\p{L}\p{N}+#-]{2,24}$`)

func qaRoutes(mux httpx.Router) {
	h := func(p string, fn http.HandlerFunc) { handle(mux, FeatureCourseQA, p, fn) }
	h("GET /api/qa/questions", qaList)
	h("GET /api/qa/questions/{id}", qaGet)
	h("POST /api/qa/questions", write(qaAsk))
	h("DELETE /api/qa/questions/{id}", write(qaDeleteQuestion))
	h("POST /api/qa/questions/{id}/answers", write(qaAnswer))
	h("DELETE /api/qa/answers/{id}", write(qaDeleteAnswer))
	h("POST /api/qa/questions/{id}/accept", write(qaAccept))
	h("POST /api/qa/vote", write(qaVote))
	h("POST /api/admin/qa/moderate", admin(qaModerate))
	h("POST /api/admin/qa/questions/{id}/lock", admin(qaLock))
}

type qaAuthor struct {
	Name string `json:"name"`
}

type qaQuestion struct {
	ID           int64     `json:"id"`
	Subject      string    `json:"subject"`
	SubjectKey   string    `json:"subjectKey"`
	FacultyOnly  bool      `json:"facultyOnly"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	Tags         []string  `json:"tags"`
	Anonymous    bool      `json:"anonymous"`
	Author       *qaAuthor `json:"author"` // null: anonymous, or a signed-out viewer
	Status       string    `json:"status"`
	Locked       bool      `json:"locked"`
	LockedReason *string   `json:"lockedReason"`
	Accepted     *int64    `json:"acceptedAnswerId"`
	Answers      int       `json:"answers"`
	Score        int       `json:"score"`
	Voted        bool      `json:"voted"`
	Mine         bool      `json:"mine"`
	CreatedAt    time.Time `json:"createdAt"`
	// admins only: who really wrote it (anonymous included)
	AuthorID *int64 `json:"authorId,omitempty"`
}

type qaAnswerOut struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Anonymous bool      `json:"anonymous"`
	Author    *qaAuthor `json:"author"`
	Score     int       `json:"score"`
	Voted     bool      `json:"voted"`
	Mine      bool      `json:"mine"`
	Accepted  bool      `json:"accepted"`
	CreatedAt time.Time `json:"createdAt"`
	AuthorID  *int64    `json:"authorId,omitempty"`
}

// ShortName turns «Аружан Тестова» into «Аружан Т.»: classmates recognise each other, strangers
// do not get a full name.
func ShortName(name string) string {
	f := strings.Fields(name)
	switch len(f) {
	case 0:
		return ""
	case 1:
		return f[0]
	}
	r, _ := utf8.DecodeRuneInString(f[1])
	return f[0] + " " + string(r) + "."
}

func author(viewer *auth.User, anonymous bool, name string, id int64) (*qaAuthor, *int64) {
	var aid *int64
	if viewer != nil && viewer.Role == "admin" {
		v := id
		aid = &v
	}
	if anonymous || viewer == nil {
		return nil, aid
	}
	return &qaAuthor{Name: ShortName(name)}, aid
}

const qaCols = `q.id, q.subject_name, q.subject_key, q.faculty_key <> '', q.title, q.body, q.tags, q.anonymous, q.status, q.locked,
	q.locked_reason, q.accepted_answer, q.answers, q.score, q.created_at, q.user_id, coalesce(u.name, '')`

func scanQuestion(row pgx.Row, v *auth.User, voted bool) (qaQuestion, error) {
	var q qaQuestion
	var uid int64
	var name string
	err := row.Scan(&q.ID, &q.Subject, &q.SubjectKey, &q.FacultyOnly, &q.Title, &q.Body, &q.Tags, &q.Anonymous, &q.Status, &q.Locked,
		&q.LockedReason, &q.Accepted, &q.Answers, &q.Score, &q.CreatedAt, &uid, &name)
	if err != nil {
		return q, err
	}
	q.Author, q.AuthorID = author(v, q.Anonymous, name, uid)
	q.Mine = v != nil && v.ID == uid
	q.Voted = voted
	if q.Tags == nil {
		q.Tags = []string{}
	}
	return q, nil
}

// qaList: GET /api/qa/questions?subject=&faculty=mine|all&university=&tag=&q=&sort=new|open&before=
// Signed-in default: the viewer's university, whole-university questions plus their faculty's.
func qaList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := auth.Current(r)
	qs := r.URL.Query()
	subject := strings.TrimSpace(qs.Get("subject"))
	key := ""
	if subject != "" {
		if key = NormKey(subject); utf8.RuneCountInString(key) < 2 {
			bad(w, r)
			return
		}
	}
	uni := strings.TrimSpace(qs.Get("university"))
	fk := "" // '' = every faculty
	if v != nil {
		if uni == "" {
			uni = deref(v.University)
		}
		if qs.Get("faculty") != "all" {
			fk = NormKey(deref(v.Faculty))
		}
	}
	if uni == "all" {
		uni = ""
	}
	tag := strings.ToLower(strings.TrimSpace(qs.Get("tag")))
	text := strings.TrimSpace(qs.Get("q"))
	sort := qs.Get("sort")
	before, _ := strconv.ParseInt(qs.Get("before"), 10, 64)
	if (tag != "" && !tagRe.MatchString(tag)) || utf8.RuneCountInString(text) > 64 || utf8.RuneCountInString(uni) > 120 ||
		(sort != "" && sort != "new" && sort != "open") {
		bad(w, r)
		return
	}
	like := ""
	if text != "" {
		like = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
	}
	var viewer int64
	if v != nil {
		viewer = v.ID
	}
	out := []qaQuestion{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+qaCols+`, exists (select 1 from qa_votes x where x.user_id = $8 and x.target_kind = 'question' and x.target_id = q.id)
			from qa_questions q join users u on u.id = q.user_id
			where q.status = 'active' and u.deleted_at is null
				and ($1 = '' or q.university = $1) and ($2 = '' or q.subject_key = $2)
				and ($3 = '' or q.faculty_key in ('', $3)) and ($4 = '' or $4 = any(q.tags))
				and ($5 = '' or q.title ilike $5 or q.body ilike $5) and ($6 = 0 or q.id < $6)
				and ($7 <> 'open' or q.accepted_answer is null)
			order by q.id desc limit 30`, uni, key, fk, tag, like, before, sort, viewer)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var voted bool
			q, err := scanQuestion(scanExtra{rows, &voted}, v, false)
			if err != nil {
				return err
			}
			q.Voted = voted
			if n := []rune(q.Body); len(n) > 200 {
				q.Body = string(n[:200]) + "…"
			}
			out = append(out, q)
		}
		return rows.Err()
	})
	if respond(w, r, "qa list", err) {
		return
	}
	resp := map[string]any{"questions": out, "subjectKey": key, "university": uni, "facultyScoped": fk != ""}
	if len(out) == 30 {
		resp["nextBefore"] = out[len(out)-1].ID
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// scanExtra appends extra destinations to a row scan.
type scanExtra struct {
	row   pgx.Row
	extra *bool
}

func (s scanExtra) Scan(dest ...any) error { return s.row.Scan(append(dest, s.extra)...) }

// qaGet: GET /api/qa/questions/{id} — the question with its answers (accepted first, then by
// votes). Hidden/deleted questions only for the author and admins.
func qaGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	v := auth.Current(r)
	var viewer int64
	isAdmin := false
	if v != nil {
		viewer, isAdmin = v.ID, v.Role == "admin"
	}
	var q qaQuestion
	answers := []qaAnswerOut{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var voted bool
		var err error
		q, err = scanQuestion(scanExtra{tx.QueryRow(ctx, `select `+qaCols+`,
				exists (select 1 from qa_votes x where x.user_id = $2 and x.target_kind = 'question' and x.target_id = q.id)
			from qa_questions q join users u on u.id = q.user_id where q.id = $1`, id, viewer), &voted}, v, false)
		if err != nil {
			return err
		}
		q.Voted = voted
		if q.Status != "active" && !q.Mine && !isAdmin {
			return pgx.ErrNoRows
		}
		if q.Status == "deleted" && !isAdmin {
			return pgx.ErrNoRows
		}
		rows, err := tx.Query(ctx, `select a.id, a.body, a.anonymous, a.score, a.created_at, a.user_id, coalesce(u.name, ''),
				exists (select 1 from qa_votes x where x.user_id = $2 and x.target_kind = 'answer' and x.target_id = a.id)
			from qa_answers a join users u on u.id = a.user_id
			where a.question_id = $1 and (a.status = 'active' or (a.status = 'hidden' and ($3 or a.user_id = $2)))
			order by a.id = coalesce($4, 0) desc, a.score desc, a.id`, id, viewer, isAdmin, q.Accepted)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a qaAnswerOut
			var uid int64
			var name string
			if err := rows.Scan(&a.ID, &a.Body, &a.Anonymous, &a.Score, &a.CreatedAt, &uid, &name, &a.Voted); err != nil {
				return err
			}
			a.Author, a.AuthorID = author(v, a.Anonymous, name, uid)
			a.Mine = viewer != 0 && viewer == uid
			a.Accepted = q.Accepted != nil && *q.Accepted == a.ID
			answers = append(answers, a)
		}
		return rows.Err()
	})
	if respond(w, r, "qa get", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"question": q, "answers": answers})
}

func cleanTags(in []string) ([]string, bool) {
	if len(in) > 5 {
		return nil, false
	}
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if !tagRe.MatchString(t) {
			return nil, false
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, true
}

// qaAsk: POST /api/qa/questions {subject, title, body, tags, anonymous, scope: university|faculty,
// honesty: true}.
func qaAsk(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, true) {
		return
	}
	var b struct {
		Subject   string   `json:"subject"`
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Tags      []string `json:"tags"`
		Anonymous bool     `json:"anonymous"`
		Scope     string   `json:"scope"`
		Honesty   bool     `json:"honesty"`
	}
	if err := httpx.Decode(r, &b, 16<<10); err != nil || (b.Scope != "" && b.Scope != "university" && b.Scope != "faculty") {
		bad(w, r)
		return
	}
	if !b.Honesty {
		fail(w, r, "honesty", http.StatusBadRequest)
		return
	}
	subject, ok1 := oneLine(b.Subject, 2, 80)
	title, ok2 := oneLine(b.Title, 8, 150)
	body, ok3 := cleanText(b.Body, 0, 2000)
	key := NormKey(subject)
	if !ok1 || !ok2 || !ok3 || utf8.RuneCountInString(key) < 2 {
		fail(w, r, "bad_text", http.StatusBadRequest)
		return
	}
	tags, ok := cleanTags(b.Tags)
	if !ok {
		fail(w, r, "bad_tags", http.StatusBadRequest)
		return
	}
	if HasPhoneOrHandle(title + "\n" + body) {
		fail(w, r, "has_contact", http.StatusBadRequest)
		return
	}
	fk := ""
	if b.Scope == "faculty" {
		if fk = NormKey(deref(u.Faculty)); fk == "" {
			fail(w, r, "bad_scope", http.StatusBadRequest)
			return
		}
	}
	ctx := r.Context()
	var q qaQuestion
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from users where id = $1 for update`, u.ID); err != nil {
			return err
		}
		n, err := countToday(ctx, tx, `select count(*) from qa_questions where user_id = $1 and created_at >= $2`, u.ID)
		if err != nil {
			return err
		}
		if n >= limit("QA_DAILY_QUESTIONS", 5) {
			return refuse("daily_limit", http.StatusTooManyRequests)
		}
		var id int64
		if err := tx.QueryRow(ctx, `insert into qa_questions (user_id, university, faculty_key, subject_key, subject_name, title, body, tags, anonymous)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9) returning id`,
			u.ID, strings.TrimSpace(deref(u.University)), fk, key, subject, title, body, tags, b.Anonymous).Scan(&id); err != nil {
			return err
		}
		q, err = scanQuestion(scanExtra{tx.QueryRow(ctx, `select `+qaCols+`, false from qa_questions q join users u on u.id = q.user_id where q.id = $1`, id), new(bool)}, u, false)
		return err
	})
	if respond(w, r, "qa ask", err) {
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"question": q})
}

// qaAnswer: POST /api/qa/questions/{id}/answers {body, anonymous}. The asker is told.
func qaAnswer(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, false) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var b struct {
		Body      string `json:"body"`
		Anonymous bool   `json:"anonymous"`
	}
	if err := httpx.Decode(r, &b, 16<<10); err != nil {
		bad(w, r)
		return
	}
	body, ok := cleanText(b.Body, 2, 2000)
	if !ok {
		fail(w, r, "bad_text", http.StatusBadRequest)
		return
	}
	if HasPhoneOrHandle(body) {
		fail(w, r, "has_contact", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var asker, aid int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from users where id = $1 for update`, u.ID); err != nil {
			return err
		}
		var locked bool
		if err := tx.QueryRow(ctx, `select user_id, locked from qa_questions where id = $1 and status = 'active' for update`, id).Scan(&asker, &locked); err != nil {
			return err
		}
		if locked {
			return refuse("locked", http.StatusConflict)
		}
		n, err := countToday(ctx, tx, `select count(*) from qa_answers where user_id = $1 and created_at >= $2`, u.ID)
		if err != nil {
			return err
		}
		if n >= limit("QA_DAILY_ANSWERS", 30) {
			return refuse("daily_limit", http.StatusTooManyRequests)
		}
		if err := tx.QueryRow(ctx, `insert into qa_answers (question_id, user_id, body, anonymous) values ($1, $2, $3, $4) returning id`,
			id, u.ID, body, b.Anonymous).Scan(&aid); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update qa_questions set answers = answers + 1, updated_at = now() where id = $1`, id)
		return err
	})
	if respond(w, r, "qa answer", err) {
		return
	}
	if asker != u.ID {
		tell(ctx, asker, fmt.Sprintf("qa:answer:%d", aid), fmt.Sprintf("/qa?id=%d", id), func(lang string) string {
			return tr(lang, "💬 Сұрағыңызға жаңа жауап келді.", "💬 На ваш вопрос пришёл новый ответ.")
		})
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"answer": map[string]any{"id": aid}})
}

// qaAccept: POST /api/qa/questions/{id}/accept {answerId} (null = un-accept) — the asker only.
// The answer's author earns a small capped award (never for answering oneself); switching to
// another answer takes the earlier award back.
func qaAccept(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var b struct {
		AnswerID *int64 `json:"answerId"`
	}
	if err := httpx.Decode(r, &b, 1<<10); err != nil {
		bad(w, r)
		return
	}
	ctx := r.Context()
	var award *points.Award
	var winner int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var prev *int64
		if err := tx.QueryRow(ctx, `select accepted_answer from qa_questions where id = $1 and user_id = $2 and status = 'active' for update`,
			id, u.ID).Scan(&prev); err != nil {
			return err
		}
		if b.AnswerID != nil {
			if err := tx.QueryRow(ctx, `select user_id from qa_answers where id = $1 and question_id = $2 and status = 'active'`,
				*b.AnswerID, id).Scan(&winner); err != nil {
				return err
			}
		}
		if prev != nil && (b.AnswerID == nil || *prev != *b.AnswerID) {
			var prevAuthor int64
			err := tx.QueryRow(ctx, `select user_id from qa_answers where id = $1`, *prev).Scan(&prevAuthor)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err == nil && prevAuthor != u.ID {
				if err := points.Reverse(ctx, tx, prevAuthor, ReasonQAAccepted, "qa_answer", strconv.FormatInt(*prev, 10)); err != nil {
					internalLog("qa reverse", err)
				}
			}
		}
		if _, err := tx.Exec(ctx, `update qa_questions set accepted_answer = $2, updated_at = now() where id = $1`, id, b.AnswerID); err != nil {
			return err
		}
		if b.AnswerID != nil && winner != u.ID {
			award = grant(ctx, tx, points.Earn{UserID: winner, Reason: ReasonQAAccepted, RefType: "qa_answer", RefID: strconv.FormatInt(*b.AnswerID, 10)})
		}
		return nil
	})
	if respond(w, r, "qa accept", err) {
		return
	}
	if b.AnswerID != nil && winner != u.ID {
		tell(ctx, winner, fmt.Sprintf("qa:accepted:%d", *b.AnswerID), fmt.Sprintf("/qa?id=%d", id), func(lang string) string {
			return tr(lang, "✅ Сіздің жауабыңыз «шешілді» деп белгіленді. Рақмет!", "✅ Ваш ответ отмечен как решение. Спасибо!")
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "acceptedAnswerId": b.AnswerID, "award": award})
}

// qaVote: POST /api/qa/vote {kind: question|answer, id, up} — an upvote toggle; not on own posts.
func qaVote(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, false) {
		return
	}
	var b struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
		Up   *bool  `json:"up"`
	}
	if err := httpx.Decode(r, &b, 1<<10); err != nil || (b.Kind != "question" && b.Kind != "answer") || b.ID <= 0 || b.Up == nil {
		bad(w, r)
		return
	}
	table := "qa_questions"
	if b.Kind == "answer" {
		table = "qa_answers"
	}
	ctx := r.Context()
	var score int
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var owner int64
		if err := tx.QueryRow(ctx, `select user_id from `+table+` where id = $1 and status = 'active' for update`, b.ID).Scan(&owner); err != nil {
			return err
		}
		if owner == u.ID {
			return refuse("own_item", http.StatusBadRequest)
		}
		var err error
		if *b.Up {
			_, err = tx.Exec(ctx, `insert into qa_votes (user_id, target_kind, target_id) values ($1, $2, $3) on conflict do nothing`, u.ID, b.Kind, b.ID)
		} else {
			_, err = tx.Exec(ctx, `delete from qa_votes where user_id = $1 and target_kind = $2 and target_id = $3`, u.ID, b.Kind, b.ID)
		}
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `update `+table+` set score = (select count(*) from qa_votes where target_kind = $2 and target_id = $1)
			where id = $1 returning score`, b.ID, b.Kind).Scan(&score)
	})
	if respond(w, r, "qa vote", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"score": score, "voted": *b.Up})
}

func qaDeleteQuestion(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update qa_questions set status = 'deleted', updated_at = now() where id = $1 and user_id = $2 and status <> 'deleted'`, id, u.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return err
	})
	if respond(w, r, "qa delete", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func qaDeleteAnswer(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var qid int64
		err := tx.QueryRow(r.Context(), `update qa_answers set status = 'deleted' where id = $1 and user_id = $2 and status <> 'deleted' returning question_id`, id, u.ID).Scan(&qid)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `update qa_questions set answers = greatest(answers - 1, 0),
			accepted_answer = case when accepted_answer = $2 then null else accepted_answer end where id = $1`, qid, id)
		return err
	})
	if respond(w, r, "qa delete answer", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// qaModerate: POST /api/admin/qa/moderate {kind: question|answer, id, hidden, reason}.
var qaModerate = adminModerate(TargetQuestion, TargetAnswer)

// qaLock: POST /api/admin/qa/questions/{id}/lock {locked, reason} — e.g. during an exam.
func qaLock(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var b struct {
		Locked *bool  `json:"locked"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &b, 2<<10); err != nil || b.Locked == nil {
		bad(w, r)
		return
	}
	reason, ok := oneLine(b.Reason, 3, 300)
	if *b.Locked && !ok {
		fail(w, r, "reason_short", http.StatusBadRequest)
		return
	}
	var stored *string
	if *b.Locked {
		stored = &reason
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update qa_questions set locked = $2, locked_reason = $3, updated_at = now() where id = $1 and status <> 'deleted'`,
			id, *b.Locked, stored)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		action := "qa_lock"
		if !*b.Locked {
			action = "qa_unlock"
		}
		_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, reason, meta) values ($1, $2, nullif($3, ''), jsonb_build_object('question', $4::bigint))`,
			u.ID, action, reason, id)
		return err
	})
	if respond(w, r, "qa lock", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "locked": *b.Locked})
}

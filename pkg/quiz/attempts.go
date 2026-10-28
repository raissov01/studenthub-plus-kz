package quiz

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// AnswerIn is one submitted answer. Only the selection is read: any "isCorrect", "score" or
// "points" the client adds is ignored, the server grades against the stored key.
type AnswerIn struct {
	QuestionID int64           `json:"questionId"`
	Answer     json.RawMessage `json:"answer"`
	TimeSpent  int             `json:"timeSpent"` // seconds, informational
}

type submitIn struct {
	Answers     []AnswerIn `json:"answers"`
	QuestionIDs []int64    `json:"questionIds"` // optional subset (retry, practice a part)
	RetryOf     int64      `json:"retryOf"`     // attempt id whose wrong answers are retried
	Share       string     `json:"share"`       // share code for unlisted quizzes
}

// AnswerOut is one graded answer in a result.
type AnswerOut struct {
	Position    int             `json:"position"`
	QuestionID  *int64          `json:"questionId"`
	Type        string          `json:"type"`
	Prompt      string          `json:"prompt"`
	View        json.RawMessage `json:"view"`
	ImageURL    string          `json:"imageUrl,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
	SourceQuote string          `json:"sourceQuote,omitempty"`
	Given       json.RawMessage `json:"given"`
	Correct     json.RawMessage `json:"correct"`
	Credit      float64         `json:"credit"`
	IsCorrect   bool            `json:"isCorrect"`
	Graded      bool            `json:"graded"`
	TimeSpent   int             `json:"timeSpent"`
}

// Result is a graded attempt.
type Result struct {
	AttemptID    *int64      `json:"attemptId"` // null for guests (not stored)
	QuizID       int64       `json:"quizId"`
	QuizTitle    string      `json:"quizTitle"`
	Mode         string      `json:"mode"`
	RetryOf      *int64      `json:"retryOf,omitempty"`
	Score        float64     `json:"score"`
	MaxScore     int         `json:"maxScore"`
	Percentage   int         `json:"percentage"`
	CorrectCount int         `json:"correctCount"`
	Total        int         `json:"total"`
	WrongCount   int         `json:"wrongCount"`
	TimeSpent    int         `json:"timeSpent"`
	CreatedAt    time.Time   `json:"createdAt"`
	Answers      []AnswerOut `json:"answers"`
}

// gradeAll scores the chosen questions. Pure function, the heart of anti-forgery: the result
// depends only on the stored keys and the submitted selections.
func gradeAll(qs []Question, views map[int64]json.RawMessage, answers []AnswerIn) (*Result, error) {
	given := map[int64]AnswerIn{}
	for _, a := range answers {
		if _, dup := given[a.QuestionID]; !dup {
			given[a.QuestionID] = a
		}
	}
	res := &Result{Answers: []AnswerOut{}}
	for i, q := range qs {
		a := given[q.ID]
		credit, correct, stored := Grade(q, a.Answer)
		key, _ := json.Marshal(RevealKey(q))
		if !Graded(q) {
			key = nil
		}
		qid := q.ID
		ts := min(max(a.TimeSpent, 0), 3600)
		out := AnswerOut{Position: i + 1, QuestionID: &qid, Type: q.Type, Prompt: q.Prompt, View: views[q.ID],
			ImageURL: mediaURL(q.Image), Explanation: q.Explanation, SourceQuote: q.SourceQuote, Given: stored,
			Correct: key, Credit: credit, IsCorrect: correct, Graded: Graded(q), TimeSpent: ts}
		res.Answers = append(res.Answers, out)
		res.TimeSpent += ts
		if Graded(q) {
			res.MaxScore++
			res.Score += credit
			if correct {
				res.CorrectCount++
			} else {
				res.WrongCount++
			}
		}
	}
	res.Total = len(qs)
	res.Score = math.Round(res.Score*100) / 100
	if res.MaxScore > 0 {
		res.Percentage = int(math.Floor(res.Score / float64(res.MaxScore) * 100))
	}
	return res, nil
}

func loadViews(ctx context.Context, tx pgx.Tx, quizID int64) (map[int64]json.RawMessage, error) {
	rows, err := tx.Query(ctx, `select id, data from quiz_questions where quiz_id = $1`, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]json.RawMessage{}
	for rows.Next() {
		var id int64
		var d []byte
		if err := rows.Scan(&id, &d); err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}

// submit: POST /api/quiz/quizzes/{id}/attempts. Guests get a graded result that is not stored.
func submit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var in submitIn
	if err := httpx.Decode(r, &in, 512<<10); err != nil || len(in.Answers) > MaxQuestions || len(in.QuestionIDs) > MaxQuestions {
		fail(w, r, "bad_answers", http.StatusBadRequest)
		return
	}
	u := auth.Current(r)
	var uid int64
	if u != nil {
		uid = u.ID
	}
	ctx := r.Context()
	m, err := loadMeta(ctx, id, uid)
	if err != nil || !canPlay(m, uid, normCode(in.Share)) {
		if err != nil && !isNoRows(err) {
			internal(w, r, "submit meta", err)
			return
		}
		notFound(w, r)
		return
	}
	if in.RetryOf != 0 && u == nil {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	var res *Result
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		qs, err := loadFull(ctx, tx, id)
		if err != nil {
			return err
		}
		ids := in.QuestionIDs
		if in.RetryOf != 0 {
			var owner, quiz int64
			if err := tx.QueryRow(ctx, `select user_id, quiz_id from quiz_attempts where id = $1`, in.RetryOf).Scan(&owner, &quiz); err != nil || owner != uid || quiz != id {
				return errBadAnswers
			}
			if len(ids) == 0 {
				rows, err := tx.Query(ctx, `select question_id from quiz_attempt_answers where attempt_id = $1
					and graded and not is_correct and question_id is not null`, in.RetryOf)
				if err != nil {
					return err
				}
				if ids, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
					return err
				}
				if len(ids) == 0 {
					return errBadAnswers
				}
			}
		}
		if len(ids) > 0 {
			var sub []Question
			for _, q := range qs {
				if slices.Contains(ids, q.ID) {
					sub = append(sub, q)
				}
			}
			for _, want := range ids {
				if !slices.ContainsFunc(qs, func(q Question) bool { return q.ID == want }) {
					return errBadAnswers
				}
			}
			qs = sub
		}
		if len(qs) == 0 {
			return errBadAnswers
		}
		views, err := loadViews(ctx, tx, id)
		if err != nil {
			return err
		}
		if res, err = gradeAll(qs, views, in.Answers); err != nil {
			return err
		}
		res.QuizID, res.QuizTitle, res.Mode, res.CreatedAt = id, m.Title, "practice", time.Now()
		if in.RetryOf != 0 {
			res.Mode, res.RetryOf = "retry", &in.RetryOf
		}
		if _, err := tx.Exec(ctx, `update quizzes set plays = plays + 1 where id = $1`, id); err != nil {
			return err
		}
		if u == nil {
			return nil
		}
		var aid int64
		var retry *int64
		if in.RetryOf != 0 {
			retry = &in.RetryOf
		}
		if err := tx.QueryRow(ctx, `insert into quiz_attempts (quiz_id, user_id, mode, retry_of, score, max_score,
			percentage, correct_count, total, time_spent) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning id, created_at`,
			id, uid, res.Mode, retry, res.Score, res.MaxScore, res.Percentage, res.CorrectCount, res.Total, res.TimeSpent).Scan(&aid, &res.CreatedAt); err != nil {
			return err
		}
		res.AttemptID = &aid
		for i, a := range res.Answers {
			q := qs[i]
			if _, err := tx.Exec(ctx, `insert into quiz_attempt_answers (attempt_id, position, question_id, type, prompt,
				data, explanation, source_quote, image, given, correct_key, credit, is_correct, graded, time_spent)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
				aid, a.Position, q.ID, q.Type, q.Prompt, rawOr(a.View, "{}"), q.Explanation, q.SourceQuote, nullable(q.Image),
				rawOrNil(a.Given), rawOrNil(a.Correct), a.Credit, a.IsCorrect, a.Graded, a.TimeSpent); err != nil {
				return err
			}
		}
		return nil
	})
	if err == errBadAnswers {
		fail(w, r, "bad_answers", http.StatusBadRequest)
		return
	}
	if err != nil {
		internal(w, r, "submit", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, res)
}

type sentinel string

func (s sentinel) Error() string { return string(s) }

const errBadAnswers = sentinel("bad_answers")

func rawOr(b json.RawMessage, def string) []byte {
	if len(b) == 0 {
		return []byte(def)
	}
	return b
}

func rawOrNil(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// history: GET /api/quiz/attempts?quizId= — the student's attempts, newest first.
func history(w http.ResponseWriter, r *http.Request, u *auth.User) {
	quizID, _ := strconv.ParseInt(r.URL.Query().Get("quizId"), 10, 64)
	ctx := r.Context()
	type row struct {
		ID           int64     `json:"id"`
		QuizID       int64     `json:"quizId"`
		QuizTitle    string    `json:"quizTitle"`
		Mode         string    `json:"mode"`
		Score        float64   `json:"score"`
		MaxScore     int       `json:"maxScore"`
		Percentage   int       `json:"percentage"`
		CorrectCount int       `json:"correctCount"`
		Total        int       `json:"total"`
		TimeSpent    int       `json:"timeSpent"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	out := []row{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		// the quiz title comes from the server-side join below only for quizzes the student
		// may still read; otherwise it is empty
		rows, err := tx.Query(ctx, `select a.id, a.quiz_id, coalesce(q.title, ''), a.mode, a.score::float8, a.max_score,
			a.percentage, a.correct_count, a.total, a.time_spent, a.created_at
			from quiz_attempts a left join quizzes q on q.id = a.quiz_id
			where a.user_id = app_user_id() and ($1 = 0 or a.quiz_id = $1) order by a.id desc limit 200`, quizID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.QuizID, &x.QuizTitle, &x.Mode, &x.Score, &x.MaxScore, &x.Percentage,
				&x.CorrectCount, &x.Total, &x.TimeSpent, &x.CreatedAt); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "history", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"attempts": out})
}

// loadAttempt reads a stored attempt under RLS (only the student's own).
func loadAttempt(ctx context.Context, uid, id int64) (*Result, error) {
	res := &Result{Answers: []AnswerOut{}}
	err := db.WithUser(ctx, uid, func(tx pgx.Tx) error {
		var aid int64
		if err := tx.QueryRow(ctx, `select a.id, a.quiz_id, coalesce(q.title, ''), a.mode, a.retry_of, a.score::float8,
			a.max_score, a.percentage, a.correct_count, a.total, a.time_spent, a.created_at
			from quiz_attempts a left join quizzes q on q.id = a.quiz_id where a.id = $1`, id).Scan(&aid, &res.QuizID,
			&res.QuizTitle, &res.Mode, &res.RetryOf, &res.Score, &res.MaxScore, &res.Percentage, &res.CorrectCount,
			&res.Total, &res.TimeSpent, &res.CreatedAt); err != nil {
			return err
		}
		res.AttemptID = &aid
		rows, err := tx.Query(ctx, `select position, question_id, type, prompt, data, explanation, source_quote,
			coalesce(image, ''), given, correct_key, credit, is_correct, graded, time_spent
			from quiz_attempt_answers where attempt_id = $1 order by position`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a AnswerOut
			var img string
			var given, key []byte
			var credit float32
			if err := rows.Scan(&a.Position, &a.QuestionID, &a.Type, &a.Prompt, &a.View, &a.Explanation, &a.SourceQuote,
				&img, &given, &key, &credit, &a.IsCorrect, &a.Graded, &a.TimeSpent); err != nil {
				return err
			}
			a.ImageURL, a.Given, a.Correct = mediaURL(img), given, key
			a.Credit = math.Round(float64(credit)*1000) / 1000
			if a.Graded && !a.IsCorrect {
				res.WrongCount++
			}
			res.Answers = append(res.Answers, a)
		}
		return rows.Err()
	})
	return res, err
}

// getAttempt: GET /api/quiz/attempts/{id} — a stored result (own attempts only).
func getAttempt(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "attempt_not_found", http.StatusNotFound)
		return
	}
	res, err := loadAttempt(r.Context(), u.ID, id)
	if isNoRows(err) {
		fail(w, r, "attempt_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		internal(w, r, "attempt", err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// retryWrong: GET /api/quiz/attempts/{id}/retry — the play view with only the questions this
// attempt got wrong (still in the quiz). Submit with retryOf = id.
func retryWrong(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "attempt_not_found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	var quizID int64
	var ids []int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select quiz_id from quiz_attempts where id = $1`, id).Scan(&quizID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select question_id from quiz_attempt_answers where attempt_id = $1
			and graded and not is_correct and question_id is not null order by position`, id)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	if isNoRows(err) {
		fail(w, r, "attempt_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		internal(w, r, "retry", err)
		return
	}
	if len(ids) == 0 {
		fail(w, r, "nothing_to_retry", http.StatusConflict)
		return
	}
	m, err := loadMeta(ctx, quizID, u.ID)
	// unlisted quizzes: the student played it before, the retry stays open to them
	if err != nil || (!canPlay(m, u.ID, "") && !(m.Visibility == "unlisted" && !m.IsDraft && !m.Hidden)) {
		notFound(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, func() map[string]any {
		var qs []PlayQuestion
		_ = db.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			qs, err = loadPlay(ctx, tx, m, ids)
			return err
		})
		return map[string]any{"quiz": m, "questions": qs, "retryOf": id}
	}())
}

// stats: GET /api/quiz/quizzes/{id}/stats — per-question accuracy over everyone's attempts,
// for the owner only (aggregates, no names).
func stats(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	m, err := loadMeta(ctx, id, u.ID)
	if err != nil || !m.IsMine {
		notFound(w, r)
		return
	}
	type qstat struct {
		QuestionID   int64            `json:"questionId"`
		Position     int              `json:"position"`
		Type         string           `json:"type"`
		Prompt       string           `json:"prompt"`
		Answered     int              `json:"answered"`
		Correct      int              `json:"correct"`
		Accuracy     int              `json:"accuracy"` // percent of answered
		AvgTime      float64          `json:"avgTimeSpent"`
		Distribution map[string]int   `json:"distribution,omitempty"` // mcq/dropdown/poll/true_false: given → count
		Skipped      int              `json:"skipped"`
	}
	var out []*qstat
	var attempts, players int
	var avgPct float64
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select count(*), count(distinct user_id), coalesce(avg(percentage), 0)::float8
			from quiz_attempts where quiz_id = $1 and mode = 'practice'`, id).Scan(&attempts, &players, &avgPct); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select q.id, q.position, q.type, q.prompt,
			count(a.attempt_id) filter (where a.given is not null), count(a.attempt_id) filter (where a.is_correct),
			coalesce(avg(a.time_spent) filter (where a.given is not null), 0)::float8,
			count(a.attempt_id) filter (where a.given is null)
			from quiz_questions q left join quiz_attempt_answers a on a.question_id = q.id
			where q.quiz_id = $1 group by q.id order by q.position`, id)
		if err != nil {
			return err
		}
		byID := map[int64]*qstat{}
		for rows.Next() {
			s := &qstat{}
			if err := rows.Scan(&s.QuestionID, &s.Position, &s.Type, &s.Prompt, &s.Answered, &s.Correct, &s.AvgTime, &s.Skipped); err != nil {
				rows.Close()
				return err
			}
			if s.Answered > 0 {
				s.Accuracy = s.Correct * 100 / s.Answered
			}
			s.AvgTime = math.Round(s.AvgTime*10) / 10
			out = append(out, s)
			byID[s.QuestionID] = s
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select a.question_id, a.given::text, count(*) from quiz_attempt_answers a
			join quiz_questions q on q.id = a.question_id
			where q.quiz_id = $1 and q.type in ('mcq', 'dropdown', 'poll', 'true_false', 'hotspot') and a.given is not null
			group by 1, 2`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var qid int64
			var given string
			var n int
			if err := rows.Scan(&qid, &given, &n); err != nil {
				return err
			}
			if s := byID[qid]; s != nil {
				if s.Distribution == nil {
					s.Distribution = map[string]int{}
				}
				s.Distribution[given] = n
			}
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "stats", err)
		return
	}
	if out == nil {
		out = []*qstat{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"attempts": attempts, "players": players,
		"avgPercentage": math.Round(avgPct), "questions": out})
}

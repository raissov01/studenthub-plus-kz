package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Meta is a quiz without its questions, as lists and headers show it.
type Meta struct {
	ID              int64     `json:"id"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Subject         string    `json:"subject"`
	Visibility      string    `json:"visibility"`
	IsDraft         bool      `json:"isDraft"`
	ShareCode       string    `json:"shareCode,omitempty"` // owner, or public/unlisted quizzes
	TimePerQuestion int       `json:"timePerQuestion"`
	Shuffle         bool      `json:"shuffle"`
	Source          string    `json:"source"`
	QuestionCount   int       `json:"questionCount"`
	Plays           int       `json:"plays"`
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	IsMine          bool      `json:"isMine"`
	Hidden          bool      `json:"hidden,omitempty"`
	OwnerID         int64     `json:"-"`
}

const metaCols = `id, title, description, subject, visibility, is_draft, share_code, time_per_question, shuffle,
	source, question_count, plays, version, created_at, updated_at, hidden_at is not null, user_id`

func scanMeta(row pgx.Row, viewer int64) (*Meta, error) {
	var m Meta
	err := row.Scan(&m.ID, &m.Title, &m.Description, &m.Subject, &m.Visibility, &m.IsDraft, &m.ShareCode,
		&m.TimePerQuestion, &m.Shuffle, &m.Source, &m.QuestionCount, &m.Plays, &m.Version, &m.CreatedAt,
		&m.UpdatedAt, &m.Hidden, &m.OwnerID)
	if err != nil {
		return nil, err
	}
	m.IsMine = viewer != 0 && viewer == m.OwnerID
	if !m.IsMine && m.Visibility == "private" {
		m.ShareCode = ""
	}
	return &m, nil
}

// Input is a full quiz as the editor sends it (POST and PUT replace everything).
type Input struct {
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Subject         string     `json:"subject"`
	Visibility      string     `json:"visibility"`
	IsDraft         *bool      `json:"isDraft"`
	TimePerQuestion int        `json:"timePerQuestion"`
	Shuffle         *bool      `json:"shuffle"`
	Version         int        `json:"version"` // PUT: the version the editor loaded (0 = skip check)
	Questions       []Question `json:"questions"`
}

type inputError struct {
	code string
	v    *ValidationError
}

func (e *inputError) Error() string { return e.code }

// normalizeInput validates the whole quiz; errors are *inputError.
func normalizeInput(in Input) (Input, error) {
	in.Title = clean(in.Title, false)
	if in.Title == "" || in.Title == "\x00" || runes(in.Title) > 200 {
		return in, &inputError{code: "bad_title"}
	}
	in.Description = clean(in.Description, true)
	in.Subject = clean(in.Subject, false)
	if in.Description == "\x00" || runes(in.Description) > 1000 || in.Subject == "\x00" || runes(in.Subject) > 80 {
		return in, &inputError{code: "bad_request"}
	}
	if in.Visibility == "" {
		in.Visibility = "private"
	}
	if !slices.Contains([]string{"public", "unlisted", "private"}, in.Visibility) {
		return in, &inputError{code: "bad_request"}
	}
	if in.TimePerQuestion == 0 {
		in.TimePerQuestion = 30
	}
	if in.TimePerQuestion < 5 || in.TimePerQuestion > 300 {
		return in, &inputError{code: "bad_request"}
	}
	if len(in.Questions) == 0 {
		return in, &inputError{code: "no_questions"}
	}
	if len(in.Questions) > MaxQuestions {
		return in, &inputError{code: "too_many"}
	}
	out := make([]Question, 0, len(in.Questions))
	for i, q := range in.Questions {
		n, err := Normalize(q, i+1)
		if err != nil {
			var ve *ValidationError
			errors.As(err, &ve)
			return in, &inputError{code: "invalid_quiz", v: ve}
		}
		out = append(out, n)
	}
	in.Questions = out
	return in, nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

var errMedia = errors.New("bad_media")

// mediaOf lists the media names the questions reference.
func mediaOf(qs []Question) []string {
	out := []string{}
	for _, q := range qs {
		for _, n := range []string{q.Image, q.Audio} {
			if n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// insertQuiz creates the quiz row and its questions inside tx (RLS: tx is the owner's).
func insertQuiz(ctx context.Context, tx pgx.Tx, userID int64, in Input, source string, clonedFrom *int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `insert into quizzes (user_id, title, description, subject, visibility, is_draft, share_code,
		time_per_question, shuffle, source, cloned_from, question_count)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) returning id`,
		userID, in.Title, in.Description, in.Subject, in.Visibility, boolOr(in.IsDraft, false), randomCode(10, alnum),
		in.TimePerQuestion, boolOr(in.Shuffle, true), source, clonedFrom, len(in.Questions)).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err := saveQuestions(ctx, tx, id, in.Questions); err != nil {
		return 0, err
	}
	return id, nil
}

// saveQuestions replaces the quiz's questions: rows whose id is resent are updated in place
// (so per-question stats keep their history), the rest are inserted, missing ones deleted.
// Media must be the owner's own uploads, unattached or already on this quiz; they are attached
// here. It returns media names that are no longer used (the caller deletes them after commit).
func saveQuestions(ctx context.Context, tx pgx.Tx, quizID int64, qs []Question) ([]string, error) {
	names := mediaOf(qs)
	if len(names) > 0 {
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from quiz_media where name = any($1) and user_id = app_user_id()
			and (quiz_id is null or quiz_id = $2)`, names, quizID).Scan(&n); err != nil {
			return nil, err
		}
		if n != len(names) {
			return nil, errMedia
		}
	}
	rows, err := tx.Query(ctx, `select id from quiz_questions where quiz_id = $1`, quizID)
	if err != nil {
		return nil, err
	}
	existing, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	kept := []int64{}
	for i, q := range qs {
		view, _ := json.Marshal(viewOf(q))
		key, _ := json.Marshal(keyOf(q))
		var tl *int
		if q.TimeLimit > 0 {
			tl = &q.TimeLimit
		}
		img, aud := nullable(q.Image), nullable(q.Audio)
		if q.ID != 0 && slices.Contains(existing, q.ID) && !slices.Contains(kept, q.ID) {
			if _, err := tx.Exec(ctx, `update quiz_questions set position = $2, type = $3, prompt = $4, data = $5,
				explanation = $6, source_quote = $7, image = $8, audio = $9, time_limit = $10 where id = $1`,
				q.ID, i+1, q.Type, q.Prompt, view, q.Explanation, q.SourceQuote, img, aud, tl); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `update quiz_answer_keys set answer = $2 where question_id = $1`, q.ID, key); err != nil {
				return nil, err
			}
			kept = append(kept, q.ID)
			continue
		}
		var qid int64
		if err := tx.QueryRow(ctx, `insert into quiz_questions (quiz_id, position, type, prompt, data, explanation,
			source_quote, image, audio, time_limit) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning id`,
			quizID, i+1, q.Type, q.Prompt, view, q.Explanation, q.SourceQuote, img, aud, tl).Scan(&qid); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `insert into quiz_answer_keys (question_id, quiz_id, answer) values ($1, $2, $3)`, qid, quizID, key); err != nil {
			return nil, err
		}
		kept = append(kept, qid)
	}
	if _, err := tx.Exec(ctx, `delete from quiz_questions where quiz_id = $1 and not (id = any($2))`, quizID, kept); err != nil {
		return nil, err
	}
	if len(names) > 0 {
		if _, err := tx.Exec(ctx, `update quiz_media set quiz_id = $1 where name = any($2)`, quizID, names); err != nil {
			return nil, err
		}
	}
	rows, err = tx.Query(ctx, `delete from quiz_media where quiz_id = $1 and not (name = any($2)) returning name`, quizID, names)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// loadFull reads every question with its key, in order (tx must be able to read the keys:
// the owner's RLS transaction or a server transaction).
func loadFull(ctx context.Context, tx pgx.Tx, quizID int64) ([]Question, error) {
	rows, err := tx.Query(ctx, `select q.id, q.type, q.prompt, q.explanation, q.source_quote, coalesce(q.image, ''),
		coalesce(q.audio, ''), coalesce(q.time_limit, 0), k.answer
		from quiz_questions q join quiz_answer_keys k on k.question_id = q.id
		where q.quiz_id = $1 order by q.position`, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		var q Question
		var id int64
		var typ, prompt, expl, quote, img, aud string
		var tl int
		var key []byte
		if err := rows.Scan(&id, &typ, &prompt, &expl, &quote, &img, &aud, &tl, &key); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(key, &q); err != nil {
			return nil, err
		}
		q.ID, q.Type, q.Prompt, q.Explanation, q.SourceQuote, q.Image, q.Audio, q.TimeLimit = id, typ, prompt, expl, quote, img, aud, tl
		out = append(out, q)
	}
	return out, rows.Err()
}

// PlayQuestion is a question as a player gets it: no answer key, no explanation.
type PlayQuestion struct {
	ID        int64  `json:"id"`
	Position  int    `json:"position"`
	Type      string `json:"type"`
	Prompt    string `json:"prompt"`
	ImageURL  string `json:"imageUrl,omitempty"`
	AudioURL  string `json:"audioUrl,omitempty"`
	TimeLimit int    `json:"timeLimit"`
	PlayView
}

func mediaURL(name string) string {
	if name == "" {
		return ""
	}
	return "/api/quiz/media/" + name
}

func loadPlay(ctx context.Context, tx pgx.Tx, m *Meta, only []int64) ([]PlayQuestion, error) {
	rows, err := tx.Query(ctx, `select id, position, type, prompt, data, coalesce(image, ''), coalesce(audio, ''),
		coalesce(time_limit, 0) from quiz_questions where quiz_id = $1 and ($2::bigint[] is null or id = any($2))
		order by position`, m.ID, only)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlayQuestion{}
	for rows.Next() {
		var p PlayQuestion
		var data []byte
		var img, aud string
		if err := rows.Scan(&p.ID, &p.Position, &p.Type, &p.Prompt, &data, &img, &aud, &p.TimeLimit); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &p.PlayView); err != nil {
			return nil, err
		}
		p.ImageURL, p.AudioURL = mediaURL(img), mediaURL(aud)
		if p.TimeLimit == 0 {
			p.TimeLimit = m.TimePerQuestion
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// canPlay: owner always; others only published public quizzes, or public/unlisted via the
// share code. Hidden quizzes stay open to the owner only.
func canPlay(m *Meta, viewer int64, share string) bool {
	if m.IsMine || (viewer != 0 && viewer == m.OwnerID) {
		return true
	}
	if m.IsDraft || m.Hidden {
		return false
	}
	switch m.Visibility {
	case "public":
		return true
	case "unlisted":
		return share != "" && share == m.ShareCode
	}
	return false
}

func normCode(s string) string { return strings.TrimSpace(s) }

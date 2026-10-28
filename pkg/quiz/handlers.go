package quiz

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

const maxQuizBody = 1 << 20

func viewerID(r *http.Request) int64 {
	if u := auth.Current(r); u != nil {
		return u.ID
	}
	return 0
}

// writeInputError maps normalizeInput / saveQuestions errors to responses.
func writeInputError(w http.ResponseWriter, r *http.Request, err error) bool {
	var ie *inputError
	if errors.As(err, &ie) {
		if ie.v != nil {
			m := msgs["invalid_quiz"]
			text := m.KK
			if httpx.Lang(r) == "ru" {
				text = m.RU
			}
			httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{
				"code": "invalid_quiz", "message": text, "question": ie.v.Index, "reason": ie.v.Code}})
			return true
		}
		fail(w, r, ie.code, http.StatusBadRequest)
		return true
	}
	if errors.Is(err, errMedia) {
		fail(w, r, "bad_media", http.StatusBadRequest)
		return true
	}
	return false
}

// browse: GET /api/quiz/quizzes?q=&subject=&sort=new|popular&page= — published public quizzes.
func browse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 0)
	order := "id desc"
	if q.Get("sort") == "popular" {
		order = "plays desc, id desc"
	}
	search := strings.TrimSpace(q.Get("q"))
	if runes(search) > 100 {
		bad(w, r)
		return
	}
	viewer := viewerID(r)
	ctx := r.Context()
	out := []*Meta{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+metaCols+` from quizzes
			where visibility = 'public' and not is_draft and hidden_at is null and question_count > 0
			and ($1 = '' or title ilike '%' || $1 || '%' or subject ilike '%' || $1 || '%')
			and ($2 = '' or lower(subject) = lower($2))
			order by `+order+` limit 21 offset $3`, escapeLike(search), q.Get("subject"), page*20)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMeta(rows, viewer)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "browse", err)
		return
	}
	more := len(out) > 20
	if more {
		out = out[:20]
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	httpx.JSON(w, http.StatusOK, map[string]any{"quizzes": out, "hasMore": more})
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// mine: GET /api/quiz/mine — the student's own quizzes (drafts included).
func mine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []*Meta{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+metaCols+` from quizzes where user_id = app_user_id() order by updated_at desc limit 500`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMeta(rows, u.ID)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "mine", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"quizzes": out})
}

func needConsent(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return false
	}
	return true
}

func createLimit(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if ok, _ := ratelimit.Allow(r.Context(), "quiz:c:"+itoa(u.ID), config.Int("QUIZ_CREATE_PER_DAY", 50), 24*time.Hour); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return false
	}
	return true
}

// create: POST /api/quiz/quizzes with Input → 201 {id, shareCode}.
func create(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !needConsent(w, r, u) {
		return
	}
	var in Input
	if err := httpx.Decode(r, &in, maxQuizBody); err != nil {
		bad(w, r)
		return
	}
	in, err := normalizeInput(in)
	if err != nil {
		writeInputError(w, r, err)
		return
	}
	if !createLimit(w, r, u) {
		return
	}
	source := "manual"
	if s := r.URL.Query().Get("source"); s == "excel" || s == "cards" {
		source = s
	}
	id, err := createQuiz(r.Context(), u.ID, in, source, nil)
	if err != nil {
		if !writeInputError(w, r, err) {
			internal(w, r, "create", err)
		}
		return
	}
	writeCreated(w, r, u.ID, id)
}

func createQuiz(ctx context.Context, userID int64, in Input, source string, clonedFrom *int64) (int64, error) {
	var id int64
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		var err error
		id, err = insertQuiz(ctx, tx, userID, in, source, clonedFrom)
		return err
	})
	return id, err
}

func writeCreated(w http.ResponseWriter, r *http.Request, userID, id int64) {
	m, err := loadMeta(r.Context(), id, userID)
	if err != nil {
		internal(w, r, "load created", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "quiz": m})
}

// loadMeta reads a quiz header as the server (no RLS); callers check access with canPlay/IsMine.
func loadMeta(ctx context.Context, id, viewer int64) (*Meta, error) {
	var m *Meta
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		m, err = scanMeta(tx.QueryRow(ctx, `select `+metaCols+` from quizzes where id = $1`, id), viewer)
		return err
	})
	return m, err
}

func loadMetaByCode(ctx context.Context, code string, viewer int64) (*Meta, error) {
	var m *Meta
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		m, err = scanMeta(tx.QueryRow(ctx, `select `+metaCols+` from quizzes where share_code = $1`, code), viewer)
		return err
	})
	return m, err
}

// getForEdit: GET /api/quiz/quizzes/{id} — the owner's editor view with answer keys. Runs under
// RLS, so another student's quiz is simply not found.
func getForEdit(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var m *Meta
	var qs []Question
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		m, err = scanMeta(tx.QueryRow(ctx, `select `+metaCols+` from quizzes where id = $1 and user_id = app_user_id()`, id), u.ID)
		if err != nil {
			return err
		}
		qs, err = loadFull(ctx, tx, id)
		return err
	})
	if isNoRows(err) {
		notFound(w, r)
		return
	}
	if err != nil {
		internal(w, r, "get", err)
		return
	}
	if qs == nil {
		qs = []Question{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"quiz": m, "questions": qs})
}

// update: PUT /api/quiz/quizzes/{id} — replaces the quiz. With "version" set, a stale editor
// gets 409 instead of overwriting newer changes.
func update(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var in Input
	if err := httpx.Decode(r, &in, maxQuizBody); err != nil {
		bad(w, r)
		return
	}
	in, err := normalizeInput(in)
	if err != nil {
		writeInputError(w, r, err)
		return
	}
	ctx := r.Context()
	var unused []string
	var conflict bool
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var version int
		var draft bool
		if err := tx.QueryRow(ctx, `select version, is_draft from quizzes where id = $1 and user_id = app_user_id() for update`, id).Scan(&version, &draft); err != nil {
			return err
		}
		if in.Version != 0 && in.Version != version {
			conflict = true
			return nil
		}
		if _, err := tx.Exec(ctx, `update quizzes set title = $2, description = $3, subject = $4, visibility = $5,
			is_draft = $6, time_per_question = $7, shuffle = $8, question_count = $9, version = version + 1 where id = $1`,
			id, in.Title, in.Description, in.Subject, in.Visibility, boolOr(in.IsDraft, draft), in.TimePerQuestion,
			boolOr(in.Shuffle, true), len(in.Questions)); err != nil {
			return err
		}
		var err error
		unused, err = saveQuestions(ctx, tx, id, in.Questions)
		return err
	})
	if isNoRows(err) {
		notFound(w, r)
		return
	}
	if conflict {
		fail(w, r, "version_conflict", http.StatusConflict)
		return
	}
	if err != nil {
		if !writeInputError(w, r, err) {
			internal(w, r, "update", err)
		}
		return
	}
	removeFiles(unused)
	m, err := loadMeta(ctx, id, u.ID)
	if err != nil {
		internal(w, r, "reload", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"quiz": m})
}

// remove: DELETE /api/quiz/quizzes/{id} — the quiz, its questions, attempts and media files.
func remove(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var names []string
	var n int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `delete from quiz_media where quiz_id = $1 and user_id = app_user_id()
			and exists (select 1 from quizzes where id = $1 and user_id = app_user_id()) returning name`, id)
		if err != nil {
			return err
		}
		if names, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `delete from quizzes where id = $1 and user_id = app_user_id()`, id)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		internal(w, r, "delete", err)
		return
	}
	if n == 0 {
		notFound(w, r)
		return
	}
	removeFiles(names)
	closeLiveForQuiz(id)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// resetShare: POST /api/quiz/quizzes/{id}/share/reset — a new share code; old links stop working.
func resetShare(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	code := randomCode(10, alnum)
	var n int64
	// share_code has no app_user update grant on purpose: only this endpoint changes it.
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update quizzes set share_code = $2 where id = $1 and user_id = $3`, id, code, u.ID)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		internal(w, r, "reset share", err)
		return
	}
	if n == 0 {
		notFound(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"shareCode": code})
}

// play view

func writePlay(w http.ResponseWriter, r *http.Request, m *Meta, only []int64) {
	ctx := r.Context()
	var qs []PlayQuestion
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		qs, err = loadPlay(ctx, tx, m, only)
		return err
	})
	if err != nil {
		internal(w, r, "play", err)
		return
	}
	if m.Visibility == "private" || m.IsDraft {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"quiz": m, "questions": qs})
}

// playByID: GET /api/quiz/quizzes/{id}/play — public quizzes for everyone, any own quiz.
func playByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	m, err := loadMeta(r.Context(), id, viewerID(r))
	if err != nil || !canPlay(m, viewerID(r), "") {
		if err != nil && !isNoRows(err) {
			internal(w, r, "meta", err)
			return
		}
		notFound(w, r)
		return
	}
	writePlay(w, r, m, nil)
}

// playByCode: GET /api/quiz/s/{code} — the share link (public and unlisted quizzes).
func playByCode(w http.ResponseWriter, r *http.Request) {
	code := normCode(r.PathValue("code"))
	m, err := loadMetaByCode(r.Context(), code, viewerID(r))
	if err != nil || !canPlay(m, viewerID(r), code) {
		if err != nil && !isNoRows(err) {
			internal(w, r, "meta", err)
			return
		}
		notFound(w, r)
		return
	}
	writePlay(w, r, m, nil)
}

// shareMeta: GET /api/quiz/s/{code}/meta — for the share page's generateMetadata (OG tags).
// Only published public/unlisted quizzes; no author data.
func shareMeta(w http.ResponseWriter, r *http.Request) {
	code := normCode(r.PathValue("code"))
	ctx := r.Context()
	var title, descr, subject, image string
	var count, plays int
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select title, description, subject, question_count, plays,
			coalesce((select image from quiz_questions where quiz_id = quizzes.id and image is not null order by position limit 1), '')
			from quizzes where share_code = $1 and visibility in ('public', 'unlisted') and not is_draft and hidden_at is null`,
			code).Scan(&title, &descr, &subject, &count, &plays, &image)
	})
	if isNoRows(err) {
		notFound(w, r)
		return
	}
	if err != nil {
		internal(w, r, "meta", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"title": title, "description": descr, "subject": subject, "questionCount": count, "plays": plays,
		"imageUrl": mediaURL(image), "url": config.SiteURL() + "/quiz/s/" + code,
	})
}

// clone

func cloneByID(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	m, err := loadMeta(r.Context(), id, u.ID)
	cloneFrom(w, r, u, m, err, "")
}

func cloneByCode(w http.ResponseWriter, r *http.Request, u *auth.User) {
	code := normCode(r.PathValue("code"))
	m, err := loadMetaByCode(r.Context(), code, u.ID)
	cloneFrom(w, r, u, m, err, code)
}

// cloneFrom copies a playable quiz into a new private quiz of u (media files are copied too,
// so either copy can be deleted independently).
func cloneFrom(w http.ResponseWriter, r *http.Request, u *auth.User, m *Meta, err error, code string) {
	if err != nil || !canPlay(m, u.ID, code) {
		if err != nil && !isNoRows(err) {
			internal(w, r, "clone meta", err)
			return
		}
		notFound(w, r)
		return
	}
	if !needConsent(w, r, u) || !createLimit(w, r, u) {
		return
	}
	ctx := r.Context()
	var qs []Question
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		qs, err = loadFull(ctx, tx, m.ID)
		return err
	}); err != nil {
		internal(w, r, "clone load", err)
		return
	}
	copies, err := copyMedia(ctx, u.ID, mediaOf(qs))
	if err != nil {
		internal(w, r, "clone media", err)
		return
	}
	for i := range qs {
		qs[i].ID = 0
		if qs[i].Image != "" {
			qs[i].Image = copies[qs[i].Image]
		}
		if qs[i].Audio != "" {
			qs[i].Audio = copies[qs[i].Audio]
		}
	}
	title := m.Title
	if !m.IsMine {
		title = clip(title, 185) + " (көшірме)"
	} else {
		title = clip(title, 190) + " (2)"
	}
	in := Input{Title: title, Description: m.Description, Subject: m.Subject, Visibility: "private",
		TimePerQuestion: m.TimePerQuestion, Shuffle: &m.Shuffle, Questions: qs}
	src := m.ID
	id, err := createQuiz(ctx, u.ID, in, "clone", &src)
	if err != nil {
		internal(w, r, "clone", err)
		return
	}
	writeCreated(w, r, u.ID, id)
}

func clip(s string, n int) string {
	rs := []rune(s)
	if len(rs) > n {
		return string(rs[:n])
	}
	return s
}

// admin

func setHidden(w http.ResponseWriter, r *http.Request, u *auth.User, hide bool) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var n int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update quizzes set hidden_at = case when $2 then now() end where id = $1`, id, hide)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		action := "quiz_restore"
		if hide {
			action = "quiz_hide"
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, $2, null, null, jsonb_build_object('quizId', $3::bigint))`, u.ID, action, id)
		return err
	})
	if err != nil {
		internal(w, r, "admin hide", err)
		return
	}
	if n == 0 {
		notFound(w, r)
		return
	}
	if hide {
		closeLiveForQuiz(id)
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func adminHide(w http.ResponseWriter, r *http.Request, u *auth.User)    { setHidden(w, r, u, true) }
func adminRestore(w http.ResponseWriter, r *http.Request, u *auth.User) { setHidden(w, r, u, false) }

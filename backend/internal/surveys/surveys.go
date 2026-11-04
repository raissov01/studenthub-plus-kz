// Package surveys is «Сауалнамалар / Опросы» (FEATURE_SURVEYS, docs/UNIVOICE.md): university staff
// create surveys for their university, students in the audience answer them once, staff see
// k-anonymous aggregates (groups under 5 hidden) and export aggregate CSV only.
//
// Anonymity: an anonymous survey stores its answers in survey_responses with no user column and
// no timestamp. Double voting is stopped by survey_markers.marker = HMAC(K_s, user_id) with
// K_s = HMAC(server key, survey_id, marker_salt); the server key is derived from SESSION_SECRET
// and never stored, so the database alone cannot list who took part, and nothing joins a marker
// to a response. Identified surveys keep the person ↔ response link in survey_response_owners.
package surveys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// Feature is the switch: FEATURE_SURVEYS.
const Feature = "surveys"

// Now is replaced in tests.
var Now = time.Now

// OnComplete pays the completion award inside the submit transaction (wired to internal/points in
// internal/server). ref is an opaque per-(survey, student) id: the points ledger never names the survey.
var OnComplete func(ctx context.Context, tx pgx.Tx, userID int64, ref string) any

var (
	MsgInvalid  = httpx.Msg{KK: "Сауалнама толық емес не қате толтырылған.", RU: "Опрос заполнен не полностью или с ошибками."}
	MsgDone     = httpx.Msg{KK: "Сіз бұл сауалнамаға жауап бердіңіз. Жауапты өзгертуге болмайды.", RU: "Вы уже ответили на этот опрос. Изменить ответ нельзя."}
	MsgNotOpen  = httpx.Msg{KK: "Бұл сауалнама қазір ашық емес.", RU: "Этот опрос сейчас закрыт."}
	MsgFrozen   = httpx.Msg{KK: "Жарияланған сауалнаманың сұрақтарын өзгертуге болмайды.", RU: "Вопросы опубликованного опроса менять нельзя."}
	MsgConsent  = httpx.Msg{KK: "Алдымен келісім беріңіз.", RU: "Сначала дайте согласие."}
	MsgUniv     = httpx.Msg{KK: "Университетті көрсетіңіз.", RU: "Укажите университет."}
	MsgStudents = httpx.Msg{KK: "Сауалнамаға студенттер ғана жауап береді.", RU: "Отвечать на опросы могут только студенты."}
)

func init() {
	// identified surveys only: anonymous responses have no owner row and cannot be exported
	me.RegisterExport("surveyAnswers", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select r.survey_id, r.answers from survey_responses r
			join survey_response_owners o on o.response_id = r.id where o.user_id = app_user_id()`)
	})
	// account deletion: an identified response goes with its owner (anonymous ones have no owner)
	me.RegisterDeleteHook(func(ctx context.Context, tx pgx.Tx, userID int64) error {
		_, err := tx.Exec(ctx, `delete from survey_responses where id in (select response_id from survey_response_owners where user_id = $1)`, userID)
		return err
	})
}

// Routes wires every survey endpoint behind FEATURE_SURVEYS.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	h("GET /api/surveys", auth.Need(listMine))
	h("GET /api/surveys/{id}", auth.Need(getOne))
	h("POST /api/surveys/{id}/responses", auth.Write(auth.Need(submit)))

	h("GET /api/staff/surveys", auth.Need(staffList))
	h("POST /api/staff/surveys", auth.Write(auth.Need(staffCreate)))
	h("GET /api/staff/surveys/{id}", auth.Need(staffGet))
	h("PUT /api/staff/surveys/{id}", auth.Write(auth.Need(staffUpdate)))
	h("POST /api/staff/surveys/{id}/publish", auth.Write(auth.Need(staffPublish)))
	h("POST /api/staff/surveys/{id}/close", auth.Write(auth.Need(staffClose)))
	h("DELETE /api/staff/surveys/{id}", auth.Write(auth.Need(staffDelete)))
	h("GET /api/staff/surveys/{id}/results", auth.Need(staffResults))
	h("GET /api/staff/surveys/{id}/results.csv", auth.Need(staffCSV))
}

// Survey is a survey row (never with its salt).
type Survey struct {
	ID          int64      `json:"id"`
	University  string     `json:"university"`
	Title       string     `json:"title"`
	Intro       string     `json:"intro"`
	Questions   []Question `json:"questions"`
	Audience    Audience   `json:"audience"`
	Anonymous   bool       `json:"anonymous"`
	Status      string     `json:"status"`
	OpensAt     *time.Time `json:"opensAt"`
	ClosesAt    *time.Time `json:"closesAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	PublishedAt *time.Time `json:"publishedAt"`
	Responses   int        `json:"responses"`
	Done        bool       `json:"done,omitempty"` // student view: already answered
	salt        []byte
}

const cols = `s.id, s.university, s.title, s.intro, s.questions, s.audience, s.anonymous, s.status, s.opens_at, s.closes_at,
	s.created_at, s.published_at, (select count(*) from survey_markers m where m.survey_id = s.id), s.marker_salt`

func scan(row pgx.Row) (*Survey, error) {
	var s Survey
	var qs, aud []byte
	if err := row.Scan(&s.ID, &s.University, &s.Title, &s.Intro, &qs, &aud, &s.Anonymous, &s.Status, &s.OpensAt, &s.ClosesAt,
		&s.CreatedAt, &s.PublishedAt, &s.Responses, &s.salt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(qs, &s.Questions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(aud, &s.Audience); err != nil {
		return nil, err
	}
	if s.Audience.Faculties == nil {
		s.Audience.Faculties = []string{}
	}
	if s.Audience.Courses == nil {
		s.Audience.Courses = []int{}
	}
	if s.Audience.Groups == nil {
		s.Audience.Groups = []string{}
	}
	return &s, nil
}

// ---------- anonymity keys ----------

// markerKey is K_s = HMAC(server key, survey id, salt).
func markerKey(surveyID int64, salt []byte) ([]byte, error) {
	k, err := univoice.Key("survey-marker")
	if err != nil {
		return nil, err
	}
	return univoice.MAC(k, []byte(strconv.FormatInt(surveyID, 10)), salt), nil
}

// Marker is the one-way participation marker HMAC(K_s, user id).
func Marker(surveyID int64, salt []byte, userID int64) ([]byte, error) {
	k, err := markerKey(surveyID, salt)
	if err != nil {
		return nil, err
	}
	return univoice.MAC(k, []byte(strconv.FormatInt(userID, 10))), nil
}

// pointsRef is the points ledger reference: unique per (survey, student) but naming neither.
func pointsRef(surveyID, userID int64) (string, error) {
	k, err := univoice.Key("survey-points")
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(univoice.MAC(k, []byte(strconv.FormatInt(surveyID, 10)), []byte(strconv.FormatInt(userID, 10)))[:16]), nil
}

// ---------- student ----------

// eligible: published, within dates, same university, inside the audience.
const eligible = `s.status = 'published' and (s.opens_at is null or s.opens_at <= $2) and (s.closes_at is null or s.closes_at > $2)
	and s.university = u.university
	and (jsonb_array_length(coalesce(s.audience->'faculties', '[]')) = 0 or s.audience->'faculties' ? u.faculty)
	and (jsonb_array_length(coalesce(s.audience->'courses', '[]')) = 0 or s.audience->'courses' @> to_jsonb(u.course::int))
	and (jsonb_array_length(coalesce(s.audience->'groups', '[]')) = 0
		or s.audience->'groups' ? (select upper(w.study_group) from points_wallets w where w.user_id = u.id))`

func studentOK(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return false
	}
	if u.Role != "student" && u.Role != "ambassador" {
		httpx.Error(w, r, http.StatusForbidden, "students_only", MsgStudents)
		return false
	}
	return true
}

func listMine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !studentOK(w, r, u) {
		return
	}
	out := []*Survey{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select `+cols+` from surveys s, users u where u.id = $1 and `+eligible+`
			order by s.closes_at nulls last, s.id desc limit 50`, u.ID, Now())
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Survey, error) { return scan(row) })
		if err != nil {
			return err
		}
		for _, s := range list {
			m, err := Marker(s.ID, s.salt, u.ID)
			if err != nil {
				return err
			}
			if err := tx.QueryRow(r.Context(), `select exists (select 1 from survey_markers where survey_id = $1 and marker = $2)`, s.ID, m).Scan(&s.Done); err != nil {
				return err
			}
			s.Responses = 0 // students do not see participation numbers
			out = append(out, s)
		}
		return nil
	})
	if err != nil {
		univoice.Internal(w, r, "surveys list", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"surveys": out})
}

func loadEligible(ctx context.Context, tx pgx.Tx, id int64, u *auth.User, lock bool) (*Survey, error) {
	q := `select ` + cols + ` from surveys s, users u where u.id = $1 and s.id = $3 and ` + eligible
	if lock {
		q += ` for share of s`
	}
	return scan(tx.QueryRow(ctx, q, u.ID, Now(), id))
}

func getOne(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := univoice.PathID(r, "id")
	if !ok || !studentOK(w, r, u) {
		if !ok {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		return
	}
	var s *Survey
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		if s, err = loadEligible(r.Context(), tx, id, u, false); err != nil {
			return err
		}
		m, err := Marker(s.ID, s.salt, u.ID)
		if err != nil {
			return err
		}
		return tx.QueryRow(r.Context(), `select exists (select 1 from survey_markers where survey_id = $1 and marker = $2)`, s.ID, m).Scan(&s.Done)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		univoice.Internal(w, r, "survey get", err)
		return
	}
	s.Responses = 0
	httpx.JSON(w, http.StatusOK, map[string]any{"survey": s})
}

func submit(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := univoice.PathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if !studentOK(w, r, u) {
		return
	}
	var body struct {
		Answers Answers `json:"answers"`
	}
	if err := httpx.Decode(r, &body, 64<<10); err != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	var award any
	var invalid error
	var done bool
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		s, err := loadEligible(r.Context(), tx, id, u, true)
		if err != nil {
			return err
		}
		answers, err := ValidateAnswers(s.Questions, body.Answers)
		if err != nil {
			invalid = err
			return nil
		}
		m, err := Marker(s.ID, s.salt, u.ID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(r.Context(), `insert into survey_markers (survey_id, marker) values ($1, $2) on conflict do nothing`, s.ID, m)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			done = true
			return nil
		}
		raw, _ := json.Marshal(answers)
		var faculty *string
		if u.Faculty != nil && *u.Faculty != "" && len(*u.Faculty) <= 40 {
			faculty = u.Faculty
		}
		var course *int16
		if u.Course != nil && *u.Course >= 1 && *u.Course <= 6 {
			course = u.Course
		}
		var respID string
		if err := tx.QueryRow(r.Context(), `insert into survey_responses (survey_id, answers, faculty, course) values ($1, $2::jsonb, $3, $4) returning id::text`,
			s.ID, string(raw), faculty, course).Scan(&respID); err != nil {
			return err
		}
		if !s.Anonymous {
			if _, err := tx.Exec(r.Context(), `insert into survey_response_owners (response_id, user_id) values ($1::uuid, $2)`, respID, u.ID); err != nil {
				return err
			}
		}
		if OnComplete != nil {
			ref, err := pointsRef(s.ID, u.ID)
			if err != nil {
				return err
			}
			award = OnComplete(r.Context(), tx, u.ID, ref)
		}
		return nil
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_open", MsgNotOpen)
	case err != nil:
		univoice.Internal(w, r, "survey submit", err)
	case invalid != nil:
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid_answers", "message": pick(r, MsgInvalid), "detail": invalid.Error()}})
	case done:
		httpx.Error(w, r, http.StatusConflict, "already_answered", MsgDone)
	default:
		httpx.JSON(w, http.StatusCreated, map[string]any{"ok": true, "award": award})
	}
}

func pick(r *http.Request, m httpx.Msg) string {
	if httpx.Lang(r) == "ru" {
		return m.RU
	}
	return m.KK
}

// ---------- staff ----------

type input struct {
	University *string     `json:"university"` // admin only; staff are bound to theirs
	Title      *string     `json:"title"`
	Intro      *string     `json:"intro"`
	Questions  *[]Question `json:"questions"`
	Audience   *Audience   `json:"audience"`
	Anonymous  *bool       `json:"anonymous"`
	OpensAt    *time.Time  `json:"opensAt"`
	ClosesAt   *time.Time  `json:"closesAt"`
}

type cleaned struct {
	university, title, intro string
	questions                []Question
	audience                 Audience
	anonymous                bool
	opensAt, closesAt        *time.Time
}

func clean(in input, sc univoice.Scope) (cleaned, string) {
	var c cleaned
	var ok bool
	c.university = sc.University
	if sc.Admin {
		if in.University == nil {
			return c, "university"
		}
		if c.university, ok = univoice.Clean(*in.University, 1, 160, false); !ok {
			return c, "university"
		}
	}
	if in.Title == nil {
		return c, "title"
	}
	if c.title, ok = univoice.Clean(*in.Title, 3, 160, false); !ok {
		return c, "title"
	}
	if in.Intro != nil {
		if c.intro, ok = univoice.Clean(*in.Intro, 0, 1000, true); !ok {
			return c, "intro"
		}
	}
	if in.Questions == nil {
		return c, "questions"
	}
	var err error
	if c.questions, err = ValidateQuestions(*in.Questions); err != nil {
		return c, err.Error()
	}
	a := Audience{}
	if in.Audience != nil {
		a = *in.Audience
	}
	if c.audience, err = ValidateAudience(a); err != nil {
		return c, err.Error()
	}
	c.anonymous = in.Anonymous == nil || *in.Anonymous
	c.opensAt, c.closesAt = in.OpensAt, in.ClosesAt
	if c.opensAt != nil && c.closesAt != nil && !c.closesAt.After(*c.opensAt) {
		return c, "dates"
	}
	return c, ""
}

func badInput(w http.ResponseWriter, r *http.Request, field string) {
	httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid", "field": field,
		"message": pick(r, httpx.Msg{KK: "Өрісті тексеріңіз: " + field, RU: "Проверьте поле: " + field})}})
}

func staffList(w http.ResponseWriter, r *http.Request, u *auth.User) {
	sc, ok := univoice.StaffScope(w, r, u)
	if !ok {
		return
	}
	out := []*Survey{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select `+cols+` from surveys s where $1 or s.university = $2 order by s.id desc limit 200`, sc.Admin, sc.University)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Survey, error) { return scan(row) })
		return err
	})
	if err != nil {
		univoice.Internal(w, r, "staff surveys", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"surveys": out, "university": sc.University, "admin": sc.Admin})
}

func staffCreate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	sc, ok := univoice.StaffScope(w, r, u)
	if !ok {
		return
	}
	var in input
	if err := httpx.Decode(r, &in, 128<<10); err != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	c, field := clean(in, sc)
	if field != "" {
		badInput(w, r, field)
		return
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		univoice.Internal(w, r, "salt", err)
		return
	}
	var s *Survey
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		qs, _ := json.Marshal(c.questions)
		aud, _ := json.Marshal(c.audience)
		var id int64
		if err := tx.QueryRow(r.Context(), `insert into surveys (university, title, intro, questions, audience, anonymous, opens_at, closes_at, marker_salt, created_by)
			values ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, $8, $9, $10) returning id`,
			c.university, c.title, c.intro, string(qs), string(aud), c.anonymous, c.opensAt, c.closesAt, salt, u.ID).Scan(&id); err != nil {
			return err
		}
		var err error
		s, err = scan(tx.QueryRow(r.Context(), `select `+cols+` from surveys s where s.id = $1`, id))
		return err
	})
	if err != nil {
		univoice.Internal(w, r, "survey create", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"survey": s})
}

// own loads a survey the caller may manage, answering 404 otherwise (a foreign university's
// survey looks exactly like a missing one).
func own(w http.ResponseWriter, r *http.Request, u *auth.User, tx pgx.Tx, lock bool) (*Survey, bool) {
	sc, ok := univoice.StaffScope(w, r, u)
	if !ok {
		return nil, false
	}
	id, ok := univoice.PathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return nil, false
	}
	q := `select ` + cols + ` from surveys s where s.id = $1`
	if lock {
		q += ` for update`
	}
	s, err := scan(tx.QueryRow(r.Context(), q, id))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !sc.Allows(s.University)) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return nil, false
	}
	if err != nil {
		univoice.Internal(w, r, "survey own", err)
		return nil, false
	}
	return s, true
}

// withOwn runs fn on a managed survey inside one transaction; fn writes the response.
func withOwn(w http.ResponseWriter, r *http.Request, u *auth.User, lock bool, fn func(tx pgx.Tx, s *Survey) error) {
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		s, ok := own(w, r, u, tx, lock)
		if !ok {
			return nil
		}
		return fn(tx, s)
	})
	if err != nil {
		univoice.Internal(w, r, "survey staff", err)
	}
}

func staffGet(w http.ResponseWriter, r *http.Request, u *auth.User) {
	withOwn(w, r, u, false, func(_ pgx.Tx, s *Survey) error {
		httpx.JSON(w, http.StatusOK, map[string]any{"survey": s})
		return nil
	})
}

func staffUpdate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in input
	if err := httpx.Decode(r, &in, 128<<10); err != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	withOwn(w, r, u, true, func(tx pgx.Tx, s *Survey) error {
		if s.Status != "draft" {
			// a published survey may only get a new closing date
			if in.Questions != nil || in.Audience != nil || in.Anonymous != nil || in.Title != nil {
				httpx.Error(w, r, http.StatusConflict, "frozen", MsgFrozen)
				return nil
			}
			if in.ClosesAt == nil || (s.OpensAt != nil && !in.ClosesAt.After(*s.OpensAt)) {
				badInput(w, r, "closesAt")
				return nil
			}
			if _, err := tx.Exec(r.Context(), `update surveys set closes_at = $2, updated_at = now() where id = $1`, s.ID, in.ClosesAt); err != nil {
				return err
			}
		} else {
			sc := univoice.Scope{University: s.University}
			if in.University != nil && u.Role == "admin" {
				sc = univoice.Scope{Admin: true}
			}
			c, field := clean(in, sc)
			if field != "" {
				badInput(w, r, field)
				return nil
			}
			qs, _ := json.Marshal(c.questions)
			aud, _ := json.Marshal(c.audience)
			if _, err := tx.Exec(r.Context(), `update surveys set university = $2, title = $3, intro = $4, questions = $5::jsonb, audience = $6::jsonb,
				anonymous = $7, opens_at = $8, closes_at = $9, updated_at = now() where id = $1`,
				s.ID, c.university, c.title, c.intro, string(qs), string(aud), c.anonymous, c.opensAt, c.closesAt); err != nil {
				return err
			}
		}
		n, err := scan(tx.QueryRow(r.Context(), `select `+cols+` from surveys s where s.id = $1`, s.ID))
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"survey": n})
		return nil
	})
}

func staffPublish(w http.ResponseWriter, r *http.Request, u *auth.User) {
	withOwn(w, r, u, true, func(tx pgx.Tx, s *Survey) error {
		if s.Status != "draft" {
			httpx.Error(w, r, http.StatusConflict, "not_draft", MsgFrozen)
			return nil
		}
		if s.ClosesAt != nil && !s.ClosesAt.After(Now()) {
			badInput(w, r, "closesAt")
			return nil
		}
		if _, err := tx.Exec(r.Context(), `update surveys set status = 'published', published_at = $2, opens_at = coalesce(opens_at, $2), updated_at = now() where id = $1`, s.ID, Now()); err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return nil
	})
}

func staffClose(w http.ResponseWriter, r *http.Request, u *auth.User) {
	withOwn(w, r, u, true, func(tx pgx.Tx, s *Survey) error {
		if s.Status != "published" {
			httpx.Error(w, r, http.StatusConflict, "not_published", MsgNotOpen)
			return nil
		}
		if _, err := tx.Exec(r.Context(), `update surveys set status = 'closed', closes_at = least(coalesce(closes_at, $2), $2), updated_at = now() where id = $1`, s.ID, Now()); err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return nil
	})
}

func staffDelete(w http.ResponseWriter, r *http.Request, u *auth.User) {
	withOwn(w, r, u, true, func(tx pgx.Tx, s *Survey) error {
		if s.Status == "published" {
			httpx.Error(w, r, http.StatusConflict, "published", httpx.Msg{KK: "Алдымен сауалнаманы жабыңыз.", RU: "Сначала закройте опрос."})
			return nil
		}
		if _, err := tx.Exec(r.Context(), `delete from surveys where id = $1`, s.ID); err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return nil
	})
}

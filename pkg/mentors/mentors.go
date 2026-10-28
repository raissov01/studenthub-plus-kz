// Package mentors is prompt 07: mentor applications (through the shared moderation queue),
// mentoring requests with an honest waiting-list answer, manual matching by an admin or an
// ambassador of the same university, the pair lifecycle (week-3 check-in, first-month
// "continue?" question, end, feedback, "Ментор 🎓" badge) and safety reports.
//
// Limits live in Postgres: mentor_match() checks capacity, approval and the adults-only rule
// under a row lock. Reads that join other people's names run as the connection owner with
// explicit participant filters; a student's own edits run through db.WithUser (RLS).
package mentors

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

const (
	KindApplication = "mentor_application"
	KindReport      = "mentor_report"
	Feature         = "mentors"
)

// Needs are the four answers to "не керек?".
var Needs = []string{"adaptation", "study", "career", "life"}

// IsApprovedMentor reports whether userID is an approved (not paused, not removed) mentor.
// The events package (05) calls it to let mentors create `cat = mentoring` group meetings.
// tx may be an app_user transaction for that same user (RLS lets a mentor read their row).
func IsApprovedMentor(ctx context.Context, tx pgx.Tx, userID int64) bool {
	var ok bool
	err := tx.QueryRow(ctx, `select exists (select 1 from mentors where user_id = $1 and status = 'approved')`, userID).Scan(&ok)
	if err != nil {
		log.Printf("mentors: is approved %d: %v", userID, err)
		return false
	}
	return ok
}

// GroupMeeting is an upcoming `cat = mentoring` event, offered when no mentor is free.
type GroupMeeting struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	StartsAt string `json:"startsAt"`
	Place    string `json:"place"`
	Online   bool   `json:"online"`
	URL      string `json:"url"`
}

// GroupMeetings is set by the events package (05) to list the nearest open mentoring group
// meetings of a university. Until then the summary returns an empty list.
var GroupMeetings func(ctx context.Context, university string, limit int) ([]GroupMeeting, error)

// AmbassadorCanMatch is set by the ambassador package (06) to restrict the matching screen to
// team leads ("амбассадор-жетекші"). Until then any ambassador with a university may match,
// always only inside that university.
var AmbassadorCanMatch func(ctx context.Context, userID int64, university string) (bool, error)

func init() {
	moderation.OnApprove(KindApplication, approveApplication)
	moderation.OnReject(KindApplication, rejectApplication)
	moderation.OnApprove(KindReport, approveReport)
	moderation.OnReject(KindReport, rejectReport)

	me.RegisterExport("mentorProfile", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from mentors where user_id = app_user_id()")
	})
	me.RegisterExport("mentorRequests", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from mentor_requests where user_id = app_user_id() order by id")
	})
	me.RegisterExport("mentorPairs", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select id, mentor_id = app_user_id() as i_am_mentor, need, university, started_at,
			continued_at, ended_at, end_reason, mentee_score, mentor_score
			from mentor_pairs where mentor_id = app_user_id() or mentee_id = app_user_id() order by id`)
	})
	me.RegisterExport("mentorPrompts", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from mentor_pair_prompts where user_id = app_user_id() order by pair_id, kind")
	})
	me.RegisterExport("badges", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from user_badges where user_id = app_user_id() order by awarded_at")
	})
}

// Routes wires /api/mentors/*, /api/admin/mentors*, /api/ambassador/mentors* and the cron
// job. Everything answers 404 while FEATURE_MENTORS is off.
func Routes(mux *http.ServeMux) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	w := func(fn auth.Handler) http.HandlerFunc { return auth.Write(auth.Need(fn)) }

	// student / mentor
	h("GET /api/mentors/summary", summary)
	h("POST /api/mentors/requests", w(createRequest))
	h("POST /api/mentors/requests/close", w(closeRequest))
	h("POST /api/mentors/apply", w(apply))
	h("GET /api/mentors/me", auth.Need(mentorMe))
	h("PATCH /api/mentors/me", w(patchMentorMe))
	h("GET /api/mentors/pairs", auth.Need(myPairs))
	h("POST /api/mentors/pairs/{id}/checkin", w(checkin))
	h("POST /api/mentors/pairs/{id}/end", w(endPair))
	h("POST /api/mentors/pairs/{id}/score", w(scorePair))
	h("POST /api/mentors/pairs/{id}/report", w(reportPair))

	// matching screen: admin everywhere, ambassador only in their own university
	h("GET /api/admin/mentors", auth.NeedRole(adminScope(board), "admin"))
	h("GET /api/admin/mentors/requests/{id}/candidates", auth.NeedRole(adminScope(candidates), "admin"))
	h("POST /api/admin/mentors/match", auth.Write(auth.NeedRole(adminScope(match), "admin")))
	h("GET /api/ambassador/mentors", auth.NeedRole(ambassadorScope(board), "ambassador"))
	h("GET /api/ambassador/mentors/requests/{id}/candidates", auth.NeedRole(ambassadorScope(candidates), "ambassador"))
	h("POST /api/ambassador/mentors/match", auth.Write(auth.NeedRole(ambassadorScope(match), "ambassador")))

	// admin only
	h("GET /api/admin/mentors/pairs", auth.NeedRole(adminPairs, "admin"))
	h("POST /api/admin/mentors/pairs/{id}/end", auth.Write(auth.NeedRole(adminEndPair, "admin")))
	h("POST /api/admin/mentors/{userId}/status", auth.Write(auth.NeedRole(adminSetStatus, "admin")))

	h("GET /api/cron/mentors", cronHandler)
}

// ---- errors ----

var msgs = map[string]httpx.Msg{
	"consent_required":    {KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."},
	"university_required": {KK: "Профильде университетіңізді көрсетіңіз.", RU: "Укажите университет в профиле."},
	"request_exists":      {KK: "Сізде ашық сұраныс бар.", RU: "У вас уже есть открытая заявка."},
	"no_open_request":     {KK: "Күтіп тұрған сұраныс жоқ.", RU: "Нет ожидающей заявки."},
	"not_eligible":        {KK: "Ментор бола алатындар: 18-ге толған 2–4 курс студенттері мен түлектер.", RU: "Ментором могут стать совершеннолетние студенты 2–4 курса и выпускники."},
	"already_applied":     {KK: "Өтініміңіз тексерілуде.", RU: "Ваша заявка на проверке."},
	"already_mentor":      {KK: "Сіз қазірдің өзінде менторсыз.", RU: "Вы уже ментор."},
	"mentor_removed":      {KK: "Менторлық құқығыңыз алынған. Сұрақ болса — admin-ге жазыңыз.", RU: "Права ментора отозваны. Вопросы — администратору."},
	"not_editable":        {KK: "Профильді қазір өзгертуге болмайды.", RU: "Профиль сейчас нельзя изменить."},
	"pair_not_found":      {KK: "Жұп табылмады.", RU: "Пара не найдена."},
	"pair_ended":          {KK: "Бұл жұп аяқталған.", RU: "Эта пара уже завершена."},
	"pair_active":         {KK: "Бағаны жұп аяқталған соң қоюға болады.", RU: "Оценку можно поставить после завершения пары."},
	"too_early":           {KK: "Бұл сұрақтың уақыты әлі келген жоқ.", RU: "Время для этого вопроса ещё не пришло."},
	"request_not_found":   {KK: "Сұраныс табылмады.", RU: "Заявка не найдена."},
	"request_not_waiting": {KK: "Бұл сұраныс енді күтіп тұрған жоқ.", RU: "Эта заявка больше не ожидает."},
	"wrong_university":    {KK: "Тек өз университетіңізде жұптай аласыз.", RU: "Можно составлять пары только в своём университете."},
	"mentee_minor":        {KK: "18-ге толмаған студентке жеке ментор тағайындалмайды — тек топтық менторлық.", RU: "Несовершеннолетнему студенту личный ментор не назначается — только групповое менторство."},
	"group_only":          {KK: "Студент тек топтық менторлықты таңдаған.", RU: "Студент выбрал только групповое менторство."},
	"self_match":          {KK: "Өзін-өзіне ментор етуге болмайды.", RU: "Нельзя назначить ментором самого себя."},
	"mentor_not_approved": {KK: "Ментор бекітілмеген немесе уақытша тоқтаған.", RU: "Ментор не одобрен или на паузе."},
	"mentor_group_only":   {KK: "Бұл ментор тек топтық кездесулер өткізеді.", RU: "Этот ментор ведёт только групповые встречи."},
	"mentor_full":         {KK: "Ментордың бос орны жоқ.", RU: "У ментора нет свободных мест."},
	"mentee_has_mentor":   {KK: "Студентте белсенді ментор бар.", RU: "У студента уже есть активный ментор."},
	"no_university":       {KK: "Амбассадор профилінде университет көрсетілмеген.", RU: "В профиле амбассадора не указан университет."},
	"not_lead":            {KK: "Жұптауды тек амбассадор-жетекші жасайды.", RU: "Пары составляет только лидер амбассадоров."},
	"reason_required":     {KK: "Себебін жазыңыз (3–500 таңба).", RU: "Укажите причину (3–500 символов)."},
	"user_not_found":      {KK: "Мұндай қолданушы табылмады.", RU: "Пользователь не найден."},
	"not_a_mentor":        {KK: "Бұл қолданушы ментор емес.", RU: "Этот пользователь не ментор."},
}

func fail(w http.ResponseWriter, r *http.Request, code string, status int) {
	m, ok := msgs[code]
	if !ok {
		m = httpx.MsgBad
	}
	httpx.Error(w, r, status, code, m)
}

func internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("mentors: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func bad(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
}

// raised returns the code of a `raise exception '<code>'` from our SQL functions.
func raised(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "P0001" {
		return pe.Message
	}
	return ""
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

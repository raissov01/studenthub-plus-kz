// Package b2b is the employer and university cabinets (prompt 10): employer verification
// through the shared moderation queue, job posting (first 3 jobs through the queue), real
// applications with the fields the student chose, anti-ghosting (reminders, 14-day
// no_response, response-rate badges) and the university cabinet (official posts without
// moderation, aggregate-only statistics with the "< 5" rule).
//
// Everything is behind the "b2b" feature switch. API reference: docs/api/b2b.md.
package b2b

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// Feature is the switch name (FEATURE_B2B).
const Feature = "b2b"

// Kinds this package puts into the shared queue, and the report target kind.
const (
	KindVerification = "employer_verification"
	KindJob          = "employer_job"
	KindJobReport    = "job_report"
	TargetJob        = "job"
)

// Rules.
const (
	JobDays          = 30 // a job closes itself after 30 days (extendable)
	RemindAfterDays  = 5  // employer reminder when an application is not opened
	NoResponseDays   = 14 // unanswered application -> no_response
	MinForRate       = 5  // response stats appear from 5 applications
	LowRate          = 0.30
	QueueFirstJobs   = 3 // also hard-coded in jobs_insert_guard()
	InviteDays       = 7
	MaxLetter        = 300
	MaxInviteMessage = 500
)

// Notify is the direct-message sender; tests replace it.
var Notify = notify.Direct

// Digest is the one-per-day sender; tests replace it.
var Digest = notify.Digest

// Now is the clock; tests replace it.
var Now = time.Now

var msgs = map[string]httpx.Msg{
	"consent_required":      {KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."},
	"invalid":               {KK: "Деректер толық емес не қате.", RU: "Данные неполные или неверные."},
	"already_member":        {KK: "Сіз қазірдің өзінде бір компанияның мүшесісіз.", RU: "Вы уже состоите в компании."},
	"bin_taken":             {KK: "Бұл БСН-мен компания тіркелген не тексерілуде.", RU: "Компания с этим БИН уже зарегистрирована или на проверке."},
	"not_member":            {KK: "Бұл кабинет тек тексерілген компания мүшелеріне ашық.", RU: "Кабинет доступен только участникам проверенной компании."},
	"not_verified":          {KK: "Компания әлі тексерілмеген — вакансия жариялау тексеруден кейін ашылады.", RU: "Компания ещё не проверена — публикация вакансий откроется после проверки."},
	"not_owner":             {KK: "Бұл әрекет тек компания иесіне рұқсат.", RU: "Это действие доступно только владельцу компании."},
	"rules_required":        {KK: "Алдымен вакансия жариялау ережелерімен танысыңыз.", RU: "Сначала ознакомьтесь с правилами публикации вакансий."},
	"forbidden_terms":       {KK: "Алдын ала ақы, депозит, «курс сатып алу» сияқты талаптарға тыйым салынған.", RU: "Запрещены требования предоплаты, депозита, «покупки курса» и т. п."},
	"job_not_found":         {KK: "Вакансия табылмады.", RU: "Вакансия не найдена."},
	"job_status":            {KK: "Вакансия күйін бұлай өзгертуге болмайды.", RU: "Так изменить статус вакансии нельзя."},
	"minor":                 {KK: "Бұл вакансия 18 жасқа толмағандарға сай емес.", RU: "Эта вакансия не подходит для несовершеннолетних."},
	"already_applied":       {KK: "Сіз бұл вакансияға өтінім жіберіп қойғансыз.", RU: "Вы уже откликнулись на эту вакансию."},
	"own_company":           {KK: "Өз компанияңыздың вакансиясына өтінім жібере алмайсыз.", RU: "Нельзя откликнуться на вакансию своей компании."},
	"confirm_required":      {KK: "Деректерді компанияға жіберуді растаңыз.", RU: "Подтвердите отправку данных компании."},
	"nothing_shared":        {KK: "Кемінде бір өрісті таңдаңыз.", RU: "Выберите хотя бы одно поле."},
	"application_not_found": {KK: "Өтінім табылмады.", RU: "Отклик не найден."},
	"application_closed":    {KK: "Бұл өтінімнің күйін енді өзгертуге болмайды.", RU: "Статус этого отклика больше нельзя изменить."},
	"invite_not_found":      {KK: "Шақыру коды жарамсыз не мерзімі өткен.", RU: "Код приглашения недействителен или истёк."},
	"member_not_found":      {KK: "Мұндай мүше жоқ.", RU: "Такого участника нет."},
	"owner_must_transfer":   {KK: "Иесі кетер алдында компанияны басқа мүшеге беруі не тоқтатуы керек.", RU: "Перед уходом владелец должен передать компанию другому участнику или остановить её."},
	"own_job":               {KK: "Өз компанияңыздың вакансиясына шағым жасай алмайсыз.", RU: "Нельзя пожаловаться на вакансию своей компании."},
	"reason_required":       {KK: "Себебін жазыңыз (3–500 таңба).", RU: "Укажите причину (3–500 символов)."},
	"not_staff":             {KK: "Бұл кабинет тек университет қызметкерлеріне ашық.", RU: "Кабинет доступен только сотрудникам университета."},
	"post_not_found":        {KK: "Жазба табылмады.", RU: "Запись не найдена."},
	"user_not_found":        {KK: "Мұндай қолданушы табылмады.", RU: "Пользователь не найден."},
	"employer_not_found":    {KK: "Компания табылмады.", RU: "Компания не найдена."},
	"author_deleted":        {KK: "Өтінім иесінің аккаунты жоқ.", RU: "Аккаунт автора заявки удалён."},
	"employer_not_verified": {KK: "Компания тексерілмеген не тоқтатылған.", RU: "Компания не проверена или приостановлена."},
}

func fail(w http.ResponseWriter, r *http.Request, code string, status int) {
	m, ok := msgs[code]
	if !ok {
		m = httpx.MsgBad
	}
	httpx.Error(w, r, status, code, m)
}

func internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("b2b: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func bad(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
}

func refusal(code string) *moderation.Refusal {
	return &moderation.Refusal{Code: code, Msg: msgs[code]}
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func raised(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "P0001" {
		return pe.Message
	}
	return ""
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// cleanText trims and validates one free-text field; multiline allows "\n".
func cleanText(s string, min, max int, multiline bool) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.IndexFunc(s, func(c rune) bool { return unicode.IsControl(c) && !(multiline && c == '\n') }) >= 0 {
		return "", false
	}
	n := utf8.RuneCountInString(s)
	return s, n >= min && n <= max
}

// httpsURL: empty (when optional) or an https:// link without spaces / markup (04 rule).
func httpsURL(s string, required bool) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", !required
	}
	ok := strings.HasPrefix(strings.ToLower(s), "https://") && len(s) > 9 && len(s) <= 300 &&
		!strings.ContainsAny(s, " <>\"'`\n\t")
	return s, ok
}

func siteLink(path string) string { return config.SiteURL() + path }

func esc(s string) string { return html.EscapeString(s) }

func pick(lang, kk, ru string) string {
	if lang == "ru" && ru != "" {
		return ru
	}
	return kk
}

func init() {
	moderation.OnApprove(KindVerification, approveVerification)
	moderation.OnReject(KindVerification, rejectVerification)
	moderation.Notice(KindVerification, verificationNotice)
	moderation.OnApprove(KindJob, approveJob)
	moderation.OnReject(KindJob, rejectJob)
	moderation.Notice(KindJob, jobNotice)
	moderation.OnApprove(KindJobReport, upholdJobReport)
	moderation.OnReject(KindJobReport, dismissJobReport)
	moderation.OnReport(TargetJob, jobReportTarget())
	me.RegisterDeleteHook(handOverOnDelete)
	registerContent()
	registerExports()
}

func registerExports() {
	me.RegisterExport("jobApplications", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select a.id, a.job_id, j.title_kk as job_title, a.shared_fields, a.status, a.invite_message,
			a.created_at, a.updated_at, a.viewed_at, a.responded_at
			from applications a join jobs j on j.id = a.job_id where a.user_id = app_user_id() order by a.id`)
	})
	me.RegisterExport("employerMembership", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select m.employer_id, e.name, m.role, m.rules_accepted_at, m.created_at
			from employer_members m join employers e on e.id = m.employer_id where m.user_id = app_user_id()`)
	})
	me.RegisterExport("universityStaff", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select university, created_at from university_staff where user_id = app_user_id()`)
	})
}

// Routes wires /api/jobs*, /api/applications*, /api/employer*, /api/university*,
// /api/announcements, the admin endpoints and the cron job — all behind the "b2b" switch.
// Employer verification, the first 3 jobs and job reports are decided in the shared admin
// queue (GET /api/admin/queue, pkg/moderation).
func Routes(mux *http.ServeMux) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	w := func(fn auth.Handler) http.HandlerFunc { return auth.Write(auth.Need(fn)) }
	admin := func(fn auth.Handler) http.HandlerFunc { return auth.NeedRole(fn, "admin") }

	// students
	h("GET /api/jobs", listJobs)
	h("GET /api/jobs/{id}", jobDetail)
	h("POST /api/jobs/{id}/apply", w(apply))
	h("POST /api/jobs/{id}/report", w(reportJob))
	h("GET /api/applications/my", auth.Need(myApplications))
	h("POST /api/applications/{id}/withdraw", w(withdraw))

	// employers
	h("POST /api/employer/apply", w(employerApply))
	h("GET /api/employer/me", auth.Need(employerMe))
	h("PATCH /api/employer/me", w(employerPatch))
	h("POST /api/employer/rules/accept", w(acceptRules))
	h("GET /api/employer/members", auth.Need(members))
	h("POST /api/employer/invites", w(createInvite))
	h("POST /api/employer/join", w(join))
	h("DELETE /api/employer/members/{userId}", w(removeMember))
	h("POST /api/employer/leave", w(leave))
	h("POST /api/employer/transfer", w(transfer))
	h("POST /api/employer/close", w(closeCompany))
	h("GET /api/employer/jobs", auth.Need(employerJobs))
	h("POST /api/employer/jobs", w(createJob))
	h("PATCH /api/employer/jobs/{id}", w(editJob))
	h("POST /api/employer/jobs/{id}/close", w(closeJob))
	h("POST /api/employer/jobs/{id}/extend", w(extendJob))
	h("GET /api/employer/jobs/{id}/applications", auth.Need(jobApplications))
	h("POST /api/employer/applications/{id}/status", w(setStatus))

	// university staff
	h("GET /api/university/me", auth.Need(staffMe))
	h("GET /api/university/posts", auth.Need(staffPosts))
	h("POST /api/university/posts", w(createPost))
	h("PATCH /api/university/posts/{id}", w(editPost))
	h("DELETE /api/university/posts/{id}", w(deletePost))
	h("GET /api/university/stats", auth.Need(staffStats))
	h("GET /api/university/stats.csv", auth.Need(staffStatsCSV))
	h("GET /api/announcements", announcements)

	// admin
	h("GET /api/admin/employers", admin(adminEmployers))
	h("POST /api/admin/employers/{id}/suspend", auth.Write(admin(adminSuspend)))
	h("POST /api/admin/employers/{id}/restore", auth.Write(admin(adminRestore)))
	h("GET /api/admin/university-staff", admin(adminStaffList))
	h("POST /api/admin/university-staff", auth.Write(admin(adminStaffGrant)))
	h("DELETE /api/admin/university-staff/{userId}", auth.Write(admin(adminStaffRevoke)))

	// daily job (Bearer CRON_SECRET)
	h("GET /api/cron/b2b", cronHandler)
}

// userLang returns the user's UI language ("kk" when unknown).
func userLang(ctx context.Context, tx pgx.Tx, userID int64) string {
	lang := "kk"
	_ = tx.QueryRow(ctx, `select lang from users where id = $1`, userID).Scan(&lang)
	return lang
}

// tell sends a direct message (in the receiver's language) after the transaction; failures are logged.
func tell(ctx context.Context, userID int64, kk, ru string) {
	lang := "kk"
	if p, err := db.Pool(ctx); err == nil {
		_ = p.QueryRow(ctx, `select lang from users where id = $1`, userID).Scan(&lang)
	}
	if err := Notify(ctx, userID, pick(lang, kk, ru)); err != nil {
		log.Printf("b2b: notify %d: %v", userID, err)
	}
}

// memberIDs lists the live members of a company.
func memberIDs(ctx context.Context, tx pgx.Tx, employerID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `select m.user_id from employer_members m join users u on u.id = m.user_id and u.deleted_at is null
		where m.employer_id = $1 order by m.created_at`, employerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func adminIDs(ctx context.Context, tx pgx.Tx) ([]int64, error) {
	rows, err := tx.Query(ctx, `select id from users where role = 'admin' and deleted_at is null`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

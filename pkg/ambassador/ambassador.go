// Package ambassador is the university ambassador programme (prompt 06): applications reviewed
// through the shared moderation queue, terms with referral codes, the cabinet (own-university
// aggregate stats, submissions, team, workload pause), opportunity / place suggestions merged
// into /api/content after approval, the university team rating, certificates and term expiry.
//
// Everything is behind the "ambassador" feature switch. API reference: docs/api/ambassador.md.
package ambassador

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// Notify is the direct-message sender; tests replace it.
var Notify = notify.Direct

// Now is the clock; tests replace it.
var Now = time.Now

// Hours are the allowed answers to "how many hours a week" and their upper bound.
var Hours = map[string]int{"1-2": 2, "2-3": 3, "3-5": 5}

// TeamMax is lead + up to 3 members per university.
const TeamMax = 4

var (
	MsgConsent   = httpx.Msg{KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."}
	MsgApply     = httpx.Msg{KK: "Өтінім толық емес: университет, себеп (10–300 таңба) және сағат керек.", RU: "Заявка неполная: нужны университет, мотивация (10–300 символов) и часы."}
	MsgPending   = httpx.Msg{KK: "Сіздің өтініміңіз қаралып жатыр.", RU: "Ваша заявка уже на рассмотрении."}
	MsgAlready   = httpx.Msg{KK: "Сіз қазірдің өзінде амбассадорсыз.", RU: "Вы уже амбассадор."}
	MsgNotAmb    = httpx.Msg{KK: "Кабинет тек белсенді амбассадорларға ашық.", RU: "Кабинет доступен только действующим амбассадорам."}
	MsgForeign   = httpx.Msg{KK: "Тек өз университетіңізге қоса аласыз.", RU: "Добавлять можно только для своего университета."}
	MsgTeamFull  = httpx.Msg{KK: "Бұл университеттің командасы толық (1 жетекші + 3 мүше).", RU: "Команда этого университета укомплектована (1 лидер + 3 участника)."}
	MsgNoUser    = httpx.Msg{KK: "Өтінім иесінің аккаунты жоқ.", RU: "Аккаунт автора заявки удалён."}
	MsgNoCert    = httpx.Msg{KK: "Сертификат табылмады.", RU: "Сертификат не найден."}
	MsgNoTerm    = httpx.Msg{KK: "Мерзім табылмады.", RU: "Срок не найден."}
	MsgReason    = httpx.Msg{KK: "Себебін жазыңыз (3–500 таңба).", RU: "Укажите причину (3–500 символов)."}
	MsgSubmit    = httpx.Msg{KK: "Ұсыныс толық емес не қате.", RU: "Предложение неполное или неверное."}
	MsgNoUniv    = httpx.Msg{KK: "Профильде университетіңізді көрсетіңіз.", RU: "Укажите университет в профиле."}
	MsgRenewWait = httpx.Msg{KK: "Ұзарту өтініміңіз қаралып жатыр.", RU: "Заявка на продление уже на рассмотрении."}
)

// Kinds this package puts into the shared queue.
const (
	KindApplication = "ambassador_application"
	KindRenewal     = "ambassador_renewal"
	KindOpportunity = "opportunity"
	KindPlace       = "place"
)

func init() {
	moderation.OnApprove(KindApplication, approveApplication)
	moderation.OnReject(KindApplication, rejectApplication)
	moderation.OnApprove(KindRenewal, approveRenewal)
	moderation.OnApprove(KindOpportunity, publish(KindOpportunity))
	moderation.OnApprove(KindPlace, publish(KindPlace))
	moderation.Notice(KindApplication, applicationNotice)
	moderation.OnReport(KindOpportunity, reportTarget(KindOpportunity))
	moderation.OnReport(KindPlace, reportTarget(KindPlace))
	registerContent()
	registerExports()
}

// Routes wires /api/ambassador/*, /api/submissions*, /api/certificates/*, the admin endpoints
// for ambassadors and the shared queue / reports — all behind the "ambassador" switch.
func Routes(mux *http.ServeMux) {
	wrap := func(h http.Handler) http.Handler { return features.Require("ambassador", h) }
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, wrap(fn)) }
	h("POST /api/ambassador/apply", auth.Write(auth.Need(apply)))
	h("GET /api/ambassador/application", auth.Need(myApplication))
	h("GET /api/ambassador/me", auth.Need(cabinet))
	h("GET /api/ambassador/stats", auth.Need(stats))
	h("POST /api/ambassador/pause", auth.Write(auth.Need(pause)))
	h("DELETE /api/ambassador/pause", auth.Write(auth.Need(unpause)))
	h("POST /api/ambassador/renew", auth.Write(auth.Need(renew)))
	h("GET /api/ambassador/rating", rating)
	h("POST /api/submissions", auth.Write(auth.Need(submit)))
	h("GET /api/submissions/my", auth.Need(mySubmissions))
	h("GET /api/certificates/{code}", certificate)
	h("GET /api/admin/ambassadors", auth.NeedRole(adminList, "admin"))
	h("PATCH /api/admin/ambassadors/{id}", auth.Write(auth.NeedRole(adminPatch, "admin")))
	h("POST /api/admin/ambassadors/{id}/end", auth.Write(auth.NeedRole(adminEnd, "admin")))
	h("POST /api/admin/ambassadors/{id}/certificate", auth.Write(auth.NeedRole(adminCertificate, "admin")))
	h("POST /api/admin/ambassadors/expire", auth.Write(auth.NeedRole(adminExpire, "admin")))
	h("GET /api/cron/ambassador", cronHandler)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("ambassador: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

// AlmatyToday is the date in Asia/Almaty (UTC+5).
func AlmatyToday(t time.Time) time.Time {
	y, m, d := t.UTC().Add(5 * time.Hour).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// SemesterEnd: 1 Feb – 30 Jun -> 30 Jun; otherwise 31 Jan.
func SemesterEnd(day time.Time) time.Time {
	y, m := day.Year(), day.Month()
	switch {
	case m >= time.February && m <= time.June:
		return time.Date(y, time.June, 30, 0, 0, 0, 0, time.UTC)
	case m == time.January:
		return time.Date(y, time.January, 31, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(y+1, time.January, 31, 0, 0, 0, 0, time.UTC)
	}
}

// Term is an ambassador term.
type Term struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	University   string     `json:"university"`
	RoleInTeam   string     `json:"role_in_team"`
	TermStart    time.Time  `json:"term_start"`
	TermEnd      time.Time  `json:"term_end"`
	HoursPerWeek int        `json:"hours_per_week"`
	RefCode      string     `json:"ref_code"`
	Status       string     `json:"status"`
	PausedUntil  *time.Time `json:"paused_until"`
	RenewAnswer  *bool      `json:"renew_answer"`
	CertCode     *string    `json:"cert_code"`
	CertIssuedAt *time.Time `json:"cert_issued_at"`
}

const termCols = `id, user_id, university, role_in_team, term_start, term_end, hours_per_week, ref_code, status,
	paused_until, renew_answer, cert_code, cert_issued_at`

func scanTerm(row pgx.Row) (*Term, error) {
	var t Term
	err := row.Scan(&t.ID, &t.UserID, &t.University, &t.RoleInTeam, &t.TermStart, &t.TermEnd, &t.HoursPerWeek,
		&t.RefCode, &t.Status, &t.PausedUntil, &t.RenewAnswer, &t.CertCode, &t.CertIssuedAt)
	return &t, err
}

// ActiveTerm returns the user's current term: status active and not past term_end. An expired
// term the daily job has not closed yet does not count, so the cabinet closes on time.
func ActiveTerm(ctx context.Context, tx pgx.Tx, userID int64, now time.Time) (*Term, error) {
	t, err := scanTerm(tx.QueryRow(ctx, `select `+termCols+` from ambassador_terms
		where user_id = $1 and status = 'active' and term_end >= $2::date`, userID, AlmatyToday(now)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// needTerm resolves the caller's active term or answers 403.
func needTerm(w http.ResponseWriter, r *http.Request, u *auth.User) (*Term, bool) {
	if u.Role != "ambassador" && u.Role != "admin" {
		httpx.Error(w, r, http.StatusForbidden, "not_ambassador", MsgNotAmb)
		return nil, false
	}
	var t *Term
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		t, err = ActiveTerm(r.Context(), tx, u.ID, Now())
		return err
	})
	if err != nil {
		fail(w, r, "term", err)
		return nil, false
	}
	if t == nil {
		httpx.Error(w, r, http.StatusForbidden, "not_ambassador", MsgNotAmb)
		return nil, false
	}
	return t, true
}

const codeAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func randomCode(n int) string {
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b[i] = codeAlphabet[k.Int64()]
	}
	return string(b)
}

// refCode reuses the person's earlier code, else draws one nobody else has used.
func refCode(ctx context.Context, tx pgx.Tx, userID int64) (string, error) {
	var code string
	err := tx.QueryRow(ctx, `select ref_code from ambassador_terms where user_id = $1 order by id desc limit 1`, userID).Scan(&code)
	if err == nil {
		return code, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	for range 10 {
		code = randomCode(8)
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from ambassador_terms where ref_code = $1`, code).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return code, nil
		}
	}
	return "", errors.New("ambassador: could not draw a free ref code")
}

// RefLink is the personal invite link.
func RefLink(code string) string { return config.SiteURL() + "/?ref=amb_" + code }

// Referrals counts sign-ups attributed to the code (users.referred_by), not analytics.
func Referrals(ctx context.Context, tx pgx.Tx, code string, self int64) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `select count(*) from users where referred_by = 'amb_' || $1 and deleted_at is null and id <> $2`,
		code, self).Scan(&n)
	return n, err
}

// ---------- apply ----------

type application struct {
	ID           int64      `json:"id"`
	University   string     `json:"university"`
	Motivation   string     `json:"motivation"`
	HoursPerWeek string     `json:"hours_per_week"`
	Status       string     `json:"status"`
	Reason       *string    `json:"reason"`
	CreatedAt    time.Time  `json:"created_at"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
}

func apply(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body struct {
		University   string `json:"university"`
		Motivation   string `json:"motivation"`
		HoursPerWeek string `json:"hours_per_week"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_application", MsgApply)
		return
	}
	univ, motivation := strings.TrimSpace(body.University), strings.TrimSpace(body.Motivation)
	if univ == "" && u.University != nil {
		univ = *u.University
	}
	if n := utf8.RuneCountInString(univ); n < 2 || n > 120 {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_application", MsgApply)
		return
	}
	if n := utf8.RuneCountInString(motivation); n < 10 || n > 300 {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_application", MsgApply)
		return
	}
	if _, ok := Hours[body.HoursPerWeek]; !ok {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_application", MsgApply)
		return
	}
	var term *Term
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		term, err = ActiveTerm(ctx, tx, u.ID, Now())
		return err
	}); err != nil {
		fail(w, r, "apply", err)
		return
	}
	if term != nil {
		httpx.Error(w, r, http.StatusConflict, "already_ambassador", MsgAlready)
		return
	}
	// As app_user (RLS: own row, pending only); the queue item goes in the same transaction
	// (the queue links back with submissions.payload.application_id).
	var app application
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into ambassador_applications (user_id, university, motivation, hours_per_week)
			values (app_user_id(), $1, $2, $3) returning id, university, motivation, hours_per_week, status::text, created_at`,
			univ, motivation, body.HoursPerWeek).Scan(&app.ID, &app.University, &app.Motivation, &app.HoursPerWeek, &app.Status, &app.CreatedAt)
		if err != nil {
			return err
		}
		_, err = moderation.Submit(ctx, tx, KindApplication, map[string]any{
			"application_id": app.ID, "university": univ, "motivation": motivation, "hours_per_week": body.HoursPerWeek,
			"name": u.Name, "username": u.Username, "faculty": u.Faculty, "course": u.Course,
		}, &u.ID, univ)
		return err
	})
	if isUnique(err) {
		httpx.Error(w, r, http.StatusConflict, "already_pending", MsgPending)
		return
	}
	if err != nil {
		fail(w, r, "apply", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"application": app})
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func myApplication(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var app *application
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		var a application
		err := tx.QueryRow(r.Context(), `select id, university, motivation, hours_per_week, status::text, reason, created_at, reviewed_at
			from ambassador_applications where user_id = app_user_id() order by id desc limit 1`).
			Scan(&a.ID, &a.University, &a.Motivation, &a.HoursPerWeek, &a.Status, &a.Reason, &a.CreatedAt, &a.ReviewedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		app = &a
		return err
	})
	if err != nil {
		fail(w, r, "application", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"application": app})
}

// ---------- moderation hooks for applications / renewals ----------

func approveApplication(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p struct {
		ApplicationID int64 `json:"application_id"`
	}
	if err := json.Unmarshal(s.Payload, &p); err != nil || p.ApplicationID == 0 || s.SubmittedBy == nil {
		return &moderation.Refusal{Code: "author_deleted", Msg: MsgNoUser}
	}
	var userID int64
	var univ, hours, role string
	err := tx.QueryRow(ctx, `select a.user_id, a.university, a.hours_per_week, u.role::text from ambassador_applications a
		join users u on u.id = a.user_id and u.deleted_at is null where a.id = $1 for update of a`, p.ApplicationID).
		Scan(&userID, &univ, &hours, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return &moderation.Refusal{Code: "author_deleted", Msg: MsgNoUser}
	}
	if err != nil {
		return err
	}
	now := Now()
	if t, err := ActiveTerm(ctx, tx, userID, now); err != nil {
		return err
	} else if t != nil {
		return &moderation.Refusal{Code: "already_ambassador", Msg: MsgAlready}
	}
	// close a stale active term (past its end, not yet expired by the daily job)
	if _, err := tx.Exec(ctx, `update ambassador_terms set status = 'ended', ended_at = now() where user_id = $1 and status = 'active'`, userID); err != nil {
		return err
	}
	// serialize approvals per university, then check the team size and the lead slot
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('ambassador_team:' || $1))`, univ); err != nil {
		return err
	}
	var size, leads int
	if err := tx.QueryRow(ctx, `select count(*), count(*) filter (where role_in_team = 'lead') from ambassador_terms
		where university = $1 and status = 'active' and term_end >= $2::date`, univ, AlmatyToday(now)).Scan(&size, &leads); err != nil {
		return err
	}
	if size >= TeamMax {
		return &moderation.Refusal{Code: "team_full", Msg: MsgTeamFull}
	}
	roleInTeam := "member"
	if leads == 0 {
		roleInTeam = "lead"
	}
	code, err := refCode(ctx, tx, userID)
	if err != nil {
		return err
	}
	today := AlmatyToday(now)
	if _, err := tx.Exec(ctx, `insert into ambassador_terms (user_id, university, role_in_team, term_start, term_end, hours_per_week, ref_code, application_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`, userID, univ, roleInTeam, today, SemesterEnd(today), Hours[hours], code, p.ApplicationID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update ambassador_applications set status = 'approved', reviewed_at = now(), submission_id = $2
		where id = $1`, p.ApplicationID, s.ID); err != nil {
		return err
	}
	// The ambassador represents this university; admins keep their role.
	if _, err := tx.Exec(ctx, `update users set university = $2, role = case when role = 'admin' then role else 'ambassador'::user_role end
		where id = $1`, userID, univ); err != nil {
		return err
	}
	if role != "ambassador" && role != "admin" {
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			select reviewed_by, 'role_grant', $1, 'ambassador application approved', jsonb_build_object('from', $2::text, 'to', 'ambassador')
			from submissions where id = $3`, userID, role, s.ID)
	}
	return err
}

func rejectApplication(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p struct {
		ApplicationID int64 `json:"application_id"`
	}
	_ = json.Unmarshal(s.Payload, &p)
	_, err := tx.Exec(ctx, `update ambassador_applications set status = 'rejected', reason = $2, reviewed_at = now(), submission_id = $3
		where id = $1`, p.ApplicationID, s.Reason, s.ID)
	return err
}

func approveRenewal(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p struct {
		TermID int64 `json:"term_id"`
	}
	if err := json.Unmarshal(s.Payload, &p); err != nil || p.TermID == 0 {
		return &moderation.Refusal{Code: "term_not_found", Msg: MsgNoTerm}
	}
	var end time.Time
	err := tx.QueryRow(ctx, `select term_end from ambassador_terms where id = $1 and status = 'active' for update`, p.TermID).Scan(&end)
	if errors.Is(err, pgx.ErrNoRows) {
		return &moderation.Refusal{Code: "term_not_found", Msg: MsgNoTerm}
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `update ambassador_terms set term_end = $2, renew_answer = true, renew_asked_at = null where id = $1`,
		p.TermID, SemesterEnd(end.AddDate(0, 0, 1)))
	return err
}

func applicationNotice(s moderation.Submission, approved bool, lang string) string {
	if !approved {
		return "" // generic message with the reason
	}
	if lang == "ru" {
		return "🎉 Ваша заявка одобрена — теперь вы амбассадор Student Hub!\nКабинет: " + config.SiteURL() + "/#/ambassador"
	}
	return "🎉 Өтініміңіз бекітілді — енді сіз Student Hub амбассадорысыз!\nКабинет: " + config.SiteURL() + "/#/ambassador"
}

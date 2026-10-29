package b2b

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// ShareInput is what the student chose to send. Account fields (name, course + faculty,
// Telegram username) are copied from the account only when ticked; skills, experience and the
// phone come from the device (they are not stored on the server otherwise).
type ShareInput struct {
	Name       bool         `json:"name"`
	Course     bool         `json:"course"` // course and specialty (faculty)
	Telegram   bool         `json:"telegram"`
	Skills     []string     `json:"skills"`
	Experience []Experience `json:"experience"`
	Phone      string       `json:"phone"` // optional, only into this application
}

type Experience struct {
	Title  string `json:"title"`
	Org    string `json:"org"`
	Period string `json:"period"`
}

// SharedFields is the snapshot the employer receives — nothing else about the student.
func SharedFields(u *auth.User, in ShareInput, letter string) (map[string]any, string) {
	out := map[string]any{}
	if in.Name && u.Name != nil && *u.Name != "" {
		out["name"] = *u.Name
	}
	if in.Course {
		if u.Course != nil {
			out["course"] = *u.Course
		}
		if u.Faculty != nil && *u.Faculty != "" {
			out["specialty"] = *u.Faculty
		}
	}
	if in.Telegram && u.Username != nil && *u.Username != "" {
		out["telegram"] = "@" + *u.Username
		out["telegramUrl"] = "https://t.me/" + *u.Username
	}
	if len(in.Skills) > 15 || len(in.Experience) > 10 {
		return nil, "invalid"
	}
	if len(in.Skills) > 0 {
		skills := []string{}
		for _, s := range in.Skills {
			s, ok := cleanText(s, 1, 40, false)
			if !ok {
				return nil, "invalid"
			}
			skills = append(skills, s)
		}
		out["skills"] = skills
	}
	if len(in.Experience) > 0 {
		exp := []Experience{}
		for _, e := range in.Experience {
			var ok1, ok2, ok3 bool
			e.Title, ok1 = cleanText(e.Title, 1, 120, false)
			e.Org, ok2 = cleanText(e.Org, 0, 120, false)
			e.Period, ok3 = cleanText(e.Period, 0, 40, false)
			if !ok1 || !ok2 || !ok3 {
				return nil, "invalid"
			}
			exp = append(exp, e)
		}
		out["experience"] = exp
	}
	if p, ok := cleanText(in.Phone, 0, 20, false); !ok || (p != "" && !rePhone.MatchString(p)) {
		return nil, "invalid"
	} else if p != "" {
		out["phone"] = p
	}
	if l, ok := cleanText(letter, 0, MaxLetter, true); !ok {
		return nil, "invalid"
	} else if l != "" {
		out["letter"] = l
	}
	if len(out) == 0 {
		return nil, "nothing_shared"
	}
	return out, ""
}

// apply: POST /api/jobs/{id}/apply {"share":{...},"letter":"...","confirm":true}
func apply(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	var body struct {
		Share   ShareInput `json:"share"`
		Letter  string     `json:"letter"`
		Confirm bool       `json:"confirm"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	fields, code := SharedFields(u, body.Share, body.Letter)
	if code != "" {
		fail(w, r, code, http.StatusBadRequest)
		return
	}
	if !body.Confirm {
		fail(w, r, "confirm_required", http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(fields)
	var result string
	var appID *int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select result, application_id from job_apply($1, $2::jsonb)`, id, string(raw)).Scan(&result, &appID)
	})
	if err != nil {
		internal(w, r, "apply", err)
		return
	}
	switch result {
	case "ok":
	case "not_found":
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	case "minor":
		fail(w, r, "minor", http.StatusForbidden)
		return
	case "already":
		fail(w, r, "already_applied", http.StatusConflict)
		return
	case "own_company":
		fail(w, r, "own_company", http.StatusBadRequest)
		return
	default:
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	var company, title string
	var members []int64
	_ = db.Tx(ctx, func(tx pgx.Tx) error {
		var eid int64
		if err := tx.QueryRow(ctx, `select e.id, e.name, j.title_kk from jobs j join employers e on e.id = j.employer_id where j.id = $1`, id).
			Scan(&eid, &company, &title); err != nil {
			return err
		}
		var err error
		members, err = memberIDs(ctx, tx, eid)
		return err
	})
	for _, m := range members {
		tell(notify.WithKey(context.WithoutCancel(ctx), fmt.Sprintf("application:%d:new", *appID)), m, "📩 Жаңа өтінім: «"+esc(title)+"». Кабинет: "+siteLink("/employer"),
			"📩 Новый отклик: «"+esc(title)+"». Кабинет: "+siteLink("/employer"))
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"application": map[string]any{"id": *appID, "status": "sent", "sharedFields": fields},
		"company": company})
}

// MyApplication is a student's own application.
type MyApplication struct {
	ID            int64           `json:"id"`
	JobID         string          `json:"jobId"` // "job-<id>", same as /api/content
	Title         string          `json:"title"`
	TitleRU       string          `json:"titleRu"`
	Company       string          `json:"company"`
	Status        string          `json:"status"`
	InviteMessage *string         `json:"inviteMessage"`
	SharedFields  json.RawMessage `json:"sharedFields"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

func myApplicationList(ctx context.Context, userID, jobID int64) ([]MyApplication, error) {
	var out []MyApplication
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select a.id, 'job-' || a.job_id, j.title_kk, j.title_ru, e.name, a.status, a.invite_message,
				a.shared_fields, a.created_at, a.updated_at
			from applications a join jobs j on j.id = a.job_id join employers e on e.id = a.employer_id
			where a.user_id = $1 and ($2 = 0 or a.job_id = $2) order by a.created_at desc limit 200`, userID, jobID)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (MyApplication, error) {
			var a MyApplication
			return a, row.Scan(&a.ID, &a.JobID, &a.Title, &a.TitleRU, &a.Company, &a.Status, &a.InviteMessage, &a.SharedFields, &a.CreatedAt, &a.UpdatedAt)
		})
		return err
	})
	if out == nil {
		out = []MyApplication{}
	}
	return out, err
}

// myApplications: GET /api/applications/my — «Менің өтінімдерім» with real statuses.
func myApplications(w http.ResponseWriter, r *http.Request, u *auth.User) {
	list, err := myApplicationList(r.Context(), u.ID, 0)
	if err != nil {
		internal(w, r, "my applications", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"applications": list})
}

// withdraw: POST /api/applications/{id}/withdraw — as app_user (RLS: own row only); the shared
// copy is erased, the employer keeps only "withdrawn".
func withdraw(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "application_not_found", http.StatusNotFound)
		return
	}
	code := ""
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var st string
		err := tx.QueryRow(ctx, `select status from applications where id = $1 and user_id = app_user_id()`, id).Scan(&st)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "application_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `update applications set status = 'withdrawn', shared_fields = '{}'::jsonb
			where id = $1 and user_id = app_user_id() and status in ('sent', 'viewed', 'invited')`, id)
		if err == nil && tag.RowsAffected() == 0 {
			code = "application_closed"
		}
		return err
	})
	switch {
	case err != nil:
		internal(w, r, "withdraw", err)
	case code == "application_not_found":
		fail(w, r, code, http.StatusNotFound)
	case code != "":
		fail(w, r, code, http.StatusConflict)
	default:
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "withdrawn"})
	}
}

// EmployerApplication is what a company sees: the shared copy and the status, nothing else
// (no user id, no profile, no other applications of the student).
type EmployerApplication struct {
	ID            int64           `json:"id"`
	Status        string          `json:"status"`
	SharedFields  json.RawMessage `json:"sharedFields"`
	InviteMessage *string         `json:"inviteMessage"`
	CreatedAt     time.Time       `json:"createdAt"`
	ViewedAt      *time.Time      `json:"viewedAt"`
	RespondedAt   *time.Time      `json:"respondedAt"`
	DaysWaiting   int             `json:"daysWaiting"`
}

const employerAppCols = `id, status, shared_fields, invite_message, created_at, viewed_at, responded_at`

func scanEmployerApp(row pgx.Row) (EmployerApplication, error) {
	var a EmployerApplication
	err := row.Scan(&a.ID, &a.Status, &a.SharedFields, &a.InviteMessage, &a.CreatedAt, &a.ViewedAt, &a.RespondedAt)
	a.DaysWaiting = int(Now().Sub(a.CreatedAt).Hours() / 24)
	return a, err
}

// jobApplications: GET /api/employer/jobs/{id}/applications — as app_user (RLS: the caller's
// verified company only).
func jobApplications(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	if _, ok := needMember(w, r, u, true, false); !ok {
		return
	}
	var out []EmployerApplication
	found := false
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select exists (select 1 from jobs where id = $1)`, id).Scan(&found); err != nil || !found {
			return err
		}
		rows, err := tx.Query(ctx, `select `+employerAppCols+` from applications where job_id = $1 order by created_at desc`, id)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (EmployerApplication, error) { return scanEmployerApp(row) })
		return err
	})
	if err != nil {
		internal(w, r, "job applications", err)
		return
	}
	if !found {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	if out == nil {
		out = []EmployerApplication{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"applications": out})
}

var employerStatuses = map[string]bool{"viewed": true, "invited": true, "rejected": true}

// setStatus: POST /api/employer/applications/{id}/status {"status":"viewed|invited|rejected","message"}
// — as app_user; the student gets a direct message on invited / rejected.
func setStatus(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	var body struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || !ok || !employerStatuses[body.Status] {
		bad(w, r)
		return
	}
	msg, mok := cleanText(body.Message, 0, MaxInviteMessage, true)
	if !mok {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	if _, ok := needMember(w, r, u, true, false); !ok {
		return
	}
	code := ""
	var a EmployerApplication
	var student int64
	var title, company string
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var st string
		err := tx.QueryRow(ctx, `select status from applications where id = $1 and app_employer_active(employer_id)`, id).Scan(&st)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "application_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		if st != "sent" && st != "viewed" && st != "invited" {
			code = "application_closed"
			return nil
		}
		var invite *string
		if msg != "" && body.Status != "viewed" {
			invite = &msg
		}
		a, err = scanEmployerApp(tx.QueryRow(ctx, `update applications set status = case when $2 = 'viewed' and status <> 'sent' then status else $2 end,
				viewed_at = coalesce(viewed_at, now()),
				responded_at = case when $2 in ('invited', 'rejected') then now() else responded_at end,
				invite_message = coalesce($3, invite_message)
			where id = $1 returning `+employerAppCols, id, body.Status, invite))
		if errors.Is(err, pgx.ErrNoRows) {
			code = "application_closed"
			return nil
		}
		return err
	})
	if err != nil {
		internal(w, r, "status", err)
		return
	}
	switch code {
	case "application_not_found":
		fail(w, r, code, http.StatusNotFound)
		return
	case "application_closed":
		fail(w, r, code, http.StatusConflict)
		return
	}
	if body.Status != "viewed" {
		_ = db.Tx(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select a.user_id, j.title_kk, e.name from applications a join jobs j on j.id = a.job_id
				join employers e on e.id = a.employer_id where a.id = $1`, id).Scan(&student, &title, &company)
		})
		if student != 0 {
			extra := ""
			if msg != "" {
				extra = "\n\n" + esc(msg)
			}
			if body.Status == "invited" {
				tell(notify.WithKey(context.WithoutCancel(ctx), fmt.Sprintf("application:%d:%s", id, body.Status)), student,
					"🎉 «"+esc(company)+"» сізді «"+esc(title)+"» вакансиясы бойынша шақырды."+extra+"\n"+siteLink("/mine"),
					"🎉 «"+esc(company)+"» пригласила вас по вакансии «"+esc(title)+"»."+extra+"\n"+siteLink("/mine"))
			} else {
				tell(notify.WithKey(context.WithoutCancel(ctx), fmt.Sprintf("application:%d:%s", id, body.Status)), student,
					"«"+esc(company)+"» «"+esc(title)+"» бойынша бас тартты. Басқа вакансияларды қараңыз: "+siteLink("/jobs")+extra,
					"«"+esc(company)+"» отказала по вакансии «"+esc(title)+"». Посмотрите другие вакансии: "+siteLink("/jobs")+extra)
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"application": a})
}

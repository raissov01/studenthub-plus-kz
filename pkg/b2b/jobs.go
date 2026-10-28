package b2b

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

func itoa(n int) string { return strconv.Itoa(n) }

// JobIDPrefix marks database jobs in /api/content ("job-<jobs.id>").
const JobIDPrefix = "job-"

// JobTags are the existing JOB_TAGS filters.
var JobTags = []string{"noexp", "daily", "remote", "near"}

// ForbiddenTerms: upfront fees, deposits, "buy a course" — refused before publishing (kk / ru).
var ForbiddenTerms = []string{
	"предоплат", "депозит", "залог", "взнос", "оплатите курс", "оплата курса", "купить курс", "купите курс",
	"оплата за обучение", "платное обучение", "алдын ала төлем", "алдын ала ақы", "кепіл ақы", "курс сатып",
	"курсты сатып", "оқу ақысы", "жарна",
}

// ContainsForbidden reports whether any text asks for money upfront.
func ContainsForbidden(texts ...string) bool {
	for _, t := range texts {
		l := strings.ToLower(t)
		for _, w := range ForbiddenTerms {
			if strings.Contains(l, w) {
				return true
			}
		}
	}
	return false
}

// JobInput is the body of POST / PATCH /api/employer/jobs.
type JobInput struct {
	TitleKK     string       `json:"titleKk"`
	TitleRU     string       `json:"titleRu"`
	DescKK      string       `json:"descKk"`
	DescRU      string       `json:"descRu"`
	City        string       `json:"city"`
	Remote      bool         `json:"remote"`
	Address     string       `json:"address"`
	Pay         float64      `json:"pay"`
	Per         string       `json:"per"`
	Hours       *float64     `json:"hours"`
	SchedKK     string       `json:"schedKk"`
	SchedRU     string       `json:"schedRu"`
	Tags        []string     `json:"tags"`
	Skills      []string     `json:"skills"`
	Spec        []string     `json:"spec"`
	Req         []content.Bi `json:"req"`
	ApplyKK     string       `json:"applyKk"`
	ApplyRU     string       `json:"applyRu"`
	MinorOK     *bool        `json:"minorOk"` // required: "18-ге толмағандарға сай ма?"
	AcceptRules bool         `json:"acceptRules"`
}

// Clean validates and normalises the input; it returns the error code or "".
func (in *JobInput) Clean() string {
	var ok [8]bool
	in.TitleKK, ok[0] = cleanText(in.TitleKK, 3, 120, false)
	in.TitleRU, ok[1] = cleanText(in.TitleRU, 0, 120, false)
	in.DescKK, ok[2] = cleanText(in.DescKK, 20, 3000, true)
	in.DescRU, ok[3] = cleanText(in.DescRU, 0, 3000, true)
	in.SchedKK, ok[4] = cleanText(in.SchedKK, 3, 200, false)
	in.SchedRU, ok[5] = cleanText(in.SchedRU, 0, 200, false)
	in.ApplyKK, ok[6] = cleanText(in.ApplyKK, 3, 500, true)
	in.ApplyRU, ok[7] = cleanText(in.ApplyRU, 0, 500, true)
	for _, o := range ok {
		if !o {
			return "invalid"
		}
	}
	var aok bool
	in.Address, aok = cleanText(in.Address, 0, 200, false)
	if !aok || (!in.Remote && len([]rune(in.Address)) < 3) {
		return "invalid"
	}
	in.City = strings.TrimSpace(in.City)
	if in.Remote && in.City == "" {
		in.City = "remote"
	}
	if !slices.Contains(content.Cities, in.City) || in.MinorOK == nil {
		return "invalid"
	}
	if !slices.Contains(content.JobPer, in.Per) || in.Pay <= 0 || in.Pay > 1e9 || in.Pay != math.Trunc(in.Pay) {
		return "invalid"
	}
	if in.Hours != nil && (*in.Hours <= 0 || *in.Hours > 168) {
		return "invalid"
	}
	if in.Per == "shift" && in.Hours == nil {
		return "invalid"
	}
	tags := []string{}
	for _, t := range in.Tags {
		if !slices.Contains(JobTags, t) {
			return "invalid"
		}
		if !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	if in.Remote && !slices.Contains(tags, "remote") {
		tags = append(tags, "remote")
	}
	in.Tags = tags
	clean := func(list []string, max, maxLen int, def []string) ([]string, bool) {
		out := []string{}
		for _, s := range list {
			s, ok := cleanText(s, 1, maxLen, false)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		if len(out) > max {
			return nil, false
		}
		if len(out) == 0 && def != nil {
			out = def
		}
		return out, true
	}
	var sok, pok bool
	if in.Skills, sok = clean(in.Skills, 10, 40, nil); !sok {
		return "invalid"
	}
	if in.Spec, pok = clean(in.Spec, 10, 40, []string{"all"}); !pok {
		return "invalid"
	}
	if len(in.Req) > 10 {
		return "invalid"
	}
	req := []content.Bi{}
	for _, b := range in.Req {
		kk, ok1 := cleanText(b.KK, 1, 200, false)
		ru, ok2 := cleanText(b.RU, 0, 200, false)
		if !ok1 || !ok2 {
			return "invalid"
		}
		req = append(req, content.Bi{KK: kk, RU: ru})
	}
	in.Req = req
	texts := []string{in.TitleKK, in.TitleRU, in.DescKK, in.DescRU, in.ApplyKK, in.ApplyRU, in.SchedKK, in.SchedRU}
	for _, b := range in.Req {
		texts = append(texts, b.KK, b.RU)
	}
	if ContainsForbidden(texts...) {
		return "forbidden_terms"
	}
	return ""
}

// Job is the employer's view of a job.
type Job struct {
	ID           int64          `json:"id"`
	TitleKK      string         `json:"titleKk"`
	TitleRU      string         `json:"titleRu"`
	DescKK       string         `json:"descKk"`
	DescRU       string         `json:"descRu"`
	City         string         `json:"city"`
	Remote       bool           `json:"remote"`
	Address      string         `json:"address"`
	Pay          float64        `json:"pay"`
	Per          string         `json:"per"`
	Hours        *float64       `json:"hours"`
	SchedKK      string         `json:"schedKk"`
	SchedRU      string         `json:"schedRu"`
	Tags         []string       `json:"tags"`
	Skills       []string       `json:"skills"`
	Spec         []string       `json:"spec"`
	Req          []content.Bi   `json:"req"`
	ApplyKK      string         `json:"applyKk"`
	ApplyRU      string         `json:"applyRu"`
	MinorOK      bool           `json:"minorOk"`
	Status       string         `json:"status"`
	HiddenReason *string        `json:"hiddenReason"`
	StatusReason *string        `json:"statusReason"`
	PublishedAt  *time.Time     `json:"publishedAt"`
	ExpiresAt    *time.Time     `json:"expiresAt"`
	CreatedAt    time.Time      `json:"createdAt"`
	Applications map[string]int `json:"applications,omitempty"`
	employerID   int64
}

const jobCols = `j.id, j.title_kk, j.title_ru, j.desc_kk, j.desc_ru, j.city, j.remote, j.address, j.pay::float8, j.per,
	j.hours::float8, j.sched_kk, j.sched_ru, j.tags, j.skills, j.spec, j.req, j.apply_kk, j.apply_ru, j.minor_ok, j.status,
	j.hidden_reason, j.status_reason, j.published_at, j.expires_at, j.created_at, j.employer_id`

func scanJob(row pgx.Row) (*Job, error) {
	var j Job
	var req []byte
	err := row.Scan(&j.ID, &j.TitleKK, &j.TitleRU, &j.DescKK, &j.DescRU, &j.City, &j.Remote, &j.Address, &j.Pay, &j.Per,
		&j.Hours, &j.SchedKK, &j.SchedRU, &j.Tags, &j.Skills, &j.Spec, &req, &j.ApplyKK, &j.ApplyRU, &j.MinorOK, &j.Status,
		&j.HiddenReason, &j.StatusReason, &j.PublishedAt, &j.ExpiresAt, &j.CreatedAt, &j.employerID)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(req, &j.Req)
	if j.Req == nil {
		j.Req = []content.Bi{}
	}
	return &j, nil
}

func reqJSON(req []content.Bi) string { b, _ := json.Marshal(req); return string(b) }

// ---------- employer side ----------

// createJob: POST /api/employer/jobs — member of a verified company. The first 3 jobs of the
// company wait in the queue (employer_job), later ones are published at once (jobs_insert_guard).
func createJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	m, ok := needMember(w, r, u, true, false)
	if !ok {
		return
	}
	var in JobInput
	if err := httpx.Decode(r, &in, 32<<10); err != nil {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	if code := in.Clean(); code != "" {
		fail(w, r, code, http.StatusBadRequest)
		return
	}
	if !m.RulesAccepted && !in.AcceptRules {
		fail(w, r, "rules_required", http.StatusForbidden)
		return
	}
	var j *Job
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if !m.RulesAccepted {
			if _, err := tx.Exec(ctx, `update employer_members set rules_accepted_at = now() where user_id = $1`, u.ID); err != nil {
				return err
			}
		}
		var id int64
		err := tx.QueryRow(ctx, `insert into jobs (employer_id, created_by, title_kk, title_ru, desc_kk, desc_ru, city, remote, address,
				pay, per, hours, sched_kk, sched_ru, tags, skills, spec, req, apply_kk, apply_ru, minor_ok)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::jsonb, $19, $20, $21) returning id`,
			m.EmployerID, u.ID, in.TitleKK, in.TitleRU, in.DescKK, in.DescRU, in.City, in.Remote, in.Address, in.Pay, in.Per, in.Hours,
			in.SchedKK, in.SchedRU, in.Tags, in.Skills, in.Spec, reqJSON(in.Req), in.ApplyKK, in.ApplyRU, *in.MinorOK).Scan(&id)
		if err != nil {
			return err
		}
		if j, err = scanJob(tx.QueryRow(ctx, `select `+jobCols+` from jobs j where j.id = $1`, id)); err != nil {
			return err
		}
		if j.Status != "pending" {
			return nil
		}
		payload := map[string]any{"job_id": id, "employer_id": m.EmployerID, "employer_name": m.Name}
		raw, _ := json.Marshal(in)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		delete(fields, "acceptRules")
		payload["job"] = fields
		sid, err := moderation.Submit(ctx, tx, KindJob, payload, &u.ID, "")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update jobs set submission_id = $2 where id = $1`, id, sid)
		return err
	})
	if err != nil {
		internal(w, r, "create job", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"job": j, "pending": j.Status == "pending"})
}

// employerJob loads one job of the caller's company as app_user (RLS: own company only).
func employerJob(ctx context.Context, tx pgx.Tx, id int64) (*Job, error) {
	return scanJob(tx.QueryRow(ctx, `select `+jobCols+` from jobs j where j.id = $1`, id))
}

// editJob: PATCH /api/employer/jobs/{id} — full replacement of the fields (same body as create).
// Pending (in the queue) and rejected jobs cannot be edited.
func editJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	if _, ok := needMember(w, r, u, true, false); !ok {
		return
	}
	var in JobInput
	if err := httpx.Decode(r, &in, 32<<10); err != nil {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	if code := in.Clean(); code != "" {
		fail(w, r, code, http.StatusBadRequest)
		return
	}
	code := ""
	var j *Job
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		cur, err := employerJob(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "job_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		if cur.Status == "pending" || cur.Status == "rejected" || cur.Status == "hidden" {
			code = "job_status"
			return nil
		}
		_, err = tx.Exec(ctx, `update jobs set title_kk = $2, title_ru = $3, desc_kk = $4, desc_ru = $5, city = $6, remote = $7,
				address = $8, pay = $9, per = $10, hours = $11, sched_kk = $12, sched_ru = $13, tags = $14, skills = $15, spec = $16,
				req = $17::jsonb, apply_kk = $18, apply_ru = $19, minor_ok = $20
			where id = $1`, id, in.TitleKK, in.TitleRU, in.DescKK, in.DescRU, in.City, in.Remote, in.Address, in.Pay, in.Per, in.Hours,
			in.SchedKK, in.SchedRU, in.Tags, in.Skills, in.Spec, reqJSON(in.Req), in.ApplyKK, in.ApplyRU, *in.MinorOK)
		if err != nil {
			return err
		}
		j, err = employerJob(ctx, tx, id)
		return err
	})
	jobAnswer(w, r, "edit job", code, err, j)
}

func jobAnswer(w http.ResponseWriter, r *http.Request, what, code string, err error, j *Job) {
	if raised(err) == "job_status" {
		code, err = "job_status", nil
	}
	switch {
	case err != nil:
		internal(w, r, what, err)
	case code == "job_not_found":
		fail(w, r, code, http.StatusNotFound)
	case code != "":
		fail(w, r, code, http.StatusConflict)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
	}
}

// jobStatusChange runs a status update as app_user (jobs_owner_guard enforces the transitions).
func jobStatusChange(w http.ResponseWriter, r *http.Request, u *auth.User, what string, set func(j *Job) string) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	if _, ok := needMember(w, r, u, true, false); !ok {
		return
	}
	code := ""
	var j *Job
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		cur, err := employerJob(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "job_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		q := set(cur)
		if q == "" {
			code = "job_status"
			return nil
		}
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return err
		}
		j, err = employerJob(ctx, tx, id)
		return err
	})
	jobAnswer(w, r, what, code, err, j)
}

// closeJob: POST /api/employer/jobs/{id}/close
func closeJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	jobStatusChange(w, r, u, "close job", func(j *Job) string {
		return `update jobs set status = 'closed' where id = $1`
	})
}

// extendJob: POST /api/employer/jobs/{id}/extend — a published job runs 30 more days from now;
// a closed / expired job that was approved before is reopened for 30 days.
func extendJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	jobStatusChange(w, r, u, "extend job", func(j *Job) string {
		switch j.Status {
		case "published":
			return `update jobs set expires_at = now() + interval '30 days' where id = $1`
		case "closed", "expired":
			return `update jobs set status = 'published' where id = $1`
		}
		return ""
	})
}

// employerJobs: GET /api/employer/jobs — every job of my company with application counts.
func employerJobs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if _, ok := needMember(w, r, u, false, false); !ok {
		return
	}
	var out []*Job
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+jobCols+` from jobs j order by j.created_at desc limit 500`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Job, error) { return scanJob(row) })
		if err != nil {
			return err
		}
		idx := map[int64]*Job{}
		for _, j := range out {
			j.Applications = map[string]int{}
			idx[j.ID] = j
		}
		rows, err = tx.Query(ctx, `select job_id, status, count(*) from applications group by job_id, status`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var st string
			var n int
			if err := rows.Scan(&id, &st, &n); err != nil {
				return err
			}
			if j := idx[id]; j != nil {
				j.Applications[st] = n
			}
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "employer jobs", err)
		return
	}
	if out == nil {
		out = []*Job{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"jobs": out})
}

// ---------- queue hooks ----------

type jobRef struct {
	JobID int64 `json:"job_id"`
}

func approveJob(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p jobRef
	if err := json.Unmarshal(s.Payload, &p); err != nil || p.JobID == 0 {
		return refusal("job_not_found")
	}
	var status, estatus string
	err := tx.QueryRow(ctx, `select j.status, e.status from jobs j join employers e on e.id = j.employer_id where j.id = $1 for update of j`, p.JobID).
		Scan(&status, &estatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return refusal("job_not_found")
	}
	if err != nil {
		return err
	}
	if estatus != "verified" {
		return refusal("employer_not_verified")
	}
	if status != "pending" {
		return refusal("job_status")
	}
	_, err = tx.Exec(ctx, `update jobs set status = 'published', approved_at = now(), published_at = now(),
		expires_at = now() + interval '30 days' where id = $1`, p.JobID)
	return err
}

func rejectJob(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p jobRef
	_ = json.Unmarshal(s.Payload, &p)
	_, err := tx.Exec(ctx, `update jobs set status = 'rejected', status_reason = $2 where id = $1 and status = 'pending'`, p.JobID, s.Reason)
	return err
}

func jobNotice(s moderation.Submission, approved bool, lang string) string {
	if !approved {
		return ""
	}
	return pick(lang, "✅ Вакансияңыз тексерілді және жарияланды (30 күн).\n"+siteLink("/#/employer"),
		"✅ Ваша вакансия проверена и опубликована (30 дней).\n"+siteLink("/#/employer"))
}

// ---------- reports ----------

// ReportReasons for jobs: scam (алаяқтық), fee (ақы сұрады), fake (сипаттама өтірік), other.
var ReportReasons = []string{"scam", "fee", "fake", "other"}

func jobReportTarget() moderation.ReportTarget {
	return moderation.ReportTarget{
		Exists: func(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil || n <= 0 {
				return false, nil
			}
			var c int
			err = tx.QueryRow(ctx, `select count(*) from jobs where id = $1 and status in ('published', 'hidden', 'expired')`, n).Scan(&c)
			return c == 1, err
		},
		SetHidden: func(ctx context.Context, tx pgx.Tx, id string, hidden bool) error {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return pgx.ErrNoRows
			}
			return setJobHidden(ctx, tx, n, hidden, "reports")
		},
	}
}

func setJobHidden(ctx context.Context, tx pgx.Tx, id int64, hidden bool, reason string) error {
	if hidden {
		_, err := tx.Exec(ctx, `update jobs set status = 'hidden', hidden_reason = $2
			where id = $1 and (status = 'published' or (status = 'hidden' and $2 = 'admin'))`, id, reason)
		return err
	}
	_, err := tx.Exec(ctx, `update jobs set hidden_reason = null,
		status = case when expires_at > now() then 'published' else 'expired' end
		where id = $1 and status = 'hidden'`, id)
	return err
}

// reportJob: POST /api/jobs/{id}/report {"reason","text"} — the first open report puts a
// job_report item into the queue; with HideAfter() counted reporters the job is hidden until an
// admin decides (shared rule, pkg/moderation).
func reportJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	var in struct {
		Reason string `json:"reason"`
		Text   string `json:"text"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok || !slices.Contains(ReportReasons, in.Reason) {
		bad(w, r)
		return
	}
	text, tok := cleanText(in.Text, 0, 300, false)
	if !tok {
		bad(w, r)
		return
	}
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	code := ""
	var res struct {
		Already bool `json:"alreadyReported"`
		Hidden  bool `json:"hidden"`
	}
	first := false
	var title string
	sid := strconv.FormatInt(id, 10)
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var eid int64
		var st string
		err := tx.QueryRow(ctx, `select employer_id, status, title_kk from jobs where id = $1 and status in ('published', 'hidden') for update`, id).
			Scan(&eid, &st, &title)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "job_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		var own bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from employer_members where employer_id = $1 and user_id = $2)`, eid, u.ID).Scan(&own); err != nil {
			return err
		}
		if own {
			code = "own_job"
			return nil
		}
		reason := in.Reason
		if text != "" {
			reason += ": " + text
		}
		tag, err := tx.Exec(ctx, `insert into reports (target_kind, target_id, reporter_id, reason) values ($1, $2, $3, $4)
			on conflict do nothing`, TargetJob, sid, u.ID, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			res.Already, res.Hidden = true, st == "hidden"
			return nil
		}
		var pending bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from submissions where kind = $1 and status = 'pending'
			and (payload->>'job_id')::bigint = $2)`, KindJobReport, id).Scan(&pending); err != nil {
			return err
		}
		if !pending {
			first = true
			if _, err := moderation.Submit(ctx, tx, KindJobReport, map[string]any{"job_id": id, "title": title,
				"employer_id": eid, "reason": in.Reason, "text": text}, &u.ID, ""); err != nil {
				return err
			}
		}
		n, err := moderation.CountedReports(ctx, tx, TargetJob, sid)
		if err != nil {
			return err
		}
		if n >= moderation.HideAfter() && st == "published" {
			if err := setJobHidden(ctx, tx, id, true, "reports"); err != nil {
				return err
			}
			st = "hidden"
		}
		res.Hidden = st == "hidden"
		return nil
	})
	if err != nil {
		internal(w, r, "report", err)
		return
	}
	switch code {
	case "job_not_found":
		fail(w, r, code, http.StatusNotFound)
		return
	case "own_job":
		fail(w, r, code, http.StatusBadRequest)
		return
	}
	if first {
		go notifyAdmins(context.WithoutCancel(ctx), "⚠️ Вакансияға шағым / Жалоба на вакансию: «"+esc(title)+"» (#"+sid+", "+in.Reason+")\n"+siteLink("/#/admin/queue"))
	}
	httpx.JSON(w, http.StatusOK, res)
}

func notifyAdmins(ctx context.Context, text string) {
	var ids []int64
	_ = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		ids, err = adminIDs(ctx, tx)
		return err
	})
	for _, id := range ids {
		_ = Notify(ctx, id, text)
	}
}

func reportJobID(s moderation.Submission) int64 {
	var p jobRef
	_ = json.Unmarshal(s.Payload, &p)
	return p.JobID
}

// upholdJobReport (admin approves the report): hidden for good ('admin'), members told.
func upholdJobReport(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id := reportJobID(s)
	if err := setJobHidden(ctx, tx, id, true, "admin"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update jobs set status = 'hidden', hidden_reason = 'admin', status_reason = $2
		where id = $1 and status in ('published', 'expired', 'hidden')`, id, s.Reason)
	return err
}

// dismissJobReport (reports groundless): reports dismissed, the job shows again.
func dismissJobReport(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id := reportJobID(s)
	if _, err := tx.Exec(ctx, `update reports set dismissed = true where target_kind = $1 and target_id = $2`, TargetJob, strconv.FormatInt(id, 10)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update jobs set hidden_reason = null, status = case when expires_at > now() then 'published' else 'expired' end
		where id = $1 and status = 'hidden' and hidden_reason = 'reports'`, id)
	return err
}

// ---------- response stats ----------

// ResponseStats: «Әдетте N күнде жауап береді», «өтінімдердің X%-ына жауап берді». Null until the
// company has MinForRate applications (withdrawn ones excluded).
type ResponseStats struct {
	Applications int  `json:"applications"`
	Days         *int `json:"days"` // median days to invited / rejected
	Rate         *int `json:"rate"` // % answered among decided (invited + rejected + no_response)
}

// Low: rate known and below 30%.
func (s *ResponseStats) Low() bool {
	return s != nil && s.Rate != nil && float64(*s.Rate) < LowRate*100
}

// ResponseStatsFor computes the stats per employer id (missing / nil = not enough data).
func ResponseStatsFor(ctx context.Context, ids []int64) (map[int64]*ResponseStats, error) {
	out := map[int64]*ResponseStats{}
	if len(ids) == 0 {
		return out, nil
	}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select employer_id,
				count(*) filter (where status <> 'withdrawn'),
				count(*) filter (where status in ('invited', 'rejected')),
				count(*) filter (where status in ('invited', 'rejected', 'no_response')),
				percentile_cont(0.5) within group (order by extract(epoch from responded_at - created_at))
					filter (where responded_at is not null and status in ('invited', 'rejected'))
			from applications where employer_id = any($1) group by employer_id`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var total, answered, decided int
			var median *float64
			if err := rows.Scan(&id, &total, &answered, &decided, &median); err != nil {
				return err
			}
			if total < MinForRate {
				continue
			}
			s := &ResponseStats{Applications: total}
			if decided > 0 {
				rate := int(math.Round(float64(answered) * 100 / float64(decided)))
				s.Rate = &rate
			}
			if median != nil {
				d := int(math.Ceil(*median / 86400))
				if d < 1 {
					d = 1
				}
				s.Days = &d
			}
			out[id] = s
		}
		return rows.Err()
	})
	return out, err
}

// ---------- public side: /api/content, /api/jobs ----------

// EmployerBadge is the company block on a public job.
type EmployerBadge struct {
	Name       string            `json:"name"`
	Website    string            `json:"website"`
	About      string            `json:"about"`
	Verified   bool              `json:"verified"`
	VerifiedAt *string           `json:"verified_at"`
	Meaning    map[string]string `json:"verification_meaning"`
}

// PublicJob is a database job in the exact data/jobs.json shape plus a source marker and the
// fields only real jobs have.
type PublicJob struct {
	content.Job
	Source      string         `json:"source"` // "employer"
	Employer    EmployerBadge  `json:"employer"`
	AdultOnly   bool           `json:"adult_only"` // hide from students with is_adult != true
	Remote      bool           `json:"remote"`
	Address     string         `json:"address"`
	Apply       content.Bi     `json:"apply"`
	Response    *ResponseStats `json:"response"`
	LowResponse bool           `json:"low_response"`
}

func day(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Add(5 * time.Hour).Format("2006-01-02")
	return &s
}

// liveJobs returns published, unexpired jobs of verified companies, low-response companies
// last. adultOK = false drops jobs that are not suitable for minors.
func liveJobs(ctx context.Context, adultOK bool, onlyID int64) ([]PublicJob, error) {
	type row struct {
		j                  *Job
		ename, site, about string
		verified           *time.Time
	}
	var list []row
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+jobCols+`, e.name, e.website, e.about, e.verified_at
			from jobs j join employers e on e.id = j.employer_id and e.status = 'verified'
			where j.status = 'published' and j.expires_at > now() and ($1 or j.minor_ok) and ($2 = 0 or j.id = $2)
			order by j.published_at desc limit 500`, adultOK, onlyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			var j Job
			var req []byte
			if err := rows.Scan(&j.ID, &j.TitleKK, &j.TitleRU, &j.DescKK, &j.DescRU, &j.City, &j.Remote, &j.Address, &j.Pay, &j.Per,
				&j.Hours, &j.SchedKK, &j.SchedRU, &j.Tags, &j.Skills, &j.Spec, &req, &j.ApplyKK, &j.ApplyRU, &j.MinorOK, &j.Status,
				&j.HiddenReason, &j.StatusReason, &j.PublishedAt, &j.ExpiresAt, &j.CreatedAt, &j.employerID,
				&x.ename, &x.site, &x.about, &x.verified); err != nil {
				return err
			}
			_ = json.Unmarshal(req, &j.Req)
			x.j = &j
			list = append(list, x)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, x := range list {
		ids = append(ids, x.j.employerID)
	}
	stats, err := ResponseStatsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]PublicJob, 0, len(list))
	for _, x := range list {
		j := x.j
		cj := content.Job{
			ID: JobIDPrefix + strconv.FormatInt(j.ID, 10), Title: content.Bi{KK: j.TitleKK, RU: j.TitleRU},
			Org: content.Bi{KK: x.ename, RU: x.ename}, City: j.City, Pay: j.Pay, Per: j.Per,
			Sched: content.Bi{KK: j.SchedKK, RU: j.SchedRU}, Tags: j.Tags, Skills: j.Skills, Spec: j.Spec,
			Desc: content.Bi{KK: j.DescKK, RU: j.DescRU}, Req: j.Req, SourceURL: x.site,
			PostedAt: day(j.PublishedAt), VerifiedAt: day(x.verified), VerifiedBy: "Student Hub",
			Contact: content.Contact{}, Deadline: day(j.ExpiresAt),
		}
		if j.Hours != nil {
			cj.Hours = *j.Hours
		}
		if cj.Req == nil {
			cj.Req = []content.Bi{}
		}
		st := stats[j.employerID]
		out = append(out, PublicJob{
			Job: cj, Source: "employer",
			Employer:  EmployerBadge{Name: x.ename, Website: x.site, About: x.about, Verified: true, VerifiedAt: day(x.verified), Meaning: VerificationMeaning},
			AdultOnly: !j.MinorOK, Remote: j.Remote, Address: j.Address, Apply: content.Bi{KK: j.ApplyKK, RU: j.ApplyRU},
			Response: st, LowResponse: st.Low(),
		})
	}
	// companies answering < 30% go to the end of the list
	slices.SortStableFunc(out, func(a, b PublicJob) int {
		switch {
		case a.LowResponse == b.LowResponse:
			return 0
		case a.LowResponse:
			return 1
		}
		return -1
	})
	return out, nil
}

func registerContent() {
	content.RegisterExtra("b2b", func(ctx context.Context) (map[string][]content.Record, error) {
		if !features.Enabled(Feature) {
			return nil, nil
		}
		if _, err := db.Pool(ctx); errors.Is(err, db.ErrNoDB) {
			return nil, nil
		}
		jobs, err := liveJobs(ctx, true, 0) // /api/content is public and cached: minors filter on the client by adult_only
		if err != nil {
			return nil, err
		}
		out := map[string][]content.Record{}
		for _, j := range jobs {
			raw, err := json.Marshal(j)
			if err != nil {
				continue
			}
			d := ""
			if j.Deadline != nil {
				d = *j.Deadline
			}
			out["jobs"] = append(out["jobs"], content.RawRecord(raw, d))
		}
		opps, err := officialOpportunities(ctx)
		if err != nil {
			return nil, err
		}
		out["opportunities"] = opps
		return out, nil
	})
}

// adultOK: anonymous visitors see everything (the flag is in the record); a signed-in student
// whose is_adult is not true never gets adult-only jobs.
func adultOK(r *http.Request) (bool, *auth.User) {
	u := auth.Current(r)
	if u == nil {
		return true, nil
	}
	return u.IsAdult != nil && *u.IsAdult, u
}

// listJobs: GET /api/jobs — real jobs only, filtered for the caller's age.
func listJobs(w http.ResponseWriter, r *http.Request) {
	ok, _ := adultOK(r)
	jobs, err := liveJobs(r.Context(), ok, 0)
	if err != nil {
		internal(w, r, "jobs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// jobDetail: GET /api/jobs/{id} — one real job (+ my application). 404 for minors on adult-only jobs.
func jobDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	adult, u := adultOK(r)
	jobs, err := liveJobs(r.Context(), adult, id)
	if err != nil {
		internal(w, r, "job", err)
		return
	}
	if len(jobs) == 0 {
		fail(w, r, "job_not_found", http.StatusNotFound)
		return
	}
	var my *MyApplication
	if u != nil {
		list, err := myApplicationList(r.Context(), u.ID, id)
		if err != nil {
			internal(w, r, "job my", err)
			return
		}
		if len(list) > 0 {
			my = &list[0]
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": jobs[0], "myApplication": my})
}

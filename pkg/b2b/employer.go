package b2b

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

// VerificationMeaning is shown next to «Тексерілген»: what the check means and what it does not.
var VerificationMeaning = map[string]string{
	"kk": "Тексерілген: компания ресми тіркелген (БСН тексерілді) және байланыс деректері нақты. Бұл жұмысқа немесе жалақыға кепілдік ЕМЕС.",
	"ru": "Проверено: компания официально зарегистрирована (БИН проверен) и контакты настоящие. Это НЕ гарантия трудоустройства или оплаты.",
}

var reBIN = regexp.MustCompile(`^[0-9]{12}$`)

// Employer is the cabinet view of a company.
type Employer struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	BIN          string     `json:"bin"`
	Website      string     `json:"website"`
	ContactName  string     `json:"contactName"`
	ContactPhone string     `json:"contactPhone"`
	Address      string     `json:"address"`
	About        string     `json:"about"`
	Status       string     `json:"status"`
	StatusReason *string    `json:"statusReason"`
	VerifiedAt   *time.Time `json:"verifiedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

const employerCols = `id, name, bin, website, contact_name, contact_phone, address, about, status, status_reason, verified_at, created_at`

func scanEmployer(row pgx.Row) (*Employer, error) {
	var e Employer
	err := row.Scan(&e.ID, &e.Name, &e.BIN, &e.Website, &e.ContactName, &e.ContactPhone, &e.Address, &e.About,
		&e.Status, &e.StatusReason, &e.VerifiedAt, &e.CreatedAt)
	return &e, err
}

// membership is the caller's link to a company.
type membership struct {
	EmployerID    int64
	Role          string // owner | member
	Status        string // employers.status
	Name          string
	RulesAccepted bool
}

func myMembership(ctx context.Context, tx pgx.Tx, userID int64) (*membership, error) {
	var m membership
	err := tx.QueryRow(ctx, `select m.employer_id, m.role, e.status, e.name, m.rules_accepted_at is not null
		from employer_members m join employers e on e.id = m.employer_id where m.user_id = $1`, userID).
		Scan(&m.EmployerID, &m.Role, &m.Status, &m.Name, &m.RulesAccepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

// needMember resolves the caller's company. verified: the company must be verified;
// owner: the caller must be its owner. Answers 403 otherwise.
func needMember(w http.ResponseWriter, r *http.Request, u *auth.User, verified, owner bool) (*membership, bool) {
	var m *membership
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		m, err = myMembership(r.Context(), tx, u.ID)
		return err
	})
	if err != nil {
		internal(w, r, "membership", err)
		return nil, false
	}
	switch {
	case m == nil:
		fail(w, r, "not_member", http.StatusForbidden)
		return nil, false
	case verified && m.Status != "verified":
		fail(w, r, "not_verified", http.StatusForbidden)
		return nil, false
	case owner && m.Role != "owner":
		fail(w, r, "not_owner", http.StatusForbidden)
		return nil, false
	}
	return m, true
}

// grantEmployerRole: a student becomes "employer" (ambassadors, staff and admins keep their role).
func grantEmployerRole(ctx context.Context, tx pgx.Tx, userID int64, actor *int64, reason string) error {
	tag, err := tx.Exec(ctx, `update users set role = 'employer' where id = $1 and role = 'student'`, userID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values ($1, 'role_grant', $2, $3, jsonb_build_object('from', 'student', 'to', 'employer'))`, actor, userID, reason)
	return err
}

// revertEmployerRole: back to "student" once the person belongs to no company.
func revertEmployerRole(ctx context.Context, tx pgx.Tx, userID int64, reason string) error {
	tag, err := tx.Exec(ctx, `update users set role = 'student' where id = $1 and role = 'employer'
		and not exists (select 1 from employer_members where user_id = $1)`, userID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values (null, 'role_revoke', $1, $2, jsonb_build_object('from', 'employer', 'to', 'student'))`, userID, reason)
	return err
}

// ---------- apply for verification ----------

type employerInput struct {
	Name         string `json:"name"`
	BIN          string `json:"bin"`
	Website      string `json:"website"`
	ContactName  string `json:"contactName"`
	ContactPhone string `json:"contactPhone"`
	Address      string `json:"address"`
	About        string `json:"about"`
}

var rePhone = regexp.MustCompile(`^\+?[0-9 ()-]{7,20}$`)

func (in *employerInput) clean() bool {
	var ok1, ok2, ok3, ok4, ok5, ok6 bool
	in.Name, ok1 = cleanText(in.Name, 2, 120, false)
	in.BIN = strings.ReplaceAll(strings.TrimSpace(in.BIN), " ", "")
	in.Website, ok2 = httpsURL(in.Website, true)
	in.ContactName, ok3 = cleanText(in.ContactName, 2, 80, false)
	in.ContactPhone, ok4 = cleanText(in.ContactPhone, 0, 30, false)
	in.Address, ok5 = cleanText(in.Address, 0, 200, false)
	in.About, ok6 = cleanText(in.About, 0, 1000, true)
	if in.ContactPhone != "" && !rePhone.MatchString(in.ContactPhone) {
		return false
	}
	return ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && reBIN.MatchString(in.BIN)
}

// employerApply: POST /api/employer/apply — the company application goes to the queue
// (employer_verification). The applicant becomes the owner-member of a pending company.
func employerApply(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	var in employerInput
	if err := httpx.Decode(r, &in, 8<<10); err != nil || !in.clean() {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	code := ""
	var e *Employer
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		m, err := myMembership(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if m != nil {
			if m.Status != "rejected" && m.Status != "closed" {
				code = "already_member"
				return nil
			}
			// a rejected / closed company does not block a new application
			if _, err := tx.Exec(ctx, `delete from employer_members where user_id = $1`, u.ID); err != nil {
				return err
			}
		}
		e, err = scanEmployer(tx.QueryRow(ctx, `insert into employers (owner_user_id, name, bin, website, contact_name, contact_phone, address, about)
			values ($1, $2, $3, $4, $5, $6, $7, $8) returning `+employerCols,
			u.ID, in.Name, in.BIN, in.Website, in.ContactName, in.ContactPhone, in.Address, in.About))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into employer_members (employer_id, user_id, role, added_by) values ($1, $2, 'owner', $2)`, e.ID, u.ID); err != nil {
			return err
		}
		sid, err := moderation.Submit(ctx, tx, KindVerification, map[string]any{
			"employer_id": e.ID, "name": in.Name, "bin": in.BIN, "website": in.Website, "contact_name": in.ContactName,
			"contact_phone": in.ContactPhone, "address": in.Address, "about": in.About,
			"applicant": map[string]any{"name": u.Name, "username": u.Username},
			"check":     "БСН-ді ресми тізілімнен (stat.gov.kz / egov.kz) қолмен тексеріңіз; сайт немесе 2GIS беті ашылатынын тексеріңіз.",
		}, &u.ID, "")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update employers set submission_id = $2 where id = $1`, e.ID, sid)
		return err
	})
	if isUnique(err) {
		fail(w, r, "bin_taken", http.StatusConflict)
		return
	}
	if err != nil {
		internal(w, r, "employer apply", err)
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusConflict)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"employer": e, "verificationMeaning": VerificationMeaning})
}

type employerRef struct {
	EmployerID int64 `json:"employer_id"`
}

func approveVerification(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p employerRef
	if err := json.Unmarshal(s.Payload, &p); err != nil || p.EmployerID == 0 {
		return refusal("employer_not_found")
	}
	var owner *int64
	var status string
	err := tx.QueryRow(ctx, `select e.owner_user_id, e.status from employers e where e.id = $1 for update`, p.EmployerID).Scan(&owner, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return refusal("employer_not_found")
	}
	if err != nil {
		return err
	}
	if owner == nil || status != "pending" {
		return refusal("author_deleted")
	}
	if _, err := tx.Exec(ctx, `update employers set status = 'verified', status_reason = null, verified_at = now(),
		verified_by = (select reviewed_by from submissions where id = $2), updated_at = now() where id = $1`, p.EmployerID, s.ID); err != nil {
		return err
	}
	var reviewer *int64
	_ = tx.QueryRow(ctx, `select reviewed_by from submissions where id = $1`, s.ID).Scan(&reviewer)
	ids, err := memberIDs(ctx, tx, p.EmployerID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := grantEmployerRole(ctx, tx, id, reviewer, "employer verified"); err != nil {
			return err
		}
	}
	return nil
}

func rejectVerification(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var p employerRef
	_ = json.Unmarshal(s.Payload, &p)
	_, err := tx.Exec(ctx, `update employers set status = 'rejected', status_reason = $2, updated_at = now()
		where id = $1 and status = 'pending'`, p.EmployerID, s.Reason)
	return err
}

func verificationNotice(s moderation.Submission, approved bool, lang string) string {
	if !approved {
		return "" // generic message with the reason
	}
	return pick(lang,
		"✅ Компанияңыз тексерілді — «Тексерілген» белгісі қосылды.\n"+VerificationMeaning["kk"]+"\nКабинет: "+siteLink("/#/employer"),
		"✅ Компания проверена — добавлен значок «Проверено».\n"+VerificationMeaning["ru"]+"\nКабинет: "+siteLink("/#/employer"))
}

// ---------- cabinet ----------

// employerMe: GET /api/employer/me — my company (read as app_user), my role and its stats.
func employerMe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	var m *membership
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		m, err = myMembership(ctx, tx, u.ID)
		return err
	}); err != nil {
		internal(w, r, "employer me", err)
		return
	}
	if m == nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"employer": nil, "verificationMeaning": VerificationMeaning})
		return
	}
	var e *Employer
	jobs := map[string]int{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		if e, err = scanEmployer(tx.QueryRow(ctx, `select `+employerCols+` from employers where id = $1`, m.EmployerID)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select status, count(*) from jobs where employer_id = $1 group by status`, m.EmployerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var st string
			var n int
			if err := rows.Scan(&st, &n); err != nil {
				return err
			}
			jobs[st] = n
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "employer me", err)
		return
	}
	stats, err := ResponseStatsFor(ctx, []int64{m.EmployerID})
	if err != nil {
		internal(w, r, "employer stats", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"employer": e, "role": m.Role, "rulesAccepted": m.RulesAccepted, "jobs": jobs,
		"response": stats[m.EmployerID], "verificationMeaning": VerificationMeaning,
	})
}

// employerPatch: PATCH /api/employer/me — contact block of a verified company (name and БСН are
// fixed by verification). Runs as app_user: RLS allows only the caller's own company.
func employerPatch(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	m, ok := needMember(w, r, u, true, false)
	if !ok {
		return
	}
	var body map[string]string
	if err := httpx.Decode(r, &body, 8<<10); err != nil || len(body) == 0 {
		bad(w, r)
		return
	}
	var sets []string
	var args []any
	for k, v := range body {
		var col string
		var val string
		ok := true
		switch k {
		case "website":
			col = "website"
			val, ok = httpsURL(v, true)
		case "contactName":
			col = "contact_name"
			val, ok = cleanText(v, 2, 80, false)
		case "contactPhone":
			col = "contact_phone"
			val, ok = cleanText(v, 0, 30, false)
			ok = ok && (val == "" || rePhone.MatchString(val))
		case "address":
			col = "address"
			val, ok = cleanText(v, 0, 200, false)
		case "about":
			col = "about"
			val, ok = cleanText(v, 0, 1000, true)
		default:
			ok = false
		}
		if !ok {
			fail(w, r, "invalid", http.StatusBadRequest)
			return
		}
		args = append(args, val)
		sets = append(sets, col+" = $"+itoa(len(args)+1))
	}
	var e *Employer
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		e, err = scanEmployer(tx.QueryRow(ctx, "update employers set "+strings.Join(sets, ", ")+", updated_at = now() where id = $1 returning "+employerCols,
			append([]any{m.EmployerID}, args...)...))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "not_member", http.StatusForbidden)
		return
	}
	if err != nil {
		internal(w, r, "employer patch", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"employer": e})
}

// acceptRules: POST /api/employer/rules/accept — the one-time job-posting rules screen.
func acceptRules(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if _, ok := needMember(w, r, u, false, false); !ok {
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `update employer_members set rules_accepted_at = coalesce(rules_accepted_at, now()) where user_id = $1`, u.ID)
		return err
	})
	if err != nil {
		internal(w, r, "rules", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"rulesAccepted": true})
}

// ---------- members, invites, hand-over ----------

type member struct {
	UserID   int64     `json:"userId"`
	Name     *string   `json:"name"`
	Username *string   `json:"username"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
	Me       bool      `json:"me"`
}

func members(w http.ResponseWriter, r *http.Request, u *auth.User) {
	m, ok := needMember(w, r, u, false, false)
	if !ok {
		return
	}
	var out []member
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select m.user_id, u.name, u.tg_username, m.role, m.created_at
			from employer_members m join users u on u.id = m.user_id and u.deleted_at is null
			where m.employer_id = $1 order by m.role = 'owner' desc, m.created_at`, m.EmployerID)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (member, error) {
			var x member
			err := row.Scan(&x.UserID, &x.Name, &x.Username, &x.Role, &x.JoinedAt)
			x.Me = x.UserID == u.ID
			return x, err
		})
		return err
	})
	if err != nil {
		internal(w, r, "members", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
}

const inviteChars = "abcdefghijkmnpqrstuvwxyz23456789"

func randomCode(n int) string {
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(inviteChars))))
		b[i] = inviteChars[k.Int64()]
	}
	return string(b)
}

// createInvite: POST /api/employer/invites — owner of a verified company; a one-time 7-day code.
func createInvite(w http.ResponseWriter, r *http.Request, u *auth.User) {
	m, ok := needMember(w, r, u, true, true)
	if !ok {
		return
	}
	code := randomCode(12)
	exp := Now().Add(InviteDays * 24 * time.Hour)
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `insert into employer_invites (code, employer_id, created_by, expires_at) values ($1, $2, $3, $4)`,
			code, m.EmployerID, u.ID, exp)
		return err
	})
	if err != nil {
		internal(w, r, "invite", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"code": code, "expiresAt": exp, "link": siteLink("/#/employer/join/" + code)})
}

// join: POST /api/employer/join {"code"} — a colleague joins the company.
func join(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil {
		bad(w, r)
		return
	}
	code := ""
	var name string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		m, err := myMembership(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if m != nil && m.Status != "rejected" && m.Status != "closed" {
			code = "already_member"
			return nil
		}
		var eid int64
		err = tx.QueryRow(ctx, `select i.employer_id, e.name from employer_invites i join employers e on e.id = i.employer_id and e.status = 'verified'
			where i.code = $1 and i.used_at is null and i.expires_at > now() for update of i`, strings.TrimSpace(body.Code)).Scan(&eid, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "invite_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from employer_members where user_id = $1`, u.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into employer_members (employer_id, user_id, role, added_by)
			select $1, $2, 'member', created_by from employer_invites where code = $3`, eid, u.ID, body.Code); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update employer_invites set used_by = $2, used_at = now() where code = $1`, body.Code, u.ID); err != nil {
			return err
		}
		return grantEmployerRole(ctx, tx, u.ID, nil, "joined a verified company")
	})
	if err != nil {
		internal(w, r, "join", err)
		return
	}
	if code != "" {
		status := http.StatusNotFound
		if code == "already_member" {
			status = http.StatusConflict
		}
		fail(w, r, code, status)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"joined": true, "company": name})
}

// removeMember: DELETE /api/employer/members/{userId} — the owner removes a colleague.
func removeMember(w http.ResponseWriter, r *http.Request, u *auth.User) {
	m, ok := needMember(w, r, u, false, true)
	if !ok {
		return
	}
	target, ok := pathID(r, "userId")
	if !ok || target == u.ID {
		fail(w, r, "member_not_found", http.StatusNotFound)
		return
	}
	found := false
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `delete from employer_members where employer_id = $1 and user_id = $2 and role = 'member'`, m.EmployerID, target)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		found = true
		if err := reassignSubmissions(r.Context(), tx, target, u.ID); err != nil {
			return err
		}
		return revertEmployerRole(r.Context(), tx, target, "removed from company")
	})
	if err != nil {
		internal(w, r, "remove member", err)
		return
	}
	if !found {
		fail(w, r, "member_not_found", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// leave: POST /api/employer/leave — a member leaves; the owner must transfer or close first.
func leave(w http.ResponseWriter, r *http.Request, u *auth.User) {
	m, ok := needMember(w, r, u, false, false)
	if !ok {
		return
	}
	if m.Role == "owner" && m.Status != "rejected" && m.Status != "closed" {
		fail(w, r, "owner_must_transfer", http.StatusConflict)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `delete from employer_members where user_id = $1`, u.ID); err != nil {
			return err
		}
		var owner *int64
		_ = tx.QueryRow(r.Context(), `select owner_user_id from employers where id = $1`, m.EmployerID).Scan(&owner)
		if owner != nil && *owner != u.ID {
			if err := reassignSubmissions(r.Context(), tx, u.ID, *owner); err != nil {
				return err
			}
		}
		return revertEmployerRole(r.Context(), tx, u.ID, "left company")
	})
	if err != nil {
		internal(w, r, "leave", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"left": true})
}

// transfer: POST /api/employer/transfer {"userId"} — the owner hands the company to a member.
func transfer(w http.ResponseWriter, r *http.Request, u *auth.User) {
	m, ok := needMember(w, r, u, false, true)
	if !ok {
		return
	}
	var body struct {
		UserID int64 `json:"userId"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.UserID <= 0 || body.UserID == u.ID {
		fail(w, r, "member_not_found", http.StatusNotFound)
		return
	}
	found := false
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(r.Context(), `select count(*) from employer_members m join users u on u.id = m.user_id and u.deleted_at is null
			where m.employer_id = $1 and m.user_id = $2`, m.EmployerID, body.UserID).Scan(&n); err != nil || n == 0 {
			return err
		}
		found = true
		return handOver(r.Context(), tx, m.EmployerID, u.ID, body.UserID)
	})
	if err != nil {
		internal(w, r, "transfer", err)
		return
	}
	if !found {
		fail(w, r, "member_not_found", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"owner": body.UserID})
}

// handOver makes `to` the owner of the company instead of `from`.
func handOver(ctx context.Context, tx pgx.Tx, employerID, from, to int64) error {
	if _, err := tx.Exec(ctx, `update employer_members set role = 'member' where employer_id = $1 and user_id = $2`, employerID, from); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update employer_members set role = 'owner' where employer_id = $1 and user_id = $2`, employerID, to); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update employers set owner_user_id = $2, updated_at = now() where id = $1`, employerID, to); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values (null, 'employer_transfer', $2, null, jsonb_build_object('employer', $1::bigint))`, employerID, to)
	return err
}

// reassignSubmissions moves someone's pending company queue items to another member, so they
// survive the author leaving (submissions.submitted_by cascades with the account).
func reassignSubmissions(ctx context.Context, tx pgx.Tx, from, to int64) error {
	_, err := tx.Exec(ctx, `update submissions set submitted_by = $2
		where submitted_by = $1 and status = 'pending' and kind in ($3, $4)`, from, to, KindJob, KindVerification)
	return err
}

// stopCompany closes a company: no more jobs, no members. Open applications stay with their
// students and become no_response through the daily job if nobody answered.
func stopCompany(ctx context.Context, tx pgx.Tx, employerID int64, reason string) error {
	if _, err := tx.Exec(ctx, `update employers set status = 'closed', status_reason = $2, updated_at = now() where id = $1`, employerID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update jobs set status = 'closed', closed_at = now() where employer_id = $1 and status in ('pending', 'published', 'expired')`, employerID); err != nil {
		return err
	}
	ids, err := memberIDs(ctx, tx, employerID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from employer_members where employer_id = $1`, employerID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := revertEmployerRole(ctx, tx, id, "company closed"); err != nil {
			return err
		}
	}
	return nil
}

// closeCompany: POST /api/employer/close {"confirm":"CLOSE"} — the owner stops the company.
func closeCompany(w http.ResponseWriter, r *http.Request, u *auth.User) {
	m, ok := needMember(w, r, u, false, true)
	if !ok {
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Confirm != "CLOSE" {
		fail(w, r, "confirm_required", http.StatusBadRequest)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error { return stopCompany(r.Context(), tx, m.EmployerID, "closed by owner") })
	if err != nil {
		internal(w, r, "close company", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// handOverOnDelete (account deletion, prompt 11): an owner who deletes the account without
// handing over passes the company to the longest-standing member; with no members left the
// company is stopped. Pending queue items of the leaving person move to the new owner.
func handOverOnDelete(ctx context.Context, tx pgx.Tx, userID int64) error {
	m, err := myMembership(ctx, tx, userID)
	if err != nil || m == nil {
		return err
	}
	var next int64
	err = tx.QueryRow(ctx, `select m.user_id from employer_members m join users u on u.id = m.user_id and u.deleted_at is null
		where m.employer_id = $1 and m.user_id <> $2 order by (m.role = 'owner') desc, m.created_at limit 1`, m.EmployerID, userID).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		if m.Status == "closed" || m.Status == "rejected" {
			return nil
		}
		return stopCompany(ctx, tx, m.EmployerID, "owner account deleted")
	}
	if err != nil {
		return err
	}
	if m.Role == "owner" {
		if err := handOver(ctx, tx, m.EmployerID, userID, next); err != nil {
			return err
		}
	} else {
		var owner *int64
		if err := tx.QueryRow(ctx, `select owner_user_id from employers where id = $1`, m.EmployerID).Scan(&owner); err != nil {
			return err
		}
		if owner != nil {
			next = *owner
		}
	}
	return reassignSubmissions(ctx, tx, userID, next)
}

// ---------- admin ----------

type adminEmployer struct {
	Employer
	Members  int            `json:"members"`
	Jobs     int            `json:"jobs"`
	Response *ResponseStats `json:"response"`
	LowRate  bool           `json:"lowRate"`
}

// adminEmployers: GET /api/admin/employers?status= — companies with their response stats.
func adminEmployers(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	var out []adminEmployer
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+employerCols+`,
				(select count(*) from employer_members m where m.employer_id = employers.id),
				(select count(*) from jobs j where j.employer_id = employers.id)
			from employers where ($1 = '' or status = $1) order by created_at desc limit 500`, status)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (adminEmployer, error) {
			var a adminEmployer
			e := &a.Employer
			return a, row.Scan(&e.ID, &e.Name, &e.BIN, &e.Website, &e.ContactName, &e.ContactPhone, &e.Address, &e.About,
				&e.Status, &e.StatusReason, &e.VerifiedAt, &e.CreatedAt, &a.Members, &a.Jobs)
		})
		return err
	})
	if err != nil {
		internal(w, r, "admin employers", err)
		return
	}
	ids := make([]int64, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	stats, err := ResponseStatsFor(ctx, ids)
	if err != nil {
		internal(w, r, "admin employers stats", err)
		return
	}
	for i := range out {
		out[i].Response = stats[out[i].ID]
		out[i].LowRate = out[i].Response.Low()
	}
	if out == nil {
		out = []adminEmployer{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"employers": out})
}

func adminStatus(to string, from []string, action string) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, admin *auth.User) {
		id, ok := pathID(r, "id")
		var body struct {
			Reason string `json:"reason"`
		}
		_ = httpx.Decode(r, &body, 2<<10)
		reason, rok := cleanText(body.Reason, 3, 500, false)
		if !ok {
			bad(w, r)
			return
		}
		if !rok {
			fail(w, r, "reason_required", http.StatusBadRequest)
			return
		}
		found := false
		err := db.Tx(r.Context(), func(tx pgx.Tx) error {
			tag, err := tx.Exec(r.Context(), `update employers set status = $2, status_reason = $3, updated_at = now()
				where id = $1 and status = any($4)`, id, to, reason, from)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			found = true
			_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, reason, meta)
				values ($1, $2, $3, jsonb_build_object('employer', $4::bigint))`, admin.ID, action, reason, id)
			return err
		})
		if err != nil {
			internal(w, r, action, err)
			return
		}
		if !found {
			fail(w, r, "employer_not_found", http.StatusNotFound)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": to})
	}
}

// Suspended companies disappear from /api/content and refuse applications at once (job_apply
// and the content query both require status = verified).
var (
	adminSuspend = adminStatus("suspended", []string{"verified"}, "employer_suspend")
	adminRestore = adminStatus("verified", []string{"suspended"}, "employer_restore")
)

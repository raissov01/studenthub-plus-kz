package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
)

// The user directory of /admin/users (admin only: personal data): a filtered, paginated list, one account's
// detail (profile, roles and memberships, activity, reports against their content, audit trail), block /
// unblock and account deletion on request. Every change needs a reason and writes audit_log.

var (
	MsgAdminTarget = httpx.Msg{KK: "Әкімшіні бұғаттауға не өшіруге болмайды: алдымен рөлін алыңыз.", RU: "Администратора нельзя заблокировать или удалить: сначала снимите роль."}
	MsgSelf        = httpx.Msg{KK: "Өз аккаунтыңызға бұл әрекет жасалмайды.", RU: "Это действие нельзя применить к своему аккаунту."}
	MsgConfirm     = httpx.Msg{KK: "Өшіруді растау үшін DELETE деп жазыңыз.", RU: "Для подтверждения удаления введите DELETE."}
)

// listFilter is the parsed query of GET /api/admin/users.
type listFilter struct {
	Q, Role, Scope, University, Active, Consent, Sort string
	Blocked                                           bool
	From, To                                          *time.Time
	Page, Limit                                       int
}

func parseDate(s string) (*time.Time, bool) {
	if s == "" {
		return nil, true
	}
	t, err := time.ParseInLocation(time.DateOnly, s, almaty)
	if err != nil {
		return nil, false
	}
	return &t, true
}

func parseList(r *http.Request) (listFilter, bool) {
	q := r.URL.Query()
	f := listFilter{Q: strings.TrimSpace(q.Get("q")), Role: q.Get("role"), Scope: q.Get("scope"), University: strings.TrimSpace(q.Get("university")),
		Active: q.Get("active"), Consent: q.Get("consent"), Sort: q.Get("sort"), Blocked: q.Get("blocked") == "1"}
	ok := utf8.RuneCountInString(f.Q) <= 64 && utf8.RuneCountInString(f.University) <= 160
	ok = ok && (f.Role == "" || validRole(f.Role)) && (f.Scope == "" || f.Scope == auth.ScopeModerator)
	ok = ok && (f.Active == "" || f.Active == "7" || f.Active == "30" || f.Active == "never")
	ok = ok && (f.Consent == "" || f.Consent == "yes" || f.Consent == "no")
	ok = ok && (f.Sort == "" || f.Sort == "new" || f.Sort == "old" || f.Sort == "active" || f.Sort == "name")
	var okFrom, okTo bool
	f.From, okFrom = parseDate(q.Get("from"))
	f.To, okTo = parseDate(q.Get("to"))
	f.Page, _ = strconv.Atoi(q.Get("page"))
	if f.Page < 1 {
		f.Page = 1
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 25
	}
	return f, ok && okFrom && okTo && f.Page <= 10000
}

// where builds the shared filter of the list and the CSV export; args start at $1.
func (f listFilter) where() (string, []any) {
	num, _ := strconv.ParseInt(strings.TrimPrefix(f.Q, "@"), 10, 64)
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimPrefix(f.Q, "@")) + "%"
	var to *time.Time
	if f.To != nil {
		t := f.To.AddDate(0, 0, 1)
		to = &t
	}
	cond := `u.deleted_at is null
		and ($1 = '' or u.id = $2 or u.telegram_id = $2 or u.name ilike $3 or u.tg_username ilike $3 or u.email ilike $3)
		and ($4 = '' or u.role::text = $4)
		and ($5 = '' or exists (select 1 from admin_grants g where g.user_id = u.id and g.scope = $5))
		and ($6 = '' or ($6 = '-' and u.university is null) or u.university = $6)
		and (not $7 or exists (select 1 from user_blocks b where b.user_id = u.id))
		and ($8::timestamptz is null or u.created_at >= $8) and ($9::timestamptz is null or u.created_at < $9)
		and ($10 = '' or ($10 = 'yes') = (u.consent_at is not null))
		and ($11 = '' or ($11 = 'never' and not exists (select 1 from user_active_days a where a.user_id = u.id))
			or ($11 = 'recent' and exists (select 1 from user_active_days a where a.user_id = u.id
				and a.day > (now() at time zone 'Asia/Almaty')::date - $12::int)))`
	mode, days := f.Active, 0
	if n, err := strconv.Atoi(f.Active); err == nil {
		mode, days = "recent", n
	}
	return cond, []any{f.Q, num, like, f.Role, f.Scope, f.University, f.Blocked, f.From, to, f.Consent, mode, days}
}

func (f listFilter) order() string {
	switch f.Sort {
	case "old":
		return "u.created_at, u.id"
	case "active":
		return "last_active desc nulls last, u.id desc"
	case "name":
		return "u.name nulls last, u.id"
	}
	return "u.created_at desc, u.id desc"
}

type listRow struct {
	userRow
	LastActive *string  `json:"lastActive"`
	Blocked    bool     `json:"blocked"`
	Scopes     []string `json:"scopes"`
}

const listCols = `u.id, u.telegram_id, u.email, u.tg_username, u.name, u.role::text, u.university, u.consent_at, u.created_at,
	(select to_char(max(a.day), 'YYYY-MM-DD') from user_active_days a where a.user_id = u.id) as last_active,
	exists (select 1 from user_blocks b where b.user_id = u.id),
	array(select g.scope from admin_grants g where g.user_id = u.id order by g.scope)`

func scanListRow(row pgx.CollectableRow) (listRow, error) {
	var u listRow
	return u, row.Scan(&u.ID, &u.TelegramID, &u.Email, &u.Username, &u.Name, &u.Role, &u.University, &u.ConsentAt, &u.CreatedAt,
		&u.LastActive, &u.Blocked, &u.Scopes)
}

// users: GET /api/admin/users?q=&role=&scope=&blocked=1&university=&from=&to=&active=7|30|never&consent=yes|no
// &sort=new|old|active|name&page=&limit= → {users, total, page, limit, universities}. ?q= alone keeps the
// old search behaviour (staff screens use it).
func users(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	f, ok := parseList(r)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	cond, args := f.where()
	out := []listRow{}
	type univ struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	univs := []univ{}
	var total int
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select count(*) from users u where `+cond, args...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select `+listCols+` from users u where `+cond+` order by `+f.order()+
			` limit `+strconv.Itoa(f.Limit)+` offset `+strconv.Itoa((f.Page-1)*f.Limit), args...)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, scanListRow); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select university, count(*) from users where deleted_at is null and university is not null
			group by university order by count(*) desc, university limit 100`)
		if err != nil {
			return err
		}
		univs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (univ, error) {
			var u univ
			return u, row.Scan(&u.Name, &u.Count)
		})
		return err
	})
	if err != nil {
		fail(w, r, "users", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out, "total": total, "page": f.Page, "limit": f.Limit, "universities": univs})
}

func pathUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
		return 0, false
	}
	return id, true
}

// userDetail: GET /api/admin/users/{id}.
func userDetail(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var out json.RawMessage
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, detailSQL, id).Scan(&out)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
		return
	case err != nil:
		fail(w, r, "user detail", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// detailSQL builds the whole detail as one JSON document. Reports against the user are counted over the content
// they wrote (lost & found, Q&A, roommates, market items, community opportunities/places, company jobs).
const detailSQL = `
with u as (select * from users where id = $1 and deleted_at is null),
against as (
	select r.target_kind as kind, r.target_id as id, count(*) as reports, count(*) filter (where r.dismissed) as dismissed,
		min(r.created_at) as first_at, max(r.created_at) as last_at
	from reports r
	where (r.target_kind = 'lostfound_post' and exists (select 1 from lostfound_posts x where x.id::text = r.target_id and x.user_id = $1))
	   or (r.target_kind = 'qa_question' and exists (select 1 from qa_questions x where x.id::text = r.target_id and x.user_id = $1))
	   or (r.target_kind = 'qa_answer' and exists (select 1 from qa_answers x where x.id::text = r.target_id and x.user_id = $1))
	   or (r.target_kind = 'roommate_listing' and exists (select 1 from roommate_listings x where x.id::text = r.target_id and x.user_id = $1))
	   or (r.target_kind in ('opportunity', 'place') and exists (select 1 from community_content x where x.id::text = r.target_id and x.author_id = $1))
	   or (r.target_kind = 'job' and exists (select 1 from jobs x where x.id::text = r.target_id and x.created_by = $1))
	group by r.target_kind, r.target_id
	union all
	select 'market_item', s.payload->>'itemId', count(*), count(*) filter (where s.status = 'rejected'), min(s.created_at), max(s.created_at)
	from submissions s join market_items m on m.id::text = s.payload->>'itemId'
	where s.kind = 'market_report' and m.user_id = $1
	group by s.payload->>'itemId'
)
select jsonb_build_object(
	'user', (select jsonb_build_object('id', id, 'telegramId', telegram_id, 'email', email, 'emailVerified', email_verified_at is not null,
		'username', tg_username, 'name', name, 'lang', lang, 'role', role::text, 'university', university, 'faculty', faculty,
		'course', course, 'city', city, 'isAdult', is_adult, 'consentAt', consent_at, 'createdAt', created_at,
		'lastSignIn', last_seen_at, 'referredBy', referred_by, 'botInactive', bot_inactive) from u),
	'scopes', coalesce((select jsonb_agg(jsonb_build_object('scope', g.scope, 'reason', g.reason, 'createdAt', g.created_at,
		'grantedBy', jsonb_build_object('id', gb.id, 'name', gb.name)) order by g.scope)
		from admin_grants g left join users gb on gb.id = g.granted_by where g.user_id = $1), '[]'),
	'block', (select jsonb_build_object('reason', b.reason, 'createdAt', b.created_at, 'by', jsonb_build_object('id', bb.id, 'name', bb.name))
		from user_blocks b left join users bb on bb.id = b.blocked_by where b.user_id = $1),
	'memberships', jsonb_build_object(
		'cafes', coalesce((select jsonb_agg(jsonb_build_object('id', c.id, 'nameKk', c.name_kk, 'nameRu', c.name_ru, 'role', s.role) order by c.id)
			from cafe_staff s join cafes c on c.id = s.cafe_id where s.user_id = $1), '[]'),
		'printShops', coalesce((select jsonb_agg(jsonb_build_object('id', p.id, 'nameKk', p.name_kk, 'nameRu', p.name_ru, 'role', s.role) order by p.id)
			from print_staff s join print_shops p on p.id = s.shop_id where s.user_id = $1), '[]'),
		'clubs', coalesce((select jsonb_agg(jsonb_build_object('id', c.id, 'nameKk', c.name_kk, 'nameRu', c.name_ru) order by c.id)
			from club_admins a join clubs c on c.id = a.club_id where a.user_id = $1), '[]'),
		'university', (select university from university_staff where user_id = $1),
		'feedback', coalesce((select jsonb_agg(jsonb_build_object('id', fr.id, 'university', fr.university, 'nameKk', fr.name_kk, 'nameRu', fr.name_ru) order by fr.id)
			from feedback_recipient_staff s join feedback_recipients fr on fr.id = s.recipient_id where s.user_id = $1), '[]'),
		'employer', (select jsonb_build_object('id', e.id, 'name', e.name, 'status', e.status, 'role', m.role)
			from employer_members m join employers e on e.id = m.employer_id where m.user_id = $1 order by e.id limit 1),
		'ambassador', (select jsonb_build_object('university', t.university, 'status', t.status, 'termEnd', t.term_end)
			from ambassador_terms t where t.user_id = $1 order by t.term_end desc limit 1),
		'mentor', (select jsonb_build_object('university', m.university, 'status', m.status) from mentors m where m.user_id = $1)
	),
	'activity', jsonb_build_object(
		'lastActive', (select to_char(max(day), 'YYYY-MM-DD') from user_active_days where user_id = $1),
		'activeDays30', (select count(*) from user_active_days where user_id = $1 and day > (now() at time zone 'Asia/Almaty')::date - 30),
		'cafeOrders', (select count(*) from cafe_orders where user_id = $1),
		'printJobs', (select count(*) from print_jobs where user_id = $1),
		'submissions', (select count(*) from submissions where submitted_by = $1),
		'reportsFiled', (select count(*) from reports where reporter_id = $1),
		'marketItems', (select count(*) from market_items where user_id = $1),
		'lostfoundPosts', (select count(*) from lostfound_posts where user_id = $1),
		'qaQuestions', (select count(*) from qa_questions where user_id = $1),
		'qaAnswers', (select count(*) from qa_answers where user_id = $1),
		'clubMemberships', (select count(*) from club_members where user_id = $1 and status = 'member'),
		'eventRegs', (select count(*) from event_regs where user_id = $1)
	),
	'reportsAgainst', coalesce((select jsonb_agg(jsonb_build_object('kind', kind, 'id', id, 'reports', reports, 'dismissed', dismissed,
		'firstAt', first_at, 'lastAt', last_at) order by last_at desc) from against), '[]'),
	'audit', coalesce((select jsonb_agg(x order by (x->>'id')::bigint desc) from (
		select jsonb_build_object('id', l.id, 'action', l.action, 'reason', l.reason, 'meta', l.meta, 'createdAt', l.created_at,
			'actor', case when l.actor_id is null then null else jsonb_build_object('id', a.id, 'name', a.name) end,
			'target', case when l.target_id is null then null else jsonb_build_object('id', t.id, 'name', t.name) end) as x
		from audit_log l left join users a on a.id = l.actor_id left join users t on t.id = l.target_id
		where l.target_id = $1 or l.actor_id = $1 order by l.id desc limit 50) q), '[]')
) from u`

type reasonBody struct {
	Reason  string `json:"reason"`
	Confirm string `json:"confirm"`
}

func readReason(w http.ResponseWriter, r *http.Request) (reasonBody, bool) {
	var b reasonBody
	if err := httpx.Decode(r, &b, 2<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return b, false
	}
	var ok bool
	if b.Reason, ok = cleanReason(b.Reason); !ok {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
		return b, false
	}
	return b, true
}

// cleanReason trims a required reason and checks its 3–500 characters.
func cleanReason(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 3 && n <= 500
}

var errAdminTarget = errors.New("admin: target is an admin")

// liveTarget locks the target row; refuses self and admins when noAdmin.
func liveTarget(ctx context.Context, tx pgx.Tx, actorID, id int64, noAdmin bool) error {
	var role string
	if err := tx.QueryRow(ctx, `select role::text from users where id = $1 and deleted_at is null for update`, id).Scan(&role); err != nil {
		return err
	}
	if id == actorID {
		return errSelf
	}
	if noAdmin && role == "admin" {
		return errAdminTarget
	}
	return nil
}

func targetErr(w http.ResponseWriter, r *http.Request, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
	case errors.Is(err, errSelf):
		httpx.Error(w, r, http.StatusBadRequest, "self", MsgSelf)
	case errors.Is(err, errAdminTarget):
		httpx.Error(w, r, http.StatusConflict, "admin_target", MsgAdminTarget)
	default:
		fail(w, r, what, err)
	}
	return true
}

func audit(ctx context.Context, tx pgx.Tx, actor int64, action string, target *int64, reason string, meta map[string]any) error {
	if meta == nil {
		meta = map[string]any{}
	}
	raw, _ := json.Marshal(meta)
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta) values ($1, $2, $3, nullif($4, ''), $5::jsonb)`,
		actor, action, target, reason, string(raw))
	return err
}

// block: POST /api/admin/users/{id}/block {"reason"}. The account keeps reading, signing in and deleting
// itself; every other write answers 403 account_restricted (auth.Write).
func block(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	b, ok := readReason(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	changed := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveTarget(ctx, tx, actor.ID, id, true); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `insert into user_blocks (user_id, reason, blocked_by) values ($1, $2, $3) on conflict (user_id) do nothing`,
			id, b.Reason, actor.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		changed = true
		return audit(ctx, tx, actor.ID, "user_block", &id, b.Reason, nil)
	})
	if targetErr(w, r, "block", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"blocked": true, "changed": changed})
}

// unblock: POST /api/admin/users/{id}/unblock {"reason"}.
func unblock(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	b, ok := readReason(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	changed := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveTarget(ctx, tx, actor.ID, id, false); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `delete from user_blocks where user_id = $1`, id)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		changed = true
		return audit(ctx, tx, actor.ID, "user_unblock", &id, b.Reason, nil)
	})
	if targetErr(w, r, "unblock", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"blocked": false, "changed": changed})
}

// deleteUser: POST /api/admin/users/{id}/delete {"reason", "confirm": "DELETE"} — a deletion the student asked
// for outside the app (lost access, wrote to support). It runs the same code path as DELETE /api/me
// (me.Delete: hooks, files, cascades); the audit row names the admin and the reason, never the deleted account.
func deleteUser(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	b, ok := readReason(w, r)
	if !ok {
		return
	}
	if b.Confirm != "DELETE" {
		httpx.Error(w, r, http.StatusBadRequest, "confirm_required", MsgConfirm)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error { return liveTarget(ctx, tx, actor.ID, id, true) })
	if targetErr(w, r, "delete check", err) {
		return
	}
	if err := me.Delete(ctx, id); err != nil {
		fail(w, r, "delete", err)
		return
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return audit(ctx, tx, actor.ID, "account_delete_admin", nil, b.Reason, nil)
	})
	if err != nil {
		fail(w, r, "delete audit", err) // the account is gone; the response says so honestly
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

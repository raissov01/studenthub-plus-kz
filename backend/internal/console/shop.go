package console

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// The shop workspace of a café or print shop: GET .../stats for every staff member (today's board numbers);
// revenue by day, the Kaspi session status and the team only for the shop's manager and admins. A manager adds
// and removes the shop's baristas / operators (never managers, never themselves); every change is audited.
// Kaspi login stays with admins (/admin/cafe, /admin/print).

type shop struct {
	kind, feature                    string
	shops, staff, key, orders, kaspi string
	worker                           string   // the non-manager staff role
	open                             []string // statuses on the board
}

var shops = map[string]shop{
	"cafe": {kind: "cafe", feature: "cafe", shops: "cafes", staff: "cafe_staff", key: "cafe_id", orders: "cafe_orders",
		kaspi: "cafe_kaspi_sessions", worker: "barista", open: []string{"new", "accepted", "ready"}},
	"print": {kind: "print", feature: "print", shops: "print_shops", staff: "print_staff", key: "shop_id", orders: "print_jobs",
		kaspi: "print_kaspi_sessions", worker: "operator", open: []string{"new", "printing", "ready"}},
}

func shopRoutes(mux httpx.Router) {
	for _, s := range shops {
		s := s
		h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.Require(s.feature, fn)) }
		h("GET /api/console/"+s.kind+"/{id}/stats", auth.Need(s.stats))
		h("POST /api/console/"+s.kind+"/{id}/team", auth.Write(auth.Need(s.addMember)))
		h("DELETE /api/console/"+s.kind+"/{id}/team/{userId}", auth.Write(auth.Need(s.removeMember)))
	}
}

var (
	MsgNotStaff  = httpx.Msg{KK: "Бұл панель тек осы орынның қызметкерлеріне арналған.", RU: "Эта панель только для сотрудников этого места."}
	MsgManager   = httpx.Msg{KK: "Бұл әрекет тек менеджерге қолжетімді.", RU: "Это действие доступно только менеджеру."}
	MsgNoUser    = httpx.Msg{KK: "Мұндай қолданушы табылмады. @username, email не id жазыңыз.", RU: "Пользователь не найден. Укажите @username, email или id."}
	MsgManyUsers = httpx.Msg{KK: "Бұл белгімен бірнеше аккаунт бар: email не id жазыңыз.", RU: "Под этим именем несколько аккаунтов: укажите email или id."}
	MsgSelf      = httpx.Msg{KK: "Өзіңізді өзгерте алмайсыз.", RU: "Нельзя изменить себя."}
	MsgNotWorker = httpx.Msg{KK: "Менеджерді тек әкімші өзгертеді.", RU: "Менеджера меняет только администратор."}
)

// roleAt: the caller's role at the shop: "manager", the worker role, "admin" (site admin, not on the staff),
// or "" (no access). ok is false when the shop does not exist.
func (s shop) roleAt(ctx context.Context, u *auth.User, id int64) (role string, ok bool, err error) {
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var r *string
		err := tx.QueryRow(ctx, `select (select role from `+s.staff+` where `+s.key+` = $1 and user_id = $2)
			from `+s.shops+` where id = $1`, id, u.ID).Scan(&r)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ok = true
		if r != nil {
			role = *r
		}
		return nil
	})
	if err == nil && ok && role == "" && u.Role == "admin" {
		role = "admin"
	}
	return role, ok, err
}

// access answers 404 for an unknown shop and 403 without a role there; managers says the manager part is needed.
func (s shop) access(w http.ResponseWriter, r *http.Request, u *auth.User, manager bool) (int64, string, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return 0, "", false
	}
	role, exists, err := s.roleAt(r.Context(), u, id)
	switch {
	case err != nil:
		fail(w, r, s.kind+" role", err)
		return 0, "", false
	case !exists:
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return 0, "", false
	case role == "":
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return 0, "", false
	case manager && role != "manager" && role != "admin":
		httpx.Error(w, r, http.StatusForbidden, "manager_only", MsgManager)
		return 0, "", false
	}
	return id, role, true
}

type dayRow struct {
	Day     string `json:"day"`
	Orders  int    `json:"orders"`  // created that day
	Done    int    `json:"done"`    // handed over that day
	Revenue int64  `json:"revenue"` // tiyn, handed-over orders that day (refunds excluded)
}

type teamRow struct {
	UserID   int64     `json:"userId"`
	Name     *string   `json:"name"`
	Username *string   `json:"username"`
	Role     string    `json:"role"`
	AddedAt  time.Time `json:"addedAt"`
}

type kaspiState struct {
	Connected   bool   `json:"connected"`
	Broken      bool   `json:"broken"`
	PhoneMasked string `json:"phoneMasked"`
	OrgName     string `json:"orgName"`
}

// stats: GET /api/console/{cafe|print}/{id}/stats?days=14 (1–90).
func (s shop) stats(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, role, ok := s.access(w, r, u, false)
	if !ok {
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 90 {
		days = 14
	}
	ctx := r.Context()
	manager := role == "manager" || role == "admin"
	now := time.Now()
	today := shopkit.DayStart(now)
	out := map[string]any{"role": role, "serverTime": now.UTC()}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var name struct {
			Kk, Ru, Mode string
			Active       bool
		}
		if err := tx.QueryRow(ctx, `select name_kk, name_ru, payment_mode, active from `+s.shops+` where id = $1`, id).
			Scan(&name.Kk, &name.Ru, &name.Mode, &name.Active); err != nil {
			return err
		}
		out["shop"] = map[string]any{"id": id, "kind": s.kind, "nameKk": name.Kk, "nameRu": name.Ru, "paymentMode": name.Mode, "active": name.Active}
		// today: every order created today plus the ones still open from before
		byStatus := map[string]int{}
		rows, err := tx.Query(ctx, `select status, count(*) from `+s.orders+`
			where `+s.key+` = $1 and (created_at >= $2 or status = any($3))
			  and (payment_method = 'pickup' or payment_status in ('paid', 'refunded'))
			group by status`, id, today, s.open)
		if err != nil {
			return err
		}
		for rows.Next() {
			var st string
			var n int
			if err := rows.Scan(&st, &n); err != nil {
				rows.Close()
				return err
			}
			byStatus[st] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var created, done int
		var revenue int64
		var refundManual int
		if err := tx.QueryRow(ctx, `select
				count(*) filter (where created_at >= $2),
				count(*) filter (where status = 'picked_up' and closed_at >= $2),
				coalesce(sum(coalesce(paid_amount, total)) filter (where status = 'picked_up' and closed_at >= $2 and payment_status <> 'refunded'), 0),
				count(*) filter (where refund_manual and payment_status = 'paid')
			from `+s.orders+` where `+s.key+` = $1`, id, today).Scan(&created, &done, &revenue, &refundManual); err != nil {
			return err
		}
		todayOut := map[string]any{"byStatus": byStatus, "created": created, "done": done, "refundsToReturn": refundManual}
		if manager {
			todayOut["revenue"] = revenue
		}
		out["today"] = todayOut
		if !manager {
			return nil
		}
		from := today.AddDate(0, 0, -(days - 1))
		rows, err = tx.Query(ctx, `with d as (select generate_series($2::date, $3::date, interval '1 day')::date as day)
			select to_char(d.day, 'YYYY-MM-DD'),
				(select count(*) from `+s.orders+` o where o.`+s.key+` = $1 and (o.created_at at time zone 'Asia/Almaty')::date = d.day),
				(select count(*) from `+s.orders+` o where o.`+s.key+` = $1 and o.status = 'picked_up' and (o.closed_at at time zone 'Asia/Almaty')::date = d.day),
				(select coalesce(sum(coalesce(o.paid_amount, o.total)), 0) from `+s.orders+` o where o.`+s.key+` = $1 and o.status = 'picked_up'
					and o.payment_status <> 'refunded' and (o.closed_at at time zone 'Asia/Almaty')::date = d.day)
			from d order by d.day`, id, from.Format(time.DateOnly), today.Format(time.DateOnly))
		if err != nil {
			return err
		}
		series, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (dayRow, error) {
			var d dayRow
			return d, row.Scan(&d.Day, &d.Orders, &d.Done, &d.Revenue)
		})
		if err != nil {
			return err
		}
		out["days"] = series
		var k kaspiState
		err = tx.QueryRow(ctx, `select true, broken, phone_masked, org_name from `+s.kaspi+` where `+s.key+` = $1`, id).
			Scan(&k.Connected, &k.Broken, &k.PhoneMasked, &k.OrgName)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out["kaspi"] = k
		team, err := s.team(ctx, tx, id)
		out["team"] = team
		return err
	})
	if err != nil {
		fail(w, r, s.kind+" stats", err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s shop) team(ctx context.Context, tx pgx.Tx, id int64) ([]teamRow, error) {
	rows, err := tx.Query(ctx, `select s.user_id, u.name, u.tg_username, s.role, s.created_at from `+s.staff+` s
		join users u on u.id = s.user_id where s.`+s.key+` = $1 order by s.role = 'manager' desc, s.created_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (teamRow, error) {
		var t teamRow
		return t, row.Scan(&t.UserID, &t.Name, &t.Username, &t.Role, &t.AddedAt)
	})
}

var reDigits = regexp.MustCompile(`^[0-9]{1,18}$`)

// FindUser resolves "@username", an email or a numeric user id to one live account.
func FindUser(ctx context.Context, tx pgx.Tx, handle string) (int64, error) {
	h := strings.TrimSpace(handle)
	var rows pgx.Rows
	var err error
	switch {
	case reDigits.MatchString(h):
		n, _ := strconv.ParseInt(h, 10, 64)
		rows, err = tx.Query(ctx, `select id from users where id = $1 and deleted_at is null`, n)
	case strings.Contains(h, "@") && !strings.HasPrefix(h, "@"):
		rows, err = tx.Query(ctx, `select id from users where lower(email) = lower($1) and deleted_at is null`, h)
	case h != "":
		rows, err = tx.Query(ctx, `select id from users where lower(tg_username) = lower($1) and deleted_at is null`, strings.TrimPrefix(h, "@"))
	default:
		return 0, pgx.ErrNoRows
	}
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	switch {
	case err != nil:
		return 0, err
	case len(ids) == 0:
		return 0, pgx.ErrNoRows
	case len(ids) > 1:
		return 0, errMany
	}
	return ids[0], nil
}

var errMany = errors.New("console: handle matches several accounts")

// addMember: POST .../team {"user": "@username | email | id", "role": "barista|operator|manager", "reason": ""}.
// A manager adds only the worker role; an admin may add a manager too.
func (s shop) addMember(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, role, ok := s.access(w, r, u, true)
	if !ok {
		return
	}
	var in struct {
		User   string `json:"user"`
		Role   string `json:"role"`
		Reason string `json:"reason"`
	}
	if httpx.Decode(r, &in, 2<<10) != nil || utf8.RuneCountInString(in.User) > 120 || utf8.RuneCountInString(in.Reason) > 500 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if in.Role == "" {
		in.Role = s.worker
	}
	if in.Role != s.worker && in.Role != "manager" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if in.Role == "manager" && role != "admin" {
		httpx.Error(w, r, http.StatusForbidden, "admin_only", MsgNotWorker)
		return
	}
	ctx := r.Context()
	var target int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if target, err = FindUser(ctx, tx, in.User); err != nil {
			return err
		}
		if target == u.ID {
			return errSelf
		}
		var cur *string
		if err := tx.QueryRow(ctx, `select (select role from `+s.staff+` where `+s.key+` = $1 and user_id = $2)`, id, target).Scan(&cur); err != nil {
			return err
		}
		if cur != nil && *cur == "manager" && role != "admin" {
			return errManager
		}
		if cur != nil && *cur == in.Role {
			return nil
		}
		if _, err := tx.Exec(ctx, `insert into `+s.staff+` (`+s.key+`, user_id, role, added_by) values ($1, $2, $3, $4)
			on conflict (`+s.key+`, user_id) do update set role = excluded.role`, id, target, in.Role, u.ID); err != nil {
			return err
		}
		return shopkit.AuditStaff(ctx, tx, u.ID, "staff_grant", target, s.kind, id, in.Role, in.Reason)
	})
	if s.refused(w, r, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "userId": target})
}

var errSelf, errManager = errors.New("console: self"), errors.New("console: manager")

func (s shop) refused(w http.ResponseWriter, r *http.Request, err error) bool {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return false
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
	case errors.Is(err, errMany):
		httpx.Error(w, r, http.StatusConflict, "ambiguous_user", MsgManyUsers)
	case errors.Is(err, errSelf):
		httpx.Error(w, r, http.StatusBadRequest, "self", MsgSelf)
	case errors.Is(err, errManager):
		httpx.Error(w, r, http.StatusForbidden, "admin_only", MsgNotWorker)
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
	default:
		fail(w, r, s.kind+" team", err)
	}
	return true
}

// removeMember: DELETE .../team/{userId}. A manager removes workers only.
func (s shop) removeMember(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, role, ok := s.access(w, r, u, true)
	if !ok {
		return
	}
	target, ok := pathID(r, "userId")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if target == u.ID {
			return errSelf
		}
		var cur string
		if err := tx.QueryRow(ctx, `select role from `+s.staff+` where `+s.key+` = $1 and user_id = $2 for update`, id, target).Scan(&cur); err != nil {
			return err
		}
		if cur == "manager" && role != "admin" {
			return errManager
		}
		if _, err := tx.Exec(ctx, `delete from `+s.staff+` where `+s.key+` = $1 and user_id = $2`, id, target); err != nil {
			return err
		}
		return shopkit.AuditStaff(ctx, tx, u.ID, "staff_revoke", target, s.kind, id, cur, "")
	})
	if s.refused(w, r, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

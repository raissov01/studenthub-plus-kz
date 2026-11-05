package admin

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// CSV exports of /admin/exports (admin only): Excel-friendly UTF-8 with a BOM, CRLF lines, formula-looking cells
// neutralised. Every export is itself audited (action "export", the kind and the filters, never the rows).
// Feedback exports metadata only (no text, no author); survey answers stay in the per-survey aggregate CSV
// (GET /api/staff/surveys/{id}/results.csv), which hides every group under 5.

const exportMax = 50000

// exportKinds maps GET /api/admin/export/{kind}.csv to its builder.
var exportKinds = map[string]func(r *http.Request) (header []string, query string, args []any, ok bool){
	"users": func(r *http.Request) ([]string, string, []any, bool) {
		f, ok := parseList(r)
		cond, args := f.where()
		return []string{"id", "name", "username", "email", "telegram_id", "role", "scopes", "university", "faculty", "course", "city",
				"consent_at", "created_at", "last_active", "blocked"},
			`select u.id::text, coalesce(u.name, ''), coalesce(u.tg_username, ''), coalesce(u.email, ''), coalesce(u.telegram_id::text, ''),
				u.role::text, array_to_string(array(select g.scope from admin_grants g where g.user_id = u.id order by g.scope), ' '),
				coalesce(u.university, ''), coalesce(u.faculty, ''), coalesce(u.course::text, ''), coalesce(u.city, ''),
				coalesce(to_char(u.consent_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), ''),
				to_char(u.created_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'),
				coalesce((select to_char(max(a.day), 'YYYY-MM-DD') from user_active_days a where a.user_id = u.id), ''),
				case when exists (select 1 from user_blocks b where b.user_id = u.id) then 'yes' else '' end
			from users u where ` + cond + ` order by u.id`, args, ok
	},
	"audit": func(r *http.Request) ([]string, string, []any, bool) {
		f, ok := parseAudit(r)
		return []string{"id", "created_at", "action", "actor_id", "actor_name", "target_id", "target_name", "reason", "meta"},
			`select l.id::text, to_char(l.created_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI:SS'), l.action,
				coalesce(l.actor_id::text, ''), coalesce(a.name, ''), coalesce(l.target_id::text, ''), coalesce(t.name, ''),
				coalesce(l.reason, ''), l.meta::text
			from audit_log l left join users a on a.id = l.actor_id left join users t on t.id = l.target_id
			where ` + auditWhere + ` order by l.id desc`, f.args(), ok
	},
	"cafe-orders": func(r *http.Request) ([]string, string, []any, bool) {
		from, to, shop, ok := periodShop(r)
		return []string{"id", "cafe", "created_at", "status", "payment_method", "payment_status", "total_tenge", "paid_tenge",
				"pickup_at", "closed_at", "refund_manual", "customer_id"},
			`select o.id::text, c.name_ru, to_char(o.created_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), o.status,
				o.payment_method, o.payment_status, (o.total / 100)::text, coalesce((o.paid_amount / 100)::text, ''),
				to_char(o.pickup_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'),
				coalesce(to_char(o.closed_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), ''),
				case when o.refund_manual then 'yes' else '' end, coalesce(o.user_id::text, '')
			from cafe_orders o join cafes c on c.id = o.cafe_id
			where ($1::timestamptz is null or o.created_at >= $1) and ($2::timestamptz is null or o.created_at < $2) and ($3 = 0 or o.cafe_id = $3)
			order by o.id`, []any{from, to, shop}, ok
	},
	"print-jobs": func(r *http.Request) ([]string, string, []any, bool) {
		from, to, shop, ok := periodShop(r)
		return []string{"id", "shop", "created_at", "status", "price_state", "payment_method", "payment_status", "total_tenge", "paid_tenge",
				"closed_at", "refund_manual", "customer_id"},
			`select j.id::text, p.name_ru, to_char(j.created_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), j.status, j.price_state,
				j.payment_method, j.payment_status, (j.total / 100)::text, coalesce((j.paid_amount / 100)::text, ''),
				coalesce(to_char(j.closed_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), ''),
				case when j.refund_manual then 'yes' else '' end, coalesce(j.user_id::text, '')
			from print_jobs j join print_shops p on p.id = j.shop_id
			where ($1::timestamptz is null or j.created_at >= $1) and ($2::timestamptz is null or j.created_at < $2) and ($3 = 0 or j.shop_id = $3)
			order by j.id`, []any{from, to, shop}, ok
	},
	"feedback": func(r *http.Request) ([]string, string, []any, bool) {
		from, to, _, ok := periodShop(r)
		return []string{"id", "university", "recipient", "category", "status", "anonymous", "flag", "created_at", "updated_at",
				"sla_due_at", "overdue", "staff_replies"},
			`select t.id::text, fr.university, fr.name_ru, t.category, t.status, case when t.anonymous then 'yes' else '' end,
				coalesce(t.flag, ''), to_char(t.created_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'),
				to_char(t.updated_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'),
				to_char(t.sla_due_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'),
				case when t.status in ('new', 'in_progress') and t.sla_due_at < now() then 'yes' else '' end,
				(select count(*) from feedback_messages m where m.ticket_id = t.id and m.from_staff)::text
			from feedback_tickets t join feedback_recipients fr on fr.id = t.recipient_id
			where ($1::timestamptz is null or t.created_at >= $1) and ($2::timestamptz is null or t.created_at < $2)
			order by t.id`, []any{from, to}, ok
	},
	"surveys": func(r *http.Request) ([]string, string, []any, bool) {
		from, to, _, ok := periodShop(r)
		return []string{"id", "university", "title", "status", "anonymous", "respondents", "questions", "created_at", "published_at", "closes_at"},
			`select s.id::text, s.university, s.title, s.status, case when s.anonymous then 'yes' else '' end,
				(select count(*) from survey_markers m where m.survey_id = s.id)::text, jsonb_array_length(s.questions)::text,
				to_char(s.created_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'),
				coalesce(to_char(s.published_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), ''),
				coalesce(to_char(s.closes_at at time zone 'Asia/Almaty', 'YYYY-MM-DD HH24:MI'), '')
			from surveys s
			where ($1::timestamptz is null or s.created_at >= $1) and ($2::timestamptz is null or s.created_at < $2)
			order by s.id`, []any{from, to}, ok
	},
}

// periodShop reads ?from=&to= (YYYY-MM-DD, Asia/Almaty, to inclusive) and ?shop=.
func periodShop(r *http.Request) (from, to *time.Time, shop int64, ok bool) {
	q := r.URL.Query()
	var okFrom, okTo bool
	from, okFrom = parseDate(q.Get("from"))
	to, okTo = parseDate(q.Get("to"))
	if to != nil {
		t := to.AddDate(0, 0, 1)
		to = &t
	}
	var err error
	if v := q.Get("shop"); v != "" {
		shop, err = strconv.ParseInt(v, 10, 64)
	}
	return from, to, shop, okFrom && okTo && err == nil && shop >= 0
}

// csvCell keeps a spreadsheet from running a cell as a formula (=, +, -, @, tab, CR at the start).
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return "'" + s
		}
	}
	return s
}

// export: GET /api/admin/export/{kind}.csv
func export(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	name := r.PathValue("file")
	kind, isCSV := strings.CutSuffix(name, ".csv")
	build, known := exportKinds[kind]
	if !isCSV || !known {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	header, query, args, ok := build(r)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // UTF-8 BOM: Excel opens the file as UTF-8
	cw := csv.NewWriter(&buf)
	cw.UseCRLF = true
	n := 0
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := cw.Write(header); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, query+fmt.Sprintf(" limit %d", exportMax), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		rec := make([]string, len(header))
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				return err
			}
			for i, v := range vals {
				s, _ := v.(string)
				rec[i] = csvCell(s)
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
			n++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
		filters := map[string]any{}
		for k, v := range r.URL.Query() {
			switch {
			case len(v) == 0 || v[0] == "":
			case k == "q": // a typed search can be a name or an email: not kept in the log
				filters[k] = "…"
			default:
				filters[k] = v[0]
			}
		}
		return audit(ctx, tx, actor.ID, "export", nil, "", map[string]any{"kind": kind, "rows": n, "filters": filters})
	})
	if err != nil {
		fail(w, r, "export "+kind, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="studenthub-%s-%s.csv"`, kind, time.Now().In(almaty).Format("2006-01-02")))
	w.Header().Set("X-Row-Count", strconv.Itoa(n))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

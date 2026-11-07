package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// payments: GET /api/admin/payments?kind=cafe|print&shop=&status=&payment=&method=&from=&to=&page=
// The money of café orders and print jobs in one list (kurs PaymentsPage, adapted): the rows, the totals of the
// whole filter, revenue by day and the shops the caller may see. The super admin sees every shop; a shop manager
// only their own shops (another shop answers 403); anyone else 403. Revenue = handed-over orders, refunds
// excluded, the same rule as the overview and the shop panel. Marking a manual refund as returned uses the shop
// endpoints (POST /api/cafe/staff/orders/{id}/refunded, /api/print/staff/jobs/{id}/refunded), audited there.

type payRow struct {
	Kind         string     `json:"kind"`
	ID           int64      `json:"id"`
	ShopID       int64      `json:"shopId"`
	ShopKK       string     `json:"shopKk"`
	ShopRU       string     `json:"shopRu"`
	Customer     *string    `json:"customer"`
	CustomerID   *int64     `json:"customerId"`
	Total        int64      `json:"total"`
	Paid         *int64     `json:"paid"`
	Status       string     `json:"status"`
	Method       string     `json:"method"`
	Payment      string     `json:"payment"`
	RefundManual bool       `json:"refundManual"`
	CreatedAt    time.Time  `json:"createdAt"`
	ClosedAt     *time.Time `json:"closedAt"`
}

type payShop struct {
	Kind   string `json:"kind"`
	ID     int64  `json:"id"`
	NameKK string `json:"nameKk"`
	NameRU string `json:"nameRu"`
}

type payDay struct {
	Day     string `json:"day"`
	Orders  int    `json:"orders"`
	Revenue int64  `json:"revenue"`
}

const payUnion = `select 'cafe' as kind, o.id, o.cafe_id as shop_id, c.name_kk, c.name_ru, u.name as customer, o.user_id, o.total, o.paid_amount,
		o.status, o.payment_method, o.payment_status, o.refund_manual, o.created_at, o.closed_at
	from cafe_orders o join cafes c on c.id = o.cafe_id left join users u on u.id = o.user_id
	where $1 and (o.cafe_id = any($2) or $3)
	union all
	select 'print', j.id, j.shop_id, p.name_kk, p.name_ru, u.name, j.user_id, j.total, j.paid_amount,
		j.status, j.payment_method, j.payment_status, j.refund_manual, j.created_at, j.closed_at
	from print_jobs j join print_shops p on p.id = j.shop_id left join users u on u.id = j.user_id
	where $4 and (j.shop_id = any($5) or $3)`

// payFilter: $6 kind, $7 shop, $8 status, $9 payment, $10 method, $11 from, $12 to (exclusive)
const payFilter = `($6 = '' or x.kind = $6) and ($7 = 0 or x.shop_id = $7) and ($8 = '' or x.status = $8)
	and ($9 = '' or ($9 = 'refund_manual' and x.refund_manual) or x.payment_status = $9)
	and ($10 = '' or x.payment_method = $10)
	and ($11::timestamptz is null or x.created_at >= $11) and ($12::timestamptz is null or x.created_at < $12)`

func payments(w http.ResponseWriter, r *http.Request, u *auth.User) {
	q := r.URL.Query()
	kind, status, payment, method := q.Get("kind"), q.Get("status"), q.Get("payment"), q.Get("method")
	var shop int64
	var err error
	if v := q.Get("shop"); v != "" {
		if shop, err = strconv.ParseInt(v, 10, 64); err != nil || shop <= 0 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
	}
	from, okFrom := parseDate(q.Get("from"))
	to, okTo := parseDate(q.Get("to"))
	if to != nil {
		t := to.AddDate(0, 0, 1)
		to = &t
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	okStatus := map[string]bool{"": true, "new": true, "accepted": true, "printing": true, "ready": true, "picked_up": true, "cancelled": true, "no_show": true}
	okPay := map[string]bool{"": true, "unpaid": true, "pending": true, "paid": true, "refunded": true, "failed": true, "refund_manual": true}
	if !okFrom || !okTo || page > 1000 || (kind != "" && kind != "cafe" && kind != "print") || (shop != 0 && kind == "") ||
		!okStatus[status] || !okPay[payment] || (method != "" && method != "pickup" && method != "kaspi") {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	cafeOn, printOn := features.Enabled("cafe"), features.Enabled("print")
	if !cafeOn && !printOn {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	all := u.Role == "admin"
	const limit = 50
	out := map[string]any{"page": page, "limit": limit, "scope": map[bool]string{true: "all", false: "managed"}[all]}
	forbidden := false
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		cafes, prints := []int64{}, []int64{}
		if !all {
			var err error
			if cafes, prints, err = managedShops(ctx, tx, u.ID); err != nil {
				return err
			}
			mine := (kind == "cafe" && contains(cafes, shop)) || (kind == "print" && contains(prints, shop))
			if len(cafes)+len(prints) == 0 || (shop != 0 && !mine) {
				forbidden = true
				return nil
			}
		}
		args := []any{cafeOn, cafes, all, printOn, prints, kind, shop, status, payment, method, from, to}
		base := `with x as (` + payUnion + `) `
		var totals struct {
			Count, ToReturn               int
			Revenue, Paid, Refunded, Owed int64
		}
		if err := tx.QueryRow(ctx, base+`select count(*),
				count(*) filter (where x.refund_manual and x.payment_status = 'paid'),
				coalesce(sum(coalesce(x.paid_amount, x.total)) filter (where x.status = 'picked_up' and x.payment_status <> 'refunded'), 0),
				coalesce(sum(x.paid_amount) filter (where x.payment_status = 'paid'), 0),
				coalesce(sum(coalesce(x.paid_amount, x.total)) filter (where x.payment_status = 'refunded'), 0),
				coalesce(sum(coalesce(x.paid_amount, x.total)) filter (where x.refund_manual and x.payment_status = 'paid'), 0)
			from x where `+payFilter, args...).Scan(&totals.Count, &totals.ToReturn, &totals.Revenue, &totals.Paid, &totals.Refunded, &totals.Owed); err != nil {
			return err
		}
		out["totals"] = map[string]any{"count": totals.Count, "revenue": totals.Revenue, "paidKaspi": totals.Paid, "refunded": totals.Refunded,
			"toReturn": totals.ToReturn, "toReturnSum": totals.Owed}
		rows, err := tx.Query(ctx, base+`select x.* from x where `+payFilter+` order by x.created_at desc, x.id desc limit `+
			strconv.Itoa(limit)+` offset `+strconv.Itoa((page-1)*limit), args...)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (payRow, error) {
			var p payRow
			return p, row.Scan(&p.Kind, &p.ID, &p.ShopID, &p.ShopKK, &p.ShopRU, &p.Customer, &p.CustomerID, &p.Total, &p.Paid,
				&p.Status, &p.Method, &p.Payment, &p.RefundManual, &p.CreatedAt, &p.ClosedAt)
		})
		if err != nil {
			return err
		}
		out["rows"] = list
		// revenue by handover day over the chosen range (default: the last 30 days)
		end := shopkit.DayStart(time.Now())
		start := end.AddDate(0, 0, -29)
		if from != nil {
			start = *from
		}
		if to != nil {
			end = to.AddDate(0, 0, -1)
		}
		if end.Sub(start) > 366*24*time.Hour || end.Before(start) {
			start = end.AddDate(0, 0, -29)
		}
		rows, err = tx.Query(ctx, base+`, d as (select generate_series($13::date, $14::date, interval '1 day')::date as day)
			select to_char(d.day, 'YYYY-MM-DD'),
				(select count(*) from x where `+payFilter+` and (x.created_at at time zone 'Asia/Almaty')::date = d.day),
				(select coalesce(sum(coalesce(x.paid_amount, x.total)), 0) from x where `+payFilter+` and x.status = 'picked_up'
					and x.payment_status <> 'refunded' and (x.closed_at at time zone 'Asia/Almaty')::date = d.day)
			from d order by d.day`, append(args, start.Format(time.DateOnly), end.Format(time.DateOnly))...)
		if err != nil {
			return err
		}
		days, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (payDay, error) {
			var d payDay
			return d, row.Scan(&d.Day, &d.Orders, &d.Revenue)
		})
		if err != nil {
			return err
		}
		out["days"] = days
		rows, err = tx.Query(ctx, `select 'cafe', id, name_kk, name_ru from cafes where $1 and ($2 or id = any($3))
			union all select 'print', id, name_kk, name_ru from print_shops where $4 and ($2 or id = any($5)) order by 1, 3`, cafeOn, all, cafes, printOn, prints)
		if err != nil {
			return err
		}
		shops, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (payShop, error) {
			var s payShop
			return s, row.Scan(&s.Kind, &s.ID, &s.NameKK, &s.NameRU)
		})
		out["shops"] = shops
		return err
	})
	if err != nil {
		fail(w, r, "payments", err)
		return
	}
	if forbidden {
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func contains(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

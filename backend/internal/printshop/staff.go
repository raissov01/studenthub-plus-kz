package printshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// Staff are print_staff rows (operator or manager of one shop), not a user role. The board reads through
// db.WithUser, so RLS alone keeps every other shop's jobs and files out of it.

func staffRoutes(h func(string, http.HandlerFunc)) {
	h("GET /api/print/staff/jobs", auth.Need(staffJobs))
	for _, a := range []string{"printing", "ready", "picked-up", "no-show", "decline"} {
		h("POST /api/print/staff/jobs/{id}/"+a, auth.Write(auth.Need(staffAction(a))))
	}
	h("POST /api/print/staff/jobs/{id}/price", auth.Write(auth.Need(adjustPrice)))
	h("POST /api/print/staff/jobs/{id}/refunded", auth.Write(auth.Need(markRefunded)))
	counterRoutes(h)
	h("GET /api/print/staff/catalog", auth.Need(staffCatalog))
	h("POST /api/print/staff/services", auth.Write(auth.Need(saveService)))
	h("PUT /api/print/staff/services/{id}", auth.Write(auth.Need(saveService)))
	h("POST /api/print/staff/goods", auth.Write(auth.Need(saveGoods)))
	h("PUT /api/print/staff/goods/{id}", auth.Write(auth.Need(saveGoods)))
	h("POST /api/print/staff/goods/{id}/stock", auth.Write(auth.Need(setStock)))
}

type membership struct {
	ShopID int64
	Role   string
}

func memberships(ctx context.Context, userID int64) ([]membership, error) {
	var out []membership
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select shop_id, role from print_staff where user_id = app_user_id() order by shop_id`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (membership, error) {
			var m membership
			return m, row.Scan(&m.ShopID, &m.Role)
		})
		return err
	})
	return out, err
}

// roleAt: the caller's role at the shop ("" = none). An admin may manage every shop's price list.
func roleAt(ctx context.Context, u *auth.User, shopID int64) (string, error) {
	ms, err := memberships(ctx, u.ID)
	if err != nil {
		return "", err
	}
	for _, m := range ms {
		if m.ShopID == shopID {
			return m.Role, nil
		}
	}
	if u.Role == "admin" {
		return "admin", nil
	}
	return "", nil
}

func pickShop(r *http.Request, ms []membership) (int64, bool) {
	if v := r.URL.Query().Get("shop"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		return id, err == nil && id > 0
	}
	if len(ms) > 0 {
		return ms[0].ShopID, true
	}
	return 0, false
}

// ---------- GET /api/print/staff/jobs?shop=&status= ----------

var boardStatuses = map[string][]string{
	"":         {"new", "printing", "ready"},
	"open":     {"new", "printing", "ready"},
	"new":      {"new"},
	"printing": {"printing"},
	"ready":    {"ready"},
	"done":     {"picked_up", "no_show", "cancelled"},
}

func staffJobs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	ms, err := memberships(ctx, u.ID)
	if err != nil {
		fail(w, r, "memberships", err)
		return
	}
	if len(ms) == 0 {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	shopID, ok := pickShop(r, ms)
	statuses, known := boardStatuses[r.URL.Query().Get("status")]
	if !ok || !known {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	mine := false
	for _, m := range ms {
		mine = mine || m.ShopID == shopID
	}
	if !mine {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	now := Now()
	shops := []*Shop{}
	jobs := []*Job{}
	counts := map[string]int{}
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+shopCols+`, st.role from print_shops s join print_staff st on st.shop_id = s.id and st.user_id = app_user_id() order by s.id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var role string
			s, err := scanShop(rows, &role)
			if err != nil {
				rows.Close()
				return err
			}
			s.MyRole = role
			shops = append(shops, s.public(now))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// the queue in order (the print order is the stack order); a closed job stays visible for today. Each
		// column holds at most boardCap jobs, so a long «Дайын» pile never pushes the new jobs out of view.
		rows, err = tx.Query(ctx, `select `+jobCols+jobFrom+`
			where j.id in (select id from (select id, row_number() over (partition by status order by queued_at, id) as n from print_jobs
				where shop_id = $1 and status = any($2) and (status in ('new', 'printing', 'ready') or closed_at >= $3)) x where x.n <= $4)
			order by j.queued_at, j.id`, shopID, statuses, shopkit.DayStart(now), boardCap)
		if err != nil {
			return err
		}
		jobs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Job, error) { return scanJob(row) })
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select status, count(*) from print_jobs where shop_id = $1 and status = any($2)
			and (status in ('new', 'printing', 'ready') or closed_at >= $3) group by status`, shopID, statuses, shopkit.DayStart(now))
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
			counts[st] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		return withFiles(ctx, tx, jobs, u.ID)
	})
	if err == nil {
		err = forStaff(ctx, jobs)
	}
	if err != nil {
		fail(w, r, "board", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"shopId": shopID, "shops": shops, "jobs": jobs, "counts": counts, "serverTime": now.UTC()})
}

// ---------- transitions ----------

type transition struct {
	from  []string
	to    string
	event string
	at    string
}

var transitions = map[string]transition{
	"printing":  {from: []string{"new"}, to: "printing", event: "printing", at: "printing_at"},
	"ready":     {from: []string{"new", "printing"}, to: "ready", event: "ready", at: "ready_at"},
	"picked-up": {from: []string{"ready"}, to: "picked_up", event: "picked_up", at: "closed_at"},
	"no-show":   {from: []string{"ready"}, to: "no_show", event: "no_show", at: "closed_at"},
	"decline":   {from: []string{"new", "printing"}, to: "cancelled", event: "declined", at: "closed_at"},
}

// Act moves a job of the staff member's shop. Exported for the bot buttons (printing, ready: no handover there).
func Act(ctx context.Context, u *auth.User, jobID int64, action, reason string) (*Job, error) {
	return act(ctx, u, jobID, action, reason, "")
}

// act: a handover (picked-up) needs proof — the code the student said, or the QR from their screen — that
// matches this job, so the operator cannot hand one student's stack to another by a slip of the finger.
func act(ctx context.Context, u *auth.User, jobID int64, action, reason, proof string) (*Job, error) {
	t, ok := transitions[action]
	if !ok {
		return nil, refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
	}
	now := Now()
	var j *Job
	var paid bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var role *string
		err := tx.QueryRow(ctx, `select (select s.role from print_staff s where s.shop_id = j.shop_id and s.user_id = $2) from print_jobs j where j.id = $1 for update of j`,
			jobID, u.ID).Scan(&role)
		// a foreign shop's job looks exactly like a missing one
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && role == nil) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		cur, err := loadJob(ctx, tx, jobID)
		if err != nil {
			return err
		}
		if cur.Status == t.to {
			j = cur
			return nil // a double tap
		}
		allowed := false
		for _, f := range t.from {
			allowed = allowed || f == cur.Status
		}
		if !allowed {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if action == "picked-up" && !proofMatches(cur, proof) {
			return refuse(http.StatusConflict, "code_mismatch", MsgMismatch)
		}
		if action != "decline" && !workable(cur) {
			if cur.PriceState != "final" {
				return refuse(http.StatusConflict, "price_pending", MsgPriceOpen)
			}
			return refuse(http.StatusConflict, "not_paid", MsgNotPaid)
		}
		if action == "no-show" {
			since := cur.ReadyAt
			if cur.PickupAt != nil && (since == nil || cur.PickupAt.After(*since)) {
				since = cur.PickupAt
			}
			if since == nil || now.Before(since.Add(time.Duration(noShowGrace())*time.Minute)) {
				return refuse(http.StatusConflict, "too_early", MsgNoShowSoon)
			}
		}
		set := t.at + " = now()"
		if action == "ready" && cur.Status == "new" {
			set += ", printing_at = now()"
		}
		if action == "picked-up" && cur.PaymentMethod == "pickup" {
			set += ", payment_status = 'paid'" // paid at the counter: Student Hub never saw the money
		}
		args := []any{jobID, t.to}
		if action == "decline" {
			set += ", cancel_reason = $3, price_state = 'final', proposed_total = null"
			args = append(args, reason)
		}
		if _, err := tx.Exec(ctx, `update print_jobs set status = $2, `+set+`, updated_at = now() where id = $1`, args...); err != nil {
			return err
		}
		detail := map[string]any{"from": cur.Status}
		if reason != "" {
			detail["reason"] = reason
		}
		if action == "picked-up" && cur.PaymentMethod == "pickup" {
			detail["paidAtCounter"] = true
		}
		if err := event(ctx, tx, jobID, uid(u), "staff", t.event, detail); err != nil {
			return err
		}
		paid = action == "decline" && cur.paymentStatus == "paid"
		j, err = loadJob(ctx, tx, jobID)
		return err
	})
	if err != nil {
		return nil, err
	}
	switch action {
	case "ready":
		go notifyStudent(context.WithoutCancel(ctx), jobID, "ready")
	case "decline":
		go func() {
			bg := context.WithoutCancel(ctx)
			notifyStudent(bg, jobID, "declined")
			if paid {
				refund(bg, jobID, uid(u), "staff")
			}
		}()
	}
	return j, nil
}

func staffAction(action string) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		id, ok := pathID(r, "id")
		if !ok {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
		var body struct {
			Reason string `json:"reason"`
			Code   string `json:"code"`  // picked-up: the code the student said
			Token  string `json:"token"` // picked-up: the QR from the student's screen
		}
		_ = httpx.Decode(r, &body, 2<<10)
		body.Reason = strings.TrimSpace(body.Reason)
		if utf8.RuneCountInString(body.Reason) > 200 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		proof := strings.TrimSpace(body.Code)
		if body.Token != "" {
			proof = strings.TrimSpace(body.Token)
		}
		if len(proof) > 80 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		j, err := act(r.Context(), u, id, action, body.Reason, proof)
		if err != nil {
			reply(w, r, "staff "+action, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
	}
}

// POST /api/print/staff/jobs/{id}/price {"total": <tiyn>, "reason": "..."}: the operator looked at the files.
// The same total on a job in review confirms it (no answer needed); another total becomes a proposal the student
// approves before paying or printing. A paid job's price is settled at the counter, not here.
func adjustPrice(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Total  int64  `json:"total"`
		Reason string `json:"reason"`
	}
	if !ok || httpx.Decode(r, &in, 2<<10) != nil || in.Total < 100 || in.Total > maxTotal() || in.Total%100 != 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) > 300 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	proposed := false
	var j *Job
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var role *string
		var status, state, pay string
		var total int64
		err := tx.QueryRow(ctx, `select j.status, j.price_state, j.payment_status, j.total, (select s.role from print_staff s where s.shop_id = j.shop_id and s.user_id = $2)
			from print_jobs j where j.id = $1 for update of j`, id, u.ID).Scan(&status, &state, &pay, &total, &role)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && role == nil) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if status != "new" {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if pay == "paid" || pay == "pending" || pay == "refunded" {
			return refuse(http.StatusConflict, "already_paid", MsgPaidAlrdy)
		}
		if in.Total == total && state != "proposed" {
			if state == "final" {
				return nil
			}
			if _, err := tx.Exec(ctx, `update print_jobs set price_state = 'final', updated_at = now() where id = $1`, id); err != nil {
				return err
			}
			return event(ctx, tx, id, uid(u), "staff", "price_confirmed", map[string]any{"total": total})
		}
		if in.Total == total && state == "proposed" {
			// back to the original price: nothing for the student to approve
			if _, err := tx.Exec(ctx, `update print_jobs set price_state = 'final', proposed_total = null, adjust_reason = null, updated_at = now() where id = $1`, id); err != nil {
				return err
			}
			return event(ctx, tx, id, uid(u), "staff", "price_confirmed", map[string]any{"total": total})
		}
		if utf8.RuneCountInString(in.Reason) < 3 {
			return refuse(http.StatusBadRequest, "reason_required", MsgReason)
		}
		if _, err := tx.Exec(ctx, `update print_jobs set price_state = 'proposed', proposed_total = $2, adjust_reason = $3, updated_at = now() where id = $1`,
			id, in.Total, in.Reason); err != nil {
			return err
		}
		proposed = true
		return event(ctx, tx, id, uid(u), "staff", "price_proposed", map[string]any{"from": total, "to": in.Total, "reason": in.Reason})
	})
	if err == nil {
		err = db.Tx(ctx, func(tx pgx.Tx) error {
			var e error
			j, e = loadJob(ctx, tx, id)
			return e
		})
	}
	if err != nil {
		reply(w, r, "price", err)
		return
	}
	if proposed {
		go notifyStudent(context.WithoutCancel(ctx), id, "price_proposed")
	} else if j.PaymentMethod == "kaspi" && j.PriceState == "final" {
		go notifyStudent(context.WithoutCancel(ctx), id, "price_confirmed")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
}

// POST /api/print/staff/jobs/{id}/refunded: a manager returned the money by hand in Kaspi Pay.
func markRefunded(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var j *Job
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var manual bool
		var role *string
		err := tx.QueryRow(ctx, `select j.refund_manual, (select s.role from print_staff s where s.shop_id = j.shop_id and s.user_id = $2)
			from print_jobs j where j.id = $1 for update of j`, id, u.ID).Scan(&manual, &role)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && role == nil && u.Role != "admin") {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if u.Role != "admin" && *role != "manager" {
			return refuse(http.StatusForbidden, "manager_only", MsgManager)
		}
		if !manual {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if _, err := tx.Exec(ctx, `update print_jobs set refund_manual = false, payment_status = 'refunded', updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		kind := "staff"
		if role == nil {
			kind = "admin"
		}
		if err := event(ctx, tx, id, uid(u), kind, "refunded_manually", nil); err != nil {
			return err
		}
		j, err = loadJob(ctx, tx, id)
		return err
	})
	if err != nil {
		reply(w, r, "refunded", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
}

// BotAction is bot.PrintAction: "p<id>" starts printing, "r<id>" marks ready, for the staff member behind the update.
func BotAction(ctx context.Context, userID int64, value string) (toast, nextLabel, nextData, linkLabel, link string, err error) {
	var u *auth.User
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var e error
		u, e = auth.ScanUser(tx.QueryRow(ctx, `select `+auth.UserColumns+` from users where id = $1 and deleted_at is null`, userID))
		return e
	})
	if err != nil || len(value) < 2 {
		return "", "", "", "", "", nil // not a site account: nothing to do
	}
	lang := u.Lang
	id, perr := strconv.ParseInt(value[1:], 10, 64)
	action := map[byte]string{'p': "printing", 'r': "ready"}[value[0]]
	if perr != nil || action == "" {
		return "", "", "", "", "", nil
	}
	j, aerr := Act(ctx, u, id, action, "")
	var ae *apiErr
	if errors.As(aerr, &ae) {
		return shopkit.Pick(lang, ae.msg.KK, ae.msg.RU), "", "", "", "", nil
	}
	if aerr != nil {
		return "", "", "", "", "", aerr
	}
	if b := boardButton(lang); len(b) > 0 {
		linkLabel, link = b[0].Text, b[0].URL
	}
	if j.Status == "printing" {
		return shopkit.Pick(lang, fmt.Sprintf("№%s басылып жатыр", j.PickupCode), fmt.Sprintf("№%s печатается", j.PickupCode)),
			shopkit.Pick(lang, "Дайын", "Готово"), fmt.Sprintf("print:r%d", j.ID), linkLabel, link, nil
	}
	return shopkit.Pick(lang, fmt.Sprintf("№%s дайын", j.PickupCode), fmt.Sprintf("№%s готов", j.PickupCode)), "", "", linkLabel, link, nil
}

// ---------- price list (any staff: stock; manager and admin: everything) ----------

func staffCatalog(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	ms, err := memberships(ctx, u.ID)
	if err != nil {
		fail(w, r, "memberships", err)
		return
	}
	shopID, ok := pickShop(r, ms)
	if !ok {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	role, err := roleAt(ctx, u, shopID)
	if err != nil {
		fail(w, r, "role", err)
		return
	}
	if role == "" {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	var svcs []Service
	var goods []Goods
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		svcs, goods, err = catalog(ctx, tx, shopID)
		return err
	})
	if err != nil {
		fail(w, r, "catalog", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"shopId": shopID, "role": role, "services": svcs, "goods": goods})
}

func managerOf(w http.ResponseWriter, r *http.Request, u *auth.User, shopID int64) bool {
	role, err := roleAt(r.Context(), u, shopID)
	if err != nil {
		fail(w, r, "role", err)
		return false
	}
	if role == "" {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return false
	}
	if role != "manager" && role != "admin" {
		httpx.Error(w, r, http.StatusForbidden, "manager_only", MsgManager)
		return false
	}
	return true
}

type serviceInput struct {
	ShopID  int64   `json:"shopId"`
	Kind    string  `json:"kind"`
	NameKK  string  `json:"nameKk"`
	NameRU  string  `json:"nameRu"`
	DescKK  string  `json:"descKk"`
	DescRU  string  `json:"descRu"`
	Pricing Pricing `json:"pricing"`
	Online  *bool   `json:"online"`
	Active  *bool   `json:"active"`
	Sort    int     `json:"sort"`
}

// ValidService checks a service as a manager entered it.
func ValidService(in serviceInput) bool {
	return in.ShopID > 0 && contains(Kinds, in.Kind) && shopkit.TextOK(in.NameKK, 1, 80) && shopkit.TextOK(in.NameRU, 1, 80) &&
		utf8.RuneCountInString(in.DescKK) <= 300 && utf8.RuneCountInString(in.DescRU) <= 300 && in.Sort >= -999 && in.Sort <= 999 &&
		ValidPricing(in.Kind, in.Pricing) == nil
}

func boolOr(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

func saveService(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in serviceInput
	if err := httpx.Decode(r, &in, 16<<10); err != nil || !ValidService(in) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_service", MsgCatalog)
		return
	}
	if !managerOf(w, r, u, in.ShopID) {
		return
	}
	raw, _ := json.Marshal(in.Pricing)
	ctx := r.Context()
	var v Service
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var row pgx.Row
		if id, ok := pathID(r, "id"); ok {
			row = tx.QueryRow(ctx, `update print_services v set kind = $3, name_kk = $4, name_ru = $5, desc_kk = $6, desc_ru = $7, pricing = $8, online = $9,
				active = $10, sort = $11, updated_at = now() where v.id = $1 and v.shop_id = $2 returning `+serviceCols,
				id, in.ShopID, in.Kind, in.NameKK, in.NameRU, in.DescKK, in.DescRU, string(raw), boolOr(in.Online, true), boolOr(in.Active, true), in.Sort)
		} else {
			row = tx.QueryRow(ctx, `insert into print_services as v (shop_id, kind, name_kk, name_ru, desc_kk, desc_ru, pricing, online, active, sort)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning `+serviceCols,
				in.ShopID, in.Kind, in.NameKK, in.NameRU, in.DescKK, in.DescRU, string(raw), boolOr(in.Online, true), boolOr(in.Active, true), in.Sort)
		}
		var err error
		v, err = scanService(row)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save service", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"service": v})
}

type goodsInput struct {
	ShopID   int64  `json:"shopId"`
	NameKK   string `json:"nameKk"`
	NameRU   string `json:"nameRu"`
	Price    int64  `json:"price"`
	InStock  *bool  `json:"inStock"`
	Archived bool   `json:"archived"`
	Sort     int    `json:"sort"`
}

func ValidGoods(in goodsInput) bool {
	return in.ShopID > 0 && shopkit.TextOK(in.NameKK, 1, 80) && shopkit.TextOK(in.NameRU, 1, 80) && priceOK(in.Price, false) && in.Sort >= -999 && in.Sort <= 999
}

func saveGoods(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in goodsInput
	if err := httpx.Decode(r, &in, 4<<10); err != nil || !ValidGoods(in) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_goods", MsgCatalog)
		return
	}
	if !managerOf(w, r, u, in.ShopID) {
		return
	}
	ctx := r.Context()
	var g Goods
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var row pgx.Row
		if id, ok := pathID(r, "id"); ok {
			row = tx.QueryRow(ctx, `update print_goods g set name_kk = $3, name_ru = $4, price = $5, in_stock = $6, archived = $7, sort = $8, updated_at = now()
				where g.id = $1 and g.shop_id = $2 returning `+goodsCols, id, in.ShopID, in.NameKK, in.NameRU, in.Price, boolOr(in.InStock, true), in.Archived, in.Sort)
		} else {
			row = tx.QueryRow(ctx, `insert into print_goods as g (shop_id, name_kk, name_ru, price, in_stock, archived, sort) values ($1, $2, $3, $4, $5, $6, $7)
				returning `+goodsCols, in.ShopID, in.NameKK, in.NameRU, in.Price, boolOr(in.InStock, true), in.Archived, in.Sort)
		}
		var err error
		g, err = scanGoods(row)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save goods", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"goods": g})
}

// POST /api/print/staff/goods/{id}/stock {"inStock": false}: any staff member of the shop.
func setStock(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		InStock *bool `json:"inStock"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || in.InStock == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var shopID int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select shop_id from print_goods where id = $1`, id).Scan(&shopID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "stock", err)
		return
	}
	role, err := roleAt(ctx, u, shopID)
	if err != nil {
		fail(w, r, "role", err)
		return
	}
	if role == "" {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	var g Goods
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		g, err = scanGoods(tx.QueryRow(ctx, `update print_goods g set in_stock = $2, updated_at = now() where g.id = $1 returning `+goodsCols, id, *in.InStock))
		return err
	})
	if err != nil {
		fail(w, r, "stock", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"goods": g})
}

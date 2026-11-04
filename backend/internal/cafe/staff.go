package cafe

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
)

// Staff are cafe_staff rows (barista or manager of one café), not a user role: a student can work at a café.
// The board reads through db.WithUser, so RLS alone keeps every other café's orders out of it.

func staffRoutes(h func(string, http.HandlerFunc)) {
	h("GET /api/cafe/staff/orders", auth.Need(staffOrders))
	for _, a := range []string{"accept", "ready", "picked-up", "no-show", "decline"} {
		h("POST /api/cafe/staff/orders/{id}/"+a, auth.Write(auth.Need(staffAction(a))))
	}
	h("POST /api/cafe/staff/orders/{id}/refunded", auth.Write(auth.Need(markRefunded)))
	h("GET /api/cafe/staff/menu", auth.Need(staffMenu))
	h("POST /api/cafe/staff/menu/categories", auth.Write(auth.Need(saveCategory)))
	h("PUT /api/cafe/staff/menu/categories/{id}", auth.Write(auth.Need(saveCategory)))
	h("POST /api/cafe/staff/menu/items", auth.Write(auth.Need(saveItem)))
	h("PUT /api/cafe/staff/menu/items/{id}", auth.Write(auth.Need(saveItem)))
	h("POST /api/cafe/staff/menu/items/{id}/availability", auth.Write(auth.Need(setAvailability)))
}

type membership struct {
	CafeID int64
	Role   string
}

func memberships(ctx context.Context, userID int64) ([]membership, error) {
	var out []membership
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select cafe_id, role from cafe_staff where user_id = app_user_id() order by cafe_id`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (membership, error) {
			var m membership
			return m, row.Scan(&m.CafeID, &m.Role)
		})
		return err
	})
	return out, err
}

// roleAt: the caller's role at the café ("" = none). An admin may manage every café's menu.
func roleAt(ctx context.Context, u *auth.User, cafeID int64) (string, error) {
	ms, err := memberships(ctx, u.ID)
	if err != nil {
		return "", err
	}
	for _, m := range ms {
		if m.CafeID == cafeID {
			return m.Role, nil
		}
	}
	if u.Role == "admin" {
		return "admin", nil
	}
	return "", nil
}

// pickCafe: ?cafe=<id>, or the only café the caller works at.
func pickCafe(r *http.Request, ms []membership) (int64, bool) {
	if v := r.URL.Query().Get("cafe"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		return id, err == nil && id > 0
	}
	if len(ms) > 0 {
		return ms[0].CafeID, true
	}
	return 0, false
}

// ---------- GET /api/cafe/staff/orders?cafe=&status= ----------

var boardStatuses = map[string][]string{
	"":       {"new", "accepted", "ready"},
	"open":   {"new", "accepted", "ready"},
	"new":    {"new"},
	"active": {"accepted"},
	"ready":  {"ready"},
	"done":   {"picked_up", "no_show", "cancelled"},
}

func staffOrders(w http.ResponseWriter, r *http.Request, u *auth.User) {
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
	cafeID, ok := pickCafe(r, ms)
	statuses, known := boardStatuses[r.URL.Query().Get("status")]
	if !ok || !known {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	mine := false
	for _, m := range ms {
		mine = mine || m.CafeID == cafeID
	}
	if !mine {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	now := Now()
	cafes := []*Cafe{}
	orders := []*Order{}
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+cafeCols+`, s.role from cafes c join cafe_staff s on s.cafe_id = c.id and s.user_id = app_user_id() order by c.id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var role string
			c, err := scanCafe(rows, &role)
			if err != nil {
				rows.Close()
				return err
			}
			c.MyRole = role
			cafes = append(cafes, c.public(now))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// a Kaspi order reaches the counter only once it is paid; a closed one stays visible for today
		rows, err = tx.Query(ctx, `select `+orderCols+`, coalesce(u.name, '') `+orderFrom+` left join users u on u.id = o.user_id
			where o.cafe_id = $1 and o.status = any($2) and (o.payment_method = 'pickup' or o.payment_status in ('paid', 'refunded'))
			  and (o.status in ('new', 'accepted', 'ready') or o.closed_at >= $3)
			order by o.pickup_at, o.id limit 200`, cafeID, statuses, dayStart(now))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			o, err := scanOrder(rows, &name)
			if err != nil {
				return err
			}
			short := shortName(name)
			o.Customer = &short
			o.Pay = nil
			orders = append(orders, o)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "board", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cafeId": cafeID, "cafes": cafes, "orders": orders, "serverTime": now.UTC()})
}

// shortName: «Аружан С.», enough to call out at the counter.
func shortName(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	if utf8.RuneCountInString(out) > 20 {
		out = string([]rune(out)[:20])
	}
	if len(parts) > 1 {
		r, _ := utf8.DecodeRuneInString(parts[1])
		out += " " + string(r) + "."
	}
	return out
}

// ---------- transitions ----------

type transition struct {
	from  []string
	to    string
	event string
	at    string // timestamp column
}

var transitions = map[string]transition{
	"accept":    {from: []string{"new"}, to: "accepted", event: "accepted", at: "accepted_at"},
	"ready":     {from: []string{"new", "accepted"}, to: "ready", event: "ready", at: "ready_at"},
	"picked-up": {from: []string{"accepted", "ready"}, to: "picked_up", event: "picked_up", at: "closed_at"},
	"no-show":   {from: []string{"accepted", "ready"}, to: "no_show", event: "no_show", at: "closed_at"},
	"decline":   {from: []string{"new", "accepted"}, to: "cancelled", event: "declined", at: "closed_at"},
}

// Act moves an order of the staff member's café. Exported for the bot buttons.
func Act(ctx context.Context, u *auth.User, orderID int64, action, reason string) (*Order, error) {
	t, ok := transitions[action]
	if !ok {
		return nil, refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
	}
	now := Now()
	var o *Order
	var paid bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var cafeID int64
		var status, method, pay string
		var pickup time.Time
		var role *string
		err := tx.QueryRow(ctx, `select o.cafe_id, o.status, o.payment_method, o.payment_status, o.pickup_at,
				(select s.role from cafe_staff s where s.cafe_id = o.cafe_id and s.user_id = $2)
			from cafe_orders o where o.id = $1 for update of o`, orderID, u.ID).Scan(&cafeID, &status, &method, &pay, &pickup, &role)
		// a foreign café's order looks exactly like a missing one
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && role == nil) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if status == t.to {
			o, err = loadOrder(ctx, tx, orderID)
			return err // a double tap
		}
		allowed := false
		for _, f := range t.from {
			allowed = allowed || f == status
		}
		if !allowed {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if method == "kaspi" && pay != "paid" && action != "decline" {
			return refuse(http.StatusConflict, "not_paid", MsgNotPaid)
		}
		if action == "no-show" && now.Before(pickup.Add(time.Duration(noShowGrace())*time.Minute)) {
			return refuse(http.StatusConflict, "too_early", MsgNoShowSoon)
		}
		set := t.at + " = now()"
		if action == "ready" && status == "new" {
			set += ", accepted_at = now()"
		}
		if action == "picked-up" && method == "pickup" {
			// paid at the counter: the café took the money, Student Hub never saw it
			set += ", payment_status = 'paid'"
		}
		args := []any{orderID, t.to}
		if action == "decline" {
			set += ", cancel_reason = $3"
			args = append(args, reason)
		}
		if _, err := tx.Exec(ctx, `update cafe_orders set status = $2, `+set+`, updated_at = now() where id = $1`, args...); err != nil {
			return err
		}
		detail := map[string]any{"from": status}
		if reason != "" {
			detail["reason"] = reason
		}
		if action == "picked-up" && method == "pickup" {
			detail["paidAtCounter"] = true
		}
		if err := event(ctx, tx, orderID, uid(u), "staff", t.event, detail); err != nil {
			return err
		}
		paid = action == "decline" && pay == "paid"
		o, err = loadOrder(ctx, tx, orderID)
		return err
	})
	if err != nil {
		return nil, err
	}
	switch action {
	case "accept":
		go notifyStudent(context.WithoutCancel(ctx), orderID, "accepted")
	case "ready":
		go notifyStudent(context.WithoutCancel(ctx), orderID, "ready")
	case "decline":
		go func() {
			bg := context.WithoutCancel(ctx)
			notifyStudent(bg, orderID, "declined")
			if paid {
				refund(bg, orderID, uid(u), "staff")
			}
		}()
	}
	return o, nil
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
		}
		_ = httpx.Decode(r, &body, 2<<10)
		body.Reason = strings.TrimSpace(body.Reason)
		if utf8.RuneCountInString(body.Reason) > 200 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		o, err := Act(r.Context(), u, id, action, body.Reason)
		var oe *orderErr
		if errors.As(err, &oe) {
			httpx.Error(w, r, oe.status, oe.code, oe.msg)
			return
		}
		if err != nil {
			fail(w, r, "staff "+action, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
	}
}

// POST /api/cafe/staff/orders/{id}/refunded: a manager returned the money by hand in Kaspi Pay.
func markRefunded(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var o *Order
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var manual bool
		var role *string
		err := tx.QueryRow(ctx, `select o.refund_manual, (select s.role from cafe_staff s where s.cafe_id = o.cafe_id and s.user_id = $2)
			from cafe_orders o where o.id = $1 for update of o`, id, u.ID).Scan(&manual, &role)
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
		if _, err := tx.Exec(ctx, `update cafe_orders set refund_manual = false, payment_status = 'refunded', updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		kind := "staff"
		if role == nil {
			kind = "admin"
		}
		if err := event(ctx, tx, id, uid(u), kind, "refunded_manually", nil); err != nil {
			return err
		}
		o, err = loadOrder(ctx, tx, id)
		return err
	})
	var oe *orderErr
	if errors.As(err, &oe) {
		httpx.Error(w, r, oe.status, oe.code, oe.msg)
		return
	}
	if err != nil {
		fail(w, r, "refunded", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
}

// BotAction is bot.CafeAction: "a<id>" accepts, "r<id>" marks ready, for the staff member behind the update.
func BotAction(ctx context.Context, userID int64, value string) (toast, nextLabel, nextData, linkLabel, link string, err error) {
	var u *auth.User
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var e error
		u, e = auth.ScanUser(tx.QueryRow(ctx, `select `+auth.UserColumns+` from users where id = $1 and deleted_at is null`, userID))
		return e
	})
	if err != nil {
		return "", "", "", "", "", nil // not a site account: nothing to do
	}
	lang := u.Lang
	if len(value) < 2 {
		return "", "", "", "", "", nil
	}
	id, perr := strconv.ParseInt(value[1:], 10, 64)
	action := map[byte]string{'a': "accept", 'r': "ready"}[value[0]]
	if perr != nil || action == "" {
		return "", "", "", "", "", nil
	}
	o, aerr := Act(ctx, u, id, action, "")
	var oe *orderErr
	if errors.As(aerr, &oe) {
		msg := oe.msg.KK
		if lang == "ru" {
			msg = oe.msg.RU
		}
		return msg, "", "", "", "", nil
	}
	if aerr != nil {
		return "", "", "", "", "", aerr
	}
	if b := boardButton(lang); len(b) > 0 {
		linkLabel, link = b[0].Text, b[0].URL
	}
	switch o.Status {
	case "accepted":
		return pick(lang, fmt.Sprintf("№%s қабылданды", o.PickupCode), fmt.Sprintf("№%s принят", o.PickupCode)),
			pick(lang, "Дайын", "Готово"), fmt.Sprintf("cafe:r%d", o.ID), linkLabel, link, nil
	default:
		return pick(lang, fmt.Sprintf("№%s дайын", o.PickupCode), fmt.Sprintf("№%s готов", o.PickupCode)), "", "", linkLabel, link, nil
	}
}

// ---------- menu (staff: availability; manager and admin: everything) ----------

func staffMenu(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	ms, err := memberships(ctx, u.ID)
	if err != nil {
		fail(w, r, "memberships", err)
		return
	}
	cafeID, ok := pickCafe(r, ms)
	if !ok {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	role, err := roleAt(ctx, u, cafeID)
	if err != nil {
		fail(w, r, "role", err)
		return
	}
	if role == "" {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	cats := []Category{}
	items := []Item{}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, name_kk, name_ru, sort, active from cafe_menu_categories where cafe_id = $1 order by sort, id`, cafeID)
		if err != nil {
			return err
		}
		cats, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Category, error) {
			var x Category
			return x, row.Scan(&x.ID, &x.NameKK, &x.NameRU, &x.Sort, &x.Active)
		})
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select `+itemCols+` from cafe_menu_items i where i.cafe_id = $1 order by i.archived, i.sort, i.id`, cafeID)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) { return scanItem(row) })
		return err
	})
	if err != nil {
		fail(w, r, "staff menu", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cafeId": cafeID, "role": role, "categories": cats, "items": items})
}

// managerOf answers 403 unless the caller is the café's manager or an admin.
func managerOf(w http.ResponseWriter, r *http.Request, u *auth.User, cafeID int64) bool {
	role, err := roleAt(r.Context(), u, cafeID)
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

func saveCategory(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in struct {
		CafeID int64  `json:"cafeId"`
		NameKK string `json:"nameKk"`
		NameRU string `json:"nameRu"`
		Sort   int    `json:"sort"`
		Active *bool  `json:"active"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || in.CafeID <= 0 || !textOK(in.NameKK, 1, 60) || !textOK(in.NameRU, 1, 60) || in.Sort < -999 || in.Sort > 999 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", MsgMenuData)
		return
	}
	if !managerOf(w, r, u, in.CafeID) {
		return
	}
	activeV := in.Active == nil || *in.Active
	ctx := r.Context()
	var c Category
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if id, ok := pathID(r, "id"); ok {
			return tx.QueryRow(ctx, `update cafe_menu_categories set name_kk = $3, name_ru = $4, sort = $5, active = $6 where id = $1 and cafe_id = $2
				returning id, name_kk, name_ru, sort, active`, id, in.CafeID, in.NameKK, in.NameRU, in.Sort, activeV).Scan(&c.ID, &c.NameKK, &c.NameRU, &c.Sort, &c.Active)
		}
		return tx.QueryRow(ctx, `insert into cafe_menu_categories (cafe_id, name_kk, name_ru, sort, active) values ($1, $2, $3, $4, $5)
			returning id, name_kk, name_ru, sort, active`, in.CafeID, in.NameKK, in.NameRU, in.Sort, activeV).Scan(&c.ID, &c.NameKK, &c.NameRU, &c.Sort, &c.Active)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save category", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"category": c})
}

type itemInput struct {
	CafeID     int64    `json:"cafeId"`
	CategoryID *int64   `json:"categoryId"`
	NameKK     string   `json:"nameKk"`
	NameRU     string   `json:"nameRu"`
	DescKK     string   `json:"descKk"`
	DescRU     string   `json:"descRu"`
	Price      int64    `json:"price"`
	Available  *bool    `json:"available"`
	Archived   bool     `json:"archived"`
	Options    []Option `json:"options"`
	Sort       int      `json:"sort"`
}

// ValidItem checks a menu item as a manager entered it: whole tenge, names, options.
func ValidItem(in itemInput) bool {
	if in.Options == nil {
		in.Options = []Option{}
	}
	return in.CafeID > 0 && textOK(in.NameKK, 1, 80) && textOK(in.NameRU, 1, 80) && utf8.RuneCountInString(in.DescKK) <= 300 &&
		utf8.RuneCountInString(in.DescRU) <= 300 && in.Price >= 100 && in.Price <= 10000000 && in.Price%100 == 0 &&
		in.Sort >= -999 && in.Sort <= 999 && ValidateOptions(in.Options) == nil
}

func saveItem(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in itemInput
	if err := httpx.Decode(r, &in, 16<<10); err != nil || !ValidItem(in) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_item", MsgMenuData)
		return
	}
	if !managerOf(w, r, u, in.CafeID) {
		return
	}
	if in.Options == nil {
		in.Options = []Option{}
	}
	optsRaw, _ := json.Marshal(in.Options)
	opts := string(optsRaw) // jsonb as text: the pool uses the simple protocol
	avail := in.Available == nil || *in.Available
	ctx := r.Context()
	var it Item
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if in.CategoryID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `select exists (select 1 from cafe_menu_categories where id = $1 and cafe_id = $2)`, *in.CategoryID, in.CafeID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return pgx.ErrNoRows
			}
		}
		var row pgx.Row
		if id, ok := pathID(r, "id"); ok {
			row = tx.QueryRow(ctx, `update cafe_menu_items i set category_id = $3, name_kk = $4, name_ru = $5, desc_kk = $6, desc_ru = $7, price = $8,
				available = $9, archived = $10, options = $11, sort = $12, updated_at = now() where i.id = $1 and i.cafe_id = $2 returning `+itemCols,
				id, in.CafeID, in.CategoryID, in.NameKK, in.NameRU, in.DescKK, in.DescRU, in.Price, avail, in.Archived, opts, in.Sort)
		} else {
			row = tx.QueryRow(ctx, `insert into cafe_menu_items as i (cafe_id, category_id, name_kk, name_ru, desc_kk, desc_ru, price, available, archived, options, sort)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning `+itemCols,
				in.CafeID, in.CategoryID, in.NameKK, in.NameRU, in.DescKK, in.DescRU, in.Price, avail, in.Archived, opts, in.Sort)
		}
		var err error
		it, err = scanItem(row)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save item", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
}

// POST /api/cafe/staff/menu/items/{id}/availability {"available": false}: any staff member of the café
// (a barista marks «ran out» at the counter).
func setAvailability(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Available *bool `json:"available"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || in.Available == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var cafeID int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select cafe_id from cafe_menu_items where id = $1`, id).Scan(&cafeID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "availability", err)
		return
	}
	role, err := roleAt(ctx, u, cafeID)
	if err != nil {
		fail(w, r, "role", err)
		return
	}
	if role == "" {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	var it Item
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRow(ctx, `update cafe_menu_items i set available = $2, updated_at = now() where i.id = $1 returning `+itemCols, id, *in.Available))
		return err
	})
	if err != nil {
		fail(w, r, "availability", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
}

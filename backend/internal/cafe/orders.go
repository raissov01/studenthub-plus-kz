package cafe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
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

type PayInfo struct {
	URL       string     `json:"url"`
	QR        string     `json:"qr"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type Order struct {
	ID            int64      `json:"id"`
	CafeID        int64      `json:"cafeId"`
	CafeNameKK    string     `json:"cafeNameKk"`
	CafeNameRU    string     `json:"cafeNameRu"`
	PlaceKK       string     `json:"placeKk"`
	PlaceRU       string     `json:"placeRu"`
	Items         []Line     `json:"items"`
	Total         int64      `json:"total"`
	PickupAt      time.Time  `json:"pickupAt"`
	PickupCode    string     `json:"pickupCode"`
	Note          string     `json:"note"`
	Status        string     `json:"status"`
	PaymentMethod string     `json:"paymentMethod"`
	PaymentStatus string     `json:"paymentStatus"`
	RefundManual  bool       `json:"refundManual"`
	Pay           *PayInfo   `json:"pay"`
	CancelReason  *string    `json:"cancelReason"`
	CreatedAt     time.Time  `json:"createdAt"`
	AcceptedAt    *time.Time `json:"acceptedAt"`
	ReadyAt       *time.Time `json:"readyAt"`
	ClosedAt      *time.Time `json:"closedAt"`
	// staff and admin views
	Customer *string `json:"customer,omitempty"`
	UserID   *int64  `json:"userId,omitempty"`
	Events   []Event `json:"events,omitempty"`

	operationID   *string
	payStartedAt  *time.Time
	userID        *int64
	kaspiExpires  *time.Time
	kaspiPayURL   *string
	kaspiQR       *string
	paymentStatus string
}

type Event struct {
	ID        int64           `json:"id"`
	ActorID   *int64          `json:"actorId"`
	ActorKind string          `json:"actorKind"`
	Kind      string          `json:"kind"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"createdAt"`
}

const orderCols = `o.id, o.cafe_id, c.name_kk, c.name_ru, c.place_kk, c.place_ru, o.items, o.total, o.pickup_at, o.pickup_code, o.note,
	o.status, o.payment_method, o.payment_status, o.refund_manual, o.kaspi_pay_url, o.kaspi_qr, o.kaspi_expires_at, o.cancel_reason,
	o.created_at, o.accepted_at, o.ready_at, o.closed_at, o.kaspi_operation_id, o.pay_started_at, o.user_id`

const orderFrom = ` from cafe_orders o join cafes c on c.id = o.cafe_id `

func scanOrder(row pgx.Row, extra ...any) (*Order, error) {
	var o Order
	var items []byte
	err := row.Scan(append([]any{&o.ID, &o.CafeID, &o.CafeNameKK, &o.CafeNameRU, &o.PlaceKK, &o.PlaceRU, &items, &o.Total, &o.PickupAt,
		&o.PickupCode, &o.Note, &o.Status, &o.PaymentMethod, &o.PaymentStatus, &o.RefundManual, &o.kaspiPayURL, &o.kaspiQR, &o.kaspiExpires,
		&o.CancelReason, &o.CreatedAt, &o.AcceptedAt, &o.ReadyAt, &o.ClosedAt, &o.operationID, &o.payStartedAt, &o.userID}, extra...)...)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(items, &o.Items); err != nil {
		return nil, err
	}
	o.paymentStatus = o.PaymentStatus
	if o.PaymentStatus == "pending" && o.kaspiPayURL != nil {
		o.Pay = &PayInfo{URL: *o.kaspiPayURL, ExpiresAt: o.kaspiExpires}
		if o.kaspiQR != nil {
			o.Pay.QR = *o.kaspiQR
		}
	}
	return &o, nil
}

// active: the order still holds one of the student's two slots.
func active(status string) bool { return status == "new" || status == "accepted" || status == "ready" }

// event appends to the audit trail inside the caller's transaction.
func event(ctx context.Context, tx pgx.Tx, orderID int64, actor *int64, kind, name string, detail map[string]any) error {
	return shopkit.Event(ctx, tx, "cafe_order_events", "order_id", orderID, actor, kind, name, detail)
}

func uid(u *auth.User) *int64 { id := u.ID; return &id }

// ---------- POST /api/cafe/orders ----------

var reKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type orderInput struct {
	CafeID   int64       `json:"cafeId"`
	Items    []LineInput `json:"items"`
	PickupAt time.Time   `json:"pickupAt"`
	Payment  string      `json:"payment"`
	Note     string      `json:"note"`
	Key      string      `json:"key"`
	// accepted and ignored: a total from the client is never trusted
	Total *int64 `json:"total,omitempty"`
}

type orderErr struct {
	status int
	code   string
	msg    httpx.Msg
}

func (e *orderErr) Error() string { return e.code }

func refuse(status int, code string, msg httpx.Msg) error { return &orderErr{status, code, msg} }

func createOrder(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	var in orderInput
	if err := httpx.Decode(r, &in, 16<<10); err != nil || in.CafeID <= 0 || !reKey.MatchString(in.Key) ||
		(in.Payment != "pickup" && in.Payment != "kaspi") || utf8.RuneCountInString(in.Note) > 140 || in.PickupAt.IsZero() {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if limited(w, r, "order:"+itoa(u.ID), ordersPerHour(), time.Hour) {
		return
	}
	ctx := r.Context()
	now := Now()
	var o *Order
	var created bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		// one student's orders are placed one at a time: the active/daily counts below cannot race
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('cafe:order:' || $1::text, 0))`, u.ID); err != nil {
			return err
		}
		var existing int64
		err := tx.QueryRow(ctx, `select id from cafe_orders where user_id = $1 and idem_key = $2`, u.ID, in.Key).Scan(&existing)
		if err == nil {
			o, err = loadOrder(ctx, tx, existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// the café row is locked so two students cannot both take the last place in a slot
		c, err := scanCafe(tx.QueryRow(ctx, `select `+cafeCols+` from cafes c where c.id = $1 and c.active for update of c`, in.CafeID))
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if !ValidSlot(c, now, in.PickupAt) {
			if len(Slots(c, now)) == 0 {
				return refuse(http.StatusConflict, "closed", MsgClosed)
			}
			return refuse(http.StatusConflict, "bad_slot", MsgSlot)
		}
		if !c.accepts(in.Payment) {
			if in.Payment == "kaspi" && (c.PaymentMode == "kaspi" || c.PaymentMode == "both") {
				return refuse(http.StatusServiceUnavailable, "kaspi_unavailable", MsgKaspiDown)
			}
			return refuse(http.StatusConflict, "payment_method", MsgPayMethod)
		}
		var activeN, today, noShows int
		if err := tx.QueryRow(ctx, `select
				count(*) filter (where status in ('new', 'accepted', 'ready')),
				count(*) filter (where created_at >= $2),
				count(*) filter (where status = 'no_show' and pickup_at >= $3)
			from cafe_orders where user_id = $1`, u.ID, dayStart(now), now.AddDate(0, 0, -noShowWindow())).Scan(&activeN, &today, &noShows); err != nil {
			return err
		}
		if activeN >= maxActive() {
			return refuse(http.StatusConflict, "too_many_active", MsgActive)
		}
		if today >= dailyCap() {
			return refuse(http.StatusTooManyRequests, "daily_cap", MsgDaily)
		}
		if noShows >= noShowLimit() && in.Payment != "kaspi" {
			return refuse(http.StatusForbidden, "pay_ahead_required", MsgPayAhead)
		}
		ids := make([]int64, 0, len(in.Items))
		for _, li := range in.Items {
			ids = append(ids, li.ItemID)
		}
		rows, err := tx.Query(ctx, `select `+itemCols+` from cafe_menu_items i where i.cafe_id = $1 and i.id = any($2)`, c.ID, ids)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) { return scanItem(row) })
		if err != nil {
			return err
		}
		menu := map[int64]Item{}
		for _, it := range list {
			menu[it.ID] = it
		}
		lines, total, err := Price(menu, in.Items)
		switch {
		case errors.Is(err, errItem):
			return refuse(http.StatusConflict, "item_unavailable", MsgItem)
		case errors.Is(err, errOptions):
			return refuse(http.StatusBadRequest, "bad_options", MsgOptions)
		case err != nil:
			return refuse(http.StatusBadRequest, "too_large", MsgTooMuch)
		}
		var used int
		if err := tx.QueryRow(ctx, `select count(*) from cafe_orders where cafe_id = $1 and pickup_at = $2 and status <> 'cancelled'`, c.ID, in.PickupAt).Scan(&used); err != nil {
			return err
		}
		if used >= c.SlotCapacity {
			return refuse(http.StatusConflict, "slot_full", MsgSlotFull)
		}
		raw, err := json.Marshal(lines)
		if err != nil {
			return err
		}
		code, err := pickupCode(ctx, tx, c.ID, now)
		if errors.Is(err, shopkit.ErrNoCode) {
			return refuse(http.StatusServiceUnavailable, "codes_exhausted", MsgCodes)
		}
		if err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(ctx, `insert into cafe_orders (cafe_id, user_id, items, total, pickup_at, pickup_code, note, payment_method, idem_key)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9) returning id`,
			c.ID, u.ID, string(raw), total, in.PickupAt, code, in.Note, in.Payment, in.Key).Scan(&id); err != nil {
			return err
		}
		if err := event(ctx, tx, id, uid(u), "student", "created", map[string]any{"total": total, "payment": in.Payment, "pickupAt": in.PickupAt.UTC()}); err != nil {
			return err
		}
		created = true
		o, err = loadOrder(ctx, tx, id)
		return err
	})
	var oe *orderErr
	if errors.As(err, &oe) {
		httpx.Error(w, r, oe.status, oe.code, oe.msg)
		return
	}
	if err != nil {
		fail(w, r, "create order", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		// a pay-at-pickup order goes to the counter now; a Kaspi order only once it is paid
		if o.PaymentMethod == "pickup" {
			go notifyStaffNew(context.WithoutCancel(ctx), o.ID)
		}
	}
	httpx.JSON(w, status, map[string]any{"order": o, "duplicate": !created})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// dayStart is Almaty midnight of now's day.
func dayStart(now time.Time) time.Time { return shopkit.DayStart(now) }

func loadOrder(ctx context.Context, q pgx.Tx, id int64) (*Order, error) {
	return scanOrder(q.QueryRow(ctx, `select `+orderCols+orderFrom+`where o.id = $1`, id))
}

// ---------- reads ----------

// GET /api/cafe/orders/mine: the student's orders (RLS: own rows only), newest first.
func myOrders(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []*Order{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+orderCols+orderFrom+`where o.user_id = app_user_id() order by o.id desc limit 30`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanOrder(rows)
			if err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "my orders", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"orders": out})
}

// GET /api/cafe/orders/{id}: one order the caller may see (RLS: the student who placed it, staff of its
// café). A pending Kaspi payment is re-checked with the provider at most every few seconds, so the status
// screen sees «paid» even before the webhook or the poller.
func getOrder(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	o, err := visibleOrder(ctx, u.ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "order", err)
		return
	}
	if o.paymentStatus == "pending" && o.operationID != nil && o.userID != nil && *o.userID == u.ID && refreshDue(o.ID) {
		if changed := checkPayment(ctx, o.ID, o.CafeID, *o.operationID, "status_read"); changed {
			if fresh, err := visibleOrder(ctx, u.ID, id); err == nil {
				o = fresh
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
}

func visibleOrder(ctx context.Context, userID, id int64) (*Order, error) {
	var o *Order
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		var err error
		o, err = scanOrder(tx.QueryRow(ctx, `select `+orderCols+orderFrom+`where o.id = $1`, id))
		return err
	})
	return o, err
}

// ---------- POST /api/cafe/orders/{id}/cancel ----------

// cancel: only while the café has not accepted the order. A paid order is refunded through the café's
// Kaspi session when the cashier may refund, otherwise the café returns the money by hand.
func cancelOrder(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var o *Order
	var paid bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var status, pay string
		err := tx.QueryRow(ctx, `select status, payment_status from cafe_orders where id = $1 and user_id = $2 for update`, id, u.ID).Scan(&status, &pay)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if status == "cancelled" {
			o, err = loadOrder(ctx, tx, id)
			return err
		}
		if status != "new" {
			return refuse(http.StatusConflict, "too_late", MsgTooLate)
		}
		if _, err := tx.Exec(ctx, `update cafe_orders set status = 'cancelled', cancel_reason = 'student', closed_at = now(), updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		if err := event(ctx, tx, id, uid(u), "student", "cancelled", map[string]any{"payment": pay}); err != nil {
			return err
		}
		paid = pay == "paid"
		o, err = loadOrder(ctx, tx, id)
		return err
	})
	var oe *orderErr
	if errors.As(err, &oe) {
		httpx.Error(w, r, oe.status, oe.code, oe.msg)
		return
	}
	if err != nil {
		fail(w, r, "cancel", err)
		return
	}
	if paid {
		refund(ctx, id, nil, "student")
		if fresh, err := visibleOrder(ctx, u.ID, id); err == nil {
			o = fresh
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
}

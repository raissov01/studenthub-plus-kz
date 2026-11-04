package cafe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// ---------- the café's cashier session ----------

var errNoMerchant = errors.New("cafe: no Kaspi session")

// merchant loads and decrypts a café's cashier session. A broken or undecryptable session is errNoMerchant:
// the café logs in again (admin → Кофейнялар → Kaspi).
func merchant(ctx context.Context, cafeID int64) (payments.Merchant, error) {
	m := payments.Merchant{CafeID: cafeID}
	key := payments.SessionKey()
	if key == nil {
		return m, errNoMerchant
	}
	var ct string
	var broken bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select ciphertext, broken from cafe_kaspi_sessions where cafe_id = $1`, cafeID).Scan(&ct, &broken)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && broken) {
		return m, errNoMerchant
	}
	if err != nil {
		return m, err
	}
	s, err := payments.Decrypt(ct, key)
	if err != nil {
		return m, errNoMerchant
	}
	m.Session = s
	return m, nil
}

// markBroken: Kaspi rejected the session (logged in elsewhere, expired). Kaspi payments stop offering until
// an admin logs the cashier in again; managers are told to switch the café to pay-at-pickup meanwhile.
func markBroken(ctx context.Context, cafeID int64, cause error) {
	var changed bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update cafe_kaspi_sessions set broken = true, last_error = left($2, 300), updated_at = now() where cafe_id = $1 and not broken`, cafeID, cause.Error())
		changed = err == nil && tag.RowsAffected() > 0
		return err
	})
	if err != nil {
		log.Printf("cafe: mark session broken %d: %v", cafeID, err)
	}
	if changed {
		log.Printf("cafe: Kaspi session of café %d stopped working: %v", cafeID, cause)
		go notifyManagers(context.WithoutCancel(ctx), cafeID, "session_broken", 0)
	}
}

// ---------- POST /api/cafe/orders/{id}/pay ----------

// payOrder creates (or returns) the Kaspi QR of an unpaid Kaspi order. Idempotent: a live QR is returned
// again, a request already in flight answers 409 pay_in_progress, a paid order just answers the order.
// {"method":"pickup"} instead moves an unpaid Kaspi order to pay-at-pickup (Kaspi down, the student changed
// their mind) when the café takes it.
func payOrder(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	var body struct {
		Method string `json:"method"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil && !errors.Is(err, io.EOF) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if body.Method == "pickup" {
		switchToPickup(w, r, u, id)
		return
	}
	if body.Method != "" && body.Method != "kaspi" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	now := Now()
	var o *Order
	var cafeID, total int64
	var previous *string
	claimed := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var method, status, pay string
		var expires, started, claim *time.Time
		var url *string
		err := tx.QueryRow(ctx, `select cafe_id, total, payment_method, status, payment_status, kaspi_expires_at, pay_started_at, pay_claimed_at, kaspi_pay_url, kaspi_operation_id
			from cafe_orders where id = $1 and user_id = $2 for update`, id, u.ID).Scan(&cafeID, &total, &method, &status, &pay, &expires, &started, &claim, &url, &previous)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if method != "kaspi" {
			return refuse(http.StatusConflict, "payment_method", MsgPayMethod)
		}
		live := pay == "pending" && url != nil &&
			((expires != nil && expires.After(now.Add(20*time.Second))) || (expires == nil && started != nil && started.After(now.Add(-3*time.Minute))))
		if pay == "paid" || pay == "refunded" || live {
			o, err = loadOrder(ctx, tx, id)
			return err
		}
		if status != "new" {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if claim != nil && claim.After(now.Add(-30*time.Second)) {
			return refuse(http.StatusConflict, "pay_in_progress", MsgPayBusy)
		}
		claimed = true
		_, err = tx.Exec(ctx, `update cafe_orders set pay_claimed_at = $2 where id = $1`, id, now)
		return err
	})
	var oe *orderErr
	if errors.As(err, &oe) {
		httpx.Error(w, r, oe.status, oe.code, oe.msg)
		return
	}
	if err != nil {
		fail(w, r, "pay", err)
		return
	}
	if !claimed {
		httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
		return
	}
	release := func() {
		_ = db.Tx(context.WithoutCancel(ctx), func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update cafe_orders set pay_claimed_at = null where id = $1`, id)
			return err
		})
	}
	p := Provider()
	m, merr := merchant(ctx, cafeID)
	if p == nil || merr != nil {
		release()
		httpx.Error(w, r, http.StatusServiceUnavailable, "kaspi_unavailable", MsgKaspiDown)
		return
	}
	inv, err := p.CreateInvoice(ctx, m, id, total)
	if err != nil {
		release()
		if errors.Is(err, payments.ErrSessionExpired) {
			markBroken(ctx, cafeID, err)
		}
		log.Printf("cafe: create invoice order %d: %v", id, err)
		httpx.Error(w, r, http.StatusServiceUnavailable, "kaspi_unavailable", MsgKaspiDown)
		return
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update cafe_orders set kaspi_operation_id = $2, kaspi_pay_url = $3, kaspi_qr = $4, kaspi_expires_at = $5,
			payment_status = 'pending', pay_started_at = $6, pay_claimed_at = null, updated_at = now()
			where id = $1 and payment_status in ('unpaid', 'pending', 'failed') and status = 'new'`,
			id, inv.OperationID, inv.PayURL, inv.QR, inv.ExpiresAt, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if err := event(ctx, tx, id, uid(u), "student", "pay_started", map[string]any{"operationId": inv.OperationID, "previous": previous, "amount": total}); err != nil {
			return err
		}
		o, err = loadOrder(ctx, tx, id)
		return err
	})
	if errors.As(err, &oe) {
		httpx.Error(w, r, oe.status, oe.code, oe.msg)
		return
	}
	if err != nil {
		fail(w, r, "pay save", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
}

func switchToPickup(w http.ResponseWriter, r *http.Request, u *auth.User, id int64) {
	ctx := r.Context()
	now := Now()
	var o *Order
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var cafeID int64
		var method, status, pay string
		var expires *time.Time
		err := tx.QueryRow(ctx, `select cafe_id, payment_method, status, payment_status, kaspi_expires_at from cafe_orders where id = $1 and user_id = $2 for update`,
			id, u.ID).Scan(&cafeID, &method, &status, &pay, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if method == "pickup" {
			o, err = loadOrder(ctx, tx, id)
			return err
		}
		// a QR that may still be paid stays: otherwise the money could arrive for a pay-at-pickup order
		if status != "new" || pay == "paid" || pay == "refunded" || (pay == "pending" && (expires == nil || expires.After(now))) {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		c, err := scanCafe(tx.QueryRow(ctx, `select `+cafeCols+` from cafes c where c.id = $1`, cafeID))
		if err != nil {
			return err
		}
		if !c.accepts("pickup") {
			return refuse(http.StatusConflict, "payment_method", MsgPayMethod)
		}
		var noShows int
		if err := tx.QueryRow(ctx, `select count(*) from cafe_orders where user_id = $1 and status = 'no_show' and pickup_at >= $2`,
			u.ID, now.AddDate(0, 0, -noShowWindow())).Scan(&noShows); err != nil {
			return err
		}
		if noShows >= noShowLimit() {
			return refuse(http.StatusForbidden, "pay_ahead_required", MsgPayAhead)
		}
		if _, err := tx.Exec(ctx, `update cafe_orders set payment_method = 'pickup', payment_status = 'unpaid', kaspi_pay_url = null, kaspi_qr = null,
			kaspi_expires_at = null, kaspi_operation_id = null, updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		if err := event(ctx, tx, id, uid(u), "student", "payment_switched", map[string]any{"to": "pickup"}); err != nil {
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
		fail(w, r, "switch to pickup", err)
		return
	}
	go notifyStaffNew(context.WithoutCancel(ctx), id)
	httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
}

// ---------- reconciliation (webhook, poller, status reads) ----------

// effect is what to do after the reconciling transaction committed.
type effect struct {
	orderID   int64
	cafeID    int64
	newOrder  bool   // tell the counter about a freshly paid order
	refundOp  string // money arrived for a cancelled order or twice: return it
	refundAmt int64
	alert     string
}

// applyTx reconciles one provider status with its order, under the order's row lock. A payment is marked
// paid exactly once: the second webhook, poll or read finds payment_status already 'paid'.
func applyTx(ctx context.Context, tx pgx.Tx, op string, st payments.State, raw string, amount int64, actor string) (string, effect, error) {
	var eff effect
	var id, cafeID, total int64
	var status, pay string
	var cur *string
	err := tx.QueryRow(ctx, `select id, cafe_id, total, status, payment_status, kaspi_operation_id from cafe_orders where kaspi_operation_id = $1 for update`, op).
		Scan(&id, &cafeID, &total, &status, &pay, &cur)
	superseded := false
	if errors.Is(err, pgx.ErrNoRows) {
		// an earlier QR of the same order (replaced after it expired) can still report
		err = tx.QueryRow(ctx, `select o.id, o.cafe_id, o.total, o.status, o.payment_status, o.kaspi_operation_id from cafe_orders o
			where o.id = (select e.order_id from cafe_order_events e where e.kind = 'pay_started' and e.detail->>'operationId' = $1 order by e.id desc limit 1)
			for update`, op).Scan(&id, &cafeID, &total, &status, &pay, &cur)
		superseded = true
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if st == payments.StatePaid {
			log.Printf("cafe: Kaspi payment for an unknown operation %s (%d tiyn)", op, amount)
		}
		return "unknown_operation", eff, nil
	}
	if err != nil {
		return "", eff, err
	}
	eff.orderID, eff.cafeID = id, cafeID
	detail := map[string]any{"operationId": op, "status": raw}
	switch st {
	case payments.StatePaid:
		if amount > 0 && amount != total {
			eff.alert = "amount_mismatch"
			detail["amount"] = amount
			return "amount_mismatch", eff, event(ctx, tx, id, nil, actor, "amount_mismatch", detail)
		}
		if pay == "paid" || pay == "refunded" {
			if cur != nil && *cur == op {
				return "already_paid", eff, nil
			}
			// paid twice (an old QR and a new one): the second money goes back
			eff.refundOp, eff.refundAmt = op, total
			return "double_payment", eff, event(ctx, tx, id, nil, actor, "double_payment", detail)
		}
		if _, err := tx.Exec(ctx, `update cafe_orders set payment_status = 'paid', paid_at = now(), paid_amount = $2, kaspi_operation_id = $3,
			pay_claimed_at = null, updated_at = now() where id = $1`, id, total, op); err != nil {
			return "", eff, err
		}
		if err := event(ctx, tx, id, nil, actor, "paid", detail); err != nil {
			return "", eff, err
		}
		if status == "cancelled" {
			eff.refundOp, eff.refundAmt = op, total
			return "paid_after_cancel", eff, nil
		}
		eff.newOrder = status == "new"
		return "paid", eff, nil
	case payments.StateFailed, payments.StateExpired:
		if superseded {
			return "superseded", eff, nil
		}
		if pay != "pending" {
			return "ignored", eff, nil
		}
		if _, err := tx.Exec(ctx, `update cafe_orders set payment_status = 'failed', updated_at = now() where id = $1`, id); err != nil {
			return "", eff, err
		}
		return string(st), eff, event(ctx, tx, id, nil, actor, "pay_failed", detail)
	}
	return "pending", eff, nil
}

func runEffect(ctx context.Context, eff effect) {
	ctx = context.WithoutCancel(ctx)
	if eff.newOrder {
		notifyStaffNew(ctx, eff.orderID)
	}
	if eff.refundOp != "" {
		refundOperation(ctx, eff.orderID, eff.cafeID, eff.refundOp, eff.refundAmt, nil, "system")
	}
	if eff.alert != "" {
		notifyManagers(ctx, eff.cafeID, eff.alert, eff.orderID)
	}
}

// checkPayment asks the provider about one operation and reconciles it. Reports whether anything changed.
func checkPayment(ctx context.Context, orderID, cafeID int64, op, source string) bool {
	p := Provider()
	if p == nil {
		return false
	}
	m, err := merchant(ctx, cafeID)
	if err != nil {
		return false
	}
	st, err := p.Status(ctx, m, op)
	if err != nil {
		if errors.Is(err, payments.ErrSessionExpired) {
			markBroken(ctx, cafeID, err)
		}
		log.Printf("cafe: status of order %d: %v", orderID, err)
		return false
	}
	var outcome string
	var eff effect
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		outcome, eff, err = applyTx(ctx, tx, op, st.State, st.Raw, st.Amount, "kaspi")
		return err
	})
	if err != nil {
		log.Printf("cafe: reconcile order %d (%s): %v", orderID, source, err)
		return false
	}
	runEffect(ctx, eff)
	return outcome != "pending" && outcome != "already_paid" && outcome != "ignored"
}

// status reads re-check a pending payment at most every few seconds per order
var (
	refreshMu   sync.Mutex
	lastRefresh = map[int64]time.Time{}
)

func refreshDue(id int64) bool {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	now := time.Now()
	if len(lastRefresh) > 2000 {
		lastRefresh = map[int64]time.Time{}
	}
	if t, ok := lastRefresh[id]; ok && now.Sub(t) < 4*time.Second {
		return false
	}
	lastRefresh[id] = now
	return true
}

// ---------- refunds ----------

// refund returns the money of a paid, cancelled order (all of it).
func refund(ctx context.Context, orderID int64, actor *int64, by string) {
	var cafeID, total int64
	var op *string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select cafe_id, total, kaspi_operation_id from cafe_orders where id = $1 and payment_status = 'paid' and payment_method = 'kaspi'`, orderID).
			Scan(&cafeID, &total, &op)
	})
	if err != nil || op == nil {
		return
	}
	refundOperation(ctx, orderID, cafeID, *op, total, actor, by)
}

// refundOperation asks Kaspi to return one operation. When the cashier may not refund (or Kaspi refuses), the
// order is flagged refund_manual and the café's managers are told to return the money by hand.
func refundOperation(ctx context.Context, orderID, cafeID int64, op string, amount int64, actor *int64, by string) {
	ctx = context.WithoutCancel(ctx)
	// one refund attempt at a time per order (pay_claimed_at doubles as the claim)
	var claimed bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update cafe_orders set pay_claimed_at = now() where id = $1 and (pay_claimed_at is null or pay_claimed_at < now() - interval '2 minutes')`, orderID)
		claimed = err == nil && tag.RowsAffected() == 1
		return err
	})
	if err != nil || !claimed {
		return
	}
	var rerr error
	if p := Provider(); p == nil {
		rerr = payments.ErrNotConfigured
	} else if m, err := merchant(ctx, cafeID); err != nil {
		rerr = err
	} else {
		rerr = p.Refund(ctx, m, op, amount)
		if errors.Is(rerr, payments.ErrSessionExpired) {
			markBroken(ctx, cafeID, rerr)
		}
	}
	kind := "staff"
	if by == "student" || by == "system" || by == "admin" {
		kind = by
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var cur *string
		if err := tx.QueryRow(ctx, `select kaspi_operation_id from cafe_orders where id = $1 for update`, orderID).Scan(&cur); err != nil {
			return err
		}
		mainOp := cur != nil && *cur == op
		detail := map[string]any{"operationId": op, "amount": amount}
		if rerr == nil {
			if mainOp {
				if _, err := tx.Exec(ctx, `update cafe_orders set payment_status = 'refunded', refund_manual = false, pay_claimed_at = null, updated_at = now() where id = $1`, orderID); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `update cafe_orders set pay_claimed_at = null where id = $1`, orderID); err != nil {
				return err
			}
			return event(ctx, tx, orderID, actor, kind, "refunded", detail)
		}
		detail["reason"] = trim(rerr.Error(), 200)
		if _, err := tx.Exec(ctx, `update cafe_orders set refund_manual = true, pay_claimed_at = null, updated_at = now() where id = $1`, orderID); err != nil {
			return err
		}
		return event(ctx, tx, orderID, actor, kind, "refund_manual", detail)
	})
	if err != nil {
		log.Printf("cafe: refund bookkeeping order %d: %v", orderID, err)
		return
	}
	if rerr != nil {
		log.Printf("cafe: refund order %d needs the café: %v", orderID, rerr)
		notifyManagers(ctx, cafeID, "refund_manual", orderID)
	}
	notifyStudent(ctx, orderID, map[bool]string{true: "refunded", false: "refund_manual"}[rerr == nil])
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------- POST /api/cafe/kaspi/webhook ----------

// VerifySignature checks the sidecar's "sha256=<hex HMAC of the raw body>" in constant time.
func VerifySignature(raw []byte, header, secret string) bool {
	if secret == "" || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	want := hex.EncodeToString(m.Sum(nil))
	return hmac.Equal([]byte(strings.TrimPrefix(header, "sha256=")), []byte(want))
}

var webhookStates = map[string]payments.State{
	"payment.success": payments.StatePaid,
	"payment.failed":  payments.StateFailed,
	"payment.expired": payments.StateExpired,
}

// webhook: the sidecar reports a final status of a QR it tracks. Only the shared secret authenticates it
// (no session, no browser origin). Every delivery is stored once; a redelivery is a no-op.
func webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || !VerifySignature(raw, r.Header.Get("X-Webhook-Signature"), config.Str("CAFE_KASPI_WEBHOOK_SECRET", "")) {
		httpx.Error(w, r, http.StatusUnauthorized, "invalid_signature", httpx.MsgForbid)
		return
	}
	var p struct {
		Event     string          `json:"event"`
		PaymentID json.RawMessage `json:"paymentId"`
		Status    string          `json:"status"`
		Amount    json.Number     `json:"amount"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	op := strings.Trim(string(p.PaymentID), `"`)
	if !reOp.MatchString(op) || len(p.Event) > 40 || len(p.Status) > 60 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var amount int64
	if f, err := p.Amount.Float64(); err == nil && f > 0 {
		amount = int64(f*100 + 0.5)
	}
	ctx := r.Context()
	outcome := "duplicate"
	var eff effect
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var hookID int64
		err := tx.QueryRow(ctx, `insert into cafe_kaspi_webhooks (operation_id, event, status, amount, outcome) values ($1, $2, $3, $4, 'pending')
			on conflict (operation_id, event) do nothing returning id`, op, p.Event, p.Status, amount).Scan(&hookID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		st, known := webhookStates[p.Event]
		switch {
		case p.Status == "SessionExpired":
			// polling lost the session: the payment's real result is unknown, the poller asks again later
			outcome = "session_expired"
			var cafeID int64
			if err := tx.QueryRow(ctx, `select cafe_id from cafe_orders where kaspi_operation_id = $1`, op).Scan(&cafeID); err == nil {
				eff.cafeID = cafeID
			}
		case !known:
			outcome = "ignored"
		default:
			outcome, eff, err = applyTx(ctx, tx, op, st, p.Status, amount, "kaspi")
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `update cafe_kaspi_webhooks set outcome = $2, order_id = nullif($3, 0) where id = $1`, hookID, outcome, eff.orderID)
		return err
	})
	if err != nil {
		fail(w, r, "webhook", err)
		return
	}
	if outcome == "session_expired" && eff.cafeID != 0 {
		markBroken(ctx, eff.cafeID, payments.ErrSessionExpired)
		eff = effect{}
	}
	runEffect(ctx, eff)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "outcome": outcome})
}

var reOp = payments.ReOperation

// ---------- background poller ----------

// StartPoller checks pending Kaspi payments every interval, fails the ones past CAFE_PAY_TIMEOUT_MIN and
// cancels Kaspi orders left unpaid. It is the backstop for a lost webhook; cmd/server starts it.
func StartPoller(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !features.Enabled(Feature) {
					continue
				}
				if _, err := Sweep(ctx); err != nil && !errors.Is(err, db.ErrNoDB) {
					log.Printf("cafe: poller: %v", err)
				}
			}
		}
	}()
}

type SweepResult struct {
	Checked   int `json:"checked"`
	TimedOut  int `json:"timedOut"`
	Cancelled int `json:"cancelled"`
}

// Sweep runs one poller pass.
func Sweep(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	now := Now()
	type pending struct {
		id, cafe int64
		op       string
		started  *time.Time
	}
	var list []pending
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, cafe_id, kaspi_operation_id, pay_started_at from cafe_orders
			where payment_status = 'pending' and kaspi_operation_id is not null order by pay_started_at limit 50`)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
			var p pending
			return p, row.Scan(&p.id, &p.cafe, &p.op, &p.started)
		})
		return err
	})
	if err != nil {
		return res, err
	}
	for _, p := range list {
		res.Checked++
		checkPayment(ctx, p.id, p.cafe, p.op, "poller")
		if p.started == nil || p.started.After(now.Add(-time.Duration(payTimeout())*time.Minute)) {
			continue
		}
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `update cafe_orders set payment_status = 'failed', updated_at = now() where id = $1 and payment_status = 'pending' and kaspi_operation_id = $2`, p.id, p.op)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			res.TimedOut++
			return event(ctx, tx, p.id, nil, "system", "pay_timeout", map[string]any{"operationId": p.op})
		})
		if err != nil {
			return res, err
		}
	}
	// Kaspi orders nobody paid free the slot and the student's active-order place
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `update cafe_orders set status = 'cancelled', cancel_reason = 'unpaid', closed_at = now(), updated_at = now()
			where payment_method = 'kaspi' and status = 'new' and payment_status in ('unpaid', 'failed')
			  and (created_at < $1 or pickup_at < $2) returning id`, now.Add(-time.Duration(unpaidCancelMin())*time.Minute), now)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := event(ctx, tx, id, nil, "system", "cancelled", map[string]any{"reason": "unpaid"}); err != nil {
				return err
			}
		}
		res.Cancelled = len(ids)
		return nil
	})
	return res, err
}

func (r SweepResult) String() string {
	return fmt.Sprintf("checked=%d timedOut=%d cancelled=%d", r.Checked, r.TimedOut, r.Cancelled)
}

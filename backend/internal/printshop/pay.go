package printshop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// Kaspi payments follow the café exactly (docs/cafe.md): the invoice is issued in the shop's own name through the
// shop's own cashier session (print_kaspi_sessions), one invoice request at a time, «paid» exactly once under the
// job's row lock, a cancelled-then-paid or double payment is refunded, a refund the cashier may not make becomes
// refund_manual for the manager. The sidecar's single webhook reaches us through the café's endpoint
// (shopkit.RegisterOperations); the poller and status reads are the backstop.

func markBroken(ctx context.Context, shopID int64, cause error) {
	if sessions.MarkBroken(ctx, shopID, cause) {
		go notifyManagers(context.WithoutCancel(ctx), shopID, "session_broken", 0)
	}
}

// ---------- POST /api/print/jobs/{id}/pay ----------

// payJob creates (or returns) the Kaspi QR of an unpaid Kaspi job whose price is agreed. {"method":"pickup"}
// moves an unpaid Kaspi job to pay-at-the-counter when the shop takes it.
func payJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
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
	var shopID, total int64
	var previous *string
	claimed := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var method, status, pay, state string
		var expires, started, claim *time.Time
		var url *string
		err := tx.QueryRow(ctx, `select shop_id, total, payment_method, status, payment_status, price_state, kaspi_expires_at, pay_started_at, pay_claimed_at,
			kaspi_pay_url, kaspi_operation_id from print_jobs where id = $1 and user_id = $2 for update`, id, u.ID).
			Scan(&shopID, &total, &method, &status, &pay, &state, &expires, &started, &claim, &url, &previous)
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
			return nil
		}
		if status != "new" {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if state != "final" {
			return refuse(http.StatusConflict, "price_pending", MsgPriceOpen)
		}
		if claim != nil && claim.After(now.Add(-30*time.Second)) {
			return refuse(http.StatusConflict, "pay_in_progress", MsgPayBusy)
		}
		claimed = true
		_, err = tx.Exec(ctx, `update print_jobs set pay_claimed_at = $2 where id = $1`, id, now)
		return err
	})
	if err != nil {
		reply(w, r, "pay", err)
		return
	}
	if !claimed {
		respondJob(w, r, u, id)
		return
	}
	release := func() {
		_ = db.Tx(context.WithoutCancel(ctx), func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update print_jobs set pay_claimed_at = null where id = $1`, id)
			return err
		})
	}
	p := Provider()
	m, merr := sessions.Merchant(ctx, shopID)
	if p == nil || merr != nil {
		release()
		httpx.Error(w, r, http.StatusServiceUnavailable, "kaspi_unavailable", MsgKaspiDown)
		return
	}
	inv, err := p.CreateInvoice(ctx, m, id, total)
	if err != nil {
		release()
		if errors.Is(err, payments.ErrSessionExpired) {
			markBroken(ctx, shopID, err)
		}
		log.Printf("print: create invoice job %d: %v", id, err)
		httpx.Error(w, r, http.StatusServiceUnavailable, "kaspi_unavailable", MsgKaspiDown)
		return
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update print_jobs set kaspi_operation_id = $2, kaspi_pay_url = $3, kaspi_qr = $4, kaspi_expires_at = $5,
			payment_status = 'pending', pay_started_at = $6, pay_claimed_at = null, updated_at = now()
			where id = $1 and payment_status in ('unpaid', 'pending', 'failed') and status = 'new' and price_state = 'final'`,
			id, inv.OperationID, inv.PayURL, inv.QR, inv.ExpiresAt, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		return event(ctx, tx, id, uid(u), "student", "pay_started", map[string]any{"operationId": inv.OperationID, "previous": previous, "amount": total})
	})
	if err != nil {
		reply(w, r, "pay save", err)
		return
	}
	respondJob(w, r, u, id)
}

func switchToPickup(w http.ResponseWriter, r *http.Request, u *auth.User, id int64) {
	ctx := r.Context()
	now := Now()
	moved := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var shopID int64
		var method, status, pay string
		var expires *time.Time
		err := tx.QueryRow(ctx, `select shop_id, payment_method, status, payment_status, kaspi_expires_at from print_jobs where id = $1 and user_id = $2 for update`,
			id, u.ID).Scan(&shopID, &method, &status, &pay, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if method == "pickup" {
			return nil
		}
		// a QR that may still be paid stays: otherwise money could arrive for a pay-at-the-counter job
		if status != "new" || pay == "paid" || pay == "refunded" || (pay == "pending" && (expires == nil || expires.After(now))) {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		s, err := scanShop(tx.QueryRow(ctx, `select `+shopCols+` from print_shops s where s.id = $1`, shopID))
		if err != nil {
			return err
		}
		if !s.accepts("pickup") {
			return refuse(http.StatusConflict, "payment_method", MsgPayMethod)
		}
		if _, err := tx.Exec(ctx, `update print_jobs set payment_method = 'pickup', payment_status = 'unpaid', kaspi_pay_url = null, kaspi_qr = null,
			kaspi_expires_at = null, kaspi_operation_id = null, queued_at = now(), updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		moved = true
		return event(ctx, tx, id, uid(u), "student", "payment_switched", map[string]any{"to": "pickup"})
	})
	if err != nil {
		reply(w, r, "switch to pickup", err)
		return
	}
	if moved {
		go notifyStaffNew(context.WithoutCancel(ctx), id)
	}
	respondJob(w, r, u, id)
}

// ---------- reconciliation ----------

type effect struct {
	jobID     int64
	shopID    int64
	newJob    bool
	refundOp  string
	refundAmt int64
	alert     string
}

func findByOperation(ctx context.Context, tx pgx.Tx, op string) (id, shopID, total int64, status, pay string, cur *string, superseded bool, err error) {
	err = tx.QueryRow(ctx, `select id, shop_id, total, status, payment_status, kaspi_operation_id from print_jobs where kaspi_operation_id = $1 for update`, op).
		Scan(&id, &shopID, &total, &status, &pay, &cur)
	if errors.Is(err, pgx.ErrNoRows) {
		// an earlier QR of the same job (replaced after it expired) can still report
		err = tx.QueryRow(ctx, `select j.id, j.shop_id, j.total, j.status, j.payment_status, j.kaspi_operation_id from print_jobs j
			where j.id = (select e.job_id from print_job_events e where e.kind = 'pay_started' and e.detail->>'operationId' = $1 order by e.id desc limit 1)
			for update`, op).Scan(&id, &shopID, &total, &status, &pay, &cur)
		superseded = true
	}
	return
}

// applyTx reconciles one provider status with its job, under the job's row lock: «paid» exactly once.
func applyTx(ctx context.Context, tx pgx.Tx, op string, st payments.State, raw string, amount int64, actor string) (string, effect, error) {
	var eff effect
	id, shopID, total, status, pay, cur, superseded, err := findByOperation(ctx, tx, op)
	if errors.Is(err, pgx.ErrNoRows) {
		return "unknown_operation", eff, nil
	}
	if err != nil {
		return "", eff, err
	}
	eff.jobID, eff.shopID = id, shopID
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
			eff.refundOp, eff.refundAmt = op, total
			return "double_payment", eff, event(ctx, tx, id, nil, actor, "double_payment", detail)
		}
		// the queue position starts at the payment
		if _, err := tx.Exec(ctx, `update print_jobs set payment_status = 'paid', paid_at = now(), paid_amount = $2, kaspi_operation_id = $3,
			pay_claimed_at = null, queued_at = case when status = 'new' then now() else queued_at end, updated_at = now() where id = $1`, id, total, op); err != nil {
			return "", eff, err
		}
		if err := event(ctx, tx, id, nil, actor, "paid", detail); err != nil {
			return "", eff, err
		}
		if status == "cancelled" {
			eff.refundOp, eff.refundAmt = op, total
			return "paid_after_cancel", eff, nil
		}
		eff.newJob = status == "new"
		return "paid", eff, nil
	case payments.StateFailed, payments.StateExpired:
		if superseded {
			return "superseded", eff, nil
		}
		if pay != "pending" {
			return "ignored", eff, nil
		}
		if _, err := tx.Exec(ctx, `update print_jobs set payment_status = 'failed', updated_at = now() where id = $1`, id); err != nil {
			return "", eff, err
		}
		return string(st), eff, event(ctx, tx, id, nil, actor, "pay_failed", detail)
	}
	return "pending", eff, nil
}

func runEffect(ctx context.Context, eff effect) {
	ctx = context.WithoutCancel(ctx)
	if eff.newJob {
		notifyStaffNew(ctx, eff.jobID)
	}
	if eff.refundOp != "" {
		refundOperation(ctx, eff.jobID, eff.shopID, eff.refundOp, eff.refundAmt, nil, "system")
	}
	if eff.alert != "" {
		notifyManagers(ctx, eff.shopID, eff.alert, eff.jobID)
	}
}

// reconcileWebhook is the print shop's part of the sidecar's webhook (POST /api/cafe/kaspi/webhook).
func reconcileWebhook(ctx context.Context, tx pgx.Tx, op string, st payments.State, raw string, amount int64) (string, func(context.Context), bool, error) {
	outcome, eff, err := applyTx(ctx, tx, op, st, raw, amount, "kaspi")
	if err != nil {
		return "", nil, true, err
	}
	if outcome == "unknown_operation" {
		return "", nil, false, nil
	}
	return "print:" + outcome, func(c context.Context) { runEffect(c, eff) }, true, nil
}

// checkPayment asks the provider about one operation and reconciles it. Reports whether anything changed.
func checkPayment(ctx context.Context, jobID, shopID int64, op, source string) bool {
	p := Provider()
	if p == nil {
		return false
	}
	m, err := sessions.Merchant(ctx, shopID)
	if err != nil {
		return false
	}
	st, err := p.Status(ctx, m, op)
	if err != nil {
		if errors.Is(err, payments.ErrSessionExpired) {
			markBroken(ctx, shopID, err)
		}
		log.Printf("print: status of job %d: %v", jobID, err)
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
		log.Printf("print: reconcile job %d (%s): %v", jobID, source, err)
		return false
	}
	runEffect(ctx, eff)
	return outcome != "pending" && outcome != "already_paid" && outcome != "ignored"
}

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

func refund(ctx context.Context, jobID int64, actor *int64, by string) {
	var shopID, total int64
	var op *string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select shop_id, total, kaspi_operation_id from print_jobs where id = $1 and payment_status = 'paid' and payment_method = 'kaspi'`, jobID).
			Scan(&shopID, &total, &op)
	})
	if err != nil || op == nil {
		return
	}
	refundOperation(ctx, jobID, shopID, *op, total, actor, by)
}

func refundOperation(ctx context.Context, jobID, shopID int64, op string, amount int64, actor *int64, by string) {
	ctx = context.WithoutCancel(ctx)
	var claimed bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update print_jobs set pay_claimed_at = now() where id = $1 and (pay_claimed_at is null or pay_claimed_at < now() - interval '2 minutes')`, jobID)
		claimed = err == nil && tag.RowsAffected() == 1
		return err
	})
	if err != nil || !claimed {
		return
	}
	var rerr error
	if p := Provider(); p == nil {
		rerr = payments.ErrNotConfigured
	} else if m, err := sessions.Merchant(ctx, shopID); err != nil {
		rerr = err
	} else {
		rerr = p.Refund(ctx, m, op, amount)
		if errors.Is(rerr, payments.ErrSessionExpired) {
			markBroken(ctx, shopID, rerr)
		}
	}
	kind := "staff"
	if by == "student" || by == "system" || by == "admin" {
		kind = by
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var cur *string
		if err := tx.QueryRow(ctx, `select kaspi_operation_id from print_jobs where id = $1 for update`, jobID).Scan(&cur); err != nil {
			return err
		}
		mainOp := cur != nil && *cur == op
		detail := map[string]any{"operationId": op, "amount": amount}
		if rerr == nil {
			set := `pay_claimed_at = null`
			if mainOp {
				set = `payment_status = 'refunded', refund_manual = false, pay_claimed_at = null, updated_at = now()`
			}
			if _, err := tx.Exec(ctx, `update print_jobs set `+set+` where id = $1`, jobID); err != nil {
				return err
			}
			return event(ctx, tx, jobID, actor, kind, "refunded", detail)
		}
		detail["reason"] = shopkit.Trim(rerr.Error(), 200)
		if _, err := tx.Exec(ctx, `update print_jobs set refund_manual = true, pay_claimed_at = null, updated_at = now() where id = $1`, jobID); err != nil {
			return err
		}
		return event(ctx, tx, jobID, actor, kind, "refund_manual", detail)
	})
	if err != nil {
		log.Printf("print: refund bookkeeping job %d: %v", jobID, err)
		return
	}
	if rerr != nil {
		log.Printf("print: refund job %d needs the shop: %v", jobID, rerr)
		notifyManagers(ctx, shopID, "refund_manual", jobID)
	}
	notifyStudent(ctx, jobID, map[bool]string{true: "refunded", false: "refund_manual"}[rerr == nil])
}

// ---------- background ----------

// StartPoller checks pending Kaspi payments every interval, cancels Kaspi jobs left unpaid, and deletes old
// files (retention) every ten minutes. cmd/server starts it; it does nothing while FEATURE_PRINT is off.
func StartPoller(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		var lastRetain time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !features.Enabled(Feature) {
					continue
				}
				if _, err := Sweep(ctx); err != nil && !errors.Is(err, db.ErrNoDB) {
					log.Printf("print: poller: %v", err)
				}
				if time.Since(lastRetain) > 10*time.Minute {
					lastRetain = time.Now()
					if res, err := Retain(ctx); err != nil && !errors.Is(err, db.ErrNoDB) {
						log.Printf("print: retention: %v", err)
					} else if res.Files+res.Orphans > 0 {
						log.Printf("print: retention: %d jobs, %d files, %d unplaced uploads deleted", res.Jobs, res.Files, res.Orphans)
					}
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

func (r SweepResult) String() string {
	return fmt.Sprintf("checked=%d timedOut=%d cancelled=%d", r.Checked, r.TimedOut, r.Cancelled)
}

// Sweep runs one payment pass.
func Sweep(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	now := Now()
	type pending struct {
		id, shop int64
		op       string
		started  *time.Time
	}
	var list []pending
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, shop_id, kaspi_operation_id, pay_started_at from print_jobs
			where payment_status = 'pending' and kaspi_operation_id is not null order by pay_started_at limit 50`)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
			var p pending
			return p, row.Scan(&p.id, &p.shop, &p.op, &p.started)
		})
		return err
	})
	if err != nil {
		return res, err
	}
	for _, p := range list {
		res.Checked++
		checkPayment(ctx, p.id, p.shop, p.op, "poller")
		if p.started == nil || p.started.After(now.Add(-time.Duration(payTimeout())*time.Minute)) {
			continue
		}
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `update print_jobs set payment_status = 'failed', updated_at = now() where id = $1 and payment_status = 'pending' and kaspi_operation_id = $2`, p.id, p.op)
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
	// Kaspi jobs with an agreed price that nobody paid leave the queue
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `update print_jobs set status = 'cancelled', cancel_reason = 'unpaid', closed_at = now(), updated_at = now()
			where payment_method = 'kaspi' and status = 'new' and price_state = 'final' and payment_status in ('unpaid', 'failed')
			  and updated_at < $1 returning id`, now.Add(-time.Duration(unpaidCancelMin())*time.Minute))
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

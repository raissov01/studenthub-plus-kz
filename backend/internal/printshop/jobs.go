package printshop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
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

type Job struct {
	ID            int64      `json:"id"`
	ShopID        int64      `json:"shopId"`
	ShopNameKK    string     `json:"shopNameKk"`
	ShopNameRU    string     `json:"shopNameRu"`
	PlaceKK       string     `json:"placeKk"`
	PlaceRU       string     `json:"placeRu"`
	Lines         []Line     `json:"lines"`
	Total         int64      `json:"total"`
	PriceState    string     `json:"priceState"`
	ProposedTotal *int64     `json:"proposedTotal"`
	AdjustReason  *string    `json:"adjustReason"`
	PickupAt      *time.Time `json:"pickupAt"`
	PickupCode    string     `json:"pickupCode"`
	Note          string     `json:"note"`
	Status        string     `json:"status"`
	PaymentMethod string     `json:"paymentMethod"`
	PaymentStatus string     `json:"paymentStatus"`
	RefundManual  bool       `json:"refundManual"`
	Pay           *PayInfo   `json:"pay"`
	CancelReason  *string    `json:"cancelReason"`
	QueuedAt      time.Time  `json:"queuedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	PrintingAt    *time.Time `json:"printingAt"`
	ReadyAt       *time.Time `json:"readyAt"`
	ClosedAt      *time.Time `json:"closedAt"`
	FilesDeleted  bool       `json:"filesDeleted"`
	Files         []Upload   `json:"files"`
	// the student's view: jobs ahead in the shop's queue (0 = next), the signed counter QR of an open job
	Ahead       *int   `json:"ahead,omitempty"`
	EtaMin      *int   `json:"etaMin,omitempty"` // a rough «ready in ≈ N min» while it waits in an open shop
	PickupToken string `json:"pickupToken,omitempty"`
	// staff and admin views
	Customer *string `json:"customer,omitempty"`
	UserID   *int64  `json:"userId,omitempty"`
	Events   []Event `json:"events,omitempty"`

	operationID   *string
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

const jobCols = `j.id, j.shop_id, s.name_kk, s.name_ru, s.place_kk, s.place_ru, j.lines, j.total, j.price_state, j.proposed_total, j.adjust_reason,
	j.pickup_at, j.pickup_code, j.note, j.status, j.payment_method, j.payment_status, j.refund_manual, j.kaspi_pay_url, j.kaspi_qr, j.kaspi_expires_at,
	j.cancel_reason, j.queued_at, j.created_at, j.printing_at, j.ready_at, j.closed_at, j.files_deleted_at is not null, j.kaspi_operation_id, j.user_id`

const jobFrom = ` from print_jobs j join print_shops s on s.id = j.shop_id `

func scanJob(row pgx.Row, extra ...any) (*Job, error) {
	var j Job
	var lines []byte
	err := row.Scan(append([]any{&j.ID, &j.ShopID, &j.ShopNameKK, &j.ShopNameRU, &j.PlaceKK, &j.PlaceRU, &lines, &j.Total, &j.PriceState,
		&j.ProposedTotal, &j.AdjustReason, &j.PickupAt, &j.PickupCode, &j.Note, &j.Status, &j.PaymentMethod, &j.PaymentStatus, &j.RefundManual,
		&j.kaspiPayURL, &j.kaspiQR, &j.kaspiExpires, &j.CancelReason, &j.QueuedAt, &j.CreatedAt, &j.PrintingAt, &j.ReadyAt, &j.ClosedAt,
		&j.FilesDeleted, &j.operationID, &j.userID}, extra...)...)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(lines, &j.Lines); err != nil {
		return nil, err
	}
	j.Files = []Upload{}
	j.paymentStatus = j.PaymentStatus
	if j.PaymentStatus == "pending" && j.kaspiPayURL != nil {
		j.Pay = &PayInfo{URL: *j.kaspiPayURL, ExpiresAt: j.kaspiExpires}
		if j.kaspiQR != nil {
			j.Pay.QR = *j.kaspiQR
		}
	}
	return &j, nil
}

// withFiles fills each job's files, with signed links for viewer (0 = no links).
func withFiles(ctx context.Context, tx pgx.Tx, jobs []*Job, viewer int64) error {
	if len(jobs) == 0 {
		return nil
	}
	ids := make([]int64, len(jobs))
	byID := map[int64]*Job{}
	for i, j := range jobs {
		ids[i] = j.ID
		byID[j.ID] = j
	}
	rows, err := tx.Query(ctx, `select `+fileCols+` from print_files f where f.job_id = any($1) order by f.id`, ids)
	if err != nil {
		return err
	}
	files, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Upload, error) { return scanFile(row) })
	if err != nil {
		return err
	}
	now := Now()
	for _, f := range files {
		if viewer != 0 {
			signFor(&f, viewer, now)
		}
		if j := byID[*f.JobID]; j != nil {
			j.Files = append(j.Files, f)
		}
	}
	return nil
}

func loadJob(ctx context.Context, tx pgx.Tx, id int64) (*Job, error) {
	j, err := scanJob(tx.QueryRow(ctx, `select `+jobCols+jobFrom+`where j.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return j, withFiles(ctx, tx, []*Job{j}, 0)
}

// codeDigits: four-digit pickup codes (1000–9999), see migrations/20261106000100_pickup_counter.sql.
const codeDigits = 4

// openStatuses: a job still at the counter (its pickup code is reserved).
var openStatuses = []string{"new", "printing", "ready"}

// active: the job still holds one of the student's places.
func active(status string) bool { return status == "new" || status == "printing" || status == "ready" }

// workable: the operator may print it — the price is agreed and it is paid or paid at the counter.
func workable(j *Job) bool {
	return j.PriceState == "final" && (j.PaymentMethod == "pickup" || j.paymentStatus == "paid")
}

func event(ctx context.Context, tx pgx.Tx, jobID int64, actor *int64, kind, name string, detail map[string]any) error {
	return shopkit.Event(ctx, tx, "print_job_events", "job_id", jobID, actor, kind, name, detail)
}

// ---------- quote and create ----------

// priced loads the shop (row-locked when lock) and the student's own free files, and quotes the input.
func priced(ctx context.Context, tx pgx.Tx, userID int64, in QuoteInput, lock bool) (*Shop, *Quoted, error) {
	q := `select ` + shopCols + ` from print_shops s where s.id = $1 and s.active`
	if lock {
		q += ` for update of s`
	}
	shop, err := scanShop(tx.QueryRow(ctx, q, in.ShopID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
	}
	if err != nil {
		return nil, nil, err
	}
	svcs, goods, err := catalog(ctx, tx, shop.ID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]int64, 0, len(in.Files))
	for _, f := range in.Files {
		ids = append(ids, f.FileID)
	}
	fq := `select id, kind, display_name, pages, pages_hint from print_files where id = any($1) and user_id = $2 and job_id is null and deleted_at is null`
	if lock {
		fq += ` for update`
	}
	rows, err := tx.Query(ctx, fq, ids, userID)
	if err != nil {
		return nil, nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FileInfo, error) {
		var f FileInfo
		return f, row.Scan(&f.ID, &f.Kind, &f.DisplayName, &f.Pages, &f.PagesHint)
	})
	if err != nil {
		return nil, nil, err
	}
	files := map[int64]FileInfo{}
	for _, f := range list {
		files[f.ID] = f
	}
	quoted, err := Quote(catalogOf(shop, svcs, goods), files, in)
	return shop, quoted, err
}

// POST /api/print/quote: the server's price of a job, nothing stored. The device shows this, never its own sum.
func quoteJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in QuoteInput
	if err := httpx.Decode(r, &in, 32<<10); err != nil || in.ShopID <= 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if limited(w, r, "quote:"+itoa(u.ID), 300, time.Hour) {
		return
	}
	ctx := r.Context()
	var quoted *Quoted
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		_, quoted, err = priced(ctx, tx, u.ID, in, false)
		return err
	})
	if err != nil {
		reply(w, r, "quote", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"quote": quoted})
}

var reKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type jobInput struct {
	QuoteInput
	PickupAt *time.Time `json:"pickupAt"` // null = as soon as possible
	Payment  string     `json:"payment"`
	Note     string     `json:"note"`
	Key      string     `json:"key"`
}

// POST /api/print/jobs: the job, priced again from scratch (the quote the device saw is not trusted either).
func createJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	var in jobInput
	if err := httpx.Decode(r, &in, 32<<10); err != nil || in.ShopID <= 0 || !reKey.MatchString(in.Key) ||
		(in.Payment != "pickup" && in.Payment != "kaspi") || utf8.RuneCountInString(in.Note) > 300 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if limited(w, r, "job:"+itoa(u.ID), jobsPerHour(), time.Hour) {
		return
	}
	ctx := r.Context()
	now := Now()
	var j *Job
	created := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('print:job:' || $1::text, 0))`, u.ID); err != nil {
			return err
		}
		var existing int64
		err := tx.QueryRow(ctx, `select id from print_jobs where user_id = $1 and idem_key = $2`, u.ID, in.Key).Scan(&existing)
		if err == nil {
			j, err = loadJob(ctx, tx, existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		shop, quoted, err := priced(ctx, tx, u.ID, in.QuoteInput, true)
		if err != nil {
			return err
		}
		if in.PickupAt != nil && !shop.hours().ValidSlot(now, *in.PickupAt) {
			return refuse(http.StatusConflict, "bad_slot", MsgSlot)
		}
		if !shop.accepts(in.Payment) {
			if in.Payment == "kaspi" && (shop.PaymentMode == "kaspi" || shop.PaymentMode == "both") {
				return refuse(http.StatusServiceUnavailable, "kaspi_unavailable", MsgKaspiDown)
			}
			return refuse(http.StatusConflict, "payment_method", MsgPayMethod)
		}
		var activeN, today int
		if err := tx.QueryRow(ctx, `select count(*) filter (where status in ('new', 'printing', 'ready')), count(*) filter (where created_at >= $2)
			from print_jobs where user_id = $1`, u.ID, shopkit.DayStart(now)).Scan(&activeN, &today); err != nil {
			return err
		}
		if activeN >= maxActive() {
			return refuse(http.StatusConflict, "too_many_active", MsgActive)
		}
		if today >= dailyJobs() {
			return refuse(http.StatusTooManyRequests, "daily_cap", MsgDaily)
		}
		raw, err := json.Marshal(quoted.Lines)
		if err != nil {
			return err
		}
		state := "final"
		if quoted.Review {
			state = "review"
		}
		// unique among the shop's open jobs (and today's closed ones while possible): the shop row is locked
		code, err := shopkit.UniqueCode(ctx, tx, codeDigits, "print_jobs", "shop_id", shop.ID, openStatuses, now)
		if errors.Is(err, shopkit.ErrNoCode) {
			return refuse(http.StatusServiceUnavailable, "codes_exhausted", MsgCodes)
		}
		if err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(ctx, `insert into print_jobs (shop_id, user_id, lines, total, price_state, pickup_at, pickup_code, note, payment_method, idem_key)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning id`,
			shop.ID, u.ID, string(raw), quoted.Total, state, in.PickupAt, code, in.Note, in.Payment, in.Key).Scan(&id); err != nil {
			return err
		}
		fileIDs := make([]int64, 0, len(in.Files))
		for _, f := range in.Files {
			fileIDs = append(fileIDs, f.FileID)
		}
		if _, err := tx.Exec(ctx, `update print_files set job_id = $1 where id = any($2) and user_id = $3 and job_id is null`, id, fileIDs, u.ID); err != nil {
			return err
		}
		if err := event(ctx, tx, id, uid(u), "student", "created", map[string]any{"total": quoted.Total, "payment": in.Payment, "priceState": state,
			"files": len(fileIDs), "asap": in.PickupAt == nil}); err != nil {
			return err
		}
		created = true
		j, err = loadJob(ctx, tx, id)
		return err
	})
	if err != nil {
		reply(w, r, "create job", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		// to the counter now: pay-at-the-counter, or a price the operator must look at; a Kaspi job once paid
		if j.PaymentMethod == "pickup" || j.PriceState != "final" {
			go notifyStaffNew(context.WithoutCancel(ctx), j.ID)
		}
	}
	signAll(j, u.ID)
	j.forStudent(u.ID)
	httpx.JSON(w, status, map[string]any{"job": j, "duplicate": !created})
}

func signAll(j *Job, viewer int64) {
	now := Now()
	for i := range j.Files {
		signFor(&j.Files[i], viewer, now)
	}
}

// ---------- reads ----------

// GET /api/print/jobs/mine: the student's jobs (RLS: own rows only), newest first.
func myJobs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []*Job{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+jobCols+jobFrom+`where j.user_id = app_user_id() order by j.id desc limit 30`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Job, error) { return scanJob(row) })
		if err != nil {
			return err
		}
		return withFiles(ctx, tx, out, 0)
	})
	if err != nil {
		fail(w, r, "my jobs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func visibleJob(ctx context.Context, userID, id int64) (*Job, error) {
	var j *Job
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		var err error
		j, err = scanJob(tx.QueryRow(ctx, `select `+jobCols+jobFrom+`where j.id = $1`, id))
		if err != nil {
			return err
		}
		return withFiles(ctx, tx, []*Job{j}, userID)
	})
	return j, err
}

// ahead counts the open jobs queued before this one in the same shop.
func ahead(ctx context.Context, j *Job) *int {
	if j.Status != "new" && j.Status != "printing" {
		return nil
	}
	var n int
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from print_jobs where shop_id = $1 and status in ('new', 'printing') and (queued_at, id) < ($2, $3)
			and (payment_method = 'pickup' or payment_status = 'paid' or price_state <> 'final')`, j.ShopID, j.QueuedAt, j.ID).Scan(&n)
	})
	if err != nil {
		return nil
	}
	return &n
}

// GET /api/print/jobs/{id}: one job the caller may see (RLS: its student, staff of its shop), with the queue
// position and signed file links. A pending Kaspi payment is re-checked at most every few seconds.
func getJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	j, err := visibleJob(ctx, u.ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "job", err)
		return
	}
	if j.paymentStatus == "pending" && j.operationID != nil && j.userID != nil && *j.userID == u.ID && refreshDue(j.ID) {
		if changed := checkPayment(ctx, j.ID, j.ShopID, *j.operationID, "status_read"); changed {
			if fresh, err := visibleJob(ctx, u.ID, id); err == nil {
				j = fresh
			}
		}
	}
	j.Ahead = ahead(ctx, j)
	if j.Ahead != nil {
		eta(ctx, j, shopOpen(ctx, j.ShopID))
	}
	j.forStudent(u.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
}

// forStudent adds what only the job's own student sees: the counter QR while the job is open.
func (j *Job) forStudent(viewer int64) {
	if j.userID != nil && *j.userID == viewer && active(j.Status) {
		j.PickupToken = PickupToken(j.ID, j.PickupCode)
	}
}

// ---------- student actions ----------

// cancel: only before printing starts. A paid job is refunded through the shop's Kaspi session, or by hand.
func cancelJob(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var paid bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var status, pay string
		err := tx.QueryRow(ctx, `select status, payment_status from print_jobs where id = $1 and user_id = $2 for update`, id, u.ID).Scan(&status, &pay)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if status == "cancelled" {
			return nil
		}
		if status != "new" {
			return refuse(http.StatusConflict, "too_late", MsgTooLate)
		}
		if _, err := tx.Exec(ctx, `update print_jobs set status = 'cancelled', cancel_reason = 'student', closed_at = now(), updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		paid = pay == "paid"
		return event(ctx, tx, id, uid(u), "student", "cancelled", map[string]any{"payment": pay})
	})
	if err != nil {
		reply(w, r, "cancel", err)
		return
	}
	if paid {
		refund(ctx, id, nil, "student")
	}
	respondJob(w, r, u, id)
}

func respondJob(w http.ResponseWriter, r *http.Request, u *auth.User, id int64) {
	j, err := visibleJob(r.Context(), u.ID, id)
	if err != nil {
		fail(w, r, "job", err)
		return
	}
	j.Ahead = ahead(r.Context(), j)
	if j.Ahead != nil {
		eta(r.Context(), j, shopOpen(r.Context(), j.ShopID))
	}
	j.forStudent(u.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
}

// POST /api/print/jobs/{id}/price {"accept": true|false}: the student answers the operator's new price. Accepting
// makes it the job's price (then pay / print); refusing cancels the job.
func answerPrice(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Accept *bool `json:"accept"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || in.Accept == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var status, state string
		var proposed *int64
		err := tx.QueryRow(ctx, `select status, price_state, proposed_total from print_jobs where id = $1 and user_id = $2 for update`, id, u.ID).Scan(&status, &state, &proposed)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		if status != "new" || state != "proposed" || proposed == nil {
			return refuse(http.StatusConflict, "bad_state", MsgState)
		}
		if *in.Accept {
			if _, err := tx.Exec(ctx, `update print_jobs set total = proposed_total, proposed_total = null, price_state = 'final', updated_at = now() where id = $1`, id); err != nil {
				return err
			}
			return event(ctx, tx, id, uid(u), "student", "price_approved", map[string]any{"total": *proposed})
		}
		if _, err := tx.Exec(ctx, `update print_jobs set status = 'cancelled', cancel_reason = 'price_rejected', proposed_total = null, price_state = 'final',
			closed_at = now(), updated_at = now() where id = $1`, id); err != nil {
			return err
		}
		return event(ctx, tx, id, uid(u), "student", "price_rejected", map[string]any{"proposed": *proposed})
	})
	if err != nil {
		reply(w, r, "price answer", err)
		return
	}
	go notifyStaffPrice(context.WithoutCancel(ctx), id, *in.Accept)
	respondJob(w, r, u, id)
}

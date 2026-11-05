package printshop

// From the competitor research (docs/research/print-competitors.md): a price calculator before any upload, a
// rough ready time on top of the queue position, one reminder before a ready job counts as a no-show, and the
// operator's day in numbers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

func extraRoutes(h func(string, http.HandlerFunc)) {
	h("POST /api/print/estimate", auth.Write(estimate))
	h("GET /api/print/staff/report", auth.Need(staffReport))
}

// ---------- POST /api/print/estimate: the price before any upload ----------

// EstimateItem: one imagined file — its page count typed by the student, the same options as a real one.
type EstimateItem struct {
	ServiceID    int64  `json:"serviceId"`
	Size         string `json:"size"`
	Color        bool   `json:"color"`
	Duplex       bool   `json:"duplex"`
	Copies       int    `json:"copies"`
	Pages        int    `json:"pages"`
	BindingID    int64  `json:"bindingId"`
	LaminationID int64  `json:"laminationId"`
}

// Estimate prices imagined files with the very rules of Quote (pricing.go): nothing is stored, nothing is
// promised — the real price is the quote of the uploaded files.
func Estimate(cat Catalog, items []EstimateItem) (*Quoted, error) {
	if len(items) == 0 || len(items) > maxJobFiles {
		return nil, errNoLines
	}
	files := map[int64]FileInfo{}
	in := QuoteInput{}
	for i, it := range items {
		if it.Pages < minDeclared || it.Pages > maxDeclared {
			return nil, errPages
		}
		id := int64(i + 1)
		pages := it.Pages
		files[id] = FileInfo{ID: id, Kind: "pdf", Pages: &pages}
		in.Files = append(in.Files, FileInput{FileID: id, ServiceID: it.ServiceID, Size: it.Size, Color: it.Color, Duplex: it.Duplex,
			Copies: it.Copies, BindingID: it.BindingID, LaminationID: it.LaminationID})
	}
	return Quote(cat, files, in)
}

func estimate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ShopID int64          `json:"shopId"`
		Items  []EstimateItem `json:"items"`
	}
	if err := httpx.Decode(r, &in, 8<<10); err != nil || in.ShopID <= 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if limited(w, r, "estimate:"+httpx.ClientIP(r), 600, time.Hour) {
		return
	}
	ctx := r.Context()
	var q *Quoted
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		shop, err := scanShop(tx.QueryRow(ctx, `select `+shopCols+` from print_shops s where s.id = $1 and s.active`, in.ShopID))
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		if err != nil {
			return err
		}
		svcs, goods, err := catalog(ctx, tx, shop.ID)
		if err != nil {
			return err
		}
		cat := catalogOf(shop, svcs, goods)
		cat.Accepts = []string{"pdf"} // an imagined file is a counted PDF: the estimate never needs a review
		q, err = Estimate(cat, in.Items)
		return err
	})
	if err != nil {
		reply(w, r, "estimate", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"quote": q})
}

// ---------- the rough ready time ----------

func secondsPerPage() int { return config.Int("PRINT_SECONDS_PER_PAGE", 4) }
func secondsPerJob() int  { return config.Int("PRINT_SECONDS_PER_JOB", 60) }

// printedSides: the printed pages of a job (pages × copies of every file line).
func printedSides(lines []Line) int {
	n := 0
	for _, l := range lines {
		if l.Kind == "file" {
			n += l.Pages * l.Copies
		}
	}
	return n
}

// EtaMinutes: the queue ahead plus this job, at the shop's pace (env: seconds per printed page and per job),
// rounded up to five minutes. A rough figure, shown as «≈»; nil when the job is not waiting.
func EtaMinutes(aheadSides, aheadJobs, ownSides int) int {
	sec := (aheadSides+ownSides)*secondsPerPage() + (aheadJobs+1)*secondsPerJob()
	m := (sec + 59) / 60
	if m < 5 {
		return 5
	}
	return (m + 4) / 5 * 5
}

// eta fills j.EtaMin for a waiting job of an open shop.
func eta(ctx context.Context, j *Job, open bool) {
	if !open || (j.Status != "new" && j.Status != "printing") {
		return
	}
	var raws [][]byte
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select lines from print_jobs where shop_id = $1 and status in ('new', 'printing') and (queued_at, id) < ($2, $3)
			and (payment_method = 'pickup' or payment_status = 'paid' or price_state <> 'final') order by queued_at, id limit 300`, j.ShopID, j.QueuedAt, j.ID)
		if err != nil {
			return err
		}
		raws, err = pgx.CollectRows(rows, pgx.RowTo[[]byte])
		return err
	})
	if err != nil {
		return
	}
	sides := 0
	for _, raw := range raws {
		var ls []Line
		if json.Unmarshal(raw, &ls) == nil {
			sides += printedSides(ls)
		}
	}
	own := printedSides(j.Lines)
	if j.Status == "printing" {
		sides, own = 0, own/2 // already on the printer
	}
	m := EtaMinutes(sides, len(raws), own)
	j.EtaMin = &m
}

// shopOpen: the job's shop takes work right now (no estimate for a closed shop: it would be a lie).
func shopOpen(ctx context.Context, shopID int64) bool {
	var s *Shop
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		s, err = scanShop(tx.QueryRow(ctx, `select `+shopCols+` from print_shops s where s.id = $1`, shopID))
		return err
	})
	return err == nil && s.hours().OpenAt(Now())
}

// ---------- one reminder before no-show ----------

// remindReady: a ready job not collected after half the no-show grace (counted from ready or from the chosen
// pickup time, whichever is later) gets one reminder; the «reminded» event makes it once.
func remindReady(ctx context.Context) (int, error) {
	now := Now()
	half := time.Duration(noShowGrace()) * time.Minute / 2
	var ids []int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select j.id from print_jobs j where j.status = 'ready' and j.user_id is not null
			and greatest(j.ready_at, coalesce(j.pickup_at, j.ready_at)) < $1
			and not exists (select 1 from print_job_events e where e.job_id = j.id and e.kind = 'reminded')
			order by j.ready_at limit 50`, now.Add(-half))
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := event(ctx, tx, id, nil, "system", "reminded", nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		notifyStudent(ctx, id, "reminder")
	}
	return len(ids), nil
}

// ---------- GET /api/print/staff/report?shop=&day=YYYY-MM-DD ----------

// Report is one shop day (Asia/Almaty) for the counter and the manager: what came in, what was handed over, the
// money by way of payment (Kaspi straight to the shop, or the counter), refunds, no-shows, pages and the wait.
type Report struct {
	Day           string `json:"day"`
	Received      int    `json:"received"`
	PickedUp      int    `json:"pickedUp"`
	NoShow        int    `json:"noShow"`
	Cancelled     int    `json:"cancelled"`
	Open          int    `json:"open"`
	RevenueKaspi  int64  `json:"revenueKaspi"`
	RevenueCash   int64  `json:"revenueCounter"`
	Refunded      int64  `json:"refunded"`
	RefundsManual int    `json:"refundsManual"`
	PagesBW       int    `json:"pagesBw"`
	PagesColor    int    `json:"pagesColor"`
	Sheets        int    `json:"sheets"`
	WaitMedianMin *int   `json:"waitMedianMin"` // queued → ready, the jobs made ready that day
}

func staffReport(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	ms, err := memberships(ctx, u.ID)
	if err != nil {
		fail(w, r, "memberships", err)
		return
	}
	shopID, ok := pickShop(r, ms)
	mine := false
	for _, m := range ms {
		mine = mine || m.ShopID == shopID
	}
	if !ok || !mine {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return
	}
	day := shopkit.DayStart(Now())
	if v := r.URL.Query().Get("day"); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, shopkit.Almaty)
		if err != nil || d.After(Now()) || d.Before(Now().AddDate(-1, 0, 0)) {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		day = d
	}
	next := day.AddDate(0, 0, 1)
	rep := Report{Day: day.Format("2006-01-02")}
	var waits []float64
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select
				count(*) filter (where created_at >= $2 and created_at < $3),
				count(*) filter (where status = 'picked_up' and closed_at >= $2 and closed_at < $3),
				count(*) filter (where status = 'no_show' and closed_at >= $2 and closed_at < $3),
				count(*) filter (where status = 'cancelled' and closed_at >= $2 and closed_at < $3),
				count(*) filter (where status in ('new', 'printing', 'ready')),
				coalesce(sum(coalesce(paid_amount, total)) filter (where status = 'picked_up' and payment_method = 'kaspi' and closed_at >= $2 and closed_at < $3), 0),
				coalesce(sum(total) filter (where status = 'picked_up' and payment_method = 'pickup' and closed_at >= $2 and closed_at < $3), 0),
				coalesce(sum(total) filter (where payment_status = 'refunded' and updated_at >= $2 and updated_at < $3), 0),
				count(*) filter (where refund_manual)
			from print_jobs where shop_id = $1`, shopID, day, next).Scan(&rep.Received, &rep.PickedUp, &rep.NoShow, &rep.Cancelled, &rep.Open,
			&rep.RevenueKaspi, &rep.RevenueCash, &rep.Refunded, &rep.RefundsManual); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select lines from print_jobs where shop_id = $1 and status = 'picked_up' and closed_at >= $2 and closed_at < $3`, shopID, day, next)
		if err != nil {
			return err
		}
		raws, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
		if err != nil {
			return err
		}
		for _, raw := range raws {
			var ls []Line
			if json.Unmarshal(raw, &ls) != nil {
				continue
			}
			for _, l := range ls {
				if l.Kind != "file" {
					continue
				}
				if l.Color {
					rep.PagesColor += l.Pages * l.Copies
				} else {
					rep.PagesBW += l.Pages * l.Copies
				}
				rep.Sheets += l.Sheets * l.Copies
			}
		}
		rows, err = tx.Query(ctx, `select extract(epoch from ready_at - queued_at) from print_jobs where shop_id = $1 and ready_at >= $2 and ready_at < $3 and ready_at > queued_at`,
			shopID, day, next)
		if err != nil {
			return err
		}
		waits, err = pgx.CollectRows(rows, pgx.RowTo[float64])
		return err
	})
	if err != nil {
		fail(w, r, "report", err)
		return
	}
	if len(waits) > 0 {
		sort.Float64s(waits)
		m := int(waits[len(waits)/2]/60 + 0.5)
		rep.WaitMedianMin = &m
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"report": rep})
}

// reminderText is the «still waiting» message (notify.go).
func reminderText(lang string, j *Job, where string) string {
	t := pick(lang, fmt.Sprintf("<b>Тапсырысыңыз күтіп тұр</b> · №%s\n%s. Кодты айтыңыз не QR-ды көрсетіңіз.", j.PickupCode, where),
		fmt.Sprintf("<b>Ваш заказ ждёт</b> · №%s\n%s. Назовите код или покажите QR.", j.PickupCode, where))
	if j.PaymentMethod == "pickup" && j.paymentStatus != "paid" {
		t += pick(lang, fmt.Sprintf("\nТөлем кассада: %s.", shopkit.Tenge(j.Total)), fmt.Sprintf("\nОплата на кассе: %s.", shopkit.Tenge(j.Total)))
	}
	return t
}

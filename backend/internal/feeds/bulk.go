package feeds

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
)

// Bulk decisions on the pending feed items (POST /api/admin/feeds/items/bulk), for admins and moderators.
// Each item goes through the same moderation.Review path as a single approve / reject (the feed's approve hook
// validates and copies the fields; an item whose deadline has passed is refused there and reported back as
// skipped "expired", not failed). Items are decided in batches of bulkBatch per transaction with a savepoint per
// item, so one bad item never undoes the others. A repeated call is harmless: an item that is no longer pending
// is skipped "not_pending". One audit_log row (feeds_bulk) records the action, the filter and the counts; every
// decided item keeps decided_by.

const (
	bulkMax   = 500 // items per call
	bulkBatch = 25  // items per transaction
)

// BulkFilter selects pending items: by explicit ids, or (all=true) by the filter.
type BulkFilter struct {
	IDs           []int64 `json:"ids"`
	All           bool    `json:"all"`
	MinConfidence float64 `json:"minConfidence"`
	Source        string  `json:"source"`
	Category      string  `json:"category"`
}

func (f BulkFilter) valid() bool {
	if len(f.IDs) > bulkMax || f.MinConfidence < 0 || f.MinConfidence > 1 || utf8.RuneCountInString(f.Source) > 80 || utf8.RuneCountInString(f.Category) > 40 {
		return false
	}
	for _, id := range f.IDs {
		if id <= 0 {
			return false
		}
	}
	return len(f.IDs) > 0 != f.All
}

// pendingSQL lists the pending items matching the filter, oldest first.
const pendingSQL = `select i.id, i.submission_id, coalesce(i.data->>'title_ru', i.data->>'title_kk', ''), i.confidence, s.name, s.id,
		i.category, i.deadline::text
	from feed_items i join feed_sources s on s.id = i.source_id
	where i.status = 'pending' and i.submission_id is not null
	  and (coalesce(cardinality($1::bigint[]), 0) = 0 or i.id = any($1))
	  and i.confidence >= $2 and ($3 = '' or s.id = $3) and ($4 = '' or i.category = $4)
	order by i.created_at, i.id limit $5`

// BulkItem is one row of the preview.
type BulkItem struct {
	ID           int64   `json:"id"`
	SubmissionID int64   `json:"-"`
	Title        string  `json:"title"`
	Confidence   float64 `json:"confidence"`
	Source       string  `json:"source"`
	SourceID     string  `json:"sourceId"`
	Category     string  `json:"category"`
	Deadline     *string `json:"deadline"`
}

func matching(ctx context.Context, f BulkFilter) ([]BulkItem, error) {
	var out []BulkItem
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, pendingSQL, f.IDs, f.MinConfidence, f.Source, f.Category, bulkMax)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BulkItem, error) {
			var b BulkItem
			return b, row.Scan(&b.ID, &b.SubmissionID, &b.Title, &b.Confidence, &b.Source, &b.SourceID, &b.Category, &b.Deadline)
		})
		return err
	})
	if out == nil {
		out = []BulkItem{}
	}
	return out, err
}

// BulkResult answers a bulk decision.
type BulkResult struct {
	Published int           `json:"published"`
	Rejected  int           `json:"rejected"`
	Skipped   []BulkSkipped `json:"skipped"`
	Failed    []BulkSkipped `json:"failed"`
}

type BulkSkipped struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"` // expired, not_pending, or a refusal / error code
}

var (
	msgBulkReason = httpx.Msg{KK: "Қабылдамау себебін жазыңыз (3–500 таңба).", RU: "Укажите причину отклонения (3–500 символов)."}
	msgBulkEmpty  = httpx.Msg{KK: "Сүзгіге сай күтудегі жазба жоқ.", RU: "Нет ожидающих записей под этот фильтр."}
)

// Bulk decides the items; approve=false needs a reason. Exported for tests and tools.
func Bulk(ctx context.Context, actor int64, items []BulkItem, approve bool, reason string) (BulkResult, error) {
	res := BulkResult{Skipped: []BulkSkipped{}, Failed: []BulkSkipped{}}
	for start := 0; start < len(items); start += bulkBatch {
		batch := items[start:min(start+bulkBatch, len(items))]
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			for _, it := range batch {
				sp, err := tx.Begin(ctx) // savepoint
				if err != nil {
					return err
				}
				_, err = moderation.Review(ctx, sp, it.SubmissionID, actor, approve, reason)
				if err == nil {
					_, err = sp.Exec(ctx, `update feed_items set decided_by = $2 where id = $1`, it.ID, actor)
				}
				var ref *moderation.Refusal
				switch {
				case err == nil:
					if err := sp.Commit(ctx); err != nil {
						return err
					}
					if approve {
						res.Published++
					} else {
						res.Rejected++
					}
					continue
				case errors.Is(err, moderation.ErrNotPending):
					res.Skipped = append(res.Skipped, BulkSkipped{it.ID, "not_pending"})
				case errors.As(err, &ref) && ref.Code == "expired":
					res.Skipped = append(res.Skipped, BulkSkipped{it.ID, "expired"})
				case errors.As(err, &ref):
					res.Failed = append(res.Failed, BulkSkipped{it.ID, ref.Code})
				default:
					res.Failed = append(res.Failed, BulkSkipped{it.ID, "error"})
				}
				if err := sp.Rollback(ctx); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// bulk: POST /api/admin/feeds/items/bulk {"action": "approve|reject|preview", "filter": {...}, "reason": ""}.
// preview answers {count, items} without deciding anything; the confirm sheet shows it.
func bulk(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Action string     `json:"action"`
		Filter BulkFilter `json:"filter"`
		Reason string     `json:"reason"`
	}
	if err := httpx.Decode(r, &body, 16<<10); err != nil || !body.Filter.valid() ||
		(body.Action != "approve" && body.Action != "reject" && body.Action != "preview") {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if n := utf8.RuneCountInString(reason); body.Action == "reject" && (n < 3 || n > 500) || n > 500 {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", msgBulkReason)
		return
	}
	ctx := r.Context()
	items, err := matching(ctx, body.Filter)
	if err != nil {
		fail(w, r, "bulk match", err)
		return
	}
	if body.Action == "preview" {
		httpx.JSON(w, http.StatusOK, map[string]any{"count": len(items), "items": items, "max": bulkMax})
		return
	}
	if len(items) == 0 {
		httpx.Error(w, r, http.StatusConflict, "nothing_pending", msgBulkEmpty)
		return
	}
	if ok, retry := ratelimit.Allow(ctx, "feeds:bulk:"+strconv.FormatInt(u.ID, 10), 20, 10*time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	res, err := Bulk(ctx, u.ID, items, body.Action == "approve", reason)
	// the audit row is written even after a partial failure: what was decided stays decided
	if aerr := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, reason, meta) values ($1, 'feeds_bulk', nullif($2, ''),
			jsonb_build_object('decision', $3::text, 'matched', $4::int, 'published', $5::int, 'rejected', $6::int,
				'skipped', $7::int, 'failed', $8::int, 'filter', jsonb_build_object('ids', coalesce(cardinality($9::bigint[]), 0), 'all', $10::boolean,
				'minConfidence', $11::float8, 'source', $12::text, 'category', $13::text)))`,
			u.ID, reason, body.Action, len(items), res.Published, res.Rejected, len(res.Skipped), len(res.Failed),
			body.Filter.IDs, body.Filter.All, body.Filter.MinConfidence, body.Filter.Source, body.Filter.Category)
		return err
	}); aerr != nil && err == nil {
		err = aerr
	}
	if err != nil {
		fail(w, r, "bulk", err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

package printshop

// The counter (docs/print.md «Беру»): a small room, hundreds of jobs a day, one operator. Codes are unique among
// the shop's open jobs (shopkit.UniqueCode); the student says the code or shows a QR from the job screen; the
// operator's lookup finds the one job and shows the first name, page count and first file to check against the
// stack; «Берілді» needs that code or QR (act, proofMatches). Batch moves keep the queue order = the stack order.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// boardCap: at most this many jobs per board column (the column header shows the real count).
const boardCap = 150

// maxBulk: jobs moved by one batch action.
const maxBulk = 40

func lookupsPerHour() int { return config.Int("PRINT_LOOKUPS_PER_HOUR", 3000) }

func counterRoutes(h func(string, http.HandlerFunc)) {
	h("GET /api/print/staff/lookup", auth.Need(lookupJobs))
	h("POST /api/print/staff/jobs/bulk", auth.Write(auth.Need(bulkAction)))
}

// forStaff: the board's view of jobs RLS already let the staff member see — the student's short name to ask
// «атыңыз?» (read as the server: a student's users row is not visible to staff under RLS), no Kaspi link.
func forStaff(ctx context.Context, jobs []*Job) error {
	ids := []int64{}
	for _, j := range jobs {
		if j.userID != nil {
			ids = append(ids, *j.userID)
		}
	}
	names := map[int64]string{}
	if len(ids) > 0 {
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select id, coalesce(name, '') from users where id = any($1) and deleted_at is null`, ids)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id int64
				var n string
				if err := rows.Scan(&id, &n); err != nil {
					return err
				}
				names[id] = n
			}
			return rows.Err()
		})
		if err != nil {
			return err
		}
	}
	for _, j := range jobs {
		short := ""
		if j.userID != nil {
			short = shopkit.ShortName(names[*j.userID])
		}
		j.Customer, j.Pay = &short, nil
	}
	return nil
}

// ---------- the pickup QR ----------

func tokenKey() []byte {
	m := hmac.New(sha256.New, []byte(config.Str("SESSION_SECRET", "")))
	m.Write([]byte("print-pickup-token-v1"))
	return m.Sum(nil)
}

func tokenSig(jobID int64, code string) string {
	m := hmac.New(sha256.New, tokenKey())
	fmt.Fprintf(m, "%d.%s", jobID, code)
	return hex.EncodeToString(m.Sum(nil))[:20]
}

// PickupToken is what the student's QR holds: the job and its code, signed. It proves the screen belongs to the
// job's student (only they are shown it) and stops being useful once the job is closed. It carries no name.
func PickupToken(jobID int64, code string) string {
	return fmt.Sprintf("PJ-%d-%s-%s", jobID, code, tokenSig(jobID, code))
}

var reToken = regexp.MustCompile(`^PJ-([0-9]{1,18})-([0-9]{3,4})-([0-9a-f]{20})$`)

// parseToken returns the job id and code of a well-signed token.
func parseToken(s string) (int64, string, bool) {
	m := reToken.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, "", false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || !hmac.Equal([]byte(m[3]), []byte(tokenSig(id, m[2]))) {
		return 0, "", false
	}
	return id, m[2], true
}

var reCode = regexp.MustCompile(`^[0-9]{3,4}$`)

// proofMatches: the code the operator typed after hearing it, or a QR token of this very job.
func proofMatches(j *Job, proof string) bool {
	proof = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(proof), "№"))
	if proof == "" {
		return false
	}
	if id, code, ok := parseToken(proof); ok {
		return id == j.ID && code == j.PickupCode
	}
	return reCode.MatchString(proof) && proof == j.PickupCode
}

// ---------- GET /api/print/staff/lookup?shop=&q=<code or QR> ----------

// lookupJobs finds the counter's job by what the student says or shows: an open job with that code (codes are
// unique among open jobs), else today's closed one (so «already handed over at 10:42» is visible), or exactly the
// QR's job. Staff of the shop only; RLS hides every other shop.
func lookupJobs(w http.ResponseWriter, r *http.Request, u *auth.User) {
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
	q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("q")), "№"))
	if len(q) > 80 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if limited(w, r, "lookup:"+itoa(u.ID), lookupsPerHour(), time.Hour) {
		return
	}
	var where string
	var args []any
	byToken := false
	if id, code, ok := parseToken(q); ok {
		where, args, byToken = `j.id = $2 and j.pickup_code = $3`, []any{id, code}, true
	} else if reCode.MatchString(q) {
		where, args = `j.pickup_code = $2 and (j.status in ('new', 'printing', 'ready') or j.closed_at >= $3)`, []any{q, shopkit.DayStart(Now())}
	} else {
		httpx.JSON(w, http.StatusOK, map[string]any{"jobs": []*Job{}, "valid": false})
		return
	}
	jobs := []*Job{}
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+jobCols+jobFrom+`
			where j.shop_id = $1 and `+where+`
			order by (j.status in ('new', 'printing', 'ready')) desc, j.id desc limit 5`, append([]any{shopID}, args...)...)
		if err != nil {
			return err
		}
		jobs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Job, error) { return scanJob(row) })
		if err != nil {
			return err
		}
		return withFiles(ctx, tx, jobs, u.ID)
	})
	if err == nil {
		err = forStaff(ctx, jobs)
	}
	if err != nil {
		fail(w, r, "lookup", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"jobs": jobs, "valid": true, "byToken": byToken})
}

// ---------- POST /api/print/staff/jobs/bulk {"action":"printing"|"ready","ids":[…]} ----------

type bulkResult struct {
	ID    int64  `json:"id"`
	OK    bool   `json:"ok"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// bulkAction moves a batch the operator printed in one go, in queue order (the order the stack came out in).
// Each job is its own transaction with the usual checks; a job that cannot move is reported, not fatal. The
// handover is never a batch: it needs each student's own code.
func bulkAction(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in struct {
		Action string  `json:"action"`
		IDs    []int64 `json:"ids"`
	}
	if httpx.Decode(r, &in, 4<<10) != nil || (in.Action != "printing" && in.Action != "ready") || len(in.IDs) == 0 || len(in.IDs) > maxBulk {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	// queue order, without duplicates; RLS: only the caller's shops' jobs come back
	var ids []int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id from print_jobs where id = any($1) order by queued_at, id`, in.IDs)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	if err != nil {
		fail(w, r, "bulk", err)
		return
	}
	found := map[int64]bool{}
	out := []bulkResult{}
	for _, id := range ids {
		found[id] = true
		_, err := act(ctx, u, id, in.Action, "", "")
		res := bulkResult{ID: id, OK: err == nil}
		var ae *apiErr
		if errors.As(err, &ae) {
			res.Code, res.Error = ae.code, pickMsg(r, ae.msg)
		} else if err != nil {
			res.Code = "internal"
		}
		out = append(out, res)
	}
	for _, id := range in.IDs {
		if !found[id] {
			out = append(out, bulkResult{ID: id, Code: "not_found"})
			found[id] = true
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"results": out})
}

func pickMsg(r *http.Request, m httpx.Msg) string { return pick(httpx.Lang(r), m.KK, m.RU) }

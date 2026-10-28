// Package discounts lets students confirm whether a discount from /api/content worked
// ("Жұмыс істеді / Жұмыс істемеді", F7): POST /api/discounts/{id}/check and the public
// counts GET /api/discounts/checks. See docs/api/discounts.md.
package discounts

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

const (
	VoteEvery  = 30 // days between two votes of one student on one discount
	WindowDays = 60 // public counts cover this many days
	idsTTL     = 5 * time.Minute
)

var (
	MsgNoDiscount = httpx.Msg{KK: "Жеңілдік табылмады.", RU: "Скидка не найдена."}
	MsgAlready    = httpx.Msg{KK: "Бұл жеңілдікті жақында бағаладыңыз. 30 күннен кейін қайта бағалай аласыз.", RU: "Вы недавно отметили эту скидку. Снова можно через 30 дней."}
	MsgWorked     = httpx.Msg{KK: "«worked» өрісі керек: true не false.", RU: "Нужно поле «worked»: true или false."}
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func init() {
	me.RegisterExport("discountChecks", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select discount_id, worked, created_at from discount_checks where user_id = app_user_id() order by id")
	})
}

// Routes wires the discount check endpoints.
func Routes(mux httpx.Router) {
	mux.HandleFunc("POST /api/discounts/{id}/check", auth.Write(auth.Need(check)))
	mux.HandleFunc("GET /api/discounts/checks", list)
}

// Known reports whether id is a discount currently served by /api/content. The id set is
// cached for a few minutes so a vote does not refetch every sheet; tests replace Known.
var Known = func(ctx context.Context, id string) bool {
	ids := knownIDs(ctx)
	return ids[id]
}

var (
	idsMu  sync.Mutex
	idsAt  time.Time
	idsSet map[string]bool
)

func knownIDs(ctx context.Context) map[string]bool {
	idsMu.Lock()
	defer idsMu.Unlock()
	if idsSet != nil && time.Since(idsAt) < idsTTL {
		return idsSet
	}
	c := content.Default.Load(context.WithoutCancel(ctx), time.Now())
	set := make(map[string]bool, len(c.Discounts))
	for _, r := range c.Discounts {
		if id := content.RecordID(r); id != "" {
			set[id] = true
		}
	}
	idsSet, idsAt = set, time.Now()
	return set
}

// Stats is the public summary of one discount.
type Stats struct {
	Worked          int        `json:"worked"`
	NotWorked       int        `json:"notWorked"`
	LastConfirmedAt *time.Time `json:"lastConfirmedAt"`
}

const statsSQL = `select discount_id,
	count(*) filter (where worked and created_at > now() - make_interval(days => $1)),
	count(*) filter (where not worked and created_at > now() - make_interval(days => $1)),
	max(created_at) filter (where worked)
	from discount_checks`

func scanStats(rows pgx.Rows) (map[string]Stats, error) {
	out := map[string]Stats{}
	for rows.Next() {
		var id string
		var s Stats
		if err := rows.Scan(&id, &s.Worked, &s.NotWorked, &s.LastConfirmedAt); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}

// AllStats aggregates every discount's votes (as the connection owner: counts only, no voters).
func AllStats(ctx context.Context) (map[string]Stats, error) {
	var out map[string]Stats
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, statsSQL+" group by discount_id", WindowDays)
		if err != nil {
			return err
		}
		defer rows.Close()
		out, err = scanStats(rows)
		return err
	})
	return out, err
}

func statsOf(ctx context.Context, id string) (Stats, error) {
	var out map[string]Stats
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, statsSQL+" where discount_id = $2 group by discount_id", WindowDays, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		out, err = scanStats(rows)
		return err
	})
	return out[id], err
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("discounts: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func list(w http.ResponseWriter, r *http.Request) {
	all, err := AllStats(r.Context())
	if err != nil {
		fail(w, r, "stats", err)
		return
	}
	w.Header().Set("Cache-Control", "public, s-maxage=300")
	httpx.JSON(w, http.StatusOK, map[string]any{"checks": all, "windowDays": WindowDays})
}

var errAlready = errors.New("already voted")

func check(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", me.MsgConsent)
		return
	}
	id := r.PathValue("id")
	if !idRe.MatchString(id) || !Known(ctx, id) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoDiscount)
		return
	}
	var body struct {
		Worked *bool `json:"worked"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Worked == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", MsgWorked)
		return
	}
	if ok, retry := ratelimit.Allow(ctx, "dc:u:"+strconv.FormatInt(u.ID, 10), config.Int("DISCOUNT_CHECKS_PER_HOUR", 20), time.Hour); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	var createdAt, last time.Time
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		// one vote per student per discount per 30 days, also under concurrent requests
		if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1, 0))",
			"discount_check:"+strconv.FormatInt(u.ID, 10)+":"+id); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `select created_at from discount_checks where user_id = app_user_id()
			and discount_id = $1 and created_at > now() - make_interval(days => $2)
			order by created_at desc limit 1`, id, VoteEvery).Scan(&last)
		if err == nil {
			return errAlready
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return tx.QueryRow(ctx, `insert into discount_checks (discount_id, user_id, worked)
			values ($1, app_user_id(), $2) returning created_at`, id, *body.Worked).Scan(&createdAt)
	})
	if errors.Is(err, errAlready) {
		httpx.JSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
			"code": "already_checked", "message": pick(r, MsgAlready),
			"nextAllowedAt": last.Add(VoteEvery * 24 * time.Hour).UTC(),
		}})
		return
	}
	if err != nil {
		fail(w, r, "check", err)
		return
	}
	st, err := statsOf(ctx, id)
	if err != nil {
		fail(w, r, "stats", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"check": map[string]any{"discountId": id, "worked": *body.Worked, "createdAt": createdAt.UTC()},
		"stats": st,
	})
}

func pick(r *http.Request, m httpx.Msg) string {
	if httpx.Lang(r) == "ru" {
		return m.RU
	}
	return m.KK
}

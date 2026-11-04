package points

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/discounts"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
)

// Kinds of rewards. None of them is cash.
var Kinds = []string{"discount", "merch", "print_quota", "priority_event"}

type Reward struct {
	ID           int64   `json:"id"`
	Kind         string  `json:"kind"`
	TitleKK      string  `json:"titleKk"`
	TitleRU      string  `json:"titleRu"`
	DescKK       string  `json:"descKk"`
	DescRU       string  `json:"descRu"`
	Cost         int     `json:"cost"`
	Stock        *int    `json:"stock"` // null = unlimited
	PerUserLimit int     `json:"perUserLimit"`
	DiscountID   *string `json:"discountId"`
	Active       bool    `json:"active"`
	// admin only
	CodesLeft *int `json:"codesLeft,omitempty"`
	// student only: redemptions that still count against perUserLimit
	Mine *int `json:"mine,omitempty"`
}

const rewardCols = `r.id, r.kind, r.title_kk, r.title_ru, r.desc_kk, r.desc_ru, r.cost, r.stock, r.per_user_limit, r.discount_id, r.active`

func scanReward(row pgx.Row, extra ...any) (Reward, error) {
	var x Reward
	err := row.Scan(append([]any{&x.ID, &x.Kind, &x.TitleKK, &x.TitleRU, &x.DescKK, &x.DescRU, &x.Cost, &x.Stock,
		&x.PerUserLimit, &x.DiscountID, &x.Active}, extra...)...)
	return x, err
}

// GET /api/points/rewards: the active catalog (RLS: active rows only) with how many of each the
// student already has.
func listRewards(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []Reward{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+rewardCols+`, (select count(*)::int from reward_redemptions m
			where m.reward_id = r.id and m.user_id = app_user_id() and m.status <> 'rejected')
			from rewards r where r.active order by r.cost, r.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var mine int
			x, err := scanReward(rows, &mine)
			if err != nil {
				return err
			}
			x.Mine = &mine
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "rewards", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"rewards": out})
}

type Redemption struct {
	ID          int64      `json:"id"`
	RewardID    int64      `json:"rewardId"`
	Kind        string     `json:"kind"`
	TitleKK     string     `json:"titleKk"`
	TitleRU     string     `json:"titleRu"`
	Cost        int        `json:"cost"`
	Status      string     `json:"status"`
	Code        *string    `json:"code"`
	Note        *string    `json:"note"`
	CreatedAt   time.Time  `json:"createdAt"`
	DecidedAt   *time.Time `json:"decidedAt"`
	UserID      int64      `json:"userId,omitempty"`   // admin view
	UserName    *string    `json:"userName,omitempty"` // admin view
	UserBalance *int       `json:"userBalance,omitempty"`
}

const redemptionCols = `m.id, m.reward_id, r.kind, r.title_kk, r.title_ru, m.cost, m.status, m.code, m.note, m.created_at, m.decided_at`

func scanRedemption(row pgx.Row, extra ...any) (Redemption, error) {
	var x Redemption
	err := row.Scan(append([]any{&x.ID, &x.RewardID, &x.Kind, &x.TitleKK, &x.TitleRU, &x.Cost, &x.Status, &x.Code, &x.Note,
		&x.CreatedAt, &x.DecidedAt}, extra...)...)
	return x, err
}

// GET /api/points/redemptions: the student's own requests, code visible once approved.
func myRedemptions(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []Redemption{}
	// as the owner, filtered by user: the rewards RLS policy hides archived rewards from app_user,
	// and a past redemption must keep its title
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+redemptionCols+` from reward_redemptions m join rewards r on r.id = m.reward_id
			where m.user_id = $1 order by m.id desc limit 50`, u.ID)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Redemption, error) { return scanRedemption(row) })
		return err
	})
	if err != nil {
		fail(w, r, "redemptions", err)
		return
	}
	for i := range out {
		if out[i].Status != "approved" {
			out[i].Code = nil
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"redemptions": out})
}

var (
	errNoReward = errors.New("points: no such reward")
	errLimit    = errors.New("points: per-user limit")
)

// Redeem spends coins on a reward and queues the request for an admin. Concurrency: the wallet
// row is locked (SELECT … FOR UPDATE) for the whole transaction and coins >= 0 is a CHECK, so
// parallel requests can never overspend; (user_id, idem_key) makes a retried request a no-op
// that returns the first result.
func Redeem(ctx context.Context, userID, rewardID int64, key string, now time.Time) (*Redemption, bool, error) {
	var out Redemption
	created := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		wl, err := lockWallet(ctx, tx, userID)
		if err != nil {
			return err
		}
		var existing int64
		err = tx.QueryRow(ctx, `select id from reward_redemptions where user_id = $1 and idem_key = $2`, userID, key).Scan(&existing)
		if err == nil {
			out, err = scanRedemption(tx.QueryRow(ctx, `select `+redemptionCols+` from reward_redemptions m join rewards r on r.id = m.reward_id where m.id = $1`, existing))
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		rw, err := scanReward(tx.QueryRow(ctx, `select `+rewardCols+` from rewards r where r.id = $1 and r.active for update`, rewardID))
		if errors.Is(err, pgx.ErrNoRows) {
			return errNoReward
		}
		if err != nil {
			return err
		}
		if rw.Stock != nil && *rw.Stock <= 0 {
			return errNoReward
		}
		var mine int
		if err := tx.QueryRow(ctx, `select count(*) from reward_redemptions where user_id = $1 and reward_id = $2 and status <> 'rejected'`,
			userID, rewardID).Scan(&mine); err != nil {
			return err
		}
		if mine >= rw.PerUserLimit {
			return errLimit
		}
		if wl.Coins < rw.Cost {
			return errNoCoins
		}
		var id int64
		if err := tx.QueryRow(ctx, `insert into reward_redemptions (user_id, reward_id, cost, idem_key, created_at)
			values ($1, $2, $3, $4, $5) returning id`, userID, rewardID, rw.Cost, key, now).Scan(&id); err != nil {
			return err
		}
		if err := spend(ctx, tx, userID, wl, -rw.Cost, ReasonRedeem, "redemption", itoa(id), nil, "", now); err != nil {
			return err
		}
		if rw.Stock != nil {
			if _, err := tx.Exec(ctx, `update rewards set stock = stock - 1, updated_at = now() where id = $1`, rewardID); err != nil {
				return err
			}
		}
		created = true
		out, err = scanRedemption(tx.QueryRow(ctx, `select `+redemptionCols+` from reward_redemptions m join rewards r on r.id = m.reward_id where m.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, false, err
	}
	if out.Status != "approved" {
		out.Code = nil
	}
	return &out, created, nil
}

// POST /api/points/redeem {"rewardId": 3, "key": "<client idempotency key>"}
func redeem(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) || limited(w, r, "redeem", u, "POINTS_REDEEMS_PER_HOUR", 10, time.Hour) {
		return
	}
	var body struct {
		RewardID int64  `json:"rewardId"`
		Key      string `json:"key"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.RewardID <= 0 || !reKey.MatchString(body.Key) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	red, created, err := Redeem(r.Context(), u.ID, body.RewardID, body.Key, Now())
	switch {
	case errors.Is(err, errNoReward):
		httpx.Error(w, r, http.StatusNotFound, "reward_unavailable", MsgNoReward)
	case errors.Is(err, errLimit):
		httpx.Error(w, r, http.StatusConflict, "limit_reached", MsgLimit)
	case errors.Is(err, errNoCoins):
		httpx.Error(w, r, http.StatusConflict, "not_enough_coins", MsgNoCoins)
	case err != nil:
		fail(w, r, "redeem", err)
	default:
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		s, err := load(r.Context(), u.ID, Now())
		if err != nil {
			fail(w, r, "me", err)
			return
		}
		httpx.JSON(w, status, map[string]any{"redemption": red, "me": s})
	}
}

// ---------- admin ----------

func adminRoutes(h func(string, http.HandlerFunc)) {
	admin := func(fn auth.Handler) http.HandlerFunc { return auth.NeedRole(fn, "admin") }
	h("GET /api/admin/points/rewards", admin(adminRewards))
	h("POST /api/admin/points/rewards", auth.Write(admin(saveReward)))
	h("PUT /api/admin/points/rewards/{id}", auth.Write(admin(saveReward)))
	h("GET /api/admin/points/redemptions", admin(adminRedemptions))
	h("POST /api/admin/points/redemptions/{id}/approve", auth.Write(admin(decide(true))))
	h("POST /api/admin/points/redemptions/{id}/reject", auth.Write(admin(decide(false))))
	h("GET /api/admin/points/users/{userId}", admin(adminLedger))
	h("POST /api/admin/points/users/{userId}/adjust", auth.Write(admin(adjust)))
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func adminRewards(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	out := []Reward{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+rewardCols+`, (select count(*)::int from reward_codes c where c.reward_id = r.id and c.redemption_id is null)
			from rewards r order by r.active desc, r.id desc`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var left int
			x, err := scanReward(rows, &left)
			if err != nil {
				return err
			}
			x.CodesLeft = &left
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "admin rewards", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"rewards": out, "kinds": Kinds})
}

type rewardIn struct {
	Kind         string   `json:"kind"`
	TitleKK      string   `json:"titleKk"`
	TitleRU      string   `json:"titleRu"`
	DescKK       string   `json:"descKk"`
	DescRU       string   `json:"descRu"`
	Cost         int      `json:"cost"`
	Stock        *int     `json:"stock"`
	PerUserLimit int      `json:"perUserLimit"`
	DiscountID   *string  `json:"discountId"`
	Active       *bool    `json:"active"`
	AddCodes     []string `json:"addCodes"`
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// validate returns the field that is wrong, or "".
func (in *rewardIn) validate(ctx context.Context) string {
	in.TitleKK, in.TitleRU = strings.TrimSpace(in.TitleKK), strings.TrimSpace(in.TitleRU)
	in.DescKK, in.DescRU = strings.TrimSpace(in.DescKK), strings.TrimSpace(in.DescRU)
	switch {
	case !contains(Kinds, in.Kind):
		return "kind"
	case runes(in.TitleKK) < 2 || runes(in.TitleKK) > 120:
		return "titleKk"
	case runes(in.TitleRU) < 2 || runes(in.TitleRU) > 120:
		return "titleRu"
	case runes(in.DescKK) > 600:
		return "descKk"
	case runes(in.DescRU) > 600:
		return "descRu"
	case in.Cost < 1 || in.Cost > 100000:
		return "cost"
	case in.Stock != nil && (*in.Stock < 0 || *in.Stock > 1000000):
		return "stock"
	case in.PerUserLimit < 1 || in.PerUserLimit > 50:
		return "perUserLimit"
	case len(in.AddCodes) > 500:
		return "addCodes"
	}
	if in.DiscountID != nil && *in.DiscountID == "" {
		in.DiscountID = nil
	}
	if in.Kind == "discount" && (in.DiscountID == nil || !discounts.Known(ctx, *in.DiscountID)) {
		return "discountId"
	}
	if in.Kind != "discount" {
		in.DiscountID = nil
	}
	codes := in.AddCodes[:0]
	for _, c := range in.AddCodes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if runes(c) > 200 {
			return "addCodes"
		}
		codes = append(codes, c)
	}
	in.AddCodes = codes
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// POST /api/admin/points/rewards, PUT /api/admin/points/rewards/{id}. Archive = active:false; a
// reward is never deleted, so past redemptions keep their title.
func saveReward(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	ctx := r.Context()
	var id int64
	if r.Method == http.MethodPut {
		var ok bool
		if id, ok = pathID(r, "id"); !ok {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
	}
	var in rewardIn
	if err := httpx.Decode(r, &in, 64<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if field := in.validate(ctx); field != "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "invalid", "field": field, "message": pick(r, httpx.MsgBad)}})
		return
	}
	active := in.Active == nil || *in.Active
	var out Reward
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `insert into rewards (kind, title_kk, title_ru, desc_kk, desc_ru, cost, stock, per_user_limit, discount_id, active, created_by)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning id`,
				in.Kind, in.TitleKK, in.TitleRU, in.DescKK, in.DescRU, in.Cost, in.Stock, in.PerUserLimit, in.DiscountID, active, admin.ID).Scan(&id)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `update rewards set kind = $2, title_kk = $3, title_ru = $4, desc_kk = $5, desc_ru = $6, cost = $7,
				stock = $8, per_user_limit = $9, discount_id = $10, active = $11, updated_at = now() where id = $1`,
				id, in.Kind, in.TitleKK, in.TitleRU, in.DescKK, in.DescRU, in.Cost, in.Stock, in.PerUserLimit, in.DiscountID, active)
			if err == nil && tag.RowsAffected() == 0 {
				return pgx.ErrNoRows
			}
		}
		if err != nil {
			return err
		}
		for _, c := range in.AddCodes {
			if _, err := tx.Exec(ctx, `insert into reward_codes (reward_id, code) values ($1, $2) on conflict do nothing`, id, c); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, reason, meta) values ($1, 'points_reward_save', null,
			jsonb_build_object('reward', $2::bigint, 'cost', $3::int, 'active', $4::boolean, 'codesAdded', $5::int))`,
			admin.ID, id, in.Cost, active, len(in.AddCodes)); err != nil {
			return err
		}
		var left int
		out, err = scanReward(tx.QueryRow(ctx, `select `+rewardCols+`, (select count(*)::int from reward_codes c where c.reward_id = r.id and c.redemption_id is null)
			from rewards r where r.id = $1`, id), &left)
		out.CodesLeft = &left
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save reward", err)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, map[string]any{"reward": out})
}

// GET /api/admin/points/redemptions?status=pending|approved|rejected (default pending, oldest first)
func adminRedemptions(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_status", httpx.MsgBad)
		return
	}
	order := "m.created_at"
	if status != "pending" {
		order = "m.decided_at desc"
	}
	ctx := r.Context()
	var out []Redemption
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+redemptionCols+`, m.user_id, u.name, coalesce(pw.coins, 0)
			from reward_redemptions m join rewards r on r.id = m.reward_id join users u on u.id = m.user_id
			left join points_wallets pw on pw.user_id = m.user_id
			where m.status = $1 order by `+order+` limit 200`, status)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Redemption, error) {
			var uid int64
			var name *string
			var bal int
			x, err := scanRedemption(row, &uid, &name, &bal)
			x.UserID, x.UserName, x.UserBalance = uid, name, &bal
			return x, err
		})
		return err
	})
	if err != nil {
		fail(w, r, "admin redemptions", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"redemptions": out})
}

var errNotPending = errors.New("points: not pending")
var errNeedCode = errors.New("points: code required")

// Decide approves (with a code: the admin's, else the next one from the reward's pool) or
// rejects (coins refunded and stock restored in the same transaction) a pending redemption.
func Decide(ctx context.Context, adminID, id int64, approve bool, code, note string, now time.Time) (*Redemption, int64, error) {
	var out Redemption
	var userID int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var status, kind string
		var cost int
		var rewardID int64
		var stock *int
		if err := tx.QueryRow(ctx, `select m.user_id, m.status, m.cost, m.reward_id, r.kind, r.stock from reward_redemptions m
			join rewards r on r.id = m.reward_id where m.id = $1`, id).Scan(&userID, &status, &cost, &rewardID, &kind, &stock); err != nil {
			return err
		}
		// lock order: wallet, then redemption (the same order as Redeem)
		wl, err := lockWallet(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select status from reward_redemptions where id = $1 for update`, id).Scan(&status); err != nil {
			return err
		}
		if status != "pending" {
			return errNotPending
		}
		var c, n *string
		if note != "" {
			n = &note
		}
		if approve {
			if code == "" {
				var pooled string
				err := tx.QueryRow(ctx, `update reward_codes set redemption_id = $2 where id = (
					select id from reward_codes where reward_id = $1 and redemption_id is null order by id limit 1 for update skip locked)
					returning code`, rewardID, id).Scan(&pooled)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				code = pooled
			}
			if code == "" && kind == "discount" {
				return errNeedCode
			}
			if code != "" {
				c = &code
			}
			_, err = tx.Exec(ctx, `update reward_redemptions set status = 'approved', code = $2, note = $3, decided_by = $4, decided_at = $5 where id = $1`,
				id, c, n, adminID, now)
		} else {
			if _, err = tx.Exec(ctx, `update reward_redemptions set status = 'rejected', note = $2, decided_by = $3, decided_at = $4 where id = $1`,
				id, n, adminID, now); err != nil {
				return err
			}
			if err := spend(ctx, tx, userID, wl, cost, ReasonRefund, "redemption", itoa(id), &adminID, "", now); err != nil {
				return err
			}
			if stock != nil {
				_, err = tx.Exec(ctx, `update rewards set stock = stock + 1, updated_at = now() where id = $1`, rewardID)
			}
		}
		if err != nil {
			return err
		}
		action := "points_redeem_approve"
		if !approve {
			action = "points_redeem_reject"
		}
		if _, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, $2, $3, nullif($4, ''), jsonb_build_object('redemption', $5::bigint, 'cost', $6::int))`,
			adminID, action, userID, note, id, cost); err != nil {
			return err
		}
		out, err = scanRedemption(tx.QueryRow(ctx, `select `+redemptionCols+` from reward_redemptions m join rewards r on r.id = m.reward_id where m.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, userID, nil
}

// Notify tells the student about a decision through the shared dispatcher (inbox first). The
// promo code itself is not sent to Telegram: the student opens /points to see it.
var Notify = func(ctx context.Context, userID int64, red *Redemption, lang string) {
	title, text := red.TitleKK, ""
	if lang == "ru" {
		title = red.TitleRU
	}
	switch {
	case red.Status == "approved" && lang == "ru":
		text = fmt.Sprintf("Заявка на награду «%s» одобрена. Код и детали — в разделе «Баллы».", title)
	case red.Status == "approved":
		text = fmt.Sprintf("«%s» сыйлығына өтінім бекітілді. Код пен мәлімет — «Ұпайлар» бөлімінде.", title)
	case lang == "ru":
		text = fmt.Sprintf("Заявка на награду «%s» отклонена, %d монет вернулись на баланс.", title, red.Cost)
	default:
		text = fmt.Sprintf("«%s» сыйлығына өтінім қабылданбады, %d монета балансқа қайтарылды.", title, red.Cost)
	}
	if red.Status == "rejected" && red.Note != nil {
		if lang == "ru" {
			text += " Причина: " + *red.Note
		} else {
			text += " Себебі: " + *red.Note
		}
	}
	if _, err := notify.Send(ctx, userID, notify.CatModeration, text, notify.Opts{Link: "/points", Key: "points_redemption:" + itoa(red.ID) + ":" + red.Status}); err != nil {
		log.Printf("points: notify redemption %d: %v", red.ID, err)
	}
}

func decide(approve bool) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, admin *auth.User) {
		id, ok := pathID(r, "id")
		if !ok {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
		var body struct {
			Code string `json:"code"`
			Note string `json:"note"`
		}
		if err := httpx.Decode(r, &body, 4<<10); err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		body.Code, body.Note = strings.TrimSpace(body.Code), strings.TrimSpace(body.Note)
		if runes(body.Code) > 200 || runes(body.Note) > 500 || (!approve && runes(body.Note) < 3) {
			httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
			return
		}
		red, userID, err := Decide(r.Context(), admin.ID, id, approve, body.Code, body.Note, Now())
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		case errors.Is(err, errNotPending):
			httpx.Error(w, r, http.StatusConflict, "not_pending", MsgDecided)
			return
		case errors.Is(err, errNeedCode):
			httpx.Error(w, r, http.StatusBadRequest, "code_required", MsgNeedCode)
			return
		case err != nil:
			fail(w, r, "decide", err)
			return
		}
		var lang string
		_ = db.Tx(r.Context(), func(tx pgx.Tx) error {
			return tx.QueryRow(r.Context(), `select lang from users where id = $1`, userID).Scan(&lang)
		})
		Notify(context.WithoutCancel(r.Context()), userID, red, lang)
		httpx.JSON(w, http.StatusOK, map[string]any{"redemption": red})
	}
}

// GET /api/admin/points/users/{userId}: wallet + the whole coin ledger (newest first) + XP events.
func adminLedger(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	uid, ok := pathID(r, "userId")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoAccount)
		return
	}
	ctx := r.Context()
	out := map[string]any{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var name *string
		var coins int
		var xp int64
		var streak, best, freezes int
		var last *time.Time
		err := tx.QueryRow(ctx, `select u.name, coalesce(pw.coins, 0), coalesce(pw.xp, 0), coalesce(pw.streak_days, 0), coalesce(pw.streak_best, 0),
			pw.last_active_day, coalesce(pw.freezes, 0) from users u left join points_wallets pw on pw.user_id = u.id
			where u.id = $1 and u.deleted_at is null`, uid).Scan(&name, &coins, &xp, &streak, &best, &last, &freezes)
		if err != nil {
			return err
		}
		var sum int
		if err := tx.QueryRow(ctx, `select coalesce(sum(delta), 0) from coin_ledger where user_id = $1`, uid).Scan(&sum); err != nil {
			return err
		}
		out["user"] = map[string]any{"id": uid, "name": name, "coins": coins, "ledgerSum": sum, "xp": xp,
			"streak": CurrentStreak(last, streak, freezes, Day(Now())), "streakBest": best, "freezes": freezes}
		rows, err := tx.Query(ctx, `select l.id, l.delta, l.reason, l.ref_type, l.ref_id, l.note, l.actor_id, l.created_at
			from coin_ledger l where l.user_id = $1 order by l.id desc limit 500`, uid)
		if err != nil {
			return err
		}
		type row struct {
			ID        int64     `json:"id"`
			Delta     int       `json:"delta"`
			Reason    string    `json:"reason"`
			RefType   string    `json:"refType"`
			RefID     string    `json:"refId"`
			Note      *string   `json:"note"`
			ActorID   *int64    `json:"actorId"`
			CreatedAt time.Time `json:"createdAt"`
		}
		ledger, err := pgx.CollectRows(rows, func(rr pgx.CollectableRow) (row, error) {
			var x row
			return x, rr.Scan(&x.ID, &x.Delta, &x.Reason, &x.RefType, &x.RefID, &x.Note, &x.ActorID, &x.CreatedAt)
		})
		if err != nil {
			return err
		}
		out["ledger"] = ledger
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoAccount)
		return
	}
	if err != nil {
		fail(w, r, "admin ledger", err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /api/admin/points/users/{userId}/adjust {"delta": -5, "reason": "...", "key": "..."}:
// a manual correction with a required reason, recorded in the ledger (actor_id) and audit_log.
func adjust(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	uid, ok := pathID(r, "userId")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoAccount)
		return
	}
	var body struct {
		Delta  int    `json:"delta"`
		Reason string `json:"reason"`
		Key    string `json:"key"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || body.Delta == 0 || body.Delta < -100000 || body.Delta > 100000 || !reKey.MatchString(body.Key) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if n := runes(body.Reason); n < 5 || n > 500 {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from users where id = $1 and deleted_at is null)`, uid).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		wl, err := lockWallet(ctx, tx, uid)
		if err != nil {
			return err
		}
		if err := spend(ctx, tx, uid, wl, body.Delta, ReasonAdjust, "admin_adjust", body.Key, &admin.ID, body.Reason, Now()); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'points_adjust', $2, $3, jsonb_build_object('delta', $4::int))`, admin.ID, uid, body.Reason, body.Delta)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoAccount)
	case errors.Is(err, errNoCoins):
		httpx.Error(w, r, http.StatusConflict, "not_enough_coins", MsgNoCoins)
	case errors.Is(err, errDuplicate):
		adminLedger(w, r, admin) // a retried request with the same key
	case err != nil:
		fail(w, r, "adjust", err)
	default:
		adminLedger(w, r, admin)
	}
}

func pick(r *http.Request, m httpx.Msg) string {
	if httpx.Lang(r) == "ru" {
		return m.RU
	}
	return m.KK
}

// Package points is the gamification layer («Ұпайлар / Баллы»): XP and levels, a daily streak in
// Asia/Almaty days with one purchasable streak freeze, coins in an append-only ledger, weekly
// leaderboards and a rewards shop whose redemptions an admin moderates. See docs/api/points.md.
//
// Coins are not money: there are no cash payouts and no transfers between students. Turning
// coins into cash would need a licensed payment partner and legal review first (TODO, not built).
//
// Other modules award points through hooks wired in internal/server/glue.go (quiz attempts, event
// check-ins), inside their own transaction, so an action and its award commit together. Award is
// idempotent: the ledgers' unique (user_id, reason, ref_type, ref_id) means one action pays once.
package points

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	_ "time/tzdata" // Asia/Almaty must resolve without system zoneinfo

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Feature is the switch name: FEATURE_POINTS. While it is off nothing is awarded and every
// /api/points route answers 404.
const Feature = "points"

// Reasons. Server-verifiable actions earn XP and coins; the device-only roadmap step (the
// roadmap lives in the browser's localStorage) earns a little XP and no coins, with tight caps,
// because the server cannot check it. Personal calendar deadlines pay nothing: they are
// device-only and have no «done» state, so any award would be a free self-click.
const (
	ReasonDailyOpen    = "daily_open"
	ReasonQuiz         = "quiz"
	ReasonEventCheckin = "event_checkin"
	ReasonRoadmapStep  = "roadmap_step"

	ReasonFreeze      = "freeze_purchase" // coins only, negative
	ReasonRedeem      = "redeem"          // coins only, negative
	ReasonRefund      = "redeem_refund"   // coins only, positive
	ReasonAdjust      = "admin_adjust"    // coins only, either sign
	reasonRevokeSufix = "_revoked"        // a reversed award: event_checkin_revoked
)

// Rule is what one action earns and how often it may pay.
type Rule struct {
	XP      int  `json:"xp"`
	Coins   int  `json:"coins"`
	PerDay  int  `json:"perDay"`            // awards of this reason per Almaty day
	PerWeek int  `json:"perWeek,omitempty"` // awards per Almaty week (Mon–Sun), 0 = no weekly cap
	Device  bool `json:"deviceOnly"`        // self-reported from the device: XP only
}

// Rules is the earning table. Changing it changes future awards only.
var Rules = map[string]Rule{
	ReasonDailyOpen:    {XP: 5, Coins: 1, PerDay: 1},
	ReasonQuiz:         {XP: 10, Coins: 2, PerDay: 5}, // +5 XP at 80 %+; coins only at 50 %+
	ReasonEventCheckin: {XP: 30, Coins: 15, PerDay: 3},
	ReasonRoadmapStep:  {XP: 5, PerDay: 1, PerWeek: 3, Device: true},
}

// TaskOrder is the order of «Бүгінгі тапсырмалар».
var TaskOrder = []string{ReasonDailyOpen, ReasonQuiz, ReasonEventCheckin, ReasonRoadmapStep}

const (
	DailyXPCap   = 150 // all earning reasons together, per Almaty day
	DailyCoinCap = 30
	FreezeCost   = 20
	MaxFreezes   = 1 // with FEATURE_STREAK on: StreakMaxFreezes (see maxFreezes)
	QuizBonusXP  = 5
)

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

// Now is replaced in tests.
var Now = time.Now

// Day is the Asia/Almaty calendar day of t, as midnight UTC of that date (a pgx date value).
func Day(t time.Time) time.Time {
	a := t.In(almaty)
	return time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
}

// WeekStart is Monday 00:00 Asia/Almaty of the week containing t.
func WeekStart(t time.Time) time.Time {
	a := t.In(almaty)
	offset := (int(a.Weekday()) + 6) % 7 // Monday = 0
	return time.Date(a.Year(), a.Month(), a.Day()-offset, 0, 0, 0, 0, almaty)
}

// Level for a total XP: level L starts at 50·L·(L−1) XP (0, 100, 300, 600, 1000, ...).
func Level(xp int64) (level int, floor, next int64) {
	level = 1
	for 50*int64(level+1)*int64(level) <= xp {
		level++
	}
	return level, 50 * int64(level) * int64(level-1), 50 * int64(level+1) * int64(level)
}

// Award is the outcome of one earning action.
type Award struct {
	XP        int    `json:"xp"`
	Coins     int    `json:"coins"`
	Duplicate bool   `json:"duplicate,omitempty"` // this action already paid earlier
	Capped    bool   `json:"capped,omitempty"`    // a daily/weekly cap stopped (part of) it
	Reason    string `json:"reason"`
	Milestone int    `json:"milestone,omitempty"` // a streak milestone this action reached (spec 08)
}

// Earn describes one action to award.
type Earn struct {
	UserID  int64
	Reason  string
	RefType string
	RefID   string
	XP      int // 0 = the rule's XP
	Coins   int // -1 = none, 0 = the rule's coins
}

// Querier is the part of pgx.Tx the ledger needs.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// lockWallet creates the wallet if needed and locks it for the rest of the transaction. Every
// balance change goes through it, so per-user awards, caps and spending are serialized.
func lockWallet(ctx context.Context, tx Querier, userID int64) (*wallet, error) {
	if _, err := tx.Exec(ctx, `insert into points_wallets (user_id) values ($1) on conflict do nothing`, userID); err != nil {
		return nil, err
	}
	var w wallet
	err := tx.QueryRow(ctx, `select coins, xp, streak_days, streak_best, last_active_day, freezes,
		session_pause_from, session_pause_until, last_milestone
		from points_wallets where user_id = $1 for update`, userID).
		Scan(&w.Coins, &w.XP, &w.Streak, &w.Best, &w.LastDay, &w.Freezes, &w.PauseFrom, &w.PauseUntil, &w.LastMilestone)
	return &w, err
}

type wallet struct {
	Coins, Streak, Best int
	XP                  int64
	LastDay             *time.Time
	Freezes             int
	// «Сессия режимі» (streak.go): missed days inside [PauseFrom, PauseUntil] cost nothing
	PauseFrom, PauseUntil *time.Time
	LastMilestone         int
	Milestone             int // set by saveWallet when this action crossed a milestone
}

// streakAfter applies one active day to the streak (the wallet row must be locked). Missed days
// inside a session pause are free; each other missed day takes one freeze, and the streak
// restarts when there are not enough freezes.
func (w *wallet) streakAfter(day time.Time) (used bool) {
	switch {
	case w.LastDay != nil && !w.LastDay.Before(day):
		return false
	case w.LastDay != nil && missedDays(*w.LastDay, day, w.PauseFrom, w.PauseUntil) <= w.Freezes:
		n := missedDays(*w.LastDay, day, w.PauseFrom, w.PauseUntil)
		w.Freezes -= n
		w.Streak++
		used = n > 0
	default:
		w.Streak = 1
	}
	if w.Streak > w.Best {
		w.Best = w.Streak
	}
	d := day
	w.LastDay = &d
	return used
}

// CurrentStreak is the streak as of today: kept while yesterday (or, with a freeze in hand, the
// day before) was active.
func CurrentStreak(last *time.Time, streak, freezes int, today time.Time) int {
	return CurrentStreakPaused(last, streak, freezes, today, nil, nil)
}

// CurrentStreakPaused is CurrentStreak with a session pause: missed days inside it are free.
// Today itself is not missed yet.
func CurrentStreakPaused(last *time.Time, streak, freezes int, today time.Time, from, until *time.Time) int {
	if last == nil {
		return 0
	}
	if missedDays(*last, today, from, until) <= freezes {
		return streak
	}
	return 0
}

// missedDays counts the days strictly between last and day that a session pause does not cover.
func missedDays(last, day time.Time, from, until *time.Time) int {
	n := 0
	for d := last.AddDate(0, 0, 1); d.Before(day); d = d.AddDate(0, 0, 1) {
		if from != nil && until != nil && !d.Before(*from) && !d.After(*until) {
			continue
		}
		n++
		if n > 3 { // more than any freeze count: no need to walk a long gap
			return n
		}
	}
	return n
}

// Grant awards e inside tx (the caller's transaction): a savepoint keeps a points failure from
// undoing the caller's own work. It returns nil when the feature is off.
func Grant(ctx context.Context, tx pgx.Tx, e Earn) (*Award, error) {
	if !enabled() {
		return nil, nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	a, err := award(ctx, sp, e, Now())
	if err != nil {
		_ = sp.Rollback(ctx)
		return nil, err
	}
	return a, sp.Commit(ctx)
}

// enabled is replaced by the feature switch check in init (kept as a variable for tests).
var enabled = func() bool { return true }

var errUnknownReason = errors.New("points: unknown reason")

func award(ctx context.Context, tx Querier, e Earn, now time.Time) (*Award, error) {
	rule, ok := Rules[e.Reason]
	if !ok || e.UserID == 0 || e.RefType == "" || e.RefID == "" || len(e.RefID) > 80 {
		return nil, errUnknownReason
	}
	xp, coins := rule.XP, rule.Coins
	if e.XP > 0 {
		xp = e.XP
	}
	if e.Coins > 0 {
		coins = e.Coins
	} else if e.Coins < 0 {
		coins = 0
	}
	if rule.Device {
		coins = 0 // device-only actions never mint coins
	}
	w, err := lockWallet(ctx, tx, e.UserID)
	if err != nil {
		return nil, err
	}
	day := Day(now)
	a := &Award{Reason: e.Reason}
	var dup bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from xp_events where user_id = $1 and reason = $2 and ref_type = $3 and ref_id = $4)`,
		e.UserID, e.Reason, e.RefType, e.RefID).Scan(&dup); err != nil {
		return nil, err
	}
	if dup {
		a.Duplicate = true
		return a, nil
	}
	// per-reason caps
	var today, week, dayXP, dayCoins int
	if err := tx.QueryRow(ctx, `select
		count(*) filter (where reason = $2 and day = $3::date),
		count(*) filter (where reason = $2 and day >= $4::date),
		coalesce(sum(delta) filter (where day = $3::date and delta > 0), 0)
		from xp_events where user_id = $1 and day >= least($3::date, $4::date)`,
		e.UserID, e.Reason, day, Day(WeekStart(now))).Scan(&today, &week, &dayXP); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `select coalesce(sum(delta), 0) from coin_ledger where user_id = $1 and day = $2::date
		and reason = any($3)`, e.UserID, day, earningReasons()).Scan(&dayCoins); err != nil {
		return nil, err
	}
	if today >= rule.PerDay || (rule.PerWeek > 0 && week >= rule.PerWeek) {
		a.Capped = true
		// still an active day for the streak
		err := saveWallet(ctx, tx, e.UserID, w, day)
		a.Milestone = w.Milestone
		return a, err
	}
	if left := DailyXPCap - dayXP; xp > left {
		xp, a.Capped = max(left, 0), true
	}
	if left := DailyCoinCap - dayCoins; coins > left {
		coins, a.Capped = max(left, 0), true
	}
	if xp > 0 {
		tag, err := tx.Exec(ctx, `insert into xp_events (user_id, delta, reason, ref_type, ref_id, day, created_at)
			values ($1, $2, $3, $4, $5, $6, $7) on conflict do nothing`, e.UserID, xp, e.Reason, e.RefType, e.RefID, day, now)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			a.Duplicate = true
			return a, nil
		}
		w.XP += int64(xp)
		a.XP = xp
	}
	if coins > 0 {
		tag, err := tx.Exec(ctx, `insert into coin_ledger (user_id, delta, reason, ref_type, ref_id, day, created_at)
			values ($1, $2, $3, $4, $5, $6, $7) on conflict do nothing`, e.UserID, coins, e.Reason, e.RefType, e.RefID, day, now)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			w.Coins += coins
			a.Coins = coins
		}
	}
	err = saveWallet(ctx, tx, e.UserID, w, day)
	a.Milestone = w.Milestone
	return a, err
}

func saveWallet(ctx context.Context, tx Querier, userID int64, w *wallet, day time.Time) error {
	w.streakAfter(day)
	if err := milestone(ctx, tx, userID, w, day); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update points_wallets set coins = $2, xp = $3, streak_days = $4, streak_best = $5,
		last_active_day = $6, freezes = $7, last_milestone = $8, updated_at = now() where user_id = $1`,
		userID, w.Coins, w.XP, w.Streak, w.Best, w.LastDay, w.Freezes, w.LastMilestone)
	return err
}

func earningReasons() []string {
	out := make([]string, 0, len(Rules))
	for r := range Rules {
		out = append(out, r)
	}
	return out
}

// Reverse takes back an earlier award (an organizer revoked a doubtful check-in). XP is removed
// in full; coins only as far as the balance allows (spent coins are not clawed back below 0).
// Idempotent; the original action still never pays again.
func Reverse(ctx context.Context, tx pgx.Tx, userID int64, reason, refType, refID string) error {
	if !enabled() {
		return nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	if err := reverse(ctx, sp, userID, reason, refType, refID, Now()); err != nil {
		_ = sp.Rollback(ctx)
		return err
	}
	return sp.Commit(ctx)
}

func reverse(ctx context.Context, tx Querier, userID int64, reason, refType, refID string, now time.Time) error {
	w, err := lockWallet(ctx, tx, userID)
	if err != nil {
		return err
	}
	var xp, coins int
	if err := tx.QueryRow(ctx, `select coalesce((select delta from xp_events where user_id = $1 and reason = $2 and ref_type = $3 and ref_id = $4), 0),
		coalesce((select delta from coin_ledger where user_id = $1 and reason = $2 and ref_type = $3 and ref_id = $4), 0)`,
		userID, reason, refType, refID).Scan(&xp, &coins); err != nil {
		return err
	}
	day := Day(now)
	back := reason + reasonRevokeSufix
	if xp > 0 {
		take := min(int64(xp), w.XP)
		if take > 0 {
			tag, err := tx.Exec(ctx, `insert into xp_events (user_id, delta, reason, ref_type, ref_id, day, created_at)
				values ($1, $2, $3, $4, $5, $6, $7) on conflict do nothing`, userID, -take, back, refType, refID, day, now)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				w.XP -= take
			}
		}
	}
	if take := min(coins, w.Coins); take > 0 {
		tag, err := tx.Exec(ctx, `insert into coin_ledger (user_id, delta, reason, ref_type, ref_id, day, created_at)
			values ($1, $2, $3, $4, $5, $6, $7) on conflict do nothing`, userID, -take, back, refType, refID, day, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			w.Coins -= take
		}
	}
	_, err = tx.Exec(ctx, `update points_wallets set coins = $2, xp = $3, updated_at = now() where user_id = $1`, userID, w.Coins, w.XP)
	return err
}

// spend moves coins out (redeem, freeze) or in (refund, admin adjust) with a ledger row in the
// same transaction. The wallet must be locked by the caller (lockWallet). A negative balance is
// refused here and, as a last line, by the CHECK (coins >= 0).
func spend(ctx context.Context, tx Querier, userID int64, w *wallet, delta int, reason, refType, refID string, actor *int64, note string, now time.Time) error {
	if delta == 0 {
		return fmt.Errorf("points: zero delta")
	}
	if w.Coins+delta < 0 {
		return errNoCoins
	}
	var n *string
	if note != "" {
		n = &note
	}
	tag, err := tx.Exec(ctx, `insert into coin_ledger (user_id, delta, reason, ref_type, ref_id, actor_id, note, day, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9) on conflict do nothing`, userID, delta, reason, refType, refID, actor, n, Day(now), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errDuplicate
	}
	w.Coins += delta
	_, err = tx.Exec(ctx, `update points_wallets set coins = $2, updated_at = now() where user_id = $1`, userID, w.Coins)
	return err
}

var (
	errNoCoins   = errors.New("points: not enough coins")
	errDuplicate = errors.New("points: duplicate ledger entry")
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

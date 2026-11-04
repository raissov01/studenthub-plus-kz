package points

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

var (
	MsgNoCoins   = httpx.Msg{KK: "Монета жетпейді.", RU: "Недостаточно монет."}
	MsgHaveFrz   = httpx.Msg{KK: "Сізде қорғаныс бар — бір уақытта біреуі ғана сақталады.", RU: "Защита уже есть — одновременно можно хранить только одну."}
	MsgNoReward  = httpx.Msg{KK: "Сыйлық табылмады не таусылды.", RU: "Награда не найдена или закончилась."}
	MsgLimit     = httpx.Msg{KK: "Бұл сыйлықты алу шегіне жеттіңіз.", RU: "Вы достигли лимита на эту награду."}
	MsgGroup     = httpx.Msg{KK: "Топ атауы: 1–24 таңба, әріп, сан, «-», «/», «.».", RU: "Название группы: 1–24 символа, буквы, цифры, «-», «/», «.»."}
	MsgDecided   = httpx.Msg{KK: "Бұл өтінім бойынша шешім қабылданған.", RU: "По этой заявке уже есть решение."}
	MsgNeedCode  = httpx.Msg{KK: "Промокод керек: оны жазыңыз не сыйлыққа кодтар қосыңыз.", RU: "Нужен промокод: впишите его или добавьте коды к награде."}
	MsgReason    = httpx.Msg{KK: "Себебін жазыңыз (5–500 таңба).", RU: "Укажите причину (5–500 символов)."}
	MsgNoAccount = httpx.Msg{KK: "Пайдаланушы табылмады.", RU: "Пользователь не найден."}
	MsgBadClaim  = httpx.Msg{KK: "Бұл әрекетке ұпай берілмейді.", RU: "За это действие баллы не начисляются."}
)

func init() {
	enabled = func() bool { return features.Enabled(Feature) }
	me.RegisterExport("points", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select coins, xp, streak_days, streak_best, last_active_day, freezes, study_group, leaderboard_opt_out
			from points_wallets where user_id = app_user_id()`)
	})
	me.RegisterExport("xpEvents", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select delta, reason, ref_type, ref_id, created_at from xp_events where user_id = app_user_id() order by id")
	})
	me.RegisterExport("coinLedger", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select delta, reason, ref_type, ref_id, note, created_at from coin_ledger where user_id = app_user_id() order by id")
	})
	me.RegisterExport("rewardRedemptions", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select reward_id, cost, status, code, note, created_at, decided_at from reward_redemptions where user_id = app_user_id() order by id")
	})
}

// Routes wires the student and admin endpoints, all behind FEATURE_POINTS.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.Require(Feature, fn)) }
	h("GET /api/points/me", auth.Need(getMe))
	h("POST /api/points/daily", auth.Write(auth.Need(daily)))
	h("POST /api/points/claim", auth.Write(auth.Need(claim)))
	h("POST /api/points/freeze", auth.Write(auth.Need(buyFreeze)))
	h("PATCH /api/points/settings", auth.Write(auth.Need(patchSettings)))
	h("GET /api/points/leaderboard", auth.Need(leaderboard))
	h("GET /api/points/rewards", auth.Need(listRewards))
	h("POST /api/points/redeem", auth.Write(auth.Need(redeem)))
	h("GET /api/points/redemptions", auth.Need(myRedemptions))
	adminRoutes(h)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("points: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

// limited applies an extra per-user budget on top of auth.Write's shared one.
func limited(w http.ResponseWriter, r *http.Request, key string, u *auth.User, env string, def int, window time.Duration) bool {
	ok, retry := ratelimit.Allow(r.Context(), "pt:"+key+":"+itoa(u.ID), config.Int(env, def), window)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
	}
	return !ok
}

func consented(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", me.MsgConsent)
		return false
	}
	return true
}

// ---------- GET /api/points/me ----------

type Task struct {
	ID string `json:"id"`
	Rule
	Done int `json:"done"` // awards of this reason today
	Week int `json:"week"` // awards this week
}

type Summary struct {
	XP          int64  `json:"xp"`
	Level       int    `json:"level"`
	LevelXP     int64  `json:"levelXp"`
	NextLevelXP int64  `json:"nextLevelXp"`
	Coins       int    `json:"coins"`
	Streak      Streak `json:"streak"`
	Today       struct {
		XP       int `json:"xp"`
		Coins    int `json:"coins"`
		XPCap    int `json:"xpCap"`
		CoinsCap int `json:"coinsCap"`
	} `json:"today"`
	Tasks    []Task   `json:"tasks"`
	Settings Settings `json:"settings"`
	Recent   []Entry  `json:"recent"`
}

type Streak struct {
	Days        int  `json:"days"`
	Best        int  `json:"best"`
	Freezes     int  `json:"freezes"`
	MaxFreezes  int  `json:"maxFreezes"`
	FreezeCost  int  `json:"freezeCost"`
	ActiveToday bool `json:"activeToday"`
}

type Settings struct {
	LeaderboardOptOut bool    `json:"leaderboardOptOut"`
	StudyGroup        *string `json:"studyGroup"`
}

type Entry struct {
	Delta     int       `json:"delta"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// load reads the student's summary through RLS (own rows only).
func load(ctx context.Context, userID int64, now time.Time) (*Summary, error) {
	s := &Summary{Tasks: []Task{}, Recent: []Entry{}}
	today := Day(now)
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		var last *time.Time
		err := tx.QueryRow(ctx, `select coins, xp, streak_days, streak_best, last_active_day, freezes, study_group, leaderboard_opt_out
			from points_wallets where user_id = app_user_id()`).
			Scan(&s.Coins, &s.XP, &s.Streak.Days, &s.Streak.Best, &last, &s.Streak.Freezes, &s.Settings.StudyGroup, &s.Settings.LeaderboardOptOut)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		s.Streak.Days = CurrentStreak(last, s.Streak.Days, s.Streak.Freezes, today)
		s.Streak.ActiveToday = last != nil && last.Equal(today)
		counts := map[string][2]int{}
		rows, err := tx.Query(ctx, `select reason, count(*) filter (where day = $1::date), count(*)
			from xp_events where user_id = app_user_id() and day >= $2::date and delta > 0 group by reason`, today, Day(WeekStart(now)))
		if err != nil {
			return err
		}
		for rows.Next() {
			var reason string
			var d, wk int
			if err := rows.Scan(&reason, &d, &wk); err != nil {
				return err
			}
			counts[reason] = [2]int{d, wk}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select coalesce(sum(delta) filter (where delta > 0), 0) from xp_events where user_id = app_user_id() and day = $1::date`, today).Scan(&s.Today.XP); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select coalesce(sum(delta), 0) from coin_ledger where user_id = app_user_id() and day = $1::date and reason = any($2)`,
			today, earningReasons()).Scan(&s.Today.Coins); err != nil {
			return err
		}
		// a capped award (no row) still marks the task as done for today
		for _, id := range TaskOrder {
			c := counts[id]
			s.Tasks = append(s.Tasks, Task{ID: id, Rule: Rules[id], Done: c[0], Week: c[1]})
		}
		rows, err = tx.Query(ctx, `select delta, reason, note, created_at from coin_ledger where user_id = app_user_id() order by id desc limit 20`)
		if err != nil {
			return err
		}
		s.Recent, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
			var e Entry
			return e, row.Scan(&e.Delta, &e.Reason, &e.Note, &e.CreatedAt)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.Level, s.LevelXP, s.NextLevelXP = Level(s.XP)
	s.Streak.MaxFreezes, s.Streak.FreezeCost = MaxFreezes, FreezeCost
	s.Today.XPCap, s.Today.CoinsCap = DailyXPCap, DailyCoinCap
	return s, nil
}

func getMe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	s, err := load(r.Context(), u.ID, Now())
	if err != nil {
		fail(w, r, "me", err)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

// grantNow runs one award in its own transaction (owner role: the ledger is server-written).
func grantNow(ctx context.Context, e Earn) (*Award, error) {
	var a *Award
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		a, err = award(ctx, tx, e, Now())
		return err
	})
	return a, err
}

func awardReply(w http.ResponseWriter, r *http.Request, u *auth.User, a *Award) {
	s, err := load(r.Context(), u.ID, Now())
	if err != nil {
		fail(w, r, "me", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"award": a, "me": s})
}

// ---------- POST /api/points/daily ----------

// daily: the first open of the app on an Almaty day. Idempotent per day.
func daily(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	a, err := grantNow(r.Context(), Earn{UserID: u.ID, Reason: ReasonDailyOpen, RefType: "day", RefID: Day(Now()).Format("2006-01-02")})
	if err != nil {
		fail(w, r, "daily", err)
		return
	}
	awardReply(w, r, u, a)
}

// ---------- POST /api/points/claim ----------

var reRoadmapRef = regexp.MustCompile(`^[a-z0-9_-]{1,40}:[0-9]{1,2}$`)

// claim: device-only actions (a roadmap step). XP only, never coins, each ref pays once, tight
// per-day and per-week caps; the server cannot verify them.
func claim(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) || limited(w, r, "claim", u, "POINTS_CLAIMS_PER_HOUR", 20, time.Hour) {
		return
	}
	var body struct {
		Action string `json:"action"`
		Ref    string `json:"ref"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var e Earn
	switch {
	case body.Action == ReasonRoadmapStep && reRoadmapRef.MatchString(body.Ref):
		e = Earn{Reason: ReasonRoadmapStep, RefType: "roadmap", RefID: body.Ref}
	default:
		httpx.Error(w, r, http.StatusBadRequest, "bad_claim", MsgBadClaim)
		return
	}
	e.UserID = u.ID
	a, err := grantNow(r.Context(), e)
	if err != nil {
		fail(w, r, "claim", err)
		return
	}
	awardReply(w, r, u, a)
}

// ---------- POST /api/points/freeze ----------

var reKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func buyFreeze(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || !reKey.MatchString(body.Key) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		wl, err := lockWallet(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		var done bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from coin_ledger where user_id = $1 and reason = $2 and ref_type = 'freeze' and ref_id = $3)`,
			u.ID, ReasonFreeze, body.Key).Scan(&done); err != nil || done {
			return err // a retried request: already bought
		}
		if wl.Freezes >= MaxFreezes {
			return errHaveFreeze
		}
		if err := spend(ctx, tx, u.ID, wl, -FreezeCost, ReasonFreeze, "freeze", body.Key, nil, "", Now()); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update points_wallets set freezes = freezes + 1 where user_id = $1`, u.ID)
		return err
	})
	switch {
	case errors.Is(err, errHaveFreeze):
		httpx.Error(w, r, http.StatusConflict, "freeze_owned", MsgHaveFrz)
	case errors.Is(err, errNoCoins):
		httpx.Error(w, r, http.StatusConflict, "not_enough_coins", MsgNoCoins)
	case err != nil:
		fail(w, r, "freeze", err)
	default:
		awardReply(w, r, u, nil)
	}
}

var errHaveFreeze = errors.New("points: freeze owned")

// ---------- PATCH /api/points/settings ----------

// NormalizeGroup upper-cases and trims a group name; "" clears it. ok is false when invalid.
func NormalizeGroup(s string) (string, bool) {
	s = strings.ToUpper(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return "", true
	}
	if utf8.RuneCountInString(s) > 24 {
		return "", false
	}
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && !strings.ContainsRune(" -/._", c) {
			return "", false
		}
	}
	return s, true
}

func patchSettings(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	var body struct {
		LeaderboardOptOut *bool   `json:"leaderboardOptOut"`
		StudyGroup        *string `json:"studyGroup"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || (body.LeaderboardOptOut == nil && body.StudyGroup == nil) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var group *string
	if body.StudyGroup != nil {
		g, ok := NormalizeGroup(*body.StudyGroup)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "bad_group", MsgGroup)
			return
		}
		if g != "" {
			group = &g
		}
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := lockWallet(ctx, tx, u.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update points_wallets set
			leaderboard_opt_out = coalesce($2, leaderboard_opt_out),
			study_group = case when $3 then $4 else study_group end,
			updated_at = now() where user_id = $1`, u.ID, body.LeaderboardOptOut, body.StudyGroup != nil, group)
		return err
	})
	if err != nil {
		fail(w, r, "settings", err)
		return
	}
	awardReply(w, r, u, nil)
}

// ---------- GET /api/points/leaderboard?scope=group|faculty ----------

// MinGroup: scopes with fewer visible members are not shown (small groups identify people).
const MinGroup = 5

type Leader struct {
	Rank int    `json:"rank"`
	Name string `json:"name"`
	XP   int    `json:"xp"`
	Me   bool   `json:"me"`
}

// ShortName shows a first name and the initial of the second word: «Аружан С.».
func ShortName(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "Student"
	}
	out := parts[0]
	if utf8.RuneCountInString(out) > 20 {
		out = string([]rune(out)[:20])
	}
	if len(parts) > 1 {
		r, _ := utf8.DecodeRuneInString(parts[1])
		out += " " + string(r) + "."
	}
	return out
}

func leaderboard(w http.ResponseWriter, r *http.Request, u *auth.User) {
	scope := r.URL.Query().Get("scope")
	if scope != "group" && scope != "faculty" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_scope", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	now := Now()
	week := WeekStart(now)
	out := map[string]any{"scope": scope, "weekStart": week.UTC(), "resetsAt": week.AddDate(0, 0, 7).UTC(), "minMembers": MinGroup}
	var optOut bool
	var group *string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select leaderboard_opt_out, study_group from points_wallets where user_id = $1`, u.ID).Scan(&optOut, &group)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// the scope is the student's own university (+ faculty, + group): never someone else's
		where := `u.university = $2 and u.faculty = $3`
		args := []any{week, u.University, u.Faculty}
		missing := u.University == nil || u.Faculty == nil
		if scope == "group" {
			where = `u.university = $2 and pw.study_group = $3`
			args = []any{week, u.University, group}
			missing = u.University == nil || group == nil
		}
		if missing {
			out["status"] = "no_scope"
			return nil
		}
		var members int
		if err := tx.QueryRow(ctx, `select count(*) from points_wallets pw join users u on u.id = pw.user_id
			where u.deleted_at is null and not pw.leaderboard_opt_out and `+where+` and $1::timestamptz is not null`, args...).Scan(&members); err != nil {
			return err
		}
		out["members"] = members
		if members < MinGroup {
			out["status"] = "too_small"
			return nil
		}
		rows, err := tx.Query(ctx, `select u.id, coalesce(u.name, ''), coalesce(sum(x.delta), 0)::int as wxp
			from points_wallets pw join users u on u.id = pw.user_id
			left join xp_events x on x.user_id = pw.user_id and x.created_at >= $1
			where u.deleted_at is null and not pw.leaderboard_opt_out and `+where+`
			group by u.id, u.name order by wxp desc, u.id limit 200`, args...)
		if err != nil {
			return err
		}
		leaders := []Leader{}
		rank := 0
		for rows.Next() {
			var id int64
			var name string
			var xp int
			if err := rows.Scan(&id, &name, &xp); err != nil {
				return err
			}
			rank++
			if rank <= 20 || id == u.ID {
				leaders = append(leaders, Leader{Rank: rank, Name: ShortName(name), XP: xp, Me: id == u.ID})
			}
		}
		out["status"] = "ok"
		out["leaders"] = leaders
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "leaderboard", err)
		return
	}
	out["optedOut"] = optOut
	httpx.JSON(w, http.StatusOK, out)
}

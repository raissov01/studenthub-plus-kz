package points

// Spec 08 «kind streak», behind FEATURE_STREAK (on top of FEATURE_POINTS):
//
//   - one evening reminder (default 20:00 Almaty, 19:00–21:00, never after 22:00) when a streak of
//     3+ days has no activity yet today; the student can turn it off;
//   - milestone coins at 3, 7, 14, 30, 50, 100, 200 and 365 days, paid once, inside the award
//     transaction of the action that reached the milestone;
//   - «Сессия режимі»: a free pause of up to 14 days, once per semester, that keeps the streak;
//   - two freezes in hand instead of one, at the same fixed price.
//
// The reminder leaves this package through SendReminder, which decides nothing about channels:
// internal/server/glue.go wires it to the notify dispatcher (Telegram today, web push when the
// dispatcher gets it). This package only decides who is nudged and when.

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// StreakFeature is the switch name: FEATURE_STREAK.
const StreakFeature = "streak"

const (
	ReasonMilestone  = "streak_milestone" // coins only, once per milestone
	StreakMaxFreezes = 2
	MaxPauseDays     = 14
	ReminderMinDays  = 3
	ReminderDefault  = "20:00"
	reminderEarliest = 19 * 60
	reminderLatest   = 21 * 60
	reminderCutoff   = 22 * 60 // nothing after this, whatever the setting
)

// Milestones and what each pays (coins). Changing it changes future milestones only.
var (
	Milestones     = []int{3, 7, 14, 30, 50, 100, 200, 365}
	MilestoneCoins = map[int]int{3: 5, 7: 10, 14: 15, 30: 25, 50: 30, 100: 50, 200: 75, 365: 100}
)

var streakEnabled = func() bool { return enabled() && features.Enabled(StreakFeature) }

func maxFreezes() int {
	if streakEnabled() {
		return StreakMaxFreezes
	}
	return MaxFreezes
}

// NextMilestone is the first milestone above days, 0 after the last one.
func NextMilestone(days int) int {
	for _, m := range Milestones {
		if m > days {
			return m
		}
	}
	return 0
}

// milestone pays the highest milestone the streak has reached that was never paid. The wallet
// row is locked by the caller; the ledger's unique key makes it idempotent.
func milestone(ctx context.Context, tx Querier, userID int64, w *wallet, day time.Time) error {
	if !streakEnabled() {
		return nil
	}
	reached := 0
	for _, m := range Milestones {
		if m <= w.Streak && m > w.LastMilestone {
			reached = m
		}
	}
	if reached == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `insert into coin_ledger (user_id, delta, reason, ref_type, ref_id, day, created_at)
		values ($1, $2, $3, 'milestone', $4, $5, $6) on conflict do nothing`,
		userID, MilestoneCoins[reached], ReasonMilestone, itoa(int64(reached)), day, Now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		w.Coins += MilestoneCoins[reached]
		w.Milestone = reached
	}
	w.LastMilestone = reached
	return nil
}

// Semester names the academic half-year of an Almaty day: Aug–Jan is "<year>-fall", Feb–Jul
// "<year>-spring". The free pause is once per semester.
func Semester(day time.Time) string {
	switch m := day.Month(); {
	case m >= time.August:
		return fmt.Sprintf("%d-fall", day.Year())
	case m == time.January:
		return fmt.Sprintf("%d-fall", day.Year()-1)
	default:
		return fmt.Sprintf("%d-spring", day.Year())
	}
}

// Pause is the «Сессия режимі» state in GET /api/points/me.
type Pause struct {
	Active   bool    `json:"active"`
	From     *string `json:"from"`
	Until    *string `json:"until"`
	DaysLeft int     `json:"daysLeft"`
	Used     bool    `json:"usedThisSemester"`
	MaxDays  int     `json:"maxDays"`
}

// Reminder settings in GET /api/points/me.
type ReminderSettings struct {
	On   bool   `json:"on"`
	Time string `json:"time"`
}

const ymd = "2006-01-02"

func ymdPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(ymd)
	return &s
}

// loadStreak fills the pause-aware streak and, with the switch on, the spec 08 fields.
func loadStreak(ctx context.Context, tx pgx.Tx, s *Summary, last *time.Time, rawDays int, today time.Time) error {
	var from, until *time.Time
	var used *string
	var on bool
	var at string
	var lastM int
	err := tx.QueryRow(ctx, `select session_pause_from, session_pause_until, session_pause_used_semester,
		streak_reminder, to_char(streak_reminder_time, 'HH24:MI'), last_milestone
		from points_wallets where user_id = app_user_id()`).Scan(&from, &until, &used, &on, &at, &lastM)
	if errors.Is(err, pgx.ErrNoRows) {
		on, at = true, ReminderDefault
	} else if err != nil {
		return err
	}
	s.Streak.Days = CurrentStreakPaused(last, rawDays, s.Streak.Freezes, today, from, until)
	if !streakEnabled() {
		return nil
	}
	p := &Pause{From: ymdPtr(from), Until: ymdPtr(until), Used: used != nil && *used == Semester(today), MaxDays: MaxPauseDays}
	if from != nil && until != nil && !today.After(*until) {
		p.Active = true
		p.DaysLeft = int(until.Sub(today).Hours()/24) + 1
	}
	s.Streak.NextMilestone = NextMilestone(s.Streak.Days)
	s.Streak.LastMilestone = lastM
	s.Streak.Milestones = Milestones
	s.Streak.Pause = p
	s.Streak.Reminder = &ReminderSettings{On: on, Time: at}
	return nil
}

var (
	MsgPauseUsed = httpx.Msg{KK: "Сессия режимі осы семестрде қолданылды. Келесі семестрде қайта қосуға болады.", RU: "Режим сессии в этом семестре уже использован. Его можно включить в следующем семестре."}
	MsgPauseDays = httpx.Msg{KK: "Үзіліс 1–14 күн болады.", RU: "Пауза — от 1 до 14 дней."}
	MsgRemindAt  = httpx.Msg{KK: "Еске салу уақыты 19:00–21:00 аралығында болады.", RU: "Время напоминания — с 19:00 до 21:00."}
	MsgHaveFrz2  = httpx.Msg{KK: "Қорғаныс жеткілікті — бір уақытта екеуі ғана сақталады.", RU: "Защиты достаточно — одновременно можно хранить только две."}
)

// pause: POST /api/points/pause {days: 1..14} starts the semester's free pause; {days: 0} ends
// a running pause early (it still counts as used).
func pause(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	var body struct {
		Days *int `json:"days"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Days == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	days := *body.Days
	if days < 0 || days > MaxPauseDays {
		httpx.Error(w, r, http.StatusBadRequest, "bad_days", MsgPauseDays)
		return
	}
	ctx := r.Context()
	today := Day(Now())
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		wl, err := lockWallet(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		var used *string
		if err := tx.QueryRow(ctx, `select session_pause_used_semester from points_wallets where user_id = $1`, u.ID).Scan(&used); err != nil {
			return err
		}
		if days == 0 {
			if wl.PauseUntil == nil || today.After(*wl.PauseUntil) {
				return nil // nothing running
			}
			_, err := tx.Exec(ctx, `update points_wallets set session_pause_until = greatest(session_pause_from, $2::date) - 1,
				updated_at = now() where user_id = $1`, u.ID, today)
			return err
		}
		if used != nil && *used == Semester(today) {
			return errPauseUsed
		}
		from := today
		if wl.LastDay != nil && wl.LastDay.Equal(today) {
			from = today.AddDate(0, 0, 1) // today already counts
		}
		_, err = tx.Exec(ctx, `update points_wallets set session_pause_from = $2, session_pause_until = $3,
			session_pause_used_semester = $4, updated_at = now() where user_id = $1`,
			u.ID, from, from.AddDate(0, 0, days-1), Semester(today))
		return err
	})
	switch {
	case errors.Is(err, errPauseUsed):
		httpx.Error(w, r, http.StatusConflict, "pause_used", MsgPauseUsed)
	case err != nil:
		fail(w, r, "pause", err)
	default:
		awardReply(w, r, u, nil)
	}
}

var errPauseUsed = errors.New("points: pause used this semester")

// streakSettings: PATCH /api/points/streak {reminder?: bool, time?: "HH:MM" 19:00–21:00}.
func streakSettings(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	var body struct {
		Reminder *bool   `json:"reminder"`
		Time     *string `json:"time"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || (body.Reminder == nil && body.Time == nil) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if body.Time != nil {
		m, ok := parseClock(*body.Time)
		if !ok || m < reminderEarliest || m > reminderLatest {
			httpx.Error(w, r, http.StatusBadRequest, "bad_time", MsgRemindAt)
			return
		}
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := lockWallet(ctx, tx, u.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update points_wallets set streak_reminder = coalesce($2, streak_reminder),
			streak_reminder_time = coalesce($3::time, streak_reminder_time), updated_at = now() where user_id = $1`,
			u.ID, body.Reminder, body.Time)
		return err
	})
	if err != nil {
		fail(w, r, "streak settings", err)
		return
	}
	awardReply(w, r, u, nil)
}

func parseClock(s string) (int, bool) {
	var h, m int
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// ---------- the evening reminder ----------

// Reminder is one evening nudge, already worded in the student's language.
type Reminder struct {
	UserID int64
	Lang   string
	Streak int
	Day    string // Almaty day, YYYY-MM-DD
	Key    string // streak:<user>:<day>: one per student and day across retries
	Text   string
	Link   string
}

// SendReminder hands a nudge to whatever delivers messages. It reports false when the student
// cannot be reached right now (quiet hours, the shared daily budget is spent, no channel), so
// the next run may try again before the cut-off. Wired in internal/server/glue.go.
var SendReminder = func(ctx context.Context, r Reminder, now time.Time) (bool, error) { return false, nil }

// ReminderStats is what one cron run did.
type ReminderStats struct {
	Candidates int `json:"candidates"`
	Sent       int `json:"sent"`
	Skipped    int `json:"skipped"`
}

func almatyMinutes(t time.Time) int {
	a := t.In(almaty)
	return a.Hour()*60 + a.Minute()
}

// RunReminders nudges every student whose time has come today. Safe to run every 10 minutes:
// a student is nudged at most once per Almaty day (streak_reminded_on + the delivery key).
func RunReminders(ctx context.Context, now time.Time) (ReminderStats, error) {
	var st ReminderStats
	if !streakEnabled() || almatyMinutes(now) >= reminderCutoff {
		return st, nil
	}
	today := Day(now)
	clock := fmt.Sprintf("%02d:%02d", almatyMinutes(now)/60, almatyMinutes(now)%60)
	type cand struct {
		id                int64
		lang              string
		days, freezes     int
		last              time.Time
		pauseFrom, pauseU *time.Time
	}
	var list []cand
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select pw.user_id, coalesce(u.lang, 'kk'), pw.streak_days, pw.freezes, pw.last_active_day,
				pw.session_pause_from, pw.session_pause_until
			from points_wallets pw join users u on u.id = pw.user_id and u.deleted_at is null
			where pw.streak_reminder and pw.streak_days >= $1 and pw.last_active_day < $2::date
				and pw.last_active_day >= $2::date - 31
				and (pw.streak_reminded_on is null or pw.streak_reminded_on < $2::date)
				and pw.streak_reminder_time <= $3::time
			order by pw.user_id limit 5000`, ReminderMinDays, today, clock)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (cand, error) {
			var c cand
			return c, row.Scan(&c.id, &c.lang, &c.days, &c.freezes, &c.last, &c.pauseFrom, &c.pauseU)
		})
		return err
	})
	if err != nil {
		return st, err
	}
	for _, c := range list {
		paused := c.pauseFrom != nil && c.pauseU != nil && !today.Before(*c.pauseFrom) && !today.After(*c.pauseU)
		days := CurrentStreakPaused(&c.last, c.days, c.freezes, today, c.pauseFrom, c.pauseU)
		if paused || days < ReminderMinDays {
			continue
		}
		st.Candidates++
		r := Reminder{UserID: c.id, Lang: c.lang, Streak: days, Day: today.Format(ymd), Link: "/points"}
		r.Key = fmt.Sprintf("streak:%d:%s", c.id, r.Day)
		r.Text = ReminderText(c.lang, days)
		ok, err := SendReminder(ctx, r, now)
		if err != nil {
			log.Printf("points: reminder %d: %v", c.id, err)
			continue
		}
		if !ok {
			st.Skipped++
			continue
		}
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update points_wallets set streak_reminded_on = $2 where user_id = $1`, c.id, today)
			return err
		}); err != nil {
			return st, err
		}
		st.Sent++
	}
	return st, nil
}

// ReminderText is plain, short and guilt-free: what is at stake and the smallest way to keep it.
func ReminderText(lang string, days int) string {
	if lang == "ru" {
		return fmt.Sprintf("Сегодня ещё не было активности, а серия уже %d дн. Одного квиза хватит, чтобы её сохранить.", days)
	}
	return fmt.Sprintf("Бүгін әлі белсенділік болмады, ал серияң — %d күн. Сақтап қалуға бір квиз жеткілікті.", days)
}

func streakCron(w http.ResponseWriter, r *http.Request) {
	secret := config.Str("CRON_SECRET", "")
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if secret == "" || !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) != 1 {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	st, err := RunReminders(r.Context(), Now())
	if err != nil {
		fail(w, r, "streak cron", err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

// streakRoutes is called from Routes; every route needs FEATURE_POINTS and FEATURE_STREAK.
func streakRoutes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, features.Require(Feature, features.Require(StreakFeature, fn)))
	}
	h("POST /api/points/pause", auth.Write(auth.Need(pause)))
	h("PATCH /api/points/streak", auth.Write(auth.Need(streakSettings)))
	h("GET /api/cron/streak", streakCron)
}

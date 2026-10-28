package notify

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Category says what a message is about; students switch categories on and off.
type Category string

const (
	CatDeadlines  Category = "deadlines"  // bot deadline reminders
	CatDigest     Category = "digest"     // weekly digest and generic one-per-day digests
	CatEvents     Category = "events"     // event registrations, waitlist, after-event asks
	CatMentors    Category = "mentors"    // matches, check-ins, pair lifecycle
	CatMarket     Category = "market"     // listing expiry, seasonal prompts
	CatJobs       Category = "jobs"       // b2b job offers
	CatModeration Category = "moderation" // decisions about the student's own submissions, reports: always sent, immediately
)

// Categories lists the switchable categories in display order; moderation is not one of them.
var Categories = []Category{CatDeadlines, CatDigest, CatEvents, CatMentors, CatMarket, CatJobs}

// Transactional messages ignore preferences, quiet hours and the daily cap.
func (c Category) Transactional() bool { return c == CatModeration }

func (c Category) valid() bool {
	if c == CatModeration {
		return true
	}
	for _, k := range Categories {
		if k == c {
			return true
		}
	}
	return false
}

// Prefs are one student's notification settings (defaults when no row exists).
type Prefs struct {
	On           map[Category]bool
	QuietEnabled bool
	QuietStart   int // minutes after Almaty midnight
	QuietEnd     int
	DailyCap     int
}

const (
	DefaultQuietStart = 22 * 60
	DefaultQuietEnd   = 9 * 60
	DefaultDailyCap   = 3
	MaxDailyCap       = 20
)

// Defaults: everything on, quiet 22:00–09:00 Asia/Almaty, at most 3 messages a day.
func Defaults() Prefs {
	on := map[Category]bool{}
	for _, c := range Categories {
		on[c] = true
	}
	return Prefs{On: on, QuietEnabled: true, QuietStart: DefaultQuietStart, QuietEnd: DefaultQuietEnd, DailyCap: DefaultDailyCap}
}

// Allows reports whether the category is switched on; transactional ones always are.
func (p Prefs) Allows(c Category) bool {
	if c.Transactional() {
		return true
	}
	on, ok := p.On[c]
	return !ok || on
}

func almatyMinutes(t time.Time) int {
	l := t.In(almaty)
	return l.Hour()*60 + l.Minute()
}

// Quiet reports whether t falls into the student's quiet hours.
func (p Prefs) Quiet(t time.Time) bool {
	if !p.QuietEnabled || p.QuietStart == p.QuietEnd {
		return false
	}
	m := almatyMinutes(t)
	if p.QuietStart < p.QuietEnd {
		return m >= p.QuietStart && m < p.QuietEnd
	}
	return m >= p.QuietStart || m < p.QuietEnd // wraps midnight
}

// QuietEnds is the first moment after t when the quiet hours are over (t when not quiet).
func (p Prefs) QuietEnds(t time.Time) time.Time {
	if !p.Quiet(t) {
		return t
	}
	l := t.In(almaty)
	end := time.Date(l.Year(), l.Month(), l.Day(), p.QuietEnd/60, p.QuietEnd%60, 0, 0, almaty)
	if !end.After(t) {
		end = end.AddDate(0, 0, 1)
	}
	return end
}

// dayStart is Almaty midnight of t's day.
func dayStart(t time.Time) time.Time {
	l := t.In(almaty)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, almaty)
}

// Clock formats minutes after midnight as "HH:MM".
func Clock(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

// ParseClock reads "HH:MM" (00:00–23:59).
func ParseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// Querier is what pgxpool.Pool and pgx.Tx have in common; the bot passes its own pool.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const prefsColumns = `deadlines, digest, events, mentors, market, jobs, quiet_enabled,
	to_char(quiet_start, 'HH24:MI'), to_char(quiet_end, 'HH24:MI'), daily_cap`

func scanPrefs(row pgx.Row) (Prefs, error) {
	p := Defaults()
	var on [6]bool
	var qs, qe string
	var cap int16
	err := row.Scan(&on[0], &on[1], &on[2], &on[3], &on[4], &on[5], &p.QuietEnabled, &qs, &qe, &cap)
	if errors.Is(err, pgx.ErrNoRows) {
		return Defaults(), nil
	}
	if err != nil {
		return p, err
	}
	for i, c := range Categories {
		p.On[c] = on[i]
	}
	p.QuietStart, _ = ParseClock(qs)
	p.QuietEnd, _ = ParseClock(qe)
	p.DailyCap = int(cap)
	return p, nil
}

// LoadPrefs reads a student's preferences (defaults when they never changed them). With a
// WithUser transaction RLS limits it to the student's own row.
func LoadPrefs(ctx context.Context, q Querier, userID int64) (Prefs, error) {
	return scanPrefs(q.QueryRow(ctx, `select `+prefsColumns+` from notify_prefs where user_id = $1`, userID))
}

// SavePrefs writes the full preference row.
func SavePrefs(ctx context.Context, q Querier, userID int64, p Prefs) error {
	_, err := q.Exec(ctx, `insert into notify_prefs (user_id, deadlines, digest, events, mentors, market, jobs,
			quiet_enabled, quiet_start, quiet_end, daily_cap, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9::time, $10::time, $11, now())
		on conflict (user_id) do update set deadlines = excluded.deadlines, digest = excluded.digest,
			events = excluded.events, mentors = excluded.mentors, market = excluded.market, jobs = excluded.jobs,
			quiet_enabled = excluded.quiet_enabled, quiet_start = excluded.quiet_start, quiet_end = excluded.quiet_end,
			daily_cap = excluded.daily_cap, updated_at = now()`,
		userID, p.Allows(CatDeadlines), p.Allows(CatDigest), p.Allows(CatEvents), p.Allows(CatMentors),
		p.Allows(CatMarket), p.Allows(CatJobs), p.QuietEnabled, Clock(p.QuietStart), Clock(p.QuietEnd), p.DailyCap)
	return err
}

// sentToday counts the student's non-transactional messages on t's Almaty day.
func sentToday(ctx context.Context, q Querier, userID int64, t time.Time) (int, error) {
	start := dayStart(t)
	var n int
	err := q.QueryRow(ctx, `select count(*) from notify_sends where user_id = $1 and sent_at >= $2 and sent_at < $3`,
		userID, start, start.AddDate(0, 0, 1)).Scan(&n)
	return n, err
}

// RecordSend logs one sent non-transactional message so the daily cap counts it. The bot
// calls it for deadline reminders and digests it sends itself.
func RecordSend(ctx context.Context, q Querier, userID int64, c Category, at time.Time) error {
	_, err := q.Exec(ctx, `insert into notify_sends (user_id, category, sent_at) values ($1, $2, $3)`, userID, string(c), at)
	return err
}

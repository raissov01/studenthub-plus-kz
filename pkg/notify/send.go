package notify

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

// Now is the clock Send uses when Opts.Now is zero; tests replace it.
var Now = time.Now

// Opts tune one Send.
type Opts struct {
	Now time.Time // zero = Now()
}

// Result of Send.
type Result string

const (
	Sent   Result = "sent"
	Queued Result = "queued" // quiet hours or daily cap: in the outbox, GET /api/cron/notify sends it later
	Off    Result = "off"    // the student switched the category off: dropped
)

const (
	outboxTTL    = 72 * time.Hour   // a deferred message older than this is no longer worth sending
	maxAttempts  = 5                // Telegram failures before a queued message is dropped
	staleClaim   = 15 * time.Minute // a "sending" row older than this was left by a crashed run
	keepDone     = 7 * 24 * time.Hour
	keepSends    = 3 * 24 * time.Hour
	flushBatch   = 500
	retryBackoff = time.Hour
)

// Send delivers text (HTML parse mode — escape user input) to the student under their
// preferences:
//   - transactional categories (moderation) go out immediately, always;
//   - a switched-off category is dropped (Off);
//   - during quiet hours, or when the daily cap is used up, the message waits in the outbox
//     (Queued) until the hourly GET /api/cron/notify can send it;
//   - otherwise it is sent now and counted against the daily cap.
func Send(ctx context.Context, userID int64, c Category, text string, opts Opts) (Result, error) {
	if !c.valid() {
		return "", fmt.Errorf("notify: unknown category %q", c)
	}
	if c.Transactional() {
		return Sent, Direct(ctx, userID, text)
	}
	now := opts.Now
	if now.IsZero() {
		now = Now()
	}
	p, err := db.Pool(ctx)
	if err != nil {
		return "", err
	}
	prefs, err := LoadPrefs(ctx, p, userID)
	if err != nil {
		return "", err
	}
	if !prefs.Allows(c) {
		return Off, nil
	}
	if prefs.Quiet(now) {
		return Queued, enqueue(ctx, p, userID, c, text, now, prefs.QuietEnds(now))
	}
	n, err := sentToday(ctx, p, userID, now)
	if err != nil {
		return "", err
	}
	if n >= prefs.DailyCap {
		return Queued, enqueue(ctx, p, userID, c, text, now, nextDay(now, prefs))
	}
	if err := Direct(ctx, userID, text); err != nil {
		return "", err
	}
	return Sent, RecordSend(ctx, p, userID, c, now)
}

// For adapts Send to the func(ctx, userID, text) error senders feature packages keep in a
// variable (events.Notify, moderation.Notify, ...), so tests can still replace them.
func For(c Category) func(ctx context.Context, userID int64, text string) error {
	return func(ctx context.Context, userID int64, text string) error {
		_, err := Send(ctx, userID, c, text, Opts{})
		return err
	}
}

// nextDay is when a capped message may try again: the next Almaty day, after quiet hours.
func nextDay(now time.Time, p Prefs) time.Time {
	return p.QuietEnds(dayStart(now).AddDate(0, 0, 1))
}

func enqueue(ctx context.Context, q Querier, userID int64, c Category, text string, now, at time.Time) error {
	_, err := q.Exec(ctx, `insert into notify_outbox (user_id, category, text, not_before, created_at) values ($1, $2, $3, $4, $5)`,
		userID, string(c), text, at, now)
	return err
}

// FlushResult is the JSON summary of one outbox run.
type FlushResult struct {
	Sent     int `json:"sent"`
	Deferred int `json:"deferred"`
	Dropped  int `json:"dropped"`
	Failed   int `json:"failed"`
}

type queued struct {
	id       int64
	userID   int64
	category Category
	text     string
	attempts int
}

// Flush sends the outbox messages that are due at now. Rows are claimed (pending -> sending)
// with skip locked before anything is sent, so overlapping or repeated runs never send a
// message twice. Each message is checked again: a category switched off meanwhile drops it,
// quiet hours or a used-up daily cap move it further.
func Flush(ctx context.Context, now time.Time) (*FlushResult, error) {
	p, err := db.Pool(ctx)
	if err != nil {
		return nil, err
	}
	res := &FlushResult{}
	if _, err := p.Exec(ctx, `update notify_outbox set state = 'pending', claimed_at = null
		where state = 'sending' and claimed_at < $1`, now.Add(-staleClaim)); err != nil {
		return nil, err
	}
	tag, err := p.Exec(ctx, `update notify_outbox set state = 'dropped', done_at = $1
		where state = 'pending' and created_at < $2`, now, now.Add(-outboxTTL))
	if err != nil {
		return nil, err
	}
	res.Dropped += int(tag.RowsAffected())

	rows, err := p.Query(ctx, `update notify_outbox set state = 'sending', claimed_at = $1
		where id in (select id from notify_outbox where state = 'pending' and not_before <= $1
			order by user_id, id limit $2 for update skip locked)
		returning id, user_id, category, text, attempts`, now, flushBatch)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (queued, error) {
		var m queued
		var c string
		err := r.Scan(&m.id, &m.userID, &c, &m.text, &m.attempts)
		m.category = Category(c)
		return m, err
	})
	if err != nil {
		return nil, err
	}
	// returning gives no order guarantee: keep each student's messages in queue order
	sortQueued(list)

	prefs := map[int64]Prefs{}
	set := func(id int64, sql string, args ...any) error {
		_, err := p.Exec(ctx, "update notify_outbox set "+sql+" where id = $1", append([]any{id}, args...)...)
		return err
	}
	for _, m := range list {
		pr, ok := prefs[m.userID]
		if !ok {
			if pr, err = LoadPrefs(ctx, p, m.userID); err != nil {
				return nil, err
			}
			prefs[m.userID] = pr
		}
		switch {
		case !pr.Allows(m.category):
			res.Dropped++
			err = set(m.id, "state = 'dropped', done_at = $2", now)
		case pr.Quiet(now):
			res.Deferred++
			err = set(m.id, "state = 'pending', claimed_at = null, not_before = $2", pr.QuietEnds(now))
		default:
			var n int
			if n, err = sentToday(ctx, p, m.userID, now); err != nil {
				return nil, err
			}
			if n >= pr.DailyCap {
				res.Deferred++
				err = set(m.id, "state = 'pending', claimed_at = null, not_before = $2", nextDay(now, pr))
				break
			}
			if sendErr := Direct(ctx, m.userID, m.text); sendErr != nil {
				log.Printf("notify: outbox %d to %d: %v", m.id, m.userID, sendErr)
				res.Failed++
				if m.attempts+1 >= maxAttempts {
					err = set(m.id, "state = 'dropped', attempts = attempts + 1, done_at = $2", now)
				} else {
					err = set(m.id, "state = 'pending', claimed_at = null, attempts = attempts + 1, not_before = $2", now.Add(retryBackoff))
				}
				break
			}
			res.Sent++
			if err = set(m.id, "state = 'sent', done_at = $2", now); err == nil {
				err = RecordSend(ctx, p, m.userID, m.category, now)
			}
		}
		if err != nil {
			return nil, err
		}
	}

	if _, err := p.Exec(ctx, `delete from notify_outbox where state in ('sent', 'dropped') and done_at < $1`, now.Add(-keepDone)); err != nil {
		return nil, err
	}
	if _, err := p.Exec(ctx, `delete from notify_sends where sent_at < $1`, now.Add(-keepSends)); err != nil {
		return nil, err
	}
	return res, nil
}

func sortQueued(list []queued) {
	slices.SortFunc(list, func(a, b queued) int {
		if c := cmp.Compare(a.userID, b.userID); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})
}

package notify

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5"
)

var Now = time.Now

type Opts struct {
	Now  time.Time
	Link string
	// Key identifies the source event, not the delivery attempt. Reuse it on retries.
	// Without a key, identical kind/category/text/link is deduplicated for 90 days.
	Key         string
	ReplyMarkup json.RawMessage
}

type keyContext struct{}

func WithKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, keyContext{}, key)
}

type Result string

const (
	Sent      Result = "sent"
	Queued    Result = "queued"
	Off       Result = "off"
	Uncertain Result = "uncertain"
)
const (
	maxAttempts  = 5
	staleClaim   = 15 * time.Minute
	keepDone     = 90 * 24 * time.Hour
	keepSends    = 7 * 24 * time.Hour
	flushBatch   = 500
	retryBackoff = time.Hour
)

// lockUser serializes acceptance and budget reservations across processes and modules.
// It is transaction-scoped, including when a transaction-pooling proxy is used.
func lockUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('notify:' || $1::text, 0))`, userID)
	return err
}

func optsTime(opts Opts) time.Time {
	if !opts.Now.IsZero() {
		return opts.Now
	}
	return Now()
}

func (d *Dispatcher) accept(ctx context.Context, userID int64, c Category, text, kind string, opts Opts) (int64, string, error) {
	if !c.valid() {
		return 0, "", fmt.Errorf("notify: unknown category %q", c)
	}
	if PlainText(text) == "" {
		return 0, "", errors.New("notify: empty message")
	}
	if opts.Link != "" {
		ctx = WithLink(ctx, opts.Link)
	}
	now := optsTime(opts)
	key := opts.Key
	if key == "" {
		key, _ = ctx.Value(keyContext{}).(string)
	}
	if key == "" {
		key = fmt.Sprintf("text:%x", sha256.Sum256([]byte(text+"\x00"+linkFor(ctx, text))))
	}
	key = kind + ":" + string(c) + ":" + key
	if len(key) > 512 {
		return 0, "", errors.New("notify: key too long")
	}
	if len(opts.ReplyMarkup) > 0 && !json.Valid(opts.ReplyMarkup) {
		return 0, "", errors.New("notify: invalid reply markup")
	}
	var id int64
	state := "pending"
	err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `select id,state from notify_outbox where user_id=$1 and delivery_key=$2`, userID, key).Scan(&id, &state)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var legacy queued
		legacy.category, legacy.text = c, text
		err = tx.QueryRow(ctx, `select id,state,notification_id,created_at from notify_outbox
            where user_id=$1 and category=$2 and kind=$3 and text=$4 and delivery_key is null
            and created_at >= $5 order by id limit 1 for update`, userID, string(c), kind, text, now.Add(-keepDone)).Scan(&legacy.id, &state, &legacy.notificationID, &legacy.createdAt)
		if err == nil {
			id = legacy.id
			if legacy.notificationID == nil {
				if err := ensureInbox(ctx, tx, userID, legacy); err != nil {
					return err
				}
			}
			_, err = tx.Exec(ctx, `update notify_outbox set delivery_key=$2 where id=$1`, id, key)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from users where id=$1 and deleted_at is null)`, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			state = "dropped"
			return nil
		}
		inboxID, err := record(ctx, tx, userID, c, text, now)
		if err != nil {
			return err
		}
		prefs, err := LoadPrefs(ctx, tx, userID)
		if err != nil {
			return err
		}
		canDM, err := HasTelegram(ctx, tx, userID)
		if err != nil {
			return err
		}
		if !prefs.Allows(c) || !canDM {
			state = "dropped"
		}
		at := now
		if state == "pending" {
			at = prefs.QuietEnds(now)
		}
		return tx.QueryRow(ctx, `insert into notify_outbox(user_id,category,text,not_before,created_at,kind,delivery_key,notification_id,reply_markup,state,done_at)
   values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,case when $10='dropped' then $5::timestamptz end) returning id`,
			userID, string(c), text, at, now, kind, key, inboxID, opts.ReplyMarkup, state).Scan(&id)
	})
	return id, state, err
}

// Send persists before dispatch. No category, including moderation, bypasses this path.
func Send(ctx context.Context, userID int64, c Category, text string, opts Opts) (Result, error) {
	d, err := dispatcher(ctx)
	if err != nil {
		return "", err
	}
	return d.Send(ctx, userID, c, text, opts)
}
func (d *Dispatcher) Send(ctx context.Context, userID int64, c Category, text string, opts Opts) (Result, error) {
	id, state, err := d.accept(ctx, userID, c, text, "direct", opts)
	if err != nil {
		return "", err
	}
	if state == "pending" {
		if _, _, err := d.dispatch(ctx, userID, "direct", id, optsTime(opts)); err != nil {
			return "", err
		}
		if err := d.Pool.QueryRow(ctx, `select state from notify_outbox where id=$1`, id).Scan(&state); err != nil {
			return "", err
		}
	}
	switch state {
	case "sent":
		return Sent, nil
	case "dropped":
		return Off, nil
	case "uncertain":
		return Uncertain, nil
	default:
		return Queued, nil
	}
}
func For(c Category) func(context.Context, int64, string) error {
	return func(ctx context.Context, userID int64, text string) error {
		_, err := Send(ctx, userID, c, text, Opts{})
		return err
	}
}
func nextDay(now time.Time, p Prefs) time.Time { return p.QuietEnds(dayStart(now).AddDate(0, 0, 1)) }

type FlushResult struct {
	Sent      int `json:"sent"`
	Deferred  int `json:"deferred"`
	Dropped   int `json:"dropped"`
	Failed    int `json:"failed"`
	Uncertain int `json:"uncertain"`
}

func (r *FlushResult) add(other FlushResult) {
	r.Sent += other.Sent
	r.Deferred += other.Deferred
	r.Dropped += other.Dropped
	r.Failed += other.Failed
	r.Uncertain += other.Uncertain
}

type queued struct {
	id             int64
	category       Category
	text           string
	markup         json.RawMessage
	attempts       int
	notificationID *int64
	createdAt      time.Time
}

// ensureInbox repairs pre-dispatcher rows (and a receipt whose old read inbox row was
// pruned). It uses the application's exact HTML-to-text conversion, not an SQL approximation.
func ensureInbox(ctx context.Context, tx pgx.Tx, userID int64, q queued) error {
	var nid int64
	err := tx.QueryRow(ctx, `select id from notifications where user_id=$1 and category=$2 and text=$3 order by id desc limit 1`, userID, string(q.category), PlainText(q.text)).Scan(&nid)
	if errors.Is(err, pgx.ErrNoRows) {
		nid, err = record(ctx, tx, userID, q.category, q.text, q.createdAt)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `update notify_outbox set notification_id=$2 where id=$1`, q.id, nid)
	return err
}

// telegramText truncates plain text at a UTF-16 boundary (Telegram's length unit).
func telegramText(raw string, limit int) string {
	var b strings.Builder
	n := 0
	for _, r := range PlainText(raw) {
		size := utf16.RuneLen(r)
		if n+size > limit {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
		n += size
	}
	return html.EscapeString(b.String())
}
func textSize(s string) int { return len(utf16.Encode([]rune(PlainText(s)))) }

// dispatch reserves one direct message or a combined digest. HTTP runs only after the
// inbox, claim and budget reservation commit. Concurrent callers see the reservation.
func (d *Dispatcher) dispatch(ctx context.Context, userID int64, kind string, onlyID int64, now time.Time) (FlushResult, bool, error) {
	var res FlushResult
	var m delivery
	err := pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		prefs, err := LoadPrefs(ctx, tx, userID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select id,category,text,reply_markup,attempts,notification_id,created_at from notify_outbox
   where user_id=$1 and kind=$2 and state='pending' and not_before<=$3 and ($4::bigint=0 or id=$4)
   order by case when $2='digest' then case category when 'deadlines' then 0 when 'events' then 1 else 2 end else 0 end,id limit $5 for update`, userID, kind, now, onlyID, flushBatch)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (queued, error) {
			var q queued
			err := r.Scan(&q.id, &q.category, &q.text, &q.markup, &q.attempts, &q.notificationID, &q.createdAt)
			return q, err
		})
		if err != nil || len(list) == 0 {
			return err
		}
		canDM, err := HasTelegram(ctx, tx, userID)
		if err != nil {
			return err
		}
		var eligible []queued
		for _, q := range list {
			if q.notificationID == nil {
				if err := ensureInbox(ctx, tx, userID, q); err != nil {
					return err
				}
			}
			if !prefs.Allows(q.category) || !canDM {
				if _, err := tx.Exec(ctx, `update notify_outbox set state='dropped',done_at=$2 where id=$1`, q.id, now); err != nil {
					return err
				}
				res.Dropped++
				continue
			}
			eligible = append(eligible, q)
		}
		if len(eligible) == 0 {
			return nil
		}
		ids := make([]int64, len(eligible))
		for i, q := range eligible {
			ids[i] = q.id
		}
		deferUntil := prefs.QuietEnds(now)
		var pause *time.Time
		if err := tx.QueryRow(ctx, `select telegram_id,paused_until from users where id=$1`, userID).Scan(&m.chatID, &pause); err != nil {
			return err
		}
		if kind == "digest" && pause != nil && pause.After(deferUntil) {
			deferUntil = prefs.QuietEnds(*pause)
		}
		if d.Token == "" {
			deferUntil = now.Add(retryBackoff)
		}
		if !deferUntil.After(now) {
			if kind == "digest" {
				var claimed bool
				if err := tx.QueryRow(ctx, `select exists(select 1 from bot_daily_sends where user_id=$1 and day=$2::date)`, userID, Day(now)).Scan(&claimed); err != nil {
					return err
				}
				if claimed {
					deferUntil = nextDay(now, prefs)
				}
			} else {
				n, err := sentToday(ctx, tx, userID, now)
				if err != nil {
					return err
				}
				if n >= min(prefs.DailyCap, MaxDailyCap) {
					deferUntil = nextDay(now, prefs)
				}
			}
		}
		if deferUntil.After(now) {
			_, err := tx.Exec(ctx, `update notify_outbox set not_before=$2 where id=any($1)`, ids, deferUntil)
			res.Deferred = len(ids)
			return err
		}
		var parts []string
		var keyboard []json.RawMessage
		size := 0
		for _, q := range eligible {
			text := q.text
			if textSize(text) > 3800 {
				text = telegramText(text, 3799)
			}
			if kind == "digest" {
				text = telegramText(text, 3799)
			}
			partSize := textSize(text) + 2
			if len(m.ids) > 0 && (kind == "direct" || size+partSize > 3900) {
				break
			}
			m.ids = append(m.ids, q.id)
			parts = append(parts, text)
			size += partSize
			if kind == "direct" {
				m.markup = q.markup
			} else if len(q.markup) > 0 {
				var markup struct {
					Keyboard []json.RawMessage `json:"inline_keyboard"`
				}
				if err := json.Unmarshal(q.markup, &markup); err != nil {
					return err
				}
				keyboard = append(keyboard, markup.Keyboard...)
			}
		}
		m.text = strings.Join(parts, "\n\n")
		if len(keyboard) > 0 {
			m.markup, _ = json.Marshal(map[string]any{"inline_keyboard": keyboard[:min(len(keyboard), 50)]})
		}
		m.kind = kind
		if kind == "digest" {
			if _, err := tx.Exec(ctx, `insert into bot_daily_sends(user_id,day) values($1,$2::date)`, userID, Day(now)); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `insert into notify_sends(user_id,category,sent_at,kind,outbox_id) values($1,$2,$3,$4,$5) returning id`, userID, string(eligible[0].category), now, kind, m.ids[0]).Scan(&m.reservation); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update notify_outbox set state='sending',claimed_at=$2,attempts=attempts+1 where id=any($1)`, m.ids, now)
		return err
	})
	if err != nil || len(m.ids) == 0 {
		return res, false, err
	}
	outcome := d.transmit(ctx, m)
	// Finish even if the HTTP request was cancelled; otherwise stale claims become uncertain.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = pgx.BeginFunc(finishCtx, d.Pool, func(tx pgx.Tx) error {
		if err := lockUser(finishCtx, tx, userID); err != nil {
			return err
		}
		if outcome.rejected {
			if _, err := tx.Exec(finishCtx, `delete from notify_sends where id=$1`, m.reservation); err != nil {
				return err
			}
			if kind == "digest" {
				if _, err := tx.Exec(finishCtx, `delete from bot_daily_sends where user_id=$1 and day=$2::date`, userID, Day(now)); err != nil {
					return err
				}
			}
			if outcome.blocked {
				if _, err := tx.Exec(finishCtx, `update users set bot_inactive=true where id=$1`, userID); err != nil {
					return err
				}
			}
			_, err := tx.Exec(finishCtx, `update notify_outbox set state=case when $4 or attempts >= $5 then 'dropped' else 'pending' end,
    done_at=case when $4 or attempts >= $5 then $2::timestamptz end,claimed_at=null,not_before=$3 where id=any($1)`, m.ids, now, now.Add(retryBackoff), outcome.blocked, maxAttempts)
			res.Failed++
			return err
		}
		state := "uncertain"
		if outcome.sent {
			state = "sent"
			res.Sent++
		} else {
			res.Uncertain++
		}
		_, err := tx.Exec(finishCtx, `update notify_outbox set state=$2,done_at=$3 where id=any($1)`, m.ids, state, now)
		return err
	})
	if outcome.err != nil {
		log.Printf("notify user %d: %v", userID, outcome.err)
	}
	return res, true, err
}

func Flush(ctx context.Context, now time.Time) (*FlushResult, error) {
	d, err := dispatcher(ctx)
	if err != nil {
		return nil, err
	}
	return d.Flush(ctx, now)
}

// Flush owns all digest delivery and deferred direct delivery. Pending items never expire
// just because quiet hours or the cap held them back. Terminal receipts last 90 days.
func (d *Dispatcher) Flush(ctx context.Context, now time.Time) (*FlushResult, error) {
	res := &FlushResult{}
	tag, err := d.Pool.Exec(ctx, `update notify_outbox set state='uncertain',done_at=$1 where state='sending' and claimed_at<$2`, now, now.Add(-staleClaim))
	if err != nil {
		return nil, err
	}
	res.Uncertain = int(tag.RowsAffected())
	rows, err := d.Pool.Query(ctx, `select user_id from notify_outbox where state='pending' and not_before<=$1 group by user_id order by min(not_before),user_id limit $2`, now, flushBatch)
	if err != nil {
		return nil, err
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	for _, uid := range users {
		for _, kind := range []string{"direct", "digest"} {
			// At most three direct calls and one digest can reserve a slot. One extra pass
			// moves the remaining items to tomorrow (or records a known rejection retry).
			for i := 0; i <= MaxDailyCap; i++ {
				part, sent, err := d.dispatch(ctx, uid, kind, 0, now)
				res.add(part)
				if err != nil {
					return nil, err
				}
				if !sent {
					break
				}
			}
		}
	}
	if _, err := d.Pool.Exec(ctx, `delete from notify_outbox where state in ('sent','dropped','uncertain') and done_at<$1`, now.Add(-keepDone)); err != nil {
		return nil, err
	}
	if _, err := d.Pool.Exec(ctx, `delete from notify_sends where sent_at<$1`, now.Add(-keepSends)); err != nil {
		return nil, err
	}
	if _, err := d.Pool.Exec(ctx, `delete from bot_daily_sends where day<$1::date`, Day(now.Add(-keepSends))); err != nil {
		return nil, err
	}
	return res, nil
}

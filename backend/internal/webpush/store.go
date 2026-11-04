package webpush

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is what pgxpool.Pool and pgx.Tx have in common.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// MaxPerUser bounds the browsers one account keeps; the oldest subscription goes first.
const MaxPerUser = 10

// MaxFailures: a subscription that failed this many times in a row (without a 404/410) is dropped.
const MaxFailures = 5

// Save stores (or moves) a browser subscription. The endpoint is unique: when another
// account subscribed this browser before, the row now belongs to the current account.
// Runs as the connection owner because the old row is invisible to the student under RLS.
func Save(ctx context.Context, q Querier, userID int64, endpoint string, k Keys, ua string) error {
	if _, err := q.Exec(ctx, `insert into push_subscriptions (user_id, endpoint, p256dh, auth, ua)
  values ($1, $2, $3, $4, nullif($5, ''))
  on conflict (endpoint) do update set user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth,
    ua = excluded.ua, created_at = now(), failures = 0`,
		userID, endpoint, b64.EncodeToString(k.P256dh), b64.EncodeToString(k.Auth), ua); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `delete from push_subscriptions where user_id = $1 and id not in
  (select id from push_subscriptions where user_id = $1 order by created_at desc, id desc limit $2)`, userID, MaxPerUser)
	return err
}

// Remove deletes the account's subscription for endpoint (RLS-safe: own rows only).
func Remove(ctx context.Context, q Querier, userID int64, endpoint string) (bool, error) {
	tag, err := q.Exec(ctx, `delete from push_subscriptions where user_id = $1 and endpoint = $2`, userID, endpoint)
	return tag.RowsAffected() > 0, err
}

// List returns the account's subscriptions, newest first. Rows with unreadable keys
// (never written by Save) are skipped.
func List(ctx context.Context, q Querier, userID int64) ([]Subscription, error) {
	rows, err := q.Query(ctx, `select id, endpoint, p256dh, auth from push_subscriptions where user_id = $1 order by created_at desc, id desc`, userID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id                  int64
		endpoint, pub, auth string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.id, &x.endpoint, &x.pub, &x.auth)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Subscription, 0, len(list))
	for _, x := range list {
		k, err := ParseKeys(x.pub, x.auth)
		if err != nil {
			continue
		}
		out = append(out, Subscription{ID: x.id, Endpoint: x.endpoint, Keys: k})
	}
	return out, nil
}

// Has reports whether the account has at least one subscription.
func Has(ctx context.Context, q Querier, userID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `select exists (select 1 from push_subscriptions where user_id = $1)`, userID).Scan(&ok)
	return ok, err
}

// Result summarizes one fan-out to an account's subscriptions.
type Result struct {
	Delivered, Gone, Failed int
	// Retryable is true when no subscription took the message but at least one may later.
	Retryable bool
}

// SendAll sends payload to every subscription and records the outcome per row: last_ok on
// success, deletion on 404/410 (or after MaxFailures failures), failures+1 otherwise.
func (s *Sender) SendAll(ctx context.Context, q Querier, subs []Subscription, payload []byte, urgency string) Result {
	var res Result
	now := time.Now()
	for _, sub := range subs {
		out, err := s.Send(ctx, sub, payload, urgency)
		var dbErr error
		switch out {
		case Delivered:
			res.Delivered++
			_, dbErr = q.Exec(ctx, `update push_subscriptions set last_ok = $2, failures = 0 where id = $1`, sub.ID, now)
		case Gone:
			res.Gone++
			_, dbErr = q.Exec(ctx, `delete from push_subscriptions where id = $1`, sub.ID)
		default:
			res.Failed++
			if out == Retry {
				res.Retryable = true
			}
			if _, dbErr = q.Exec(ctx, `update push_subscriptions set failures = failures + 1 where id = $1`, sub.ID); dbErr == nil {
				_, dbErr = q.Exec(ctx, `delete from push_subscriptions where id = $1 and failures >= $2`, sub.ID, MaxFailures)
			}
		}
		if err != nil {
			log.Printf("webpush subscription %d: %v", sub.ID, err)
		}
		if dbErr != nil {
			log.Printf("webpush subscription %d: record outcome: %v", sub.ID, dbErr)
		}
	}
	return res
}

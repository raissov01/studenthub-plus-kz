// Package moderation is the shared review queue. Features add work with Submit and react to a
// decision by registering an OnApprove/OnReject hook for their kind; the admin queue endpoints
// (prompt 06) call Review.
package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
)

type Submission struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	SubmittedBy *int64          `json:"submitted_by"`
	University  *string         `json:"university"`
	Status      string          `json:"status"`
	Reason      *string         `json:"reason"`
}

// Hook runs inside the review transaction.
type Hook func(ctx context.Context, tx pgx.Tx, s Submission) error

var (
	mu        sync.RWMutex
	onApprove = map[string]Hook{}
	onReject  = map[string]Hook{}
)

// OnApprove / OnReject register what happens when a submission of `kind` is decided.
func OnApprove(kind string, h Hook) { mu.Lock(); onApprove[kind] = h; mu.Unlock() }
func OnReject(kind string, h Hook)  { mu.Lock(); onReject[kind] = h; mu.Unlock() }

// Submit adds a pending item to the queue and returns its id.
func Submit(ctx context.Context, tx pgx.Tx, kind string, payload any, by *int64, university string) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `insert into submissions (kind, payload, submitted_by, university)
		values ($1, $2::jsonb, $3, nullif($4, '')) returning id`, kind, string(raw), by, university).Scan(&id)
	return id, err
}

var (
	ErrNotPending = errors.New("submission is not pending")
	ErrNeedReason = errors.New("a reason is required to reject")
)

// Review approves or rejects a pending submission and runs the kind's hook in the same transaction.
func Review(ctx context.Context, tx pgx.Tx, id, reviewer int64, approve bool, reason string) (Submission, error) {
	if !approve && reason == "" {
		return Submission{}, ErrNeedReason
	}
	status := "rejected"
	if approve {
		status = "approved"
	}
	var s Submission
	err := tx.QueryRow(ctx, `update submissions set status = $2, reason = nullif($3, ''), reviewed_by = $4, reviewed_at = now()
		where id = $1 and status = 'pending'
		returning id, kind, payload, submitted_by, university, status::text, reason`, id, status, reason, reviewer).
		Scan(&s.ID, &s.Kind, &s.Payload, &s.SubmittedBy, &s.University, &s.Status, &s.Reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotPending
	}
	if err != nil {
		return s, err
	}
	mu.RLock()
	h := onReject[s.Kind]
	if approve {
		h = onApprove[s.Kind]
	}
	mu.RUnlock()
	if h != nil {
		err = h(ctx, tx, s)
	}
	return s, err
}

// CountedReports is the shared "hide after N reports" rule: distinct reporters whose accounts
// are at least 7 days old, ignoring reports an admin dismissed.
func CountedReports(ctx context.Context, tx pgx.Tx, kind, target string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `select count(distinct r.reporter_id) from reports r join users u on u.id = r.reporter_id
		where r.target_kind = $1 and r.target_id = $2 and not r.dismissed and u.created_at <= now() - interval '7 days'`, kind, target).Scan(&n)
	return n, err
}

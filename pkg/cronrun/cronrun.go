// Package cronrun records successful daily jobs. Failed jobs leave the previous timestamp intact.
package cronrun

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

var Endpoints = []string{"/api/cron", "/api/cron/events", "/api/cron/ambassador", "/api/cron/mentors", "/api/cron/market", "/api/cron/b2b"}

// Success also performs the daily retention in the main bot cron: client errors after 30
// days, read inbox notifications after 90 (notify.InboxRetention).
// Retention and the timestamp commit together; neither succeeds if storage fails.
func Success(ctx context.Context, endpoint string) error {
	return db.Tx(ctx, func(tx pgx.Tx) error { return SuccessTx(ctx, tx, endpoint) })
}

// SuccessTx records a run using the caller's transaction (including a Bot's own pool).
func SuccessTx(ctx context.Context, tx pgx.Tx, endpoint string) error {
	if endpoint == "/api/cron" {
		if _, err := tx.Exec(ctx, `delete from client_errors where created_at < now() - interval '30 days'`); err != nil {
			return err
		}
		if _, err := notify.PruneInbox(ctx, tx, time.Now()); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `insert into cron_runs (endpoint, last_success_at) values ($1, $2)
   on conflict (endpoint) do update set last_success_at = greatest(cron_runs.last_success_at, excluded.last_success_at)`, endpoint, time.Now().UTC())
	return err
}

package notify_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/cronrun"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// Read rows older than 90 days go in the daily /api/cron run; unread and recent ones stay.
// (External test package: cronrun imports notify.)
func TestInboxRetentionInDailyCron(t *testing.T) {
	testdb.Need(t)
	ctx := context.Background()
	var got []string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var uid int64
		if err := tx.QueryRow(ctx, `insert into users (telegram_id, name) values (889999, 'R') returning id`).Scan(&uid); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into notifications (user_id, category, text, created_at, read_at) values
			($1, 'events', 'old read', now() - interval '91 days', now() - interval '80 days'),
			($1, 'events', 'old unread', now() - interval '91 days', null),
			($1, 'events', 'recent read', now() - interval '10 days', now())`, uid)
		if err != nil {
			return err
		}
		if err := cronrun.SuccessTx(ctx, tx, "/api/cron"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select text from notifications where user_id = $1 order by id`, uid)
		if err != nil {
			return err
		}
		got, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "old unread" || got[1] != "recent read" {
		t.Fatalf("retention: %v", got)
	}
}

package notify

import (
	"os"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// Run the actual upgrade against pre-dispatcher tables in a private pg_temp schema.
// This also checks existing preference values and the new hard database constraint.
func TestUpgradeReservesLegacyInFlightSends(t *testing.T) {
	testdb.Need(t)
	pool, err := db.Pool(ctx())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx())
	_, err = tx.Exec(ctx(), `
 create temporary table notify_prefs (
  user_id bigint primary key, quiet_end time default '09:00',
  daily_cap smallint constraint notify_prefs_daily_cap_check check(daily_cap between 1 and 20)
 ) on commit drop;
 create temporary table notifications (
  id bigint primary key, user_id bigint, category text, text text
 ) on commit drop;
 create temporary table notify_outbox (
  id bigint primary key, user_id bigint, category text, text text,
  state text constraint notify_outbox_state_check check(state in ('pending','sending','sent','dropped')),
  claimed_at timestamptz, created_at timestamptz, done_at timestamptz
 ) on commit drop;
 create temporary table notify_sends (
  id bigint generated always as identity primary key, user_id bigint, category text, sent_at timestamptz
 ) on commit drop;
 insert into notify_prefs values(1,'09:00',20);
 insert into notifications values(1,1,'moderation','already being sent');
 insert into notify_outbox values
  (1,1,'moderation','already being sent','sending',$1,$1,null),
  (2,1,'events','still waiting','pending',null,$1,null);
 `, at(12, 0))
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../supabase/migrations/20261029000024_notification_dispatch.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx(), string(migration)); err != nil {
		t.Fatal(err)
	}
	var state, kind, quiet string
	var cap int
	var nid int64
	var atTime time.Time
	if err = tx.QueryRow(ctx(), `select o.state,s.kind,s.sent_at,o.notification_id from notify_outbox o join notify_sends s on s.outbox_id=o.id where o.id=1`).Scan(&state, &kind, &atTime, &nid); err != nil {
		t.Fatal(err)
	}
	if state != "uncertain" || kind != "direct" || !atTime.Equal(at(12, 0)) || nid != 1 {
		t.Fatalf("legacy in-flight receipt: %s %s %v %d", state, kind, atTime, nid)
	}
	var count int
	if err = tx.QueryRow(ctx(), `select count(*) from notify_sends`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pending work reserved a slot: %d %v", count, err)
	}
	if err = tx.QueryRow(ctx(), `select daily_cap,to_char(quiet_end,'HH24:MI') from notify_prefs where user_id=1`).Scan(&cap, &quiet); err != nil {
		t.Fatal(err)
	}
	if cap != 3 || quiet != "09:00" {
		t.Fatalf("cap not clamped or existing quiet preference changed: %d %s", cap, quiet)
	}
	if _, err = tx.Exec(ctx(), `insert into notify_prefs(user_id,daily_cap) values(2,3)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx(), `select to_char(quiet_end,'HH24:MI') from notify_prefs where user_id=2`).Scan(&quiet); err != nil || quiet != "08:00" {
		t.Fatalf("new quiet default: %s %v", quiet, err)
	}
	if _, err = tx.Exec(ctx(), `update notify_prefs set daily_cap=4 where user_id=1`); err == nil {
		t.Fatal("database accepted cap above three")
	}
}

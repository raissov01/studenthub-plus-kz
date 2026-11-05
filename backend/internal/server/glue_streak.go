package server

import (
	"context"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/points"
)

// StreakCategory is the notify category of the evening streak reminder. It rides «reminders»
// (deadlines) until a dedicated category exists; the student's own on/off switch for it lives in
// points_wallets.streak_reminder, so turning reminders off here or there both stop it.
var StreakCategory = notify.CatDeadlines

// Spec 08: the evening streak nudge goes through the one notify dispatcher (inbox row, outbox,
// shared daily budget, quiet hours, idempotency key), whatever channels it delivers on.
func init() {
	points.SendReminder = func(ctx context.Context, r points.Reminder, now time.Time) (bool, error) {
		pool, err := db.Pool(ctx)
		if err != nil {
			return false, err
		}
		ok, err := notify.DeliverableNow(ctx, pool, r.UserID, StreakCategory, now)
		if err != nil || !ok {
			return false, err
		}
		_, err = notify.Send(ctx, r.UserID, StreakCategory, r.Text, notify.Opts{Now: now, Key: r.Key, Link: r.Link})
		return err == nil, err
	}
}

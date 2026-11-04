package notify

import (
	"context"
	"time"
)

// DeliverableNow reports whether a direct message in category c would leave right now instead of
// being held back: the category is on, the student has a channel, it is not their quiet hours,
// the bot is not paused for them and today's shared budget has room. Time-bound nudges (the
// evening streak reminder) check it first, because a held message would arrive the next morning
// and no longer be true. It reserves nothing; Send still applies every rule itself.
func DeliverableNow(ctx context.Context, q Querier, userID int64, c Category, now time.Time) (bool, error) {
	prefs, err := LoadPrefs(ctx, q, userID)
	if err != nil || !prefs.Allows(c) || prefs.Quiet(now) {
		return false, err
	}
	ok, err := HasTelegram(ctx, q, userID)
	if err != nil || !ok {
		return false, err
	}
	var paused bool
	if err := q.QueryRow(ctx, `select coalesce(paused_until > $2, false) from users where id = $1`, userID, now).Scan(&paused); err != nil || paused {
		return false, err
	}
	n, err := sentToday(ctx, q, userID, now)
	if err != nil {
		return false, err
	}
	return n < min(prefs.DailyCap, MaxDailyCap), nil
}

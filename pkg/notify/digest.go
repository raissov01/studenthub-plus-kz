package notify

import (
	"context"
	"time"
	_ "time/tzdata" // Asia/Almaty must resolve even without system zoneinfo

	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

// Day is the Almaty calendar day of t (YYYY-MM-DD), the unit of the "one digest per day" rule.
func Day(t time.Time) string { return t.In(almaty).Format("2006-01-02") }

// Digest results.
const (
	DigestSent    = "sent"
	DigestClaimed = "claimed" // the student already got a bot message today; try again tomorrow
	DigestFailed  = "failed"
)

// Digest sends a non-urgent message under the bot's hard rule of at most ONE message per
// student per Almaty day (bot_daily_sends, shared with deadline reminders). Whoever inserts
// the day row owns the day; when the day is taken the message is not sent and the caller
// retries on a later run. A failed send gives the day back.
func Digest(ctx context.Context, userID int64, now time.Time, text string) (string, error) {
	p, err := db.Pool(ctx)
	if err != nil {
		return "", err
	}
	day := Day(now)
	tag, err := p.Exec(ctx, `insert into bot_daily_sends (user_id, day) values ($1, $2::date) on conflict do nothing`, userID, day)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return DigestClaimed, nil
	}
	if err := Direct(ctx, userID, text); err != nil {
		_, _ = p.Exec(ctx, `delete from bot_daily_sends where user_id = $1 and day = $2::date`, userID, day)
		return DigestFailed, err
	}
	return DigestSent, nil
}

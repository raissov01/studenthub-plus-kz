package notify

import (
	"context"
	"log"
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
	DigestSent     = "sent"
	DigestClaimed  = "claimed" // the student already got a bot message today; try again tomorrow
	DigestFailed   = "failed"
	DigestDeferred = "deferred" // quiet hours or the daily cap; try again on a later run
	DigestOff      = "off"      // the student switched the category off; not sent
)

// Digest sends a non-urgent message under the bot's hard rule of at most ONE message per
// student per Almaty day (bot_daily_sends, shared with deadline reminders), in the "digest"
// category. Feature crons with their own category use DigestAs.
func Digest(ctx context.Context, userID int64, now time.Time, text string) (string, error) {
	return DigestAs(ctx, userID, CatDigest, now, text)
}

// DigestAs is Digest for category c. The in-site inbox row is written first, always (once per
// text and category within two weeks, see record). Whoever inserts the day row owns the day; when the day
// is taken, the category is off (or the account has no Telegram), it is quiet time or the
// daily cap is used up, nothing is sent and the caller retries on a later run (any status but DigestSent). A failed send
// gives the day back.
func DigestAs(ctx context.Context, userID int64, c Category, now time.Time, text string) (string, error) {
	p, err := db.Pool(ctx)
	if err != nil {
		return "", err
	}
	// The inbox gets it on the first try; crons retry digests that were not sent, and the
	// dedupe keeps those retries from piling up in the inbox.
	recordLogged(ctx, p, userID, c, text, true)
	if tg, err := HasTelegram(ctx, p, userID); err != nil {
		return "", err
	} else if !tg {
		return DigestOff, nil // email-only account: no chat, and no day claim used up
	}
	if !c.Transactional() {
		prefs, err := LoadPrefs(ctx, p, userID)
		if err != nil {
			return "", err
		}
		if !prefs.Allows(c) {
			return DigestOff, nil
		}
		if prefs.Quiet(now) {
			return DigestDeferred, nil
		}
		n, err := sentToday(ctx, p, userID, now)
		if err != nil {
			return "", err
		}
		if n >= prefs.DailyCap {
			return DigestDeferred, nil
		}
	}
	day := Day(now)
	tag, err := p.Exec(ctx, `insert into bot_daily_sends (user_id, day) values ($1, $2::date) on conflict do nothing`, userID, day)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return DigestClaimed, nil
	}
	if err := deliver(ctx, userID, text); err != nil {
		_, _ = p.Exec(ctx, `delete from bot_daily_sends where user_id = $1 and day = $2::date`, userID, day)
		return DigestFailed, err
	}
	if !c.Transactional() {
		if err := RecordSend(ctx, p, userID, c, now); err != nil {
			log.Printf("notify: record digest %d: %v", userID, err) // sent anyway: never report a retry
		}
	}
	return DigestSent, nil
}

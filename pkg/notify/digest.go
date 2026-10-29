package notify

import (
	"context"
	"time"
	_ "time/tzdata"
)

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

// Day is the Almaty calendar day, shared by both delivery budgets.
func Day(t time.Time) string { return t.In(almaty).Format("2006-01-02") }

const (
	DigestSent     = "sent"
	DigestQueued   = "queued"
	DigestClaimed  = "claimed" // retained for callers that import historical status values
	DigestFailed   = "failed"
	DigestDeferred = "deferred"
	DigestOff      = "off"
)

// DigestAccepted means the dispatcher owns the message; a feature must not recreate it.
// Off is also final: the student explicitly disabled delivery, but has the inbox row.
func DigestAccepted(status string) bool {
	return status == DigestSent || status == DigestQueued || status == DigestOff
}

func Digest(ctx context.Context, userID int64, now time.Time, text string) (string, error) {
	return DigestAs(ctx, userID, CatDigest, now, text)
}

// DigestAs persists an individual item. Flush combines all due items across modules,
// ordered deadlines > events > other categories, in at most one Telegram message/day.
func DigestAs(ctx context.Context, userID int64, c Category, now time.Time, text string) (string, error) {
	d, err := dispatcher(ctx)
	if err != nil {
		return "", err
	}
	return d.Digest(ctx, userID, c, text, Opts{Now: now})
}

func (d *Dispatcher) Digest(ctx context.Context, userID int64, c Category, text string, opts Opts) (string, error) {
	_, state, err := d.accept(ctx, userID, c, text, "digest", opts)
	if err != nil {
		return "", err
	}
	switch state {
	case "sent", "uncertain", "sending":
		return DigestSent, nil
	case "dropped":
		return DigestOff, nil
	default:
		return DigestQueued, nil
	}
}

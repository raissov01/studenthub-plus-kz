package auth

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
)

// Account activity for the admin dashboard (DAU/WAU/MAU of signed-in accounts): the first signed-in request
// of an Asia/Almaty day writes one user_active_days row. The in-memory map keeps it to one write per account
// per day per process; a lost write only undercounts.

var (
	activeMu   sync.Mutex
	activeSeen = map[int64]string{}
	activeDay  string
	almaty     = func() *time.Location {
		if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
			return loc
		}
		return time.FixedZone("Asia/Almaty", 5*3600)
	}()
)

// ActiveDay is the Asia/Almaty calendar day of t ("2026-11-06").
func ActiveDay(t time.Time) string { return t.In(almaty).Format(time.DateOnly) }

// presence: when each account last made a signed-in request (this process only, memory only). Adapted from the
// owner's StudyWithRaissov admin (internal/presence): "online now" = seen in the last OnlineWindow.
var (
	presenceMu sync.Mutex
	presence   = map[int64]time.Time{}
)

// OnlineWindow is how recent a request must be to count as online.
const OnlineWindow = 5 * time.Minute

// OnlineCount counts accounts seen within OnlineWindow and forgets older ones.
func OnlineCount(now time.Time) int {
	presenceMu.Lock()
	defer presenceMu.Unlock()
	n := 0
	for id, at := range presence {
		if now.Sub(at) > OnlineWindow {
			delete(presence, id)
			continue
		}
		n++
	}
	return n
}

func recordActive(ctx context.Context, userID int64, now time.Time) {
	presenceMu.Lock()
	presence[userID] = now
	presenceMu.Unlock()
	day := ActiveDay(now)
	activeMu.Lock()
	if day != activeDay { // a new day: forget yesterday's accounts
		activeSeen, activeDay = map[int64]string{}, day
	}
	if activeSeen[userID] == day {
		activeMu.Unlock()
		return
	}
	activeSeen[userID] = day
	activeMu.Unlock()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into user_active_days (user_id, day) values ($1, $2::date) on conflict do nothing`, userID, day)
		return err
	})
	if err != nil {
		log.Printf("auth: record activity %d: %v", userID, err)
		activeMu.Lock()
		delete(activeSeen, userID) // try again on the next request
		activeMu.Unlock()
	}
}

// blockExempt: the writes a blocked account keeps: signing in and out, password/email settings, deleting the
// account, and the anonymous usage/error beacons (no content).
func blockExempt(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/auth/"), p == "/api/me/password", p == "/api/me/email", p == "/api/me/email/confirm":
		return true
	case r.Method == http.MethodDelete && p == "/api/me":
		return true
	case r.Method == http.MethodPost && (p == "/api/events" || p == "/api/client-errors"):
		return true
	}
	return false
}

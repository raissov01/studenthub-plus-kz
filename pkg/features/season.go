package features

import (
	"strings"
	"time"
	_ "time/tzdata" // Asia/Almaty must resolve even without system zoneinfo

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
)

// Season is the market's "moving out?" period (prompt 08, CONFIG.marketSeason).
type Season struct {
	From   string `json:"from"` // YYYY-MM-DD, inclusive
	To     string `json:"to"`   // YYYY-MM-DD, inclusive
	Active bool   `json:"active"`
}

// Key identifies the season (the seasonal digest goes once per student per season).
func (s Season) Key() string { return s.From + ".." + s.To }

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

// MarketSeason reads MARKET_SEASON, a comma-separated list of "YYYY-MM-DD..YYYY-MM-DD" ranges
// (Almaty days, inclusive), e.g. "2026-12-15..2027-01-31,2027-05-20..2027-06-30". It returns
// the range containing now, else the next upcoming one, else nil. Invalid ranges are ignored.
func MarketSeason(now time.Time) *Season {
	today := now.In(almaty).Format("2006-01-02")
	var next *Season
	for _, part := range config.List("MARKET_SEASON", "") {
		from, to, ok := strings.Cut(part, "..")
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if !ok || !isDate(from) || !isDate(to) || from > to || to < today {
			continue
		}
		s := &Season{From: from, To: to, Active: from <= today}
		if s.Active {
			return s
		}
		if next == nil || from < next.From {
			next = s
		}
	}
	return next
}

func isDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

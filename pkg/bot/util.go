package bot

import (
	"crypto/sha256"
	"crypto/subtle"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Vercel's runtime may lack zoneinfo; Asia/Almaty must always resolve

	"github.com/jackc/pgx/v5/pgtype"
)

var (
	remindDays = []int{7, 3, 1}
	idRe       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	isoRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	almaty     = mustLoc("Asia/Almaty")
)

const (
	maxReminders = 30
	isoLayout    = "2006-01-02"
)

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone(name, 5*3600)
	}
	return loc
}

// safeEqual compares in constant time; hashing first equalises lengths. An empty expected
// secret never matches, so a missing env var fails closed.
func safeEqual(given, expected string) bool {
	if expected == "" {
		return false
	}
	a := sha256.Sum256([]byte(given))
	b := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// localDate is the YYYY-MM-DD calendar day in Almaty.
func localDate(t time.Time) string { return t.In(almaty).Format(isoLayout) }

func parseISO(s string) (time.Time, bool) {
	if !isoRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(isoLayout, s)
	return t, err == nil
}

func isISODate(s string) bool { _, ok := parseISO(s); return ok }

func daysBetween(from, to string) int {
	a, _ := parseISO(from)
	b, _ := parseISO(to)
	return int(b.Sub(a) / (24 * time.Hour)) // both are UTC midnights: an exact multiple
}

func addDays(iso string, n int) string {
	t, _ := parseISO(iso)
	return t.AddDate(0, 0, n).Format(isoLayout)
}

// weekday: 1 = Monday ... 7 = Sunday.
func weekday(iso string) int {
	t, _ := parseISO(iso)
	return (int(t.Weekday())+6)%7 + 1
}

// weekStart is the Monday of the week, the digest "week id".
func weekStart(iso string) string { return addDays(iso, 1-weekday(iso)) }

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func esc(s string) string { return escaper.Replace(s) }

func pgDate(iso string) pgtype.Date {
	t, ok := parseISO(iso)
	return pgtype.Date{Time: t, Valid: ok}
}

func isoOf(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format(isoLayout)
}

// Pauses are stored as Almaty midnight of the first day reminders resume.
func pgPause(iso string) pgtype.Timestamptz {
	if iso == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.ParseInLocation(isoLayout, iso, almaty)
	return pgtype.Timestamptz{Time: t, Valid: err == nil}
}

func pauseISO(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.In(almaty).Format(isoLayout)
}

func text(t pgtype.Text) string { return t.String }

func pgText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func pgInt8(n int64) pgtype.Int8 { return pgtype.Int8{Int64: n, Valid: true} }

func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

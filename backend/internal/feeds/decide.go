package feeds

import (
	"net/url"
	"strings"
	"time"
)

// Item statuses.
const (
	StatusPending   = "pending"
	StatusPublished = "published"
	StatusRejected  = "rejected"
	StatusExpired   = "expired"
)

// Decide: an item publishes itself only when autopublish is on (threshold > 0), its source is
// trusted ("high") and the validated confidence reaches the threshold. Everything else waits in
// the moderation queue.
func Decide(confidence float64, trust string, threshold float64) string {
	if threshold > 0 && trust == "high" && confidence >= threshold {
		return StatusPublished
	}
	return StatusPending
}

// Expired: the deadline passed; with no deadline, the event date passed; with neither, the item
// is older than ttlDays (an undated announcement goes stale). Dates are YYYY-MM-DD, today is in
// Asia/Almaty.
func Expired(deadline, eventDate string, created time.Time, today string, ttlDays int, now time.Time) bool {
	switch {
	case deadline != "":
		return deadline < today
	case eventDate != "":
		return eventDate < today
	case ttlDays > 0:
		return now.Sub(created) > time.Duration(ttlDays)*24*time.Hour
	}
	return false
}

// URLKey is the apply link as a dedupe key, or "" when the link is too generic to identify one
// opportunity (a bare domain, a Telegram channel root).
func URLKey(raw string) string {
	c := CanonicalURL(raw)
	if c == "" {
		return ""
	}
	u, err := url.Parse("https://" + c)
	if err != nil {
		return ""
	}
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	host := u.Hostname()
	if len(segs) == 0 && u.RawQuery == "" {
		return ""
	}
	if (host == "t.me" || host == "telegram.me") && len(segs) < 2 {
		return ""
	}
	return c
}

// OppType maps a feed category onto the four opportunity types of /api/content.
func OppType(category string) string {
	switch category {
	case "mobility", "exchange":
		return "mobility"
	case "internship":
		return "company"
	case "grant", "scholarship", "hackathon", "startup", "competition":
		return "grant"
	default: // conference, volunteer, course, other
		return "intl"
	}
}

var cityAliases = map[string]string{
	"astana": "astana", "nur-sultan": "astana", "nursultan": "astana", "астана": "astana",
	"almaty": "almaty", "алматы": "almaty", "shymkent": "shymkent", "шымкент": "shymkent",
	"karaganda": "karaganda", "qaragandy": "karaganda", "караганда": "karaganda", "қарағанды": "karaganda",
	"aktobe": "aktobe", "актобе": "aktobe", "taraz": "taraz", "тараз": "taraz", "turkistan": "turkistan",
	"pavlodar": "pavlodar", "павлодар": "pavlodar", "oskemen": "oskemen", "ust-kamenogorsk": "oskemen",
	"semey": "semey", "семей": "semey", "kostanay": "kostanay", "костанай": "kostanay", "atyrau": "atyrau",
	"aktau": "aktau", "oral": "oral", "uralsk": "oral", "kyzylorda": "kyzylorda", "petropavl": "petropavl",
	"petropavlovsk": "petropavl", "taldykorgan": "taldykorgan", "kokshetau": "kokshetau", "кокшетау": "kokshetau",
}

// CityCode maps the AI's city onto the content city list: online -> remote, unknown -> other.
func CityCode(city string, online bool) string {
	if c, ok := cityAliases[strings.ToLower(strings.TrimSpace(city))]; ok {
		return c
	}
	if online {
		return "remote"
	}
	return "other"
}

// Package analytics stores anonymous product events in Postgres and answers the three questions
// the owner cares about: do students come back on day 1 and day 7, and how many save a first
// opportunity. Every event name and property is whitelisted; anything else is dropped.
package analytics

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

// prop validators: only short categorical values pass.
var (
	reID     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	reKey    = regexp.MustCompile(`^[a-z0-9_]{1,30}$`)
	reAmb    = regexp.MustCompile(`^amb_[A-Za-z0-9_-]{1,30}$`)
	reUUID   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	isID     = func(v any) bool { s, ok := v.(string); return ok && reID.MatchString(s) }
	isKey    = func(v any) bool { s, ok := v.(string); return ok && reKey.MatchString(s) }
	isBool   = func(v any) bool { _, ok := v.(bool); return ok }
	intRange = func(lo, hi float64) func(any) bool {
		return func(v any) bool { f, ok := v.(float64); return ok && f == float64(int(f)) && f >= lo && f <= hi }
	}
	isFeedback = func(v any) bool { s, ok := v.(string); return ok && len([]rune(s)) <= 500 }
)

// Events lists every accepted event and its allowed properties.
var Events = map[string]map[string]func(any) bool{
	"visit":               {},
	"onboarding_complete": {"faculty": isKey, "course": intRange(1, 6), "city": isKey},
	"opportunity_view":    {"id": isID},
	"job_view":            {"id": isID},
	"share_click":         {"kind": isKey, "id": isID, "channel": isKey},
	"deadline_saved":      {"id": isID},
	"ai_message_sent":     {},
	"roadmap_item_done":   {"item": isKey},
	"telegram_open":       {"in_telegram": isBool},
	"feedback":            {"score": intRange(1, 5), "missing": isFeedback},
	// quiz module: categorical props only, never quiz titles or question text
	"quiz_created":     {"source": isKey, "questions": intRange(1, 200)},
	"quiz_played":      {"mode": isKey, "questions": intRange(1, 200), "percent": intRange(0, 100)},
	"quiz_live_hosted": {"players": intRange(0, 500), "questions": intRange(1, 200)},
}

type event struct {
	Name  string         `json:"name"`
	Props map[string]any `json:"props"`
}

type batch struct {
	AnonID string  `json:"anon_id"`
	Ref    string  `json:"ref"`
	Events []event `json:"events"`
}

// Clean keeps known events and their allowed properties only.
func Clean(in []event) []event {
	var out []event
	for _, e := range in {
		allowed, ok := Events[e.Name]
		if !ok {
			continue
		}
		props := map[string]any{}
		for k, v := range e.Props {
			if check, ok := allowed[k]; ok && check(v) {
				if s, isStr := v.(string); isStr && k == "missing" {
					s = Scrub(s)
					if s == "" {
						continue
					}
					v = s
				}
				props[k] = v
			}
		}
		out = append(out, event{Name: e.Name, Props: props})
	}
	return out
}

// The only free text we keep is the optional «Не жетіспейді?» answer (02-014). The form asks
// for no name or phone, but people type them anyway, so contacts, long numbers and
// name-like word pairs are replaced before the text is stored (and again when it is read,
// for rows from before this rule).
var (
	reEmail  = regexp.MustCompile(`(?i)[\p{L}\p{N}._%+-]+@[\p{L}\p{N}.-]+\.[\p{L}]{2,}`)
	reURL    = regexp.MustCompile(`(?i)\b(?:https?://|www\.|t\.me/|wa\.me/)\S+`)
	reHandle = regexp.MustCompile(`@[A-Za-z0-9_]{3,}`)
	// 5+ digits, allowing spaces, dashes, dots, brackets between them: phones, IIN, cards, ids.
	reDigits = regexp.MustCompile(`\+?\d(?:[\s().-]*\d){4,}`)
	// Two or more capitalised words in a row ("Айгерім Сапарова", "Иван Петров"): likely a name.
	reName = regexp.MustCompile(`\p{Lu}\p{Ll}+(?:[\s-]+\p{Lu}\p{Ll}+)+`)
	// GPA and grades: "GPA 3.4", "gpa: 3,67".
	reGPA = regexp.MustCompile(`(?i)\b(?:gpa|жпа|гпа)\s*[:=-]?\s*\d(?:[.,]\d+)?`)
)

// Hidden replaces anything personal in feedback text.
const Hidden = "[жасырылды]"

// MaxFeedback is the longest feedback text kept, in runes.
const MaxFeedback = 300

// Scrub removes contacts, numbers, GPA and name-like word pairs from free text, collapses
// whitespace and cuts it to MaxFeedback runes.
func Scrub(s string) string {
	for _, re := range []*regexp.Regexp{reEmail, reURL, reHandle, reGPA, reDigits, reName} {
		s = re.ReplaceAllString(s, Hidden)
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxFeedback {
		s = strings.TrimSpace(string(r[:MaxFeedback]))
	}
	if strings.Trim(strings.ReplaceAll(s, Hidden, ""), " .,;:!?-") == "" {
		return "" // nothing but removed parts
	}
	return s
}

// CleanRef accepts "share" or "amb_<code>" only.
func CleanRef(ref string) string {
	if ref == "share" || reAmb.MatchString(ref) {
		return strings.ToLower(ref)
	}
	return ""
}

func Routes(mux httpx.Router) {
	mux.HandleFunc("POST /api/events", auth.Write(ingest))
}

func ingest(w http.ResponseWriter, r *http.Request) {
	ip := httpx.ClientIP(r)
	if ok, _ := ratelimit.Allow(r.Context(), "ev:"+ip, 120, time.Minute); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	var b batch
	if err := httpx.Decode(r, &b, 16<<10); err != nil || !reUUID.MatchString(b.AnonID) || len(b.Events) > 20 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	events := Clean(b.Events)
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `insert into analytics_devices (anon_id, first_day, ref)
			values ($1, (now() at time zone 'Asia/Almaty')::date, nullif($2, '')) on conflict (anon_id) do nothing`, b.AnonID, CleanRef(b.Ref)); err != nil {
			return err
		}
		for _, e := range events {
			props, _ := json.Marshal(e.Props)
			if _, err := tx.Exec(r.Context(), `insert into analytics_events (anon_id, name, props) values ($1, $2, $3::jsonb)`, b.AnonID, e.Name, string(props)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && err != db.ErrNoDB {
		log.Printf("analytics ingest: %v", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Summary is what the admin dashboard shows.
type Summary struct {
	Devices        int            `json:"devices"`       // devices first seen in the window
	ReturnedDay1   int            `json:"returned_day1"` // of those (old enough), came back the next day
	EligibleDay1   int            `json:"eligible_day1"`
	ReturnedDay7   int            `json:"returned_day7"` // came back on day 7 after the first visit
	EligibleDay7   int            `json:"eligible_day7"`
	SavedFirst     int            `json:"saved_first"` // saved at least one deadline
	FromShare      int            `json:"from_share"`
	Ambassadors    map[string]int `json:"ambassadors"` // amb_<code> -> devices brought
	FeedbackAvg    float64        `json:"feedback_avg"`
	FeedbackCount  int            `json:"feedback_count"`
	FeedbackRecent []string       `json:"feedback_recent"` // last "what is missing" answers
}

// Report computes the summary for devices first seen in the last `days` days (Almaty dates).
func Report(ctx context.Context, days int) (Summary, error) {
	s := Summary{Ambassadors: map[string]int{}, FeedbackRecent: []string{}}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
with today as (select (now() at time zone 'Asia/Almaty')::date as d),
cohort as (select anon_id, first_day, ref from analytics_devices, today where first_day > today.d - $1::int)
select
  count(*),
  count(*) filter (where first_day + 1 <= (select d from today)),
  count(*) filter (where exists (select 1 from analytics_events e where e.anon_id = c.anon_id and e.day = c.first_day + 1)),
  count(*) filter (where first_day + 7 <= (select d from today)),
  count(*) filter (where exists (select 1 from analytics_events e where e.anon_id = c.anon_id and e.day = c.first_day + 7)),
  count(*) filter (where exists (select 1 from analytics_events e where e.anon_id = c.anon_id and e.name = 'deadline_saved')),
  count(*) filter (where ref = 'share')
from cohort c`, days).Scan(&s.Devices, &s.EligibleDay1, &s.ReturnedDay1, &s.EligibleDay7, &s.ReturnedDay7, &s.SavedFirst, &s.FromShare)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select ref, count(*) from analytics_devices where ref like 'amb\_%' group by ref order by 2 desc limit 100`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var ref string
			var n int
			if err := rows.Scan(&ref, &n); err != nil {
				return err
			}
			s.Ambassadors[strings.TrimPrefix(ref, "amb_")] = n
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select coalesce(avg((props->>'score')::numeric), 0)::float8, count(*) from analytics_events where name = 'feedback' and props ? 'score'`).Scan(&s.FeedbackAvg, &s.FeedbackCount); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select props->>'missing' from analytics_events where name = 'feedback' and coalesce(props->>'missing', '') <> '' order by at desc limit 20`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				return err
			}
			if t = Scrub(t); t != "" {
				s.FeedbackRecent = append(s.FeedbackRecent, t)
			}
		}
		return rows.Err()
	})
	return s, err
}

package feeds

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Result is the AI's structured answer, flat, every field required (see aiSchema).
type Result struct {
	IsRelevant  bool    `json:"is_relevant"`
	Category    string  `json:"category"`
	TitleKK     string  `json:"title_kk"`
	TitleRU     string  `json:"title_ru"`
	SummaryKK   string  `json:"summary_kk"`
	SummaryRU   string  `json:"summary_ru"`
	Organizer   string  `json:"organizer"`
	Country     string  `json:"country"`
	City        string  `json:"city"`
	Online      bool    `json:"online"`
	Deadline    string  `json:"deadline"`
	EventDate   string  `json:"event_date"`
	Eligibility string  `json:"eligibility"`
	Funding     string  `json:"funding"`
	ApplyURL    string  `json:"apply_url"`
	SourceURL   string  `json:"source_url"`
	Language    string  `json:"language"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
	// filled by Validate, never by the model
	Notes []string `json:"validation_notes,omitempty"`
}

var Fundings = []string{"fully", "partly", "none", "unknown"}

// ErrIrrelevant: the model said the post is not an opportunity.
var ErrIrrelevant = errors.New("not relevant")

// GuessedCap is the highest confidence an item keeps after a field had to be dropped.
const GuessedCap = 0.6

// Validate checks the model's answer against the post: dates must parse, be plausible and be
// written in the post; URLs must be http(s) and appear in the post text or its links. A field
// that fails is cleared (never corrected) and noted; the confidence is then capped at
// GuessedCap so such an item never publishes itself. Structural problems (unknown category,
// no title) reject the answer.
func Validate(r Result, p Post, now time.Time) (Result, error) {
	if !r.IsRelevant {
		return r, ErrIrrelevant
	}
	r.Notes = nil
	note := func(f string, a ...any) { r.Notes = append(r.Notes, fmt.Sprintf(f, a...)) }
	r.Category = strings.ToLower(strings.TrimSpace(r.Category))
	if !slices.Contains(Categories, r.Category) {
		return r, fmt.Errorf("unknown category %q", r.Category)
	}
	r.TitleKK, r.TitleRU = clip(r.TitleKK, 160), clip(r.TitleRU, 160)
	if r.TitleKK == "" && r.TitleRU == "" {
		return r, errors.New("no title")
	}
	if r.TitleKK == "" {
		r.TitleKK = r.TitleRU
	}
	r.SummaryKK, r.SummaryRU = clip(r.SummaryKK, 700), clip(r.SummaryRU, 700)
	r.Organizer, r.Country, r.City = clip(r.Organizer, 160), clip(r.Country, 60), clip(r.City, 60)
	r.Eligibility, r.Reason, r.Language = clip(r.Eligibility, 400), clip(r.Reason, 300), clip(r.Language, 10)
	if !slices.Contains(Fundings, r.Funding) {
		r.Funding = "unknown"
	}
	if math.IsNaN(r.Confidence) || r.Confidence < 0 {
		r.Confidence = 0
	}
	if r.Confidence > 1 {
		r.Confidence = 1
	}
	dropped := false

	for _, f := range []struct {
		name string
		v    *string
	}{{"deadline", &r.Deadline}, {"event_date", &r.EventDate}} {
		*f.v = strings.TrimSpace(*f.v)
		if *f.v == "" {
			continue
		}
		d, err := time.Parse("2006-01-02", *f.v)
		switch {
		case err != nil:
			note("%s %q is not YYYY-MM-DD: dropped", f.name, *f.v)
		case d.Before(now.AddDate(-1, 0, 0)) || d.After(now.AddDate(3, 0, 0)):
			note("%s %s is implausible: dropped", f.name, *f.v)
		case !DateMentioned(p.Text, d):
			note("%s %s is not written in the post: dropped", f.name, *f.v)
		default:
			continue
		}
		*f.v, dropped = "", true
	}

	known := map[string]bool{}
	for _, u := range append(append([]string{}, p.Links...), URLsIn(p.Text)...) {
		if c := CanonicalURL(u); c != "" {
			known[c] = true
		}
	}
	r.ApplyURL = strings.TrimSpace(r.ApplyURL)
	if r.ApplyURL != "" {
		c := CanonicalURL(r.ApplyURL)
		switch {
		case c == "":
			note("apply_url %q is not http(s): dropped", r.ApplyURL)
			r.ApplyURL, dropped = "", true
		case !known[c]:
			note("apply_url %q is not in the post: dropped", r.ApplyURL)
			r.ApplyURL, dropped = "", true
		}
	}
	// the original is always the post itself, whatever the model wrote
	r.SourceURL = p.URL
	if dropped && r.Confidence > GuessedCap {
		r.Confidence = GuessedCap
	}
	return r, nil
}

func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n-1]) + "…"
	}
	return s
}

// ---------- "is this date written in the post?" ----------

var monthNames = [12][]string{
	{"январ", "қаңтар", "january", "jan"},
	{"феврал", "ақпан", "february", "feb"},
	{"март", "наурыз", "march", "mar"},
	{"апрел", "сәуір", "april", "apr"},
	{"мая", "май", "мамыр", "may"},
	{"июн", "маусым", "june", "jun"},
	{"июл", "шілде", "july", "jul"},
	{"август", "тамыз", "august", "aug"},
	{"сентябр", "қыркүйек", "september", "sept", "sep"},
	{"октябр", "қазан", "october", "oct"},
	{"ноябр", "қараша", "november", "nov"},
	{"декабр", "желтоқсан", "december", "dec"},
}

// DateMentioned reports whether the post text states the date d: ISO (2026-12-05), numeric
// (05.12.2026, 5/12/26, 05.12) or a day number next to the month name in Russian, Kazakh or
// English («5 декабря», «желтоқсанның 5-і», «December 5», «5 Dec»). A year written next to it
// must match.
func DateMentioned(text string, d time.Time) bool {
	t := strings.ToLower(text)
	day, mon, year := d.Day(), int(d.Month()), d.Year()
	if strings.Contains(t, d.Format("2006-01-02")) {
		return true
	}
	num := regexp.MustCompile(`(?:^|[^\d])0?` + strconv.Itoa(day) + `\s?[./]\s?0?` + strconv.Itoa(mon) + `(?:[./](\d{2,4}))?(?:[^\d]|$)`)
	for _, m := range num.FindAllStringSubmatch(t, -1) {
		if m[1] == "" || yearMatches(m[1], year) {
			return true
		}
	}
	dayRe := regexp.MustCompile(`(?:^|[^\d])0?` + strconv.Itoa(day) + `(?:[^\d]|$)`)
	yearRe := regexp.MustCompile(`(?:^|[^\d])(20\d{2})(?:[^\d]|$)`)
	for _, name := range monthNames[mon-1] {
		for start := 0; ; {
			i := strings.Index(t[start:], name)
			if i < 0 {
				break
			}
			i += start
			start = i + len(name)
			// short English abbreviations must start a word ("mar" in "seminar" is not March)
			if len(name) <= 4 && i > 0 {
				if r, _ := utf8.DecodeLastRuneInString(t[:i]); unicode.IsLetter(r) {
					continue
				}
			}
			w := window(t, i, start, 14)
			if !dayRe.MatchString(w) {
				continue
			}
			if y := yearRe.FindStringSubmatch(window(t, i, start, 22)); y != nil && !yearMatches(y[1], year) {
				continue
			}
			return true
		}
	}
	return false
}

func yearMatches(s string, year int) bool {
	n, _ := strconv.Atoi(s)
	if len(s) == 2 {
		n += 2000
	}
	return n == year
}

// window returns t[from-n : to+n] with both ends moved to rune boundaries.
func window(t string, from, to, n int) string {
	a, b := max(0, from-n), min(len(t), to+n)
	for a > 0 && !utf8.RuneStart(t[a]) {
		a--
	}
	for b < len(t) && !utf8.RuneStart(t[b]) {
		b++
	}
	return t[a:b]
}

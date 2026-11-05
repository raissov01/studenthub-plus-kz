package surveys

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// Segment is one breakdown group (a faculty or a course) with at least K responses.
type Segment struct {
	Key       string    `json:"key"` // faculty key, course number, or "other"
	Responses int       `json:"responses"`
	Questions []QResult `json:"questions"`
}

// Results is the staff dashboard. Below K responses in total nothing but the count is returned.
type Results struct {
	Survey    *Survey   `json:"survey"`
	Responses int       `json:"responses"`
	MinGroup  int       `json:"minGroup"`
	Hidden    bool      `json:"hidden"`
	Questions []QResult `json:"questions"`
	By        string    `json:"by,omitempty"`
	Segments  []Segment `json:"segments,omitempty"`
	// BreakdownHidden: the requested breakdown could not keep every group at K or more
	BreakdownHidden bool `json:"breakdownHidden,omitempty"`
}

func loadResponses(ctx context.Context, tx pgx.Tx, surveyID int64) ([]Stored, error) {
	rows, err := tx.Query(ctx, `select answers, coalesce(faculty, ''), coalesce(course, 0) from survey_responses where survey_id = $1`, surveyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Stored, error) {
		var s Stored
		var raw []byte
		if err := row.Scan(&raw, &s.Faculty, &s.Course); err != nil {
			return s, err
		}
		return s, json.Unmarshal(raw, &s.Answers)
	})
}

// BuildResults applies every k-anonymity rule; by is "", "faculty" or "course".
func BuildResults(s *Survey, rs []Stored, by string) Results {
	res := Results{Survey: s, Responses: len(rs), MinGroup: univoice.K, Questions: []QResult{}}
	if len(rs) < univoice.K {
		res.Hidden = true
		for _, q := range s.Questions {
			res.Questions = append(res.Questions, QResult{ID: q.ID, Type: q.Type, Text: q.Text, Hidden: true})
		}
		return res
	}
	res.Questions = Aggregate(s.Questions, rs, true)
	for i := range res.Questions {
		// text answers in random order: the list must not mirror any storage order
		rand.Shuffle(len(res.Questions[i].Texts), func(a, b int) {
			res.Questions[i].Texts[a], res.Questions[i].Texts[b] = res.Questions[i].Texts[b], res.Questions[i].Texts[a]
		})
	}
	if by != "faculty" && by != "course" {
		return res
	}
	res.By = by
	key := func(st Stored) string {
		if by == "faculty" {
			return st.Faculty
		}
		if st.Course == 0 {
			return ""
		}
		return strconv.Itoa(st.Course)
	}
	counts := map[string]int{}
	for _, st := range rs {
		counts[key(st)]++
	}
	to := Suppress(counts)
	groups := map[string][]Stored{}
	for _, st := range rs {
		groups[to[key(st)]] = append(groups[to[key(st)]], st)
	}
	if len(groups) < 2 || len(groups[OtherSegment]) == len(rs) {
		res.BreakdownHidden = true // one group = the total; nothing to break down safely
		return res
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == OtherSegment || keys[j] == OtherSegment {
			return keys[j] == OtherSegment && keys[i] != OtherSegment
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		res.Segments = append(res.Segments, Segment{Key: k, Responses: len(groups[k]), Questions: Aggregate(s.Questions, groups[k], false)})
	}
	// a question is broken down only if EVERY group has K answers to it; otherwise the total minus
	// the visible groups would expose the small one. Text answers are never broken down.
	for qi, q := range s.Questions {
		hide := q.Type == TypeText
		for _, seg := range res.Segments {
			hide = hide || seg.Questions[qi].Hidden
		}
		if hide {
			for si := range res.Segments {
				res.Segments[si].Questions[qi] = QResult{ID: q.ID, Type: q.Type, Text: q.Text, Hidden: true}
			}
		}
	}
	return res
}

func staffResults(w http.ResponseWriter, r *http.Request, u *auth.User) {
	by := r.URL.Query().Get("by")
	withOwn(w, r, u, false, func(tx pgx.Tx, s *Survey) error {
		rs, err := loadResponses(r.Context(), tx, s.ID)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, BuildResults(s, rs, by))
		return nil
	})
}

// staffCSV: aggregates only — one row per question × option (or matrix row × value), never a
// single response, never text answers. Questions under K answers are a «hidden» row.
func staffCSV(w http.ResponseWriter, r *http.Request, u *auth.User) {
	withOwn(w, r, u, false, func(tx pgx.Tx, s *Survey) error {
		rs, err := loadResponses(r.Context(), tx, s.ID)
		if err != nil {
			return err
		}
		res := BuildResults(s, rs, "")
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="survey-`+strconv.FormatInt(s.ID, 10)+`-aggregates.csv"`)
		w.Write([]byte("\xEF\xBB\xBF")) // Excel opens UTF-8 with a BOM
		c := csv.NewWriter(w)
		_ = c.Write([]string{"question_id", "question", "row", "answer", "count", "answered", "mean"})
		_ = c.Write([]string{"_total", "responses", "", "", countCell(res.Responses), "", ""})
		for _, q := range res.Questions {
			if q.Hidden {
				_ = c.Write([]string{q.ID, safe(q.Text), "", "hidden (<" + strconv.Itoa(univoice.K) + ")", "", countCell(q.Answered), ""})
				continue
			}
			mean := ""
			if q.Mean != nil {
				mean = strconv.FormatFloat(*q.Mean, 'f', 1, 64)
			}
			switch q.Type {
			case TypeText:
				_ = c.Write([]string{q.ID, safe(q.Text), "", "text answers (not exported)", "", strconv.Itoa(q.Answered), ""})
			case TypeMatrix:
				for ri, row := range q.Rows {
					for vi, n := range q.Matrix[ri] {
						_ = c.Write([]string{q.ID, safe(q.Text), safe(row), q.Labels[vi], strconv.Itoa(n), strconv.Itoa(q.Answered), strconv.FormatFloat(q.RowMeans[ri], 'f', 1, 64)})
					}
				}
			default:
				for i, n := range q.Counts {
					_ = c.Write([]string{q.ID, safe(q.Text), "", safe(q.Labels[i]), strconv.Itoa(n), strconv.Itoa(q.Answered), mean})
				}
			}
		}
		c.Flush()
		return c.Error()
	})
}

func countCell(n int) string {
	if n > 0 && n < univoice.K {
		return "< 5"
	}
	return strconv.Itoa(n)
}

// safe neutralizes spreadsheet formula injection in staff-written text.
func safe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@' || s[0] == '\t' || s[0] == '\r') {
		return "'" + s
	}
	return s
}

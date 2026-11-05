package surveys

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// Question types.
const (
	TypeSingle   = "single"   // one option
	TypeMultiple = "multiple" // several options
	TypeScale    = "scale"    // 1..5 or 1..10
	TypeText     = "text"     // short free text
	TypeMatrix   = "matrix"   // rows × the same 1..5 / 1..10 scale
)

const (
	MaxQuestions = 40
	MaxOptions   = 12
	MaxRows      = 10
	MaxTextLen   = 500
)

// Question is one survey question as staff write it.
type Question struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Text     string   `json:"text"`
	Help     string   `json:"help,omitempty"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`  // single, multiple
	Max      int      `json:"max,omitempty"`      // scale, matrix: 5 or 10 (min is always 1)
	MinLabel string   `json:"minLabel,omitempty"` // scale/matrix anchors
	MaxLabel string   `json:"maxLabel,omitempty"`
	Rows     []string `json:"rows,omitempty"` // matrix
}

// Audience narrows who sees a survey inside its university. Every empty list means everyone.
type Audience struct {
	Faculties []string `json:"faculties"`
	Courses   []int    `json:"courses"`
	Groups    []string `json:"groups"`
}

var (
	reQID     = regexp.MustCompile(`^[a-z0-9_-]{1,20}$`)
	reFaculty = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
)

// ValidateQuestions checks and normalizes the question list; the error names the problem.
func ValidateQuestions(qs []Question) ([]Question, error) {
	if len(qs) == 0 || len(qs) > MaxQuestions {
		return nil, fmt.Errorf("questions: 1–%d", MaxQuestions)
	}
	seen := map[string]bool{}
	out := make([]Question, 0, len(qs))
	for i, q := range qs {
		if q.ID == "" {
			q.ID = "q" + strconv.Itoa(i+1)
		}
		if !reQID.MatchString(q.ID) || seen[q.ID] {
			return nil, fmt.Errorf("question %d: id", i+1)
		}
		seen[q.ID] = true
		var ok bool
		if q.Text, ok = univoice.Clean(q.Text, 3, 300, false); !ok {
			return nil, fmt.Errorf("question %d: text 3–300", i+1)
		}
		if q.Help, ok = univoice.Clean(q.Help, 0, 300, false); !ok {
			return nil, fmt.Errorf("question %d: help ≤300", i+1)
		}
		cleanList := func(in []string, min, max int) ([]string, error) {
			if len(in) < min || len(in) > max {
				return nil, fmt.Errorf("question %d: %d–%d items", i+1, min, max)
			}
			res := make([]string, len(in))
			dup := map[string]bool{}
			for j, s := range in {
				c, ok := univoice.Clean(s, 1, 120, false)
				if !ok || dup[strings.ToLower(c)] {
					return nil, fmt.Errorf("question %d: item %d", i+1, j+1)
				}
				dup[strings.ToLower(c)] = true
				res[j] = c
			}
			return res, nil
		}
		var err error
		switch q.Type {
		case TypeSingle, TypeMultiple:
			if q.Options, err = cleanList(q.Options, 2, MaxOptions); err != nil {
				return nil, err
			}
			q.Max, q.MinLabel, q.MaxLabel, q.Rows = 0, "", "", nil
		case TypeScale, TypeMatrix:
			if q.Max != 5 && q.Max != 10 {
				return nil, fmt.Errorf("question %d: scale 1–5 or 1–10", i+1)
			}
			if q.MinLabel, ok = univoice.Clean(q.MinLabel, 0, 40, false); !ok {
				return nil, fmt.Errorf("question %d: label ≤40", i+1)
			}
			if q.MaxLabel, ok = univoice.Clean(q.MaxLabel, 0, 40, false); !ok {
				return nil, fmt.Errorf("question %d: label ≤40", i+1)
			}
			q.Options = nil
			if q.Type == TypeMatrix {
				if q.Rows, err = cleanList(q.Rows, 2, MaxRows); err != nil {
					return nil, err
				}
			} else {
				q.Rows = nil
			}
		case TypeText:
			q.Options, q.Rows, q.Max, q.MinLabel, q.MaxLabel = nil, nil, 0, "", ""
		default:
			return nil, fmt.Errorf("question %d: type", i+1)
		}
		out = append(out, q)
	}
	return out, nil
}

// ValidateAudience normalizes the targeting lists.
func ValidateAudience(a Audience) (Audience, error) {
	out := Audience{Faculties: []string{}, Courses: []int{}, Groups: []string{}}
	if len(a.Faculties) > 20 || len(a.Courses) > 6 || len(a.Groups) > 50 {
		return out, fmt.Errorf("audience: too many")
	}
	for _, f := range a.Faculties {
		f = strings.TrimSpace(f)
		if !reFaculty.MatchString(f) {
			return out, fmt.Errorf("audience: faculty")
		}
		if !slices.Contains(out.Faculties, f) {
			out.Faculties = append(out.Faculties, f)
		}
	}
	for _, c := range a.Courses {
		if c < 1 || c > 6 {
			return out, fmt.Errorf("audience: course")
		}
		if !slices.Contains(out.Courses, c) {
			out.Courses = append(out.Courses, c)
		}
	}
	for _, g := range a.Groups {
		g, ok := univoice.Clean(strings.ToUpper(g), 1, 24, false)
		if !ok {
			return out, fmt.Errorf("audience: group")
		}
		if !slices.Contains(out.Groups, g) {
			out.Groups = append(out.Groups, g)
		}
	}
	sort.Strings(out.Faculties)
	sort.Ints(out.Courses)
	sort.Strings(out.Groups)
	return out, nil
}

// Answers maps a question id to its answer: single → option index, multiple → []index,
// scale → 1..max, text → string, matrix → []value per row (0 = row skipped).
type Answers map[string]json.RawMessage

// ValidateAnswers checks a submission against the questions and returns the normalized answers
// that will be stored (unanswered optional questions are left out).
func ValidateAnswers(qs []Question, in Answers) (map[string]any, error) {
	known := map[string]bool{}
	out := map[string]any{}
	for _, q := range qs {
		known[q.ID] = true
		raw, present := in[q.ID]
		if !present || string(raw) == "null" {
			if q.Required {
				return nil, fmt.Errorf("%s: required", q.ID)
			}
			continue
		}
		switch q.Type {
		case TypeSingle:
			var v int
			if json.Unmarshal(raw, &v) != nil || v < 0 || v >= len(q.Options) {
				return nil, fmt.Errorf("%s: option", q.ID)
			}
			out[q.ID] = v
		case TypeMultiple:
			var v []int
			if json.Unmarshal(raw, &v) != nil || len(v) > len(q.Options) {
				return nil, fmt.Errorf("%s: options", q.ID)
			}
			seen := map[int]bool{}
			for _, x := range v {
				if x < 0 || x >= len(q.Options) || seen[x] {
					return nil, fmt.Errorf("%s: options", q.ID)
				}
				seen[x] = true
			}
			if len(v) == 0 {
				if q.Required {
					return nil, fmt.Errorf("%s: required", q.ID)
				}
				continue
			}
			sort.Ints(v)
			out[q.ID] = v
		case TypeScale:
			var v int
			if json.Unmarshal(raw, &v) != nil || v < 1 || v > q.Max {
				return nil, fmt.Errorf("%s: scale", q.ID)
			}
			out[q.ID] = v
		case TypeText:
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, fmt.Errorf("%s: text", q.ID)
			}
			c, ok := univoice.Clean(v, 0, MaxTextLen, true)
			if !ok {
				return nil, fmt.Errorf("%s: text ≤%d", q.ID, MaxTextLen)
			}
			if c == "" {
				if q.Required {
					return nil, fmt.Errorf("%s: required", q.ID)
				}
				continue
			}
			out[q.ID] = c
		case TypeMatrix:
			var v []int
			if json.Unmarshal(raw, &v) != nil || len(v) != len(q.Rows) {
				return nil, fmt.Errorf("%s: matrix", q.ID)
			}
			any := false
			for _, x := range v {
				if x < 0 || x > q.Max {
					return nil, fmt.Errorf("%s: matrix", q.ID)
				}
				if x == 0 && q.Required {
					return nil, fmt.Errorf("%s: required", q.ID)
				}
				any = any || x > 0
			}
			if !any {
				continue
			}
			out[q.ID] = v
		}
	}
	for id := range in {
		if !known[id] {
			return nil, fmt.Errorf("%s: unknown question", id)
		}
	}
	return out, nil
}

// ---------- results ----------

// Stored is one response as the results code reads it.
type Stored struct {
	Answers map[string]json.RawMessage
	Faculty string
	Course  int
}

// QResult is the aggregate of one question. Hidden: fewer than K people answered it, so nothing
// but the count is shown.
type QResult struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Text     string    `json:"text"`
	Answered int       `json:"answered"`
	Hidden   bool      `json:"hidden"`
	Labels   []string  `json:"labels,omitempty"` // options, or "1".."max"
	Counts   []int     `json:"counts,omitempty"` // per label
	Rows     []string  `json:"rows,omitempty"`   // matrix
	Matrix   [][]int   `json:"matrix,omitempty"` // per row, per value
	RowMeans []float64 `json:"rowMeans,omitempty"`
	Mean     *float64  `json:"mean,omitempty"`  // scale
	Texts    []string  `json:"texts,omitempty"` // text answers, shuffled
}

// Aggregate builds per-question results; texts are included only when withTexts.
func Aggregate(qs []Question, rs []Stored, withTexts bool) []QResult {
	out := make([]QResult, 0, len(qs))
	for _, q := range qs {
		res := QResult{ID: q.ID, Type: q.Type, Text: q.Text}
		switch q.Type {
		case TypeSingle, TypeMultiple:
			res.Labels = q.Options
			res.Counts = make([]int, len(q.Options))
		case TypeScale:
			res.Labels = scaleLabels(q.Max)
			res.Counts = make([]int, q.Max)
		case TypeMatrix:
			res.Labels = scaleLabels(q.Max)
			res.Rows = q.Rows
			res.Matrix = make([][]int, len(q.Rows))
			for i := range res.Matrix {
				res.Matrix[i] = make([]int, q.Max)
			}
		}
		sum := 0
		rowSum, rowN := make([]int, len(q.Rows)), make([]int, len(q.Rows))
		for _, r := range rs {
			raw, ok := r.Answers[q.ID]
			if !ok {
				continue
			}
			switch q.Type {
			case TypeSingle:
				var v int
				if json.Unmarshal(raw, &v) == nil && v >= 0 && v < len(res.Counts) {
					res.Counts[v]++
					res.Answered++
				}
			case TypeMultiple:
				var v []int
				if json.Unmarshal(raw, &v) == nil {
					for _, x := range v {
						if x >= 0 && x < len(res.Counts) {
							res.Counts[x]++
						}
					}
					res.Answered++
				}
			case TypeScale:
				var v int
				if json.Unmarshal(raw, &v) == nil && v >= 1 && v <= q.Max {
					res.Counts[v-1]++
					sum += v
					res.Answered++
				}
			case TypeText:
				var v string
				if json.Unmarshal(raw, &v) == nil && v != "" {
					res.Answered++
					if withTexts {
						res.Texts = append(res.Texts, v)
					}
				}
			case TypeMatrix:
				var v []int
				if json.Unmarshal(raw, &v) == nil && len(v) == len(q.Rows) {
					for i, x := range v {
						if x >= 1 && x <= q.Max {
							res.Matrix[i][x-1]++
							rowSum[i] += x
							rowN[i]++
						}
					}
					res.Answered++
				}
			}
		}
		if res.Answered < univoice.K {
			res = QResult{ID: q.ID, Type: q.Type, Text: q.Text, Answered: res.Answered, Hidden: true}
		} else {
			if q.Type == TypeScale {
				m := round1(float64(sum) / float64(res.Answered))
				res.Mean = &m
			}
			if q.Type == TypeMatrix {
				res.RowMeans = make([]float64, len(q.Rows))
				for i := range rowN {
					if rowN[i] > 0 {
						res.RowMeans[i] = round1(float64(rowSum[i]) / float64(rowN[i]))
					}
				}
			}
		}
		out = append(out, res)
	}
	return out
}

func scaleLabels(max int) []string {
	out := make([]string, max)
	for i := range out {
		out[i] = strconv.Itoa(i + 1)
	}
	return out
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// OtherSegment collects every segment too small to show on its own.
const OtherSegment = "other"

// Suppress maps each segment to the segment it is shown under. A segment with fewer than K
// responses goes to "other"; when "other" itself ends up with 1..K-1 responses, the smallest
// visible segments join it until it reaches K (complementary suppression), because otherwise
// "total minus the visible segments" would reveal the hidden group exactly. If even that cannot
// reach K, every segment is "other" and the caller shows no breakdown.
func Suppress(counts map[string]int) map[string]string {
	to := map[string]string{}
	other := 0
	var visible []string
	for seg, n := range counts {
		if n <= 0 {
			continue
		}
		if n < univoice.K || seg == OtherSegment || seg == "" {
			to[seg] = OtherSegment
			other += n
		} else {
			to[seg] = seg
			visible = append(visible, seg)
		}
	}
	sort.Slice(visible, func(i, j int) bool {
		if counts[visible[i]] != counts[visible[j]] {
			return counts[visible[i]] < counts[visible[j]]
		}
		return visible[i] < visible[j]
	})
	for other > 0 && other < univoice.K && len(visible) > 0 {
		s := visible[0]
		visible = visible[1:]
		to[s] = OtherSegment
		other += counts[s]
	}
	return to
}

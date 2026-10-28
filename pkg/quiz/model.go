package quiz

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Question types (ported from the reference quiz module). Not ported: drawing, voice_response
// and video — they store student recordings / external video and are out of scope here.
const (
	TMCQ            = "mcq"
	TMulti          = "mcq_multi"
	TTrueFalse      = "true_false"
	TFill           = "fill_blank"
	TReorder        = "reorder"
	TMatching       = "matching"
	TCategorization = "categorization"
	TDropdown       = "dropdown"
	TPoll           = "poll"
	THotspot        = "hotspot"
	TLabeling       = "labeling"
	TComprehension  = "comprehension"
)

var Types = []string{TMCQ, TMulti, TTrueFalse, TFill, TReorder, TMatching, TCategorization, TDropdown,
	TPoll, THotspot, TLabeling, TComprehension}

const (
	MaxQuestions   = 200
	maxPrompt      = 1000
	maxOption      = 200
	maxExplanation = 1000
	maxQuote       = 600
	maxPassage     = 5000
)

// Pair is a matching pair (left ↔ right) or, for categorization, item → category.
type Pair struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

// Zone is a rectangle on the question image in percent of its width/height.
type Zone struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	Label string  `json:"label,omitempty"` // labeling: the correct label
}

// SubQuestion is one question about a comprehension passage (mcq, true_false or fill_blank).
type SubQuestion struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Prompt       string   `json:"prompt"`
	Options      []string `json:"options,omitempty"`
	CorrectIndex *int     `json:"correctIndex,omitempty"`
	CorrectBool  *bool    `json:"correctBool,omitempty"`
	Answers      []string `json:"answers,omitempty"`
}

// Question is the full (owner/editor) form of a question, including the answer key.
type Question struct {
	ID             int64         `json:"id,omitempty"`
	Type           string        `json:"type"`
	Prompt         string        `json:"prompt"`
	Options        []string      `json:"options,omitempty"`
	CorrectIndex   *int          `json:"correctIndex,omitempty"`
	CorrectIndexes []int         `json:"correctIndexes,omitempty"`
	CorrectBool    *bool         `json:"correctBool,omitempty"`
	Answers        []string      `json:"answers,omitempty"`
	Items          []string      `json:"items,omitempty"`
	Pairs          []Pair        `json:"pairs,omitempty"`
	Zones          []Zone        `json:"zones,omitempty"`
	CorrectZone    string        `json:"correctZone,omitempty"`
	Passage        string        `json:"passage,omitempty"`
	SubQuestions   []SubQuestion `json:"subQuestions,omitempty"`
	Explanation    string        `json:"explanation,omitempty"`
	SourceQuote    string        `json:"sourceQuote,omitempty"`
	Image          string        `json:"image,omitempty"`
	Audio          string        `json:"audio,omitempty"`
	TimeLimit      int           `json:"timeLimit,omitempty"`
}

// keyOf is what goes into quiz_answer_keys.answer: the type-specific spec (options, items,
// pairs, ... and the correct answer). Prompt, media and texts have their own columns.
func keyOf(q Question) Question {
	k := q
	k.ID, k.Prompt, k.Explanation, k.SourceQuote, k.Image, k.Audio, k.TimeLimit = 0, "", "", "", "", "", 0
	return k
}

// PlayView is what a player sees: no correct answers, orders that would give the answer away
// are shuffled. Answers refer to option indexes of this view (options keep the stored order).
type PlayView struct {
	Options      []string      `json:"options,omitempty"`
	Multi        bool          `json:"multi,omitempty"`
	Items        []string      `json:"items,omitempty"`  // reorder (shuffled), categorization items
	Lefts        []string      `json:"lefts,omitempty"`  // matching
	Rights       []string      `json:"rights,omitempty"` // matching (shuffled)
	Categories   []string      `json:"categories,omitempty"`
	Zones        []Zone        `json:"zones,omitempty"`  // without labels
	Labels       []string      `json:"labels,omitempty"` // labeling word bank (shuffled)
	Passage      string        `json:"passage,omitempty"`
	SubQuestions []SubQuestion `json:"subQuestions,omitempty"` // without answers
}

func shuffled(in []string) []string {
	out := slices.Clone(in)
	if len(out) < 2 {
		return out
	}
	for range 5 {
		rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		if !slices.Equal(out, in) {
			break
		}
	}
	return out
}

func uniq(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// viewOf builds the stored player view (quiz_questions.data).
func viewOf(q Question) PlayView {
	v := PlayView{}
	switch q.Type {
	case TMCQ, TDropdown, TPoll:
		v.Options = q.Options
	case TMulti:
		v.Options, v.Multi = q.Options, true
	case TReorder:
		v.Items = shuffled(q.Items)
	case TMatching:
		for _, p := range q.Pairs {
			v.Lefts = append(v.Lefts, p.Left)
			v.Rights = append(v.Rights, p.Right)
		}
		v.Rights = shuffled(v.Rights)
	case TCategorization:
		var items, cats []string
		for _, p := range q.Pairs {
			items = append(items, p.Left)
			cats = append(cats, p.Right)
		}
		v.Items, v.Categories = shuffled(items), uniq(cats)
	case THotspot, TLabeling:
		for _, z := range q.Zones {
			v.Zones = append(v.Zones, Zone{ID: z.ID, X: z.X, Y: z.Y, W: z.W, H: z.H})
			if q.Type == TLabeling {
				v.Labels = append(v.Labels, z.Label)
			}
		}
		v.Labels = shuffled(v.Labels)
	case TComprehension:
		v.Passage = q.Passage
		for _, s := range q.SubQuestions {
			v.SubQuestions = append(v.SubQuestions, SubQuestion{ID: s.ID, Type: s.Type, Prompt: s.Prompt, Options: s.Options})
		}
	}
	return v
}

// ValidationError names the offending question (1-based) and a machine code.
type ValidationError struct {
	Index int    `json:"question"`
	Code  string `json:"code"`
}

func (e *ValidationError) Error() string { return fmt.Sprintf("question %d: %s", e.Index, e.Code) }

var (
	reZoneID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,16}$`)
	reImage  = regexp.MustCompile(`^[0-9a-f]{32}\.jpg$`)
	reAudio  = regexp.MustCompile(`^[0-9a-f]{32}\.mp3$`)
	spaces   = regexp.MustCompile(`\s+`)
)

// clean trims, collapses inner whitespace runs for single-line fields and refuses invalid UTF-8
// and control characters.
func clean(s string, multiline bool) string {
	if !utf8.ValidString(s) {
		return "\x00"
	}
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if !multiline {
		s = spaces.ReplaceAllString(s, " ")
	}
	return strings.TrimSpace(s)
}

func runes(s string) int { return utf8.RuneCountInString(s) }

func cleanList(in []string, maxLen int) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = clean(s, false)
		if s == "" || s == "\x00" || runes(s) > maxLen {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func distinctFold(l []string) bool {
	seen := map[string]bool{}
	for _, s := range l {
		k := strings.ToLower(s)
		if seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}

// Normalize validates one question and returns its cleaned form. index is 1-based (for errors).
func Normalize(q Question, index int) (Question, error) {
	bad := func(code string) (Question, error) { return Question{}, &ValidationError{index, code} }
	out := Question{ID: q.ID, Type: strings.ToLower(strings.TrimSpace(q.Type))}
	if out.Type == "" {
		out.Type = TMCQ
	}
	if !slices.Contains(Types, out.Type) {
		return bad("bad_type")
	}
	out.Prompt = clean(q.Prompt, true)
	if out.Prompt == "" || out.Prompt == "\x00" || runes(out.Prompt) > maxPrompt {
		return bad("bad_prompt")
	}
	out.Explanation = clean(q.Explanation, true)
	if out.Explanation == "\x00" || runes(out.Explanation) > maxExplanation {
		return bad("bad_explanation")
	}
	out.SourceQuote = clean(q.SourceQuote, false)
	if out.SourceQuote == "\x00" || runes(out.SourceQuote) > maxQuote {
		return bad("bad_source_quote")
	}
	if q.Image != "" && !reImage.MatchString(q.Image) {
		return bad("bad_image")
	}
	if q.Audio != "" && !reAudio.MatchString(q.Audio) {
		return bad("bad_audio")
	}
	out.Image, out.Audio = q.Image, q.Audio
	if q.TimeLimit != 0 && (q.TimeLimit < 5 || q.TimeLimit > 300) {
		return bad("bad_time_limit")
	}
	out.TimeLimit = q.TimeLimit

	options := func(minN int) bool {
		if len(q.Options) < minN || len(q.Options) > 6 {
			return false
		}
		o, ok := cleanList(q.Options, maxOption)
		if !ok || !distinctFold(o) {
			return false
		}
		out.Options = o
		return true
	}
	switch out.Type {
	case TMCQ, TDropdown:
		if !options(2) {
			return bad("bad_options")
		}
		if q.CorrectIndex == nil || *q.CorrectIndex < 0 || *q.CorrectIndex >= len(out.Options) {
			return bad("bad_correct")
		}
		out.CorrectIndex = q.CorrectIndex
	case TPoll:
		if !options(2) {
			return bad("bad_options")
		}
	case TMulti:
		if !options(3) {
			return bad("bad_options")
		}
		idx := slices.Clone(q.CorrectIndexes)
		slices.Sort(idx)
		idx = slices.Compact(idx)
		if len(idx) < 1 || len(idx) != len(q.CorrectIndexes) || idx[0] < 0 || idx[len(idx)-1] >= len(out.Options) {
			return bad("bad_correct")
		}
		out.CorrectIndexes = idx
	case TTrueFalse:
		if q.CorrectBool == nil {
			return bad("bad_correct")
		}
		out.CorrectBool = q.CorrectBool
	case TFill:
		a, ok := cleanList(q.Answers, 100)
		if !ok || len(a) < 1 || len(a) > 5 {
			return bad("bad_answers")
		}
		out.Answers = a
	case TReorder:
		it, ok := cleanList(q.Items, maxOption)
		if !ok || len(it) < 2 || len(it) > 10 || !distinctFold(it) {
			return bad("bad_items")
		}
		out.Items = it
	case TMatching, TCategorization:
		maxN := 8
		if out.Type == TCategorization {
			maxN = 12
		}
		if len(q.Pairs) < 2 || len(q.Pairs) > maxN {
			return bad("bad_pairs")
		}
		var lefts, rights []string
		for _, p := range q.Pairs {
			l, r := clean(p.Left, false), clean(p.Right, false)
			if l == "" || r == "" || l == "\x00" || r == "\x00" || runes(l) > maxOption || runes(r) > maxOption {
				return bad("bad_pairs")
			}
			lefts, rights = append(lefts, l), append(rights, r)
			out.Pairs = append(out.Pairs, Pair{l, r})
		}
		if !distinctFold(lefts) {
			return bad("bad_pairs")
		}
		if out.Type == TMatching && !distinctFold(rights) {
			return bad("bad_pairs")
		}
		if out.Type == TCategorization {
			if n := len(uniq(rights)); n < 2 || n > 4 {
				return bad("bad_categories")
			}
		}
	case THotspot, TLabeling:
		if out.Image == "" {
			return bad("image_required")
		}
		minZ := 1
		if out.Type == TLabeling {
			minZ = 2
		}
		if len(q.Zones) < minZ || len(q.Zones) > 10 {
			return bad("bad_zones")
		}
		var ids, labels []string
		for _, z := range q.Zones {
			if !reZoneID.MatchString(z.ID) || slices.Contains(ids, z.ID) ||
				z.X < 0 || z.Y < 0 || z.W <= 0 || z.H <= 0 || z.X+z.W > 100.0001 || z.Y+z.H > 100.0001 {
				return bad("bad_zones")
			}
			ids = append(ids, z.ID)
			nz := Zone{ID: z.ID, X: z.X, Y: z.Y, W: z.W, H: z.H}
			if out.Type == TLabeling {
				nz.Label = clean(z.Label, false)
				if nz.Label == "" || nz.Label == "\x00" || runes(nz.Label) > 60 {
					return bad("bad_zones")
				}
				labels = append(labels, nz.Label)
			}
			out.Zones = append(out.Zones, nz)
		}
		if out.Type == TLabeling && !distinctFold(labels) {
			return bad("bad_zones")
		}
		if out.Type == THotspot {
			if !slices.Contains(ids, q.CorrectZone) {
				return bad("bad_correct")
			}
			out.CorrectZone = q.CorrectZone
		}
	case TComprehension:
		out.Passage = clean(q.Passage, true)
		if out.Passage == "" || out.Passage == "\x00" || runes(out.Passage) > maxPassage {
			return bad("bad_passage")
		}
		if len(q.SubQuestions) < 1 || len(q.SubQuestions) > 10 {
			return bad("bad_sub_questions")
		}
		var ids []string
		for i, s := range q.SubQuestions {
			ns := SubQuestion{ID: s.ID, Type: s.Type, Prompt: clean(s.Prompt, true)}
			if ns.ID == "" {
				ns.ID = fmt.Sprintf("s%d", i+1)
			}
			if !reZoneID.MatchString(ns.ID) || slices.Contains(ids, ns.ID) || ns.Prompt == "" || ns.Prompt == "\x00" || runes(ns.Prompt) > maxPrompt {
				return bad("bad_sub_questions")
			}
			ids = append(ids, ns.ID)
			switch ns.Type {
			case TMCQ:
				o, ok := cleanList(s.Options, maxOption)
				if !ok || len(o) < 2 || len(o) > 6 || !distinctFold(o) || s.CorrectIndex == nil || *s.CorrectIndex < 0 || *s.CorrectIndex >= len(o) {
					return bad("bad_sub_questions")
				}
				ns.Options, ns.CorrectIndex = o, s.CorrectIndex
			case TTrueFalse:
				if s.CorrectBool == nil {
					return bad("bad_sub_questions")
				}
				ns.CorrectBool = s.CorrectBool
			case TFill:
				a, ok := cleanList(s.Answers, 100)
				if !ok || len(a) < 1 || len(a) > 5 {
					return bad("bad_sub_questions")
				}
				ns.Answers = a
			default:
				return bad("bad_sub_questions")
			}
			out.SubQuestions = append(out.SubQuestions, ns)
		}
	}
	return out, nil
}

// Graded reports whether a question counts towards the score (polls do not).
func Graded(q Question) bool { return q.Type != TPoll }

// normText makes short answers forgiving: case, surrounding/inner whitespace and trailing
// punctuation do not matter.
func normText(s string) string {
	s = strings.ToLower(spaces.ReplaceAllString(strings.TrimSpace(s), " "))
	return strings.TrimRight(s, ".!?;:,")
}

func textMatches(given string, accepted []string) bool {
	g := normText(given)
	if g == "" {
		return false
	}
	for _, a := range accepted {
		if normText(a) == g {
			return true
		}
	}
	return false
}

// Grade scores one answer against the key. given is the raw JSON the player sent (nil = no
// answer). It returns the credit (0..1), whether the answer is fully correct, and the cleaned
// answer to store (nil when the answer is missing or malformed). Client-provided correctness
// or score never reaches this function.
func Grade(q Question, given json.RawMessage) (credit float64, correct bool, stored json.RawMessage) {
	if len(given) == 0 || string(given) == "null" {
		return 0, false, nil
	}
	ok := func(c bool, v any) (float64, bool, json.RawMessage) {
		b, _ := json.Marshal(v)
		if c && Graded(q) {
			return 1, true, b
		}
		return 0, false, b
	}
	switch q.Type {
	case TMCQ, TDropdown, TPoll:
		var i int
		if json.Unmarshal(given, &i) != nil || i < 0 || i >= len(q.Options) {
			return 0, false, nil
		}
		return ok(q.CorrectIndex != nil && i == *q.CorrectIndex, i)
	case TMulti:
		var idx []int
		if json.Unmarshal(given, &idx) != nil || len(idx) > len(q.Options) {
			return 0, false, nil
		}
		slices.Sort(idx)
		idx = slices.Compact(idx)
		for _, i := range idx {
			if i < 0 || i >= len(q.Options) {
				return 0, false, nil
			}
		}
		return ok(slices.Equal(idx, q.CorrectIndexes), idx)
	case TTrueFalse:
		var b bool
		if json.Unmarshal(given, &b) != nil {
			return 0, false, nil
		}
		return ok(q.CorrectBool != nil && b == *q.CorrectBool, b)
	case TFill:
		var s string
		if json.Unmarshal(given, &s) != nil || runes(s) > 200 {
			return 0, false, nil
		}
		s = clean(s, false)
		if s == "" || s == "\x00" {
			return 0, false, nil
		}
		return ok(textMatches(s, q.Answers), s)
	case TReorder:
		var it []string
		if json.Unmarshal(given, &it) != nil || len(it) != len(q.Items) {
			return 0, false, nil
		}
		for i := range it {
			it[i] = clean(it[i], false)
		}
		return ok(slices.Equal(it, q.Items), it)
	case TMatching, TCategorization:
		var m map[string]string
		if json.Unmarshal(given, &m) != nil || len(m) > len(q.Pairs) {
			return 0, false, nil
		}
		all := len(m) == len(q.Pairs)
		for _, p := range q.Pairs {
			if got, has := m[p.Left]; !has || clean(got, false) != p.Right {
				all = false
			}
		}
		return ok(all, m)
	case THotspot:
		var z string
		if json.Unmarshal(given, &z) != nil || !reZoneID.MatchString(z) {
			return 0, false, nil
		}
		return ok(z == q.CorrectZone, z)
	case TLabeling:
		var m map[string]string
		if json.Unmarshal(given, &m) != nil || len(m) > len(q.Zones) {
			return 0, false, nil
		}
		all := len(m) == len(q.Zones)
		for _, z := range q.Zones {
			if got, has := m[z.ID]; !has || !strings.EqualFold(clean(got, false), z.Label) {
				all = false
			}
		}
		return ok(all, m)
	case TComprehension:
		var m map[string]json.RawMessage
		if json.Unmarshal(given, &m) != nil || len(m) > len(q.SubQuestions) {
			return 0, false, nil
		}
		right := 0
		keep := map[string]json.RawMessage{}
		for _, s := range q.SubQuestions {
			raw, has := m[s.ID]
			if !has {
				continue
			}
			sub := Question{Type: s.Type, Options: s.Options, CorrectIndex: s.CorrectIndex, CorrectBool: s.CorrectBool, Answers: s.Answers}
			c, full, st := Grade(sub, raw)
			if st != nil {
				keep[s.ID] = st
			}
			if full && c == 1 {
				right++
			}
		}
		b, _ := json.Marshal(keep)
		credit = float64(right) / float64(len(q.SubQuestions))
		return credit, right == len(q.SubQuestions), b
	}
	return 0, false, nil
}

// RevealKey is the correct answer in the same shape a player submits (for results and the live
// reveal). Polls have none.
func RevealKey(q Question) any {
	switch q.Type {
	case TMCQ, TDropdown:
		return q.CorrectIndex
	case TMulti:
		return q.CorrectIndexes
	case TTrueFalse:
		return q.CorrectBool
	case TFill:
		return q.Answers
	case TReorder:
		return q.Items
	case TMatching, TCategorization:
		m := map[string]string{}
		for _, p := range q.Pairs {
			m[p.Left] = p.Right
		}
		return m
	case THotspot:
		return q.CorrectZone
	case TLabeling:
		m := map[string]string{}
		for _, z := range q.Zones {
			m[z.ID] = z.Label
		}
		return m
	case TComprehension:
		m := map[string]any{}
		for _, s := range q.SubQuestions {
			switch s.Type {
			case TMCQ:
				m[s.ID] = s.CorrectIndex
			case TTrueFalse:
				m[s.ID] = s.CorrectBool
			default:
				m[s.ID] = s.Answers
			}
		}
		return m
	}
	return nil
}

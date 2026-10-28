package quiz

import (
	"encoding/json"
	"strings"
	"testing"
)

func ip(i int) *int                { return &i }
func bp(b bool) *bool              { return &b }
func raw(s string) json.RawMessage { return json.RawMessage(s) }

func norm(t *testing.T, q Question) Question {
	t.Helper()
	n, err := Normalize(q, 1)
	if err != nil {
		t.Fatalf("normalize %s: %v", q.Type, err)
	}
	return n
}

func TestGradeEveryType(t *testing.T) {
	img := strings.Repeat("a", 32) + ".jpg"
	mcq := norm(t, Question{Type: TMCQ, Prompt: "2+2?", Options: []string{"3", "4", "5"}, CorrectIndex: ip(1)})
	multi := norm(t, Question{Type: TMulti, Prompt: "primes", Options: []string{"2", "4", "7", "9"}, CorrectIndexes: []int{2, 0}})
	tf := norm(t, Question{Type: TTrueFalse, Prompt: "sky is blue", CorrectBool: bp(true)})
	fill := norm(t, Question{Type: TFill, Prompt: "H2O is", Answers: []string{"Water", "су"}})
	reorder := norm(t, Question{Type: TReorder, Prompt: "order", Items: []string{"one", "two", "three"}})
	match := norm(t, Question{Type: TMatching, Prompt: "match", Pairs: []Pair{{"KZ", "Astana"}, {"FR", "Paris"}}})
	cat := norm(t, Question{Type: TCategorization, Prompt: "sort", Pairs: []Pair{{"cat", "animal"}, {"rose", "plant"}, {"dog", "animal"}}})
	drop := norm(t, Question{Type: TDropdown, Prompt: "pick", Options: []string{"a", "b"}, CorrectIndex: ip(0)})
	poll := norm(t, Question{Type: TPoll, Prompt: "fav?", Options: []string{"x", "y"}})
	hot := norm(t, Question{Type: THotspot, Prompt: "click heart", Image: img, Zones: []Zone{{ID: "h", X: 10, Y: 10, W: 10, H: 10}, {ID: "l", X: 50, Y: 50, W: 10, H: 10}}, CorrectZone: "h"})
	label := norm(t, Question{Type: TLabeling, Prompt: "label", Image: img, Zones: []Zone{{ID: "1", X: 0, Y: 0, W: 10, H: 10, Label: "Heart"}, {ID: "2", X: 20, Y: 20, W: 10, H: 10, Label: "Liver"}}})
	comp := norm(t, Question{Type: TComprehension, Prompt: "read", Passage: "Abai was born in 1845.", SubQuestions: []SubQuestion{
		{ID: "a", Type: TMCQ, Prompt: "year?", Options: []string{"1845", "1900"}, CorrectIndex: ip(0)},
		{ID: "b", Type: TTrueFalse, Prompt: "poet?", CorrectBool: bp(true)},
	}})

	cases := []struct {
		name    string
		q       Question
		given   string
		credit  float64
		correct bool
		stored  bool
	}{
		{"mcq right", mcq, `1`, 1, true, true},
		{"mcq wrong", mcq, `0`, 0, false, true},
		{"mcq out of range", mcq, `7`, 0, false, false},
		{"mcq string", mcq, `"1"`, 0, false, false},
		{"mcq missing", mcq, ``, 0, false, false},
		{"mcq null", mcq, `null`, 0, false, false},
		{"multi right any order with dupes", multi, `[2,0,2]`, 1, true, true},
		{"multi subset", multi, `[0]`, 0, false, true},
		{"multi superset", multi, `[0,1,2]`, 0, false, true},
		{"multi bad index", multi, `[0,9]`, 0, false, false},
		{"tf right", tf, `true`, 1, true, true},
		{"tf wrong", tf, `false`, 0, false, true},
		{"tf string", tf, `"true"`, 0, false, false},
		{"fill case/space/punct", fill, `"  water. "`, 1, true, true},
		{"fill alt answer", fill, `"СУ"`, 1, true, true},
		{"fill wrong", fill, `"ice"`, 0, false, true},
		{"fill empty", fill, `"   "`, 0, false, false},
		{"reorder right", reorder, `["one","two","three"]`, 1, true, true},
		{"reorder wrong", reorder, `["two","one","three"]`, 0, false, true},
		{"reorder short", reorder, `["one","two"]`, 0, false, false},
		{"match right", match, `{"KZ":"Astana","FR":"Paris"}`, 1, true, true},
		{"match swapped", match, `{"KZ":"Paris","FR":"Astana"}`, 0, false, true},
		{"match missing", match, `{"KZ":"Astana"}`, 0, false, true},
		{"cat right", cat, `{"cat":"animal","rose":"plant","dog":"animal"}`, 1, true, true},
		{"cat wrong", cat, `{"cat":"plant","rose":"plant","dog":"animal"}`, 0, false, true},
		{"dropdown right", drop, `0`, 1, true, true},
		{"poll never scores", poll, `0`, 0, false, true},
		{"hotspot right", hot, `"h"`, 1, true, true},
		{"hotspot wrong", hot, `"l"`, 0, false, true},
		{"hotspot junk", hot, `"<script>"`, 0, false, false},
		{"labeling right ci", label, `{"1":"heart","2":"LIVER"}`, 1, true, true},
		{"labeling wrong", label, `{"1":"Liver","2":"Heart"}`, 0, false, true},
		{"comprehension full", comp, `{"a":0,"b":true}`, 1, true, true},
		{"comprehension half", comp, `{"a":0,"b":false}`, 0.5, false, true},
		{"comprehension none", comp, `{}`, 0, false, true},
	}
	for _, c := range cases {
		credit, correct, stored := Grade(c.q, raw(c.given))
		if credit != c.credit || correct != c.correct || (stored != nil) != c.stored {
			t.Errorf("%s: credit=%v correct=%v stored=%s", c.name, credit, correct, stored)
		}
	}
	if Graded(poll) || !Graded(mcq) {
		t.Fatal("graded flags")
	}
}

func TestNormalizeRejects(t *testing.T) {
	img := strings.Repeat("b", 32) + ".jpg"
	bad := map[string]Question{
		"unknown type":       {Type: "drawing", Prompt: "x"},
		"empty prompt":       {Type: TMCQ, Prompt: "  ", Options: []string{"a", "b"}, CorrectIndex: ip(0)},
		"one option":         {Type: TMCQ, Prompt: "x", Options: []string{"a"}, CorrectIndex: ip(0)},
		"dup options":        {Type: TMCQ, Prompt: "x", Options: []string{"a", "A"}, CorrectIndex: ip(0)},
		"correct oob":        {Type: TMCQ, Prompt: "x", Options: []string{"a", "b"}, CorrectIndex: ip(2)},
		"no correct":         {Type: TMCQ, Prompt: "x", Options: []string{"a", "b"}},
		"multi dup correct":  {Type: TMulti, Prompt: "x", Options: []string{"a", "b", "c"}, CorrectIndexes: []int{1, 1}},
		"tf nil":             {Type: TTrueFalse, Prompt: "x"},
		"fill none":          {Type: TFill, Prompt: "x"},
		"reorder 1":          {Type: TReorder, Prompt: "x", Items: []string{"a"}},
		"matching dup right": {Type: TMatching, Prompt: "x", Pairs: []Pair{{"a", "1"}, {"b", "1"}}},
		"cat one category":   {Type: TCategorization, Prompt: "x", Pairs: []Pair{{"a", "1"}, {"b", "1"}}},
		"hotspot no image":   {Type: THotspot, Prompt: "x", Zones: []Zone{{ID: "a", W: 1, H: 1}}, CorrectZone: "a"},
		"hotspot outside":    {Type: THotspot, Prompt: "x", Image: img, Zones: []Zone{{ID: "a", X: 95, W: 10, H: 1}}, CorrectZone: "a"},
		"bad image name":     {Type: TTrueFalse, Prompt: "x", CorrectBool: bp(true), Image: "../etc/passwd"},
		"time limit":         {Type: TTrueFalse, Prompt: "x", CorrectBool: bp(true), TimeLimit: 1},
		"invalid utf8":       {Type: TTrueFalse, Prompt: "x\xff", CorrectBool: bp(true)},
		"comprehension poll": {Type: TComprehension, Prompt: "x", Passage: "p", SubQuestions: []SubQuestion{{Type: TPoll, Prompt: "q"}}},
	}
	for name, q := range bad {
		if _, err := Normalize(q, 3); err == nil {
			t.Errorf("%s: accepted", name)
		} else if ve, ok := err.(*ValidationError); !ok || ve.Index != 3 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPlayViewHasNoAnswers(t *testing.T) {
	qs := []Question{
		{Type: TMCQ, Prompt: "x", Options: []string{"a", "b"}, CorrectIndex: ip(1)},
		{Type: TReorder, Prompt: "x", Items: []string{"1", "2", "3", "4", "5"}},
		{Type: TLabeling, Prompt: "x", Image: strings.Repeat("c", 32) + ".jpg", Zones: []Zone{{ID: "1", W: 1, H: 1, Label: "Heart"}, {ID: "2", X: 5, W: 1, H: 1, Label: "Liver"}}},
	}
	for _, q := range qs {
		b, _ := json.Marshal(viewOf(norm(t, q)))
		for _, leak := range []string{"correct", "answers", `"label"`} {
			if strings.Contains(string(b), leak) {
				t.Errorf("%s view leaks %s: %s", q.Type, leak, b)
			}
		}
	}
	v := viewOf(norm(t, qs[1]))
	if strings.Join(v.Items, "") == "12345" {
		t.Error("reorder items not shuffled")
	}
}

func TestQuoteMatching(t *testing.T) {
	src := normQuote("Фотосинтез — бұл   жарық энергиясын\nхимиялық энергияға «айналдыру» процесі.")
	if !quoteFound(src, `жарық энергиясын химиялық энергияға "айналдыру"`) {
		t.Error("typography/whitespace differences must still match")
	}
	if !quoteFound(src, "ФОТОСИНТЕЗ - бұл жарық") {
		t.Error("case and dash must fold")
	}
	if quoteFound(src, "жарық энергиясын электр энергиясына") {
		t.Error("changed words must not match")
	}
	if quoteFound(src, "бұл") {
		t.Error("too-short quotes must not count")
	}
}

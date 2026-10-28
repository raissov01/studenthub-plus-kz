package quiz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Excel import is read with archive/zip + encoding/xml (an .xlsx is a zip of XML parts), so
// no third-party dependency: the reference project parsed Excel in the browser with SheetJS.

const (
	xlsxMaxBytes = 2 << 20
	xlsxMaxRows  = 1000
	xlsxMaxCols  = 30
)

// readSheet returns the first worksheet as rows of cell strings.
func readSheet(data []byte) ([][]string, error) {
	o, err := openOOXML(data)
	if err != nil {
		return nil, errZip
	}
	sheet := "xl/worksheets/sheet1.xml"
	if !o.has(sheet) {
		s := o.numbered("xl/worksheets/sheet", ".xml")
		if len(s) == 0 {
			return nil, errZip
		}
		sheet = s[0]
	}
	var shared []string
	if o.has("xl/sharedStrings.xml") {
		b, err := o.read("xl/sharedStrings.xml")
		if err != nil {
			return nil, err
		}
		if shared, err = parseShared(b); err != nil {
			return nil, err
		}
	}
	b, err := o.read(sheet)
	if err != nil {
		return nil, err
	}
	return parseSheet(b, shared)
}

func parseShared(b []byte) ([]string, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var out []string
	var cur strings.Builder
	inT, inPh := false, false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, errZip
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				cur.Reset()
			case "t":
				inT = true
			case "rPh":
				inPh = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				if len(out) > 100000 {
					return nil, errTooLarge
				}
			case "t":
				inT = false
			case "rPh":
				inPh = false
			}
		case xml.CharData:
			if inT && !inPh {
				cur.Write(t)
			}
		}
	}
}

// colIndex turns "C12" into 2.
func colIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

func parseSheet(b []byte, shared []string) ([][]string, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var rows [][]string
	var row []string
	var cellType, cellRef string
	var val, inline strings.Builder
	inV, inT := false, false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, errZip
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = nil
			case "c":
				cellType, cellRef = "", ""
				val.Reset()
				inline.Reset()
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "t":
						cellType = a.Value
					case "r":
						cellRef = a.Value
					}
				}
			case "v":
				inV = true
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "c":
				col := len(row)
				if cellRef != "" {
					col = colIndex(cellRef)
				}
				if col < 0 || col >= xlsxMaxCols {
					continue
				}
				var s string
				switch cellType {
				case "s":
					i, err := strconv.Atoi(strings.TrimSpace(val.String()))
					if err == nil && i >= 0 && i < len(shared) {
						s = shared[i]
					}
				case "inlineStr":
					s = inline.String()
				case "b":
					s = map[string]string{"1": "true", "0": "false"}[val.String()]
				default:
					s = val.String()
					if f, err := strconv.ParseFloat(s, 64); err == nil && f == float64(int64(f)) {
						s = strconv.FormatInt(int64(f), 10)
					}
				}
				for len(row) <= col {
					row = append(row, "")
				}
				row[col] = s
			case "row":
				rows = append(rows, row)
				if len(rows) > xlsxMaxRows+5 {
					return nil, errTooLarge
				}
			}
		case xml.CharData:
			if inV {
				val.Write(t)
			} else if inT {
				inline.Write(t)
			}
		}
	}
}

// column roles detected from a header row
type cols struct {
	question, typ, correct, explanation, time int
	options                                   []int
}

func headerRole(cell string) string {
	c := strings.ToLower(strings.TrimSpace(cell))
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(c, w) {
				return true
			}
		}
		return false
	}
	switch {
	case c == "":
		return ""
	case has("option", "нұсқа", "вариант", "choice"):
		return "option"
	case has("correct", "дұрыс", "правильн", "ответ", "answer"):
		return "correct"
	case has("explanation", "түсіндір", "пояснен", "объяснен"):
		return "explanation"
	case has("type", "түрі", "тип"):
		return "type"
	case has("time", "уақыт", "время"):
		return "time"
	case has("question", "сұрақ", "вопрос"):
		return "question"
	}
	return ""
}

// detectColumns finds the header row (within the first 3 rows). Without one, the template
// layout is assumed: Question, Type, Option A..F, Correct, Explanation, Time.
func detectColumns(rows [][]string) (cols, int) {
	for i := 0; i < min(3, len(rows)); i++ {
		c := cols{question: -1, typ: -1, correct: -1, explanation: -1, time: -1}
		for j, cell := range rows[i] {
			switch headerRole(cell) {
			case "question":
				if c.question < 0 {
					c.question = j
				}
			case "type":
				c.typ = j
			case "option":
				c.options = append(c.options, j)
			case "correct":
				c.correct = j
			case "explanation":
				c.explanation = j
			case "time":
				c.time = j
			}
		}
		if c.question >= 0 && (c.correct >= 0 || len(c.options) > 0) {
			return c, i + 1
		}
	}
	return cols{question: 0, typ: 1, options: []int{2, 3, 4, 5, 6, 7}, correct: 8, explanation: 9, time: 10}, 0
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func normType(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(v)
	switch v {
	case "", "mcq", "multiple_choice", "multiple", "choice", "single", "single_choice", "бір_жауап", "один_ответ":
		return TMCQ
	case "mcq_multi", "multi", "multi_select", "multiple_select", "checkbox", "checkboxes", "бірнеше_жауап", "несколько_ответов":
		return TMulti
	case "tf", "true_false", "truefalse", "boolean", "ақиқат_жалған", "верно_неверно":
		return TTrueFalse
	case "fill", "fill_blank", "fill_in_the_blank", "blank", "text", "open_ended", "short_answer", "open", "толтыру", "ответ_текстом":
		return TFill
	case "reorder", "order", "ordering", "sequence", "реттеу", "порядок":
		return TReorder
	case "poll", "сауалнама", "опрос":
		return TPoll
	}
	return ""
}

var correctMarker = strings.NewReplacer("✅", "", "✔", "", "✓", "")

// stripMarker removes a leading "*"/"✓" or a trailing "(correct)" style marker.
func stripMarker(s string) (string, bool) {
	t := strings.TrimSpace(s)
	marked := false
	if strings.HasPrefix(t, "*") || strings.ContainsAny(t, "✅✔✓") {
		marked = true
		t = strings.TrimSpace(correctMarker.Replace(strings.TrimPrefix(t, "*")))
	}
	low := strings.ToLower(t)
	for _, suf := range []string{"(correct)", "(дұрыс)", "(верно)", "(правильно)"} {
		if strings.HasSuffix(low, suf) {
			marked = true
			t = strings.TrimSpace(t[:len(t)-len(suf)])
			break
		}
	}
	return t, marked
}

// letterIndex reads one correct-answer token: a–f, Cyrillic а–е, 1–6, or the option text.
func letterIndex(tok string, options []string) int {
	t := strings.ToLower(strings.TrimSpace(tok))
	t = strings.TrimPrefix(strings.TrimPrefix(t, "option "), "answer ")
	if i := slices.Index([]string{"a", "b", "c", "d", "e", "f"}, t); i >= 0 {
		return i
	}
	if i := slices.Index([]string{"а", "б", "в", "г", "д", "е"}, t); i >= 0 {
		return i
	}
	if n, err := strconv.Atoi(t); err == nil && n >= 1 && n <= 6 {
		return n - 1
	}
	for i, o := range options {
		if normText(o) == normText(tok) {
			return i
		}
	}
	return -1
}

func parseBool(s string) *bool {
	v := strings.ToLower(strings.TrimSpace(s))
	var b bool
	switch v {
	case "t", "true", "1", "yes", "y", "иә", "ия", "дұрыс", "ақиқат", "да", "верно", "правда":
		b = true
	case "f", "false", "0", "no", "n", "жоқ", "жалған", "қате", "нет", "неверно", "ложь":
		b = false
	default:
		return nil
	}
	return &b
}

// ImportWarning explains why a spreadsheet row was skipped.
type ImportWarning struct {
	Row  int    `json:"row"`
	Code string `json:"code"`
}

// importRows converts spreadsheet rows to questions. Bad rows are skipped with a warning.
func importRows(rows [][]string) ([]Question, []ImportWarning) {
	c, start := detectColumns(rows)
	qs := []Question{}
	warns := []ImportWarning{}
	for i := start; i < len(rows) && len(qs) < MaxQuestions; i++ {
		row := rows[i]
		rowNo := i + 1
		prompt := cell(row, c.question)
		var opts []string
		marked := []int{}
		for _, oc := range c.options {
			if t, m := stripMarker(cell(row, oc)); t != "" {
				if m {
					marked = append(marked, len(opts))
				}
				opts = append(opts, t)
			}
		}
		correct := cell(row, c.correct)
		if prompt == "" && len(opts) == 0 && correct == "" {
			continue // blank line
		}
		typ := normType(cell(row, c.typ))
		if typ == "" {
			warns = append(warns, ImportWarning{rowNo, "bad_type"})
			continue
		}
		q := Question{Type: typ, Prompt: prompt, Explanation: cell(row, c.explanation)}
		if t, err := strconv.Atoi(cell(row, c.time)); err == nil && t >= 5 && t <= 300 {
			q.TimeLimit = t
		}
		switch typ {
		case TMCQ, TPoll:
			q.Options = opts
			if typ == TMCQ {
				idx := -1
				if correct != "" {
					idx = letterIndex(correct, opts)
				} else if len(marked) == 1 {
					idx = marked[0]
				}
				if idx >= 0 {
					q.CorrectIndex = &idx
				}
			}
		case TMulti:
			q.Options = opts
			if correct != "" {
				for _, tok := range strings.FieldsFunc(correct, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) }) {
					q.CorrectIndexes = append(q.CorrectIndexes, letterIndex(tok, opts))
				}
			} else {
				q.CorrectIndexes = marked
			}
		case TTrueFalse:
			q.CorrectBool = parseBool(correct)
		case TFill:
			for _, a := range strings.FieldsFunc(correct, func(r rune) bool { return r == '|' || r == ';' }) {
				if a = strings.TrimSpace(a); a != "" {
					q.Answers = append(q.Answers, a)
				}
			}
		case TReorder:
			q.Items = opts // options are listed in the correct order
		}
		n, err := Normalize(q, len(qs)+1)
		if err != nil {
			code := "invalid"
			if ve, ok := err.(*ValidationError); ok {
				code = ve.Code
			}
			warns = append(warns, ImportWarning{rowNo, code})
			continue
		}
		qs = append(qs, n)
	}
	return qs, warns
}

// importXLSX: POST /api/quiz/import/xlsx (multipart field "file" or raw body) →
// {questions, warnings, imported, skipped}. Nothing is saved: the editor shows the questions,
// the student fixes or confirms them and saves the quiz with source=excel.
func importXLSX(w http.ResponseWriter, r *http.Request, u *auth.User) {
	data, err := readOne(w, r, "file", xlsxMaxBytes)
	if err != nil {
		if err == errTooLarge {
			fail(w, r, "media_too_large", http.StatusRequestEntityTooLarge)
			return
		}
		fail(w, r, "xlsx_invalid", http.StatusBadRequest)
		return
	}
	rows, err := readSheet(data)
	if err != nil {
		if err == errTooLarge {
			fail(w, r, "media_too_large", http.StatusRequestEntityTooLarge)
			return
		}
		fail(w, r, "xlsx_invalid", http.StatusBadRequest)
		return
	}
	qs, warns := importRows(rows)
	if len(qs) == 0 {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":    map[string]string{"code": "xlsx_empty", "message": pick(r, msgs["xlsx_empty"])},
			"warnings": warns,
		})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"questions": qs, "warnings": warns, "imported": len(qs), "skipped": len(warns)})
}

func pick(r *http.Request, m httpx.Msg) string {
	if httpx.Lang(r) == "ru" {
		return m.RU
	}
	return m.KK
}

var templateRows = [][]string{
	{"Question", "Type", "Option A", "Option B", "Option C", "Option D", "Option E", "Option F", "Correct", "Explanation", "Time (sec)"},
	{"Қазақстанның астанасы қай қала?", "mcq", "Алматы", "Астана", "Шымкент", "Қарағанды", "", "", "b", "1997 жылдан бері астана — Астана.", "20"},
	{"Жай сандарды белгілеңіз", "mcq_multi", "2", "4", "7", "9", "", "", "a,c", "", ""},
	{"Су 100 °C-та қайнайды (теңіз деңгейінде).", "true_false", "", "", "", "", "", "", "true", "", ""},
	{"H2O — бұл ...", "fill_blank", "", "", "", "", "", "", "су|вода|water", "", ""},
	{"Сандарды өсу ретімен қойыңыз", "reorder", "1", "2", "3", "4", "", "", "", "Нұсқалар дұрыс ретпен жазылады.", ""},
}

// template: GET /api/quiz/import/template.xlsx — the import template with one example per type.
func template(w http.ResponseWriter, r *http.Request) {
	b, err := writeXLSX(templateRows)
	if err != nil {
		internal(w, r, "template", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	h.Set("Content-Disposition", `attachment; filename="studenthub-quiz-template.xlsx"`)
	h.Set("Cache-Control", "public, max-age=3600")
	w.Write(b)
}

// writeXLSX builds a minimal single-sheet workbook with inline strings.
func writeXLSX(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for i, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, i+1)
		for j, v := range row {
			if v == "" {
				continue
			}
			ref := string(rune('A'+j)) + strconv.Itoa(i+1)
			var esc bytes.Buffer
			xml.EscapeText(&esc, []byte(v))
			fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc.String())
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Quiz" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", sheet.String()},
	}
	for _, p := range parts {
		if err := add(p.name, p.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

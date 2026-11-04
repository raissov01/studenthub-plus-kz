package quiz

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// Lecture notes used by the AI tests (Latin text so the generated PDF can carry it).
const lecture = `Photosynthesis is the process by which green plants convert light energy into chemical energy.
It takes place in the chloroplasts, which contain the green pigment chlorophyll.
The light-dependent reactions happen in the thylakoid membranes and produce ATP and NADPH.
The Calvin cycle happens in the stroma and uses carbon dioxide to build glucose.
Oxygen is released as a by-product when water molecules are split during the light reactions.`

// fakeAI is an OpenAI-compatible server. reply decides each answer; requests are recorded.
type fakeAI struct {
	mu       sync.Mutex
	srv      *httptest.Server
	requests []string
	reply    func(body string) (int, string)
}

func newFakeAI(t *testing.T, reply func(string) (int, string)) *fakeAI {
	f := &fakeAI{reply: reply}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, string(b))
		f.mu.Unlock()
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(401)
			return
		}
		status, content := f.reply(string(b))
		w.WriteHeader(status)
		out, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}}})
		w.Write(out)
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", f.srv.URL)
	t.Setenv("OPENAI_MODEL", "test-model")
	t.Setenv("QUIZ_AI_PER_MIN", "100")
	t.Setenv("QUIZ_AI_DAILY_UNITS", "100")
	return f
}

func (f *fakeAI) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.requests, "\n")
}

func goodQuizJSON(hallucinated int) string {
	qs := []map[string]any{
		{"type": "mcq", "prompt": "Where does photosynthesis take place?", "options": []string{"Mitochondria", "Chloroplasts", "Nucleus", "Ribosomes"},
			"correct_index": 1, "explanation": "Stated in the notes.", "source_quote": "It takes place in the chloroplasts"},
		{"type": "true_false", "prompt": "The Calvin cycle happens in the stroma.", "correct": true,
			"explanation": "Yes.", "source_quote": "The Calvin cycle happens in the stroma"},
		{"type": "mcq_multi", "prompt": "What do light reactions produce?", "options": []string{"ATP", "NADPH", "Glucose", "DNA"},
			"correct_indexes": []int{0, 1}, "explanation": "Both.", "source_quote": "produce ATP and NADPH"},
	}
	for i := range hallucinated {
		qs = append(qs, map[string]any{"type": "true_false", "prompt": fmt.Sprintf("Invented fact %d", i), "correct": true,
			"explanation": "x", "source_quote": "Photosynthesis was discovered on Mars in 1999"})
	}
	b, _ := json.Marshal(map[string]any{"title": "Photosynthesis", "questions": qs})
	return string(b)
}

func TestAIFromTextValidatesQuotesAndJSON(t *testing.T) {
	calls := 0
	f := newFakeAI(t, func(string) (int, string) {
		calls++
		return 200, goodQuizJSON(2)
	})
	uid := newUser(t)
	w := do(t, "POST", "/api/quiz/generate/from-text", uid, map[string]any{"text": lecture, "count": 5, "lang": "ru"})
	if w.Code != 201 {
		t.Fatalf("generate: %d %s", w.Code, w.Body)
	}
	res := decode[struct {
		ID        int64 `json:"id"`
		Quiz      Meta  `json:"quiz"`
		Generated int   `json:"generated"`
		Dropped   int   `json:"dropped"`
	}](t, w)
	// 3 good of 5 asked (< 80%) → one more round; the duplicates and fakes are dropped again
	if res.Generated != 3 || res.Dropped < 2 || calls != 2 || !res.Quiz.IsDraft || res.Quiz.Visibility != "private" || res.Quiz.Source != "ai_text" {
		t.Fatalf("draft: %+v calls=%d", res, calls)
	}
	if count(t, "select count(*) from quiz_questions where quiz_id = $1 and source_quote <> ''", res.ID) != 3 {
		t.Fatal("source quotes not stored")
	}
	if strings.Contains(f.all(), userName(t, uid)) || strings.Contains(f.all(), "Студентова") {
		t.Fatal("student's name sent upstream")
	}
	if !strings.Contains(f.all(), "Russian") || !strings.Contains(f.all(), `"json_object"`) {
		t.Fatal("prompt options missing")
	}
	// draft is private: nobody else can play it
	if w := do(t, "GET", fmt.Sprintf("/api/quiz/quizzes/%d/play", res.ID), newUser(t), nil); w.Code != 404 {
		t.Fatalf("draft visible: %d", w.Code)
	}
	// bad JSON → 502 and the unit is refunded
	used := usedToday(ctxb(), uid)
	for _, reply := range []string{"not json at all", `{"questions": "nope"}`, `{"title":"x"}`, goodQuizJSON(0)[:40], `{"questions":[{"type":"essay","prompt":"x"}]}`} {
		f.reply = func(string) (int, string) { return 200, reply }
		if w := do(t, "POST", "/api/quiz/generate/from-text", uid, map[string]any{"text": lecture}); w.Code != 502 || errCode(w) != "ai_upstream" {
			t.Errorf("reply %.30q: %d %s", reply, w.Code, w.Body)
		}
	}
	f.reply = func(string) (int, string) { return 500, "" }
	if w := do(t, "POST", "/api/quiz/generate/from-text", uid, map[string]any{"text": lecture}); w.Code != 502 {
		t.Errorf("upstream 500: %d", w.Code)
	}
	if usedToday(ctxb(), uid) != used {
		t.Fatalf("failed generations were charged: %d → %d", used, usedToday(ctxb(), uid))
	}
	// input validation
	for _, body := range []map[string]any{
		{"text": "short"},
		{"text": lecture, "count": 50},
		{"text": lecture, "types": []string{"essay"}},
		{"text": lecture, "difficulty": "insane"},
		{"text": strings.Repeat("я", maxPastedRunes+1)},
	} {
		if w := do(t, "POST", "/api/quiz/generate/from-text", uid, body); w.Code < 400 || w.Code >= 500 {
			t.Errorf("%v: %d", body["count"], w.Code)
		}
	}
}

func TestAILimits(t *testing.T) {
	newFakeAI(t, func(string) (int, string) { return 200, goodQuizJSON(0) })
	uid := newUser(t)
	t.Setenv("QUIZ_AI_DAILY_UNITS", "2")
	body := map[string]any{"text": lecture, "count": 3}
	for i := range 2 {
		if w := do(t, "POST", "/api/quiz/generate/from-text", uid, body); w.Code != 201 {
			t.Fatalf("call %d: %d %s", i, w.Code, w.Body)
		}
	}
	if w := do(t, "POST", "/api/quiz/generate/from-text", uid, body); w.Code != 429 || errCode(w) != "ai_daily" {
		t.Fatalf("daily: %d %s", w.Code, w.Body)
	}
	u := decode[map[string]any](t, do(t, "GET", "/api/quiz/generate/usage", uid, nil))
	if u["used"].(float64) != 2 || u["limit"].(float64) != 2 {
		t.Fatalf("usage: %v", u)
	}
	// per-minute
	other := newUser(t)
	t.Setenv("QUIZ_AI_DAILY_UNITS", "100")
	t.Setenv("QUIZ_AI_PER_MIN", "1")
	do(t, "POST", "/api/quiz/generate/from-text", other, body)
	if w := do(t, "POST", "/api/quiz/generate/from-text", other, body); w.Code != 429 || errCode(w) != "ai_minute" {
		t.Fatalf("minute: %d %s", w.Code, w.Body)
	}
	// no key → 503
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("QUIZ_AI_PER_MIN", "100")
	if w := do(t, "POST", "/api/quiz/generate/from-text", newUser(t), body); w.Code != 503 {
		t.Fatalf("off: %d", w.Code)
	}
}

// --- fixtures built in the test ---------------------------------------------------------

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	zw.Close()
	return buf.Bytes()
}

func mustDocx(t *testing.T, text string) []byte {
	var paras strings.Builder
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(&paras, `<w:p><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, line)
	}
	return zipOf(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			paras.String() + `</w:body></w:document>`,
	})
}

func mustPptx(t *testing.T, slides ...string) []byte {
	files := map[string]string{"ppt/presentation.xml": `<p:presentation xmlns:p="p"/>`}
	for i, s := range slides {
		var paras strings.Builder
		for _, line := range strings.Split(s, "\n") {
			fmt.Fprintf(&paras, `<a:p><a:r><a:t>%s</a:t></a:r></a:p>`, line)
		}
		files[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree><p:sp><p:txBody>` +
			paras.String() + `</p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	return zipOf(t, files)
}

// mustPDF writes a minimal valid PDF (Helvetica, one text line per row) with a correct xref.
func mustPDF(t *testing.T, pages ...[]string) []byte {
	var objs []string
	n := len(pages)
	kids := []string{}
	for i := range n {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+2*i))
	}
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>")
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, lines := range pages {
		var content strings.Builder
		content.WriteString("BT /F1 10 Tf 40 800 Td 12 TL\n")
		for _, l := range lines {
			l = strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(l)
			fmt.Fprintf(&content, "(%s) Tj T*\n", l)
		}
		content.WriteString("ET")
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i))
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()))
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := []int{}
	for i, o := range objs {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func TestExtractors(t *testing.T) {
	lines := strings.Split(lecture, "\n")
	text, pages, err := pdfText(mustPDF(t, lines[:3], lines[3:]))
	if err != nil || pages != 2 || !strings.Contains(text, "chloroplasts") || !strings.Contains(text, "Calvin cycle") {
		t.Fatalf("pdf: %v %d %q", err, pages, text)
	}
	if _, _, err := pdfText(mustPDF(t, []string{}, []string{})); err != errScanned {
		t.Fatalf("scanned pdf: %v", err)
	}
	if _, _, err := pdfText([]byte("%PDF-1.4 garbage")); err == nil {
		t.Fatal("garbage pdf accepted")
	}
	d, err := docxText(mustDocx(t, lecture))
	if err != nil || !strings.Contains(d, "thylakoid membranes") {
		t.Fatalf("docx: %v %q", err, d)
	}
	p, n, err := pptxText(mustPptx(t, lines[0], lines[1], lines[2], lines[3], lines[4], "s6", "s7", "s8", "s9", "s10", "s11"))
	if err != nil || n != 11 || strings.Index(p, "[2]") > strings.Index(p, "[10]") || !strings.Contains(p, "by-product") {
		t.Fatalf("pptx: %v %d %q", err, n, p)
	}
}

func TestGenerateFromFiles(t *testing.T) {
	var images int
	var mu sync.Mutex
	f := newFakeAI(t, func(body string) (int, string) {
		if strings.Contains(body, "image_url") {
			mu.Lock()
			images++
			mu.Unlock()
			return 200, "Oxygen is released as a by-product when water molecules are split during the light reactions."
		}
		if strings.Contains(body, "flashcards") {
			return 200, `{"cards":[{"q":"Where is the Calvin cycle?","a":"In the stroma","source_quote":"The Calvin cycle happens in the stroma"},
				{"q":"Invented","a":"x","source_quote":"made up sentence that is not in notes"}]}`
		}
		return 200, goodQuizJSON(0)
	})
	uid := newUser(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	uploadsBefore := dirList(t, UploadDir())
	lines := strings.Split(lecture, "\n")
	files := []part{
		{"files", "lecture.pdf", mustPDF(t, lines[:2])},
		{"files", "notes.docx", mustDocx(t, lines[2])},
		{"files", "slides.pptx", mustPptx(t, lines[3])},
		{"files", "photo.jpg", testJPEG(t, 300, 200, "GPS")},
		{"files", "extra.txt", []byte("\xEF\xBB\xBF" + lines[0])},
		{"count", "", []byte("5")}, {"types", "", []byte("mcq,true_false,mcq_multi")}, {"lang", "", []byte("kk")}, {"difficulty", "", []byte("hard")},
	}
	w := doMultipart(t, "/api/quiz/generate/from-files", uid, files)
	if w.Code != 201 {
		t.Fatalf("from-files: %d %s", w.Code, w.Body)
	}
	res := decode[struct {
		ID    int64      `json:"id"`
		Quiz  Meta       `json:"quiz"`
		Files []fileInfo `json:"files"`
	}](t, w)
	if res.Quiz.Source != "ai_files" || !res.Quiz.IsDraft || len(res.Files) != 5 || images != 1 {
		t.Fatalf("result: %+v images=%d", res, images)
	}
	all := f.all()
	for _, want := range []string{"chloroplasts", "thylakoid", "Calvin", "Kazakh", "data:image/jpeg;base64,"} {
		if !strings.Contains(all, want) {
			t.Errorf("upstream never saw %q", want)
		}
	}
	// a photo costs more: 1 + 2
	if u := usedToday(ctxb(), uid); u != 3 {
		t.Fatalf("units: %d", u)
	}
	// nothing persisted: no new files in UPLOAD_DIR, nothing in TMPDIR, only the quiz row
	if after := dirList(t, UploadDir()); after != uploadsBefore {
		t.Fatalf("upload dir changed: %s → %s", uploadsBefore, after)
	}
	if left := dirList(t, tmp); left != "" {
		t.Fatalf("temp files left: %s", left)
	}
	// cards variant: not stored, hallucinated card dropped
	before := count(t, "select count(*) from quizzes")
	w = doMultipart(t, "/api/quiz/generate/cards-from-files", uid, []part{{"files", "n.txt", []byte(lecture)}, {"count", "", []byte("5")}})
	cards := decode[struct {
		Cards   []Card `json:"cards"`
		Dropped int    `json:"dropped"`
	}](t, w)
	if w.Code != 200 || len(cards.Cards) != 1 || cards.Dropped != 1 || cards.Cards[0].A != "In the stroma" || count(t, "select count(*) from quizzes") != before {
		t.Fatalf("cards: %d %s", w.Code, w.Body)
	}

	bomb := zipOf(t, map[string]string{"word/document.xml": strings.Repeat(" ", 30<<20)})
	many := map[string]string{"word/document.xml": "<w:document/>"}
	for i := range 2100 {
		many[fmt.Sprintf("x/%d", i)] = ""
	}
	bad := []struct {
		name  string
		parts []part
		code  int
		err   string
	}{
		{"exe", []part{{"files", "notes.pdf", append([]byte("MZ\x90\x00"), make([]byte, 500)...)}}, 415, "file_type"},
		{"pdf named docx with zip junk", []part{{"files", "a.docx", zipOf(t, map[string]string{"hello.txt": "hi"})}}, 415, "file_type"},
		{"zip bomb", []part{{"files", "bomb.docx", bomb}}, 413, "file_too_large"},
		{"zip entry bomb", []part{{"files", "many.docx", zipOf(t, many)}}, 400, "file_invalid"},
		{"oversized", []part{{"files", "big.txt", bytes.Repeat([]byte("a "), 5<<20+1)}}, 413, "file_too_large"},
		{"heic", []part{{"files", "IMG.HEIC", append([]byte("\x00\x00\x00\x18ftypheic"), make([]byte, 64)...)}}, 415, "file_heic"},
		{"scanned pdf", []part{{"files", "scan.pdf", mustPDF(t, []string{}, []string{})}}, 422, "pdf_scanned"},
		{"too many files", []part{{"files", "1.txt", []byte(lecture)}, {"files", "2.txt", []byte(lecture)}, {"files", "3.txt", []byte(lecture)},
			{"files", "4.txt", []byte(lecture)}, {"files", "5.txt", []byte(lecture)}, {"files", "6.txt", []byte(lecture)}}, 400, "too_many_files"},
		{"too many pages", []part{{"files", "big.pptx", mustPptx(t, strings.Split(strings.Repeat("slide\n", 31), "\n")[:31]...)}}, 400, "too_many_pages"},
		{"no files", []part{{"count", "", []byte("5")}}, 400, "no_files"},
		{"too short", []part{{"files", "s.txt", []byte("tiny note")}}, 422, "text_too_short"},
		{"bad count", []part{{"files", "n.txt", []byte(lecture)}, {"count", "", []byte("500")}}, 400, "bad_options"},
	}
	for _, c := range bad {
		used := usedToday(ctxb(), uid)
		w := doMultipart(t, "/api/quiz/generate/from-files", uid, c.parts)
		if w.Code != c.code || errCode(w) != c.err {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
		if usedToday(ctxb(), uid) != used {
			t.Errorf("%s: charged", c.name)
		}
	}
	if left := dirList(t, tmp); left != "" {
		t.Fatalf("temp files left: %s", left)
	}
	// daily budget counts photos: 1 + 2 per photo
	t.Setenv("QUIZ_AI_DAILY_UNITS", "5")
	w = doMultipart(t, "/api/quiz/generate/from-files", uid, []part{{"files", "a.jpg", testJPEG(t, 50, 50, "")}})
	if w.Code != 429 || errCode(w) != "ai_daily" {
		t.Fatalf("photo budget: %d %s", w.Code, w.Body)
	}
}

func dirList(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

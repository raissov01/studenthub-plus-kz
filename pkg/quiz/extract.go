package quiz

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"

	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Lecture notes → text. Everything happens in memory: the multipart body is streamed part by
// part (no temp files: ParseMultipartForm is never called), each file is read, turned into
// text (or a small re-encoded JPEG for photos) and dropped. Neither the files nor the text are
// stored; only the generated quiz is.

const (
	maxFileBytes   = 10 << 20
	maxFiles       = 5
	maxPages       = 30
	charsPerPage   = 3000 // DOCX / TXT have no pages: every 3000 characters count as one
	notesImageSide = 1600
	minPDFChars    = 40 // non-space characters per page below which a PDF looks scanned
)

var (
	errTooManyFiles = errors.New("too_many_files")
	errTooManyPages = errors.New("too_many_pages")
	errFileLarge    = errors.New("file_too_large")
	errScanned      = errors.New("pdf_scanned")
	errNoFiles      = errors.New("no_files")
)

type fileInfo struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // pdf, docx, pptx, txt, image
	Pages int    `json:"pages"`
	Chars int    `json:"chars"`
}

type notes struct {
	texts  []string // extracted text in upload order (images get filled in by transcribe)
	images map[int][]byte
	files  []fileInfo
	pages  int
	opts   map[string]string
}

// sniffFile decides the kind by content. Zips are told apart by their parts.
func sniffFile(b []byte) (string, error) {
	switch {
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return "pdf", nil
	case isZip(b):
		o, err := openOOXML(b)
		if err != nil {
			return "", err
		}
		switch {
		case o.has("word/document.xml"):
			return "docx", nil
		case o.has("ppt/presentation.xml"):
			return "pptx", nil
		}
		return "", errType
	}
	switch sniffImage(b) {
	case "jpeg", "png", "webp":
		return "image", nil
	case "heic":
		return "", errHEIC
	}
	if looksText(b) {
		return "txt", nil
	}
	return "", errType
}

// looksText: valid UTF-8 (BOM allowed), no NUL, mostly printable.
func looksText(b []byte) bool {
	b = bytes.TrimPrefix(b, []byte("\xEF\xBB\xBF"))
	if len(b) == 0 || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	bad := 0
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			bad++
		}
	}
	return bad*100 < len(b)
}

func pagesFor(chars int) int { return max(1, (chars+charsPerPage-1)/charsPerPage) }

// pdfText extracts the text layer page by page. The parser panics on some malformed files;
// that becomes errZip ("file_invalid").
func pdfText(data []byte) (text string, pages int, err error) {
	defer func() {
		if v := recover(); v != nil {
			text, pages, err = "", 0, errZip
		}
	}()
	rd, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, errZip
	}
	pages = rd.NumPage()
	if pages < 1 {
		return "", 0, errZip
	}
	if pages > maxPages {
		return "", pages, errTooManyPages
	}
	var sb strings.Builder
	fonts := map[string]*pdf.Font{}
	for i := 1; i <= pages; i++ {
		p := rd.Page(i)
		if p.V.IsNull() {
			continue
		}
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; !ok {
				f := p.Font(name)
				fonts[name] = &f
			}
		}
		t, err := p.GetPlainText(fonts)
		if err != nil {
			return "", pages, errZip
		}
		sb.WriteString(t)
		sb.WriteString("\n\n")
		if sb.Len() > 4*maxTextRunes*4 {
			break
		}
	}
	text = sb.String()
	nonSpace := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			nonSpace++
		}
	}
	if nonSpace < minPDFChars*pages {
		return "", pages, errScanned
	}
	return text, pages, nil
}

func docxText(data []byte) (string, error) {
	o, err := openOOXML(data)
	if err != nil {
		return "", err
	}
	b, err := o.read("word/document.xml")
	if err != nil {
		return "", err
	}
	return xmlText(b, "t", "p")
}

func pptxText(data []byte) (string, int, error) {
	o, err := openOOXML(data)
	if err != nil {
		return "", 0, err
	}
	slides := o.numbered("ppt/slides/slide", ".xml")
	if len(slides) > maxPages {
		return "", len(slides), errTooManyPages
	}
	var sb strings.Builder
	for i, s := range slides {
		b, err := o.read(s)
		if err != nil {
			return "", 0, err
		}
		t, err := xmlText(b, "t", "p")
		if err != nil {
			return "", 0, err
		}
		fmt.Fprintf(&sb, "[%d]\n%s\n", i+1, t)
	}
	return sb.String(), len(slides), nil
}

// readNotes streams the multipart body: "files" parts (up to 5) and option fields.
func readNotes(ctx context.Context, w http.ResponseWriter, r *http.Request) (*notes, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFiles*maxFileBytes+1<<20)
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, errNoFiles
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, errNoFiles
	}
	n := &notes{images: map[int][]byte{}, opts: map[string]string{}}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if wrapMaxBytes(err, errDecode) == errTooLarge {
				return nil, errFileLarge
			}
			return nil, errZip
		}
		name := part.FormName()
		if name != "files" && name != "file" {
			v, _ := io.ReadAll(io.LimitReader(part, 1<<10))
			n.opts[name] = string(v)
			part.Close()
			continue
		}
		if len(n.files) >= maxFiles {
			part.Close()
			return nil, errTooManyFiles
		}
		data, err := io.ReadAll(io.LimitReader(part, maxFileBytes+1))
		part.Close()
		if err != nil {
			if wrapMaxBytes(err, errDecode) == errTooLarge {
				return nil, errFileLarge
			}
			return nil, errZip
		}
		if len(data) > maxFileBytes {
			return nil, errFileLarge
		}
		if err := n.add(ctx, clip(part.FileName(), 120), data); err != nil {
			return nil, err
		}
		if n.pages > maxPages {
			return nil, errTooManyPages
		}
	}
	if len(n.files) == 0 {
		return nil, errNoFiles
	}
	return n, nil
}

// add extracts one file; data is not kept (photos are kept only as a small re-encoded JPEG
// until they are transcribed).
func (n *notes) add(ctx context.Context, name string, data []byte) error {
	kind, err := sniffFile(data)
	if err != nil {
		return err
	}
	fi := fileInfo{Name: name, Kind: kind}
	var text string
	switch kind {
	case "pdf":
		text, fi.Pages, err = pdfText(data)
	case "docx":
		text, err = docxText(data)
		fi.Pages = pagesFor(runes(text))
	case "pptx":
		text, fi.Pages, err = pptxText(data)
	case "txt":
		text = string(bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF")))
		fi.Pages = pagesFor(runes(text))
	case "image":
		var jpg []byte
		jpg, _, _, err = processImage(ctx, data, maxFileBytes, notesImageSide, 600<<10)
		if err == nil {
			n.images[len(n.texts)] = jpg
		}
		fi.Pages = 1
	}
	if err != nil {
		return err
	}
	fi.Chars = runes(text)
	n.texts = append(n.texts, text)
	n.files = append(n.files, fi)
	n.pages += fi.Pages
	return nil
}

const transcribePrompt = `You transcribe photos of a student's lecture notes (printed or handwritten).
Return ONLY the text that is visible, faithfully, in its original language, keeping line breaks and formulas as plain text.
Do not translate, summarize, correct, explain or add anything. Write [?] for words you cannot read.
If there is no readable text, return an empty string.`

// transcribe fills in the text of photos with the vision model, one request per photo.
func (n *notes) transcribe(ctx context.Context) error {
	for i := range n.texts {
		jpg, ok := n.images[i]
		if !ok {
			continue
		}
		content := []map[string]any{
			{"type": "text", "text": "Transcribe this page."},
			{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg), "detail": "high"}},
		}
		text, err := complete(ctx, visionModel(), []chatMsg{{"system", transcribePrompt}, {"user", content}}, 3000, false)
		if err != nil {
			return err
		}
		n.texts[i] = clip(text, 20000)
		n.files[i].Chars = runes(n.texts[i])
		delete(n.images, i)
	}
	return nil
}

func (n *notes) joined() string {
	var parts []string
	for _, t := range n.texts {
		if t = strings.TrimSpace(t); t != "" {
			parts = append(parts, t)
		}
	}
	return clean(strings.Join(parts, "\n\n"), true)
}

func notesErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNoFiles):
		fail(w, r, "no_files", http.StatusBadRequest)
	case errors.Is(err, errTooManyFiles):
		fail(w, r, "too_many_files", http.StatusBadRequest)
	case errors.Is(err, errTooManyPages):
		fail(w, r, "too_many_pages", http.StatusBadRequest)
	case errors.Is(err, errFileLarge), errors.Is(err, errTooLarge):
		fail(w, r, "file_too_large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, errHEIC):
		fail(w, r, "file_heic", http.StatusUnsupportedMediaType)
	case errors.Is(err, errType):
		fail(w, r, "file_type", http.StatusUnsupportedMediaType)
	case errors.Is(err, errScanned):
		fail(w, r, "pdf_scanned", http.StatusUnprocessableEntity)
	case errors.Is(err, errPixels):
		fail(w, r, "media_dimensions", http.StatusBadRequest)
	default:
		fail(w, r, "file_invalid", http.StatusBadRequest)
	}
}

// prepareNotes reads the upload, charges the budget (1 + photo cost per photo), transcribes
// photos and returns the notes text cut to maxTextRunes. ok=false means a response was sent.
func prepareNotes(w http.ResponseWriter, r *http.Request, u *auth.User, cards bool) (n *notes, o GenOptions, src string, cost int, truncated, ok bool) {
	if !gate(w, r, u) {
		return
	}
	n, err := readNotes(r.Context(), w, r)
	if err != nil {
		notesErr(w, r, err)
		return
	}
	o = GenOptions{Difficulty: n.opts["difficulty"], Lang: n.opts["lang"], Title: n.opts["title"]}
	if c := strings.TrimSpace(n.opts["count"]); c != "" {
		if o.Count, err = strconv.Atoi(c); err != nil {
			fail(w, r, "bad_options", http.StatusBadRequest)
			return
		}
	}
	if t := strings.TrimSpace(n.opts["types"]); t != "" {
		o.Types = strings.Split(t, ",")
	}
	if !o.normalize(r, cards) {
		fail(w, r, "bad_options", http.StatusBadRequest)
		return
	}
	cost = 1 + visionCost()*len(n.images)
	if !chargeOrFail(w, r, u, cost) {
		return
	}
	if err := n.transcribe(r.Context()); err != nil {
		refund(u.ID, cost)
		aiFail(w, r, err)
		return
	}
	text := n.joined()
	if text == "" {
		refund(u.ID, cost)
		fail(w, r, "no_text", http.StatusUnprocessableEntity)
		return
	}
	if runes(text) < minSourceRunes {
		refund(u.ID, cost)
		fail(w, r, "text_too_short", http.StatusUnprocessableEntity)
		return
	}
	src, truncated = truncateSource(text)
	return n, o, src, cost, truncated, true
}

// generateFromFiles: POST /api/quiz/generate/from-files (multipart: files×≤5, count,
// difficulty, types, lang, title) → 201 {id, quiz, generated, dropped, truncated, files}.
func generateFromFiles(w http.ResponseWriter, r *http.Request, u *auth.User) {
	n, o, src, cost, truncated, ok := prepareNotes(w, r, u, false)
	if !ok {
		return
	}
	title, qs, dropped, err := generateQuestions(r.Context(), src, o)
	if err != nil {
		refund(u.ID, cost)
		aiFail(w, r, err)
		return
	}
	id, err := saveDraft(r.Context(), u.ID, o, title, "ai_files", qs)
	if err != nil {
		internal(w, r, "save draft", err)
		return
	}
	writeDraft(w, r, u.ID, id, len(qs), dropped, truncated, n.files)
}

// cardsFromFiles: POST /api/quiz/generate/cards-from-files (same form, count 1–30) →
// 200 {cards: [{q, a, sourceQuote}], dropped, truncated, files}. Nothing is stored: the cards
// tool keeps decks in the browser.
func cardsFromFiles(w http.ResponseWriter, r *http.Request, u *auth.User) {
	n, o, src, cost, truncated, ok := prepareNotes(w, r, u, true)
	if !ok {
		return
	}
	content, err := complete(r.Context(), aiModel(), []chatMsg{{"system", cardsSystemPrompt(o)}, {"user", "<notes>\n" + src + "\n</notes>"}},
		min(200*o.Count+400, 8000), true)
	var cards []Card
	dropped := 0
	if err == nil {
		cards, dropped, err = parseAICards(content, normQuote(src))
		if err == nil && len(cards) == 0 {
			err = errUpstream
		}
	}
	if err != nil {
		refund(u.ID, cost)
		aiFail(w, r, err)
		return
	}
	if len(cards) > o.Count {
		cards = cards[:o.Count]
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cards": cards, "dropped": dropped, "truncated": truncated, "files": n.files})
}

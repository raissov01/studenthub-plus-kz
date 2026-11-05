package printshop

// Uploaded files. What is checked, in order: the size (≤ 20 MB, the body is cut there), the bytes (magic numbers,
// never the name or the declared Content-Type: PDF, JPEG, PNG, or a ZIP that really is a DOCX / XLSX / PPTX — no
// macro-enabled documents), the student's daily quota (files and bytes), then the page count:
//
//   - PDF: counted on the server by a pure-Go parser (github.com/ledongthuc/pdf, catalogue → /Pages → /Count),
//     under a time limit and with panics recovered; a PDF it cannot read is treated like an Office file.
//   - JPG / PNG: one page.
//   - DOCX / XLSX / PPTX: there is no converter in the API image (memory budget, docs/print.md), so the student
//     declares the page count and the job waits for the operator to confirm or adjust the price. The file's own
//     metadata (docProps/app.xml <Pages>/<Slides>) only pre-fills the student's answer.
//
// Files live under UPLOAD_DIR/print with random names, are never public, never parsed for anything but the page
// count, never logged, and are served only through short-lived HMAC-signed links (signedURL) with a fixed
// Content-Type, nosniff, a sandboxing CSP and Content-Disposition: attachment. There is no antivirus: the risk is
// documented in docs/print.md (the operator opens files on the shop's own computer).

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/ledongthuc/pdf"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

const (
	maxFileBytes = 20 << 20
	maxPixels    = 120_000_000
	maxPages     = 5000
	linkTTL      = 5 * time.Minute
	orphanAfter  = 24 * time.Hour
)

func dailyFiles() int     { return config.Int("PRINT_DAILY_FILES", 30) }
func dailyBytes() int64   { return int64(config.Int("PRINT_DAILY_MB", 200)) << 20 }
func uploadsPerHour() int { return config.Int("PRINT_UPLOADS_PER_HOUR", 40) }

// Dir is where print files live: $UPLOAD_DIR/print (default ./uploads/print).
func Dir() string { return filepath.Join(config.Str("UPLOAD_DIR", "./uploads"), "print") }

var reFileName = regexp.MustCompile(`^[0-9a-f]{32}\.(pdf|docx|xlsx|pptx|jpg|png)$`)

var fileMsgs = map[string]httpx.Msg{
	"file_type":     {KK: "Тек PDF, DOCX, XLSX, PPTX, JPG немесе PNG файл жүктеуге болады.", RU: "Можно загрузить только PDF, DOCX, XLSX, PPTX, JPG или PNG."},
	"file_macro":    {KK: "Макросы бар құжатты қабылдамаймыз. PDF не DOCX етіп сақтаңыз.", RU: "Документы с макросами не принимаем. Сохраните как PDF или DOCX."},
	"file_large":    {KK: "Файл тым үлкен: 20 МБ-қа дейін.", RU: "Файл слишком большой: до 20 МБ."},
	"file_invalid":  {KK: "Файлды оқу мүмкін болмады. Басқа файл таңдаңыз.", RU: "Не удалось прочитать файл. Выберите другой."},
	"file_no_file":  {KK: "Файлды таңдаңыз.", RU: "Выберите файл."},
	"quota_files":   {KK: "Бүгінге файл жүктеу шегіне жеттіңіз.", RU: "Лимит загрузок на сегодня исчерпан."},
	"quota_bytes":   {KK: "Бүгінгі файл көлемінің шегіне жеттіңіз.", RU: "Лимит объёма файлов на сегодня исчерпан."},
	"file_in_job":   {KK: "Файл тапсырысқа қосылған: оны тапсырыспен бірге болдырмаңыз.", RU: "Файл уже в заказе: отмените заказ."},
	"link_expired":  {KK: "Сілтеменің мерзімі өтті. Тақтаны жаңартыңыз.", RU: "Ссылка устарела. Обновите доску."},
	"file_deleted":  {KK: "Файл сақтау мерзімі өткен соң өшірілді.", RU: "Файл удалён по сроку хранения."},
	"file_notfound": {KK: "Файл табылмады.", RU: "Файл не найден."},
}

func fileFail(w http.ResponseWriter, r *http.Request, status int, code string) {
	httpx.Error(w, r, status, code, fileMsgs[code])
}

// ---------- recognising a file ----------

// Sniff identifies an upload by its bytes: pdf, jpg, png, docx, xlsx, pptx, "macro" (a macro-enabled Office
// file) or "" (anything else, including an .exe or .html renamed to .pdf, old .doc, a plain ZIP).
func Sniff(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.HasPrefix(b, []byte("%PDF-")):
		return "pdf"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 4 && bytes.Equal(b[:4], []byte("PK\x03\x04")):
		return sniffOffice(b)
	}
	return ""
}

func sniffOffice(b []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(zr.File) > 10000 {
		return ""
	}
	has := map[string]bool{}
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		has[name] = true
		if strings.HasSuffix(name, "vbaproject.bin") {
			return "macro"
		}
	}
	if !has["[content_types].xml"] {
		return ""
	}
	if ct := zipEntry(zr, "[Content_Types].xml", 256<<10); bytes.Contains(bytes.ToLower(ct), []byte("macroenabled")) {
		return "macro"
	}
	switch {
	case has["word/document.xml"]:
		return "docx"
	case has["xl/workbook.xml"]:
		return "xlsx"
	case has["ppt/presentation.xml"]:
		return "pptx"
	}
	return ""
}

// zipEntry reads one small entry (a zip bomb is cut at max bytes).
func zipEntry(zr *zip.Reader, name string, max int64) []byte {
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		data, _ := io.ReadAll(io.LimitReader(rc, max))
		return data
	}
	return nil
}

var (
	reAppPages  = regexp.MustCompile(`<(?:\w+:)?Pages>\s*(\d{1,5})\s*</`)
	reAppSlides = regexp.MustCompile(`<(?:\w+:)?Slides>\s*(\d{1,5})\s*</`)
)

// OfficeHint is the page (DOCX) or slide (PPTX) count the document itself claims, 0 when it says nothing.
// Word writes it at save time, so it can be stale: only a pre-fill for the student, never the price.
func OfficeHint(b []byte, kind string) int {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return 0
	}
	app := zipEntry(zr, "docProps/app.xml", 64<<10)
	re := reAppPages
	if kind == "pptx" {
		re = reAppSlides
	} else if kind != "docx" {
		return 0
	}
	m := re.FindSubmatch(app)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	if n < 1 || n > maxPages {
		return 0
	}
	return n
}

var pdfSem = make(chan struct{}, 2)

// PDFPages counts a PDF's pages on the server, or 0 when the file cannot be read in time (encrypted with a
// password, broken, hostile). The parser never runs anything in the file.
func PDFPages(ctx context.Context, data []byte) int {
	select {
	case pdfSem <- struct{}{}:
		defer func() { <-pdfSem }()
	case <-ctx.Done():
		return 0
	}
	done := make(chan int, 1)
	go func() {
		n := 0
		defer func() {
			if recover() != nil {
				n = 0
			}
			done <- n
		}()
		rd, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		n = rd.NumPage()
	}()
	select {
	case n := <-done:
		if n < 1 || n > maxPages {
			return 0
		}
		return n
	case <-time.After(5 * time.Second):
		return 0
	case <-ctx.Done():
		return 0
	}
}

// Inspect validates an upload and returns its kind and what the server knows of its pages.
func Inspect(ctx context.Context, data []byte) (kind string, pages, hint int, err error) {
	if len(data) == 0 {
		return "", 0, 0, errNoFile
	}
	if len(data) > maxFileBytes {
		return "", 0, 0, errLarge
	}
	kind = Sniff(data)
	switch kind {
	case "":
		return "", 0, 0, errType
	case "macro":
		return "", 0, 0, errMacro
	case "pdf":
		return kind, PDFPages(ctx, data), 0, nil
	case "jpg", "png":
		var cfg image.Config
		if kind == "jpg" {
			cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
		} else {
			cfg, err = png.DecodeConfig(bytes.NewReader(data))
		}
		if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > maxPixels {
			return "", 0, 0, errInvalid
		}
		return kind, 1, 0, nil
	default:
		return kind, 0, OfficeHint(data, kind), nil
	}
}

var (
	errType    = errors.New("file_type")
	errMacro   = errors.New("file_macro")
	errLarge   = errors.New("file_large")
	errInvalid = errors.New("file_invalid")
	errNoFile  = errors.New("file_no_file")
)

// DisplayName keeps letters, digits and a few signs of the student's file name, with the real extension.
func DisplayName(orig, kind string) string {
	base := filepath.Base(strings.ReplaceAll(orig, "\\", "/"))
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	s := strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == ' ' || c == '-' || c == '_' || c == '(' || c == ')' || c == '.' {
			return c
		}
		return ' '
	}, base)
	s = strings.Trim(strings.Join(strings.Fields(s), " "), ". ")
	if s == "" {
		s = "file"
	}
	if r := []rune(s); len(r) > 100 {
		s = string(r[:100])
	}
	return s + "." + kind
}

func randomName(kind string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]) + "." + kind, nil
}

func saveFile(name string, data []byte) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".up-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

func removeFile(name string) error {
	if !reFileName.MatchString(name) {
		return fmt.Errorf("print: invalid file name")
	}
	if err := os.Remove(filepath.Join(Dir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ---------- POST /api/print/uploads ----------

// Upload is the student's view of one uploaded file.
type Upload struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Bytes       int        `json:"bytes"`
	Pages       *int       `json:"pages"`     // counted by the server; null = the student declares
	PagesHint   *int       `json:"pagesHint"` // the Office file's own claim, a pre-fill only
	JobID       *int64     `json:"jobId"`
	CreatedAt   time.Time  `json:"createdAt"`
	Deleted     bool       `json:"deleted"`
	URL         string     `json:"url,omitempty"` // a short-lived signed link
	URLExpires  *time.Time `json:"urlExpiresAt,omitempty"`
	fileName    string
	ownerUserID *int64
}

const fileCols = `f.id, f.display_name, f.kind, f.bytes, f.pages, f.pages_hint, f.job_id, f.created_at, f.deleted_at is not null, f.file_name, f.user_id`

func scanFile(row pgx.Row) (Upload, error) {
	var u Upload
	err := row.Scan(&u.ID, &u.Name, &u.Kind, &u.Bytes, &u.Pages, &u.PagesHint, &u.JobID, &u.CreatedAt, &u.Deleted, &u.fileName, &u.ownerUserID)
	return u, err
}

func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileBytes+64<<10)
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, "", errInvalid
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", errInvalid
	}
	var data []byte
	var orig string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", tooBig(err)
		}
		if part.FormName() != "file" || data != nil {
			part.Close()
			return nil, "", errInvalid
		}
		orig = part.FileName()
		data, err = io.ReadAll(io.LimitReader(part, maxFileBytes+1))
		part.Close()
		if err != nil {
			return nil, "", tooBig(err)
		}
		if len(data) > maxFileBytes {
			return nil, "", errLarge
		}
	}
	if len(data) == 0 {
		return nil, "", errNoFile
	}
	return data, orig, nil
}

func tooBig(err error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return errLarge
	}
	return errInvalid
}

func uploadErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errLarge):
		fileFail(w, r, http.StatusRequestEntityTooLarge, "file_large")
	case errors.Is(err, errType):
		fileFail(w, r, http.StatusUnsupportedMediaType, "file_type")
	case errors.Is(err, errMacro):
		fileFail(w, r, http.StatusUnsupportedMediaType, "file_macro")
	case errors.Is(err, errNoFile):
		fileFail(w, r, http.StatusBadRequest, "file_no_file")
	default:
		fileFail(w, r, http.StatusBadRequest, "file_invalid")
	}
}

// uploadFile: POST /api/print/uploads (multipart, one "file" part) → 201 {upload}. The file is private and
// belongs to no job until the student places one.
func uploadFile(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !consented(w, r, u) {
		return
	}
	if limited(w, r, "upload:"+itoa(u.ID), uploadsPerHour(), time.Hour) {
		return
	}
	ctx := r.Context()
	data, orig, err := readUpload(w, r)
	if err != nil {
		uploadErr(w, r, err)
		return
	}
	kind, pages, hint, err := Inspect(ctx, data)
	if err != nil {
		uploadErr(w, r, err)
		return
	}
	name, err := randomName(kind)
	if err != nil {
		fail(w, r, "file name", err)
		return
	}
	sum := sha256.Sum256(data)
	var up Upload
	saved := false
	now := Now()
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		// one student's uploads are counted one at a time: the daily quota cannot race
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('print:upload:' || $1::text, 0))`, u.ID); err != nil {
			return err
		}
		var n int
		var total int64
		if err := tx.QueryRow(ctx, `select count(*), coalesce(sum(bytes), 0) from print_files where user_id = $1 and created_at >= $2`,
			u.ID, now.Add(-24*time.Hour)).Scan(&n, &total); err != nil {
			return err
		}
		if n >= dailyFiles() {
			return refuse(http.StatusTooManyRequests, "quota_files", fileMsgs["quota_files"])
		}
		if total+int64(len(data)) > dailyBytes() {
			return refuse(http.StatusTooManyRequests, "quota_bytes", fileMsgs["quota_bytes"])
		}
		var pp, hp *int
		if pages > 0 {
			pp = &pages
		}
		if hint > 0 {
			hp = &hint
		}
		var err error
		up, err = scanFile(tx.QueryRow(ctx, `insert into print_files as f (user_id, file_name, display_name, kind, bytes, pages, pages_hint, sha256)
			values ($1, $2, $3, $4, $5, $6, $7, $8) returning `+fileCols,
			u.ID, name, DisplayName(orig, kind), kind, len(data), pp, hp, hex.EncodeToString(sum[:])))
		if err != nil {
			return err
		}
		// written before the commit; a failed commit removes it below
		if err := saveFile(name, data); err != nil {
			return err
		}
		saved = true
		return nil
	})
	if err != nil {
		if saved {
			if rmErr := removeFile(name); rmErr != nil {
				log.Printf("print: remove orphan upload: %v", rmErr)
			}
		}
		reply(w, r, "upload", err)
		return
	}
	signFor(&up, u.ID, now)
	httpx.JSON(w, http.StatusCreated, map[string]any{"upload": up})
}

// deleteUpload: DELETE /api/print/uploads/{id}: a file not yet in a job.
func deleteUpload(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var name string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var job *int64
		err := tx.QueryRow(ctx, `select file_name, job_id from print_files where id = $1 and user_id = $2 and deleted_at is null for update`, id, u.ID).Scan(&name, &job)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusNotFound, "file_notfound", fileMsgs["file_notfound"])
		}
		if err != nil {
			return err
		}
		if job != nil {
			return refuse(http.StatusConflict, "file_in_job", fileMsgs["file_in_job"])
		}
		_, err = tx.Exec(ctx, `update print_files set deleted_at = now() where id = $1`, id)
		return err
	})
	if err != nil {
		reply(w, r, "delete upload", err)
		return
	}
	if err := removeFile(name); err != nil {
		log.Printf("print: remove upload %d: %v", id, err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- signed links ----------

func linkKey() []byte {
	m := hmac.New(sha256.New, []byte(config.Str("SESSION_SECRET", "")))
	m.Write([]byte("print-file-link-v1"))
	return m.Sum(nil)
}

func linkSig(fileID, viewer, exp int64) string {
	m := hmac.New(sha256.New, linkKey())
	fmt.Fprintf(m, "%d.%d.%d", fileID, viewer, exp)
	return hex.EncodeToString(m.Sum(nil))
}

// SignedURL is a download link for one viewer, valid linkTTL.
func SignedURL(fileID, viewer int64, now time.Time) (string, time.Time) {
	exp := now.Add(linkTTL).Truncate(time.Second)
	return fmt.Sprintf("/api/print/files/%d?v=%d&e=%d&s=%s", fileID, viewer, exp.Unix(), linkSig(fileID, viewer, exp.Unix())), exp
}

func signFor(f *Upload, viewer int64, now time.Time) {
	if f.Deleted {
		return
	}
	url, exp := SignedURL(f.ID, viewer, now)
	f.URL, f.URLExpires = url, &exp
}

// serveFile: GET /api/print/files/{id}?v=<viewer>&e=<unix>&s=<hmac>. The signature binds the file, the viewer
// and the expiry; at serve time the viewer must still be the file's student or staff of the job's shop. The file
// is downloaded, never rendered: fixed Content-Type, nosniff, a sandboxing CSP, attachment, no caching.
func serveFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	q := r.URL.Query()
	viewer, err1 := strconv.ParseInt(q.Get("v"), 10, 64)
	exp, err2 := strconv.ParseInt(q.Get("e"), 10, 64)
	sig := q.Get("s")
	if !ok || err1 != nil || err2 != nil || viewer <= 0 || len(sig) != 64 ||
		!hmac.Equal([]byte(sig), []byte(linkSig(id, viewer, exp))) {
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
		return
	}
	now := Now()
	if exp < now.Unix() || exp > now.Add(linkTTL+time.Minute).Unix() {
		fileFail(w, r, http.StatusGone, "link_expired")
		return
	}
	ctx := r.Context()
	var name, display, kind string
	var deleted, allowed bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select f.file_name, f.display_name, f.kind, f.deleted_at is not null,
				f.user_id = $2 or exists (select 1 from print_jobs j join print_staff s on s.shop_id = j.shop_id where j.id = f.job_id and s.user_id = $2)
			from print_files f where f.id = $1`, id, viewer).Scan(&name, &display, &kind, &deleted, &allowed)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		fileFail(w, r, http.StatusNotFound, "file_notfound")
		return
	}
	if err != nil {
		fail(w, r, "file", err)
		return
	}
	if deleted {
		fileFail(w, r, http.StatusGone, "file_deleted")
		return
	}
	f, err := os.Open(filepath.Join(Dir(), name))
	if err != nil {
		fileFail(w, r, http.StatusGone, "file_deleted")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		fileFail(w, r, http.StatusGone, "file_deleted")
		return
	}
	h := w.Header()
	h.Set("Content-Type", mimeOf[kind])
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": display}))
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

var mimeOf = map[string]string{
	"pdf":  "application/pdf",
	"jpg":  "image/jpeg",
	"png":  "image/png",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// ---------- retention ----------

// RetentionResult counts one retention pass.
type RetentionResult struct {
	Jobs    int `json:"jobs"`
	Files   int `json:"files"`
	Orphans int `json:"orphans"`
}

// Retain deletes the bytes of closed jobs' files after the shop's retention_days (a cancelled job's at once),
// and uploads never placed in a job after a day. The rows stay (without bytes) for the audit trail.
func Retain(ctx context.Context) (RetentionResult, error) {
	var res RetentionResult
	now := Now()
	type gone struct {
		id   int64
		name string
	}
	var files []gone
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `update print_jobs j set files_deleted_at = now()
			from print_shops s where s.id = j.shop_id and j.files_deleted_at is null and j.closed_at is not null
			  and (j.closed_at < $1::timestamptz - make_interval(days => s.retention_days::int) or (j.status = 'cancelled' and j.closed_at < $1::timestamptz - interval '10 minutes'))
			returning j.id`, now)
		if err != nil {
			return err
		}
		jobs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		res.Jobs = len(jobs)
		for _, id := range jobs {
			if err := event(ctx, tx, id, nil, "system", "files_deleted", nil); err != nil {
				return err
			}
		}
		rows, err = tx.Query(ctx, `update print_files set deleted_at = now()
			where deleted_at is null and (job_id = any($1) or (job_id is null and created_at < $2)) returning id, file_name, job_id is null`, jobs, now.Add(-orphanAfter))
		if err != nil {
			return err
		}
		for rows.Next() {
			var g gone
			var orphan bool
			if err := rows.Scan(&g.id, &g.name, &orphan); err != nil {
				rows.Close()
				return err
			}
			if orphan {
				res.Orphans++
			} else {
				res.Files++
			}
			files = append(files, g)
		}
		rows.Close()
		return rows.Err()
	})
	if err != nil {
		return res, err
	}
	for _, f := range files {
		if err := removeFile(f.name); err != nil {
			log.Printf("print: retention: remove file %d: %v", f.id, err)
		}
	}
	return res, nil
}

// deleteUserFiles is the account-deletion hook: every file the student uploaded goes (the jobs stay for the
// shop, anonymized).
func deleteUserFiles(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `update print_files set deleted_at = now() where user_id = $1 and deleted_at is null returning file_name`, userID)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := removeFile(n); err != nil {
			return fmt.Errorf("delete account print file: %w", err)
		}
	}
	return nil
}

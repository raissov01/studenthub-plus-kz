package portfolio

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

// a small but structurally complete PDF
func pdfBytes() []byte {
	return []byte("%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [] /Count 0 >> endobj\n" +
		"trailer << /Root 1 0 R >>\n%%EOF\n")
}

// a JPEG with an EXIF block carrying a GPS marker
func jpegWithExif(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	exif := append([]byte("Exif\x00\x00GPSLatitude 51.1694 GPSLongitude 71.4491 SecretCamera"), make([]byte, 16)...)
	seg := append([]byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	return append(append(append([]byte{}, b[:2]...), seg...), b[2:]...)
}

func multipartCert(t *testing.T, fields map[string]string, filename string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if data != nil {
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

func send(t *testing.T, method, path string, uid int64, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	r.Header.Set("X-Real-Ip", "10.8."+strconv.FormatInt(seq.Add(1)%250, 10)+".1")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if uid != 0 {
		var ver int
		if err := db.Tx(ctx(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx(), "select session_version from users where id = $1", uid).Scan(&ver)
		}); err != nil {
			t.Fatal(err)
		}
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: ver, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	m := http.NewServeMux()
	Routes(m)
	me.Routes(m)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { notFound(w, r) })
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func errCodeOf(w *httptest.ResponseRecorder) string {
	var out struct{ Error struct{ Code string } }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out.Error.Code
}

var validFields = map[string]string{"title": "IELTS Academic 7.5", "issuer": "British Council", "issuedOn": "2026-05-12",
	"credentialId": "ABC-123", "credentialUrl": "https://example.org/verify/ABC-123", "category": "language"}

func addCert(t *testing.T, uid int64, filename string, data []byte) Cert {
	t.Helper()
	body, ct := multipartCert(t, validFields, filename, data)
	w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	var out struct{ Certificate Cert }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Certificate
}

func fileName(c Cert) string { return strings.TrimPrefix(c.URL, "/api/portfolio/files/") }

func TestCertMagicBytesRejectRenamedFiles(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	uid, _ := newUser(t, "Магия")
	exe := append([]byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff"), bytes.Repeat([]byte{0}, 200)...)
	html := []byte("<!DOCTYPE html><html><script>alert(document.cookie)</script></html>")
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)
	pdfInHTML := []byte("<html>%PDF-1.4\n%%EOF")
	for name, c := range map[string]struct {
		data   []byte
		status int
		code   string
	}{
		"exe":         {exe, 415, "cert_file_type"},
		"html":        {html, 415, "cert_file_type"},
		"svg":         {svg, 415, "cert_file_type"},
		"pdf-in-html": {pdfInHTML, 415, "cert_file_type"},
		"truncated":   {[]byte("%PDF-1.7\n1 0 obj <<>> endobj\n"), 400, "cert_invalid"},
	} {
		body, ct := multipartCert(t, validFields, "certificate.pdf", c.data)
		w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body)
		if w.Code != c.status || errCodeOf(w) != c.code {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// too large: the size limit is checked while reading
	big := append(pdfBytes(), bytes.Repeat([]byte{' '}, CertMaxPDF)...)
	body, ct := multipartCert(t, validFields, "big.pdf", big)
	if w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body); w.Code != 413 || errCodeOf(w) != "cert_too_large" {
		t.Errorf("big: %d %s", w.Code, w.Body)
	}
	// a field that is not accepted (a new file cannot be uploaded as public or verified)
	body, ct = multipartCert(t, map[string]string{"title": "x", "verified": "true"}, "a.pdf", pdfBytes())
	if w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body); w.Code != 400 {
		t.Errorf("unknown field: %d %s", w.Code, w.Body)
	}
	// missing file / missing title / bad url
	body, ct = multipartCert(t, validFields, "", nil)
	if w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body); errCodeOf(w) != "cert_no_file" {
		t.Errorf("no file: %d %s", w.Code, w.Body)
	}
	body, ct = multipartCert(t, map[string]string{"issuer": "x"}, "a.pdf", pdfBytes())
	if w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body); errCodeOf(w) != "invalid_title" {
		t.Errorf("no title: %d %s", w.Code, w.Body)
	}
	body, ct = multipartCert(t, map[string]string{"title": "x", "credentialUrl": "javascript:alert(1)"}, "a.pdf", pdfBytes())
	if w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body); errCodeOf(w) != "invalid_credentialUrl" {
		t.Errorf("bad url: %d %s", w.Code, w.Body)
	}
	entries, _ := os.ReadDir(CertDir())
	if len(entries) != 0 {
		t.Fatalf("rejected uploads left files: %v", entries)
	}

	// a real PDF named .exe is still a PDF: the bytes decide, not the name
	c := addCert(t, uid, "virus.exe", pdfBytes())
	if c.Kind != "pdf" || c.Mime != "application/pdf" || c.Public || c.Category != "language" || c.IssuedOn == nil || *c.IssuedOn != "2026-05-12" {
		t.Fatalf("pdf: %+v", c)
	}
	stored, err := os.ReadFile(filepath.Join(CertDir(), fileName(c)))
	if err != nil || !bytes.Equal(stored, pdfBytes()) || !strings.HasSuffix(fileName(c), ".pdf") {
		t.Fatalf("stored pdf: %v", err)
	}
}

func TestCertImageReencodedWithoutMetadata(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	uid, _ := newUser(t, "Сурет")
	src := jpegWithExif(t, 2400, 1200)
	if !bytes.Contains(src, []byte("GPSLatitude")) {
		t.Fatal("fixture has no EXIF")
	}
	c := addCert(t, uid, "photo.pdf", src) // the wrong extension does not matter either
	if c.Kind != "image" || c.Mime != "image/jpeg" || c.Width == nil || *c.Width != 1600 || *c.Height != 800 {
		t.Fatalf("image: %+v", c)
	}
	stored, err := os.ReadFile(filepath.Join(CertDir(), fileName(c)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("Exif")) || bytes.Contains(stored, []byte("GPSLatitude")) || bytes.Contains(stored, []byte("SecretCamera")) {
		t.Fatal("metadata survived the re-encode")
	}
	if _, err := jpeg.Decode(bytes.NewReader(stored)); err != nil || !strings.HasSuffix(fileName(c), ".jpg") {
		t.Fatalf("stored image: %v", err)
	}
	w := send(t, "GET", c.URL, uid, "", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("serve image: %d %v", w.Code, w.Header())
	}
}

func TestCertQuota(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	uid, _ := newUser(t, "Квота")
	for i := range CertMaxFiles {
		exec(t, `insert into portfolio_certificates (user_id, title, file_name, mime, bytes) values ($1, 'x', $2, 'application/pdf', 10)`,
			uid, strings.Repeat("0", 28)+strconv.Itoa(1000+i)+".pdf")
	}
	body, ct := multipartCert(t, validFields, "a.pdf", pdfBytes())
	if w := send(t, "POST", "/api/portfolio/certificates", uid, ct, body); w.Code != 409 || errCodeOf(w) != "cert_quota_files" {
		t.Fatalf("file quota: %d %s", w.Code, w.Body)
	}
	if entries, _ := os.ReadDir(CertDir()); len(entries) != 0 {
		t.Fatalf("refused upload left a file: %v", entries)
	}
	// list reports the usage
	w := send(t, "GET", "/api/portfolio/certificates", uid, "", nil)
	var list struct {
		Certificates []Cert
		Usage        Usage
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Usage.Files != CertMaxFiles || list.Usage.MaxBytes != CertMaxBytes {
		t.Fatalf("usage: %d %s", w.Code, w.Body)
	}

	other, _ := newUser(t, "Байт")
	exec(t, `insert into portfolio_certificates (user_id, title, file_name, mime, bytes) values ($1, 'x', $2, 'application/pdf', $3)`,
		other, strings.Repeat("9", 32)+".pdf", CertMaxPDF)
	for i := range 9 {
		exec(t, `insert into portfolio_certificates (user_id, title, file_name, mime, bytes) values ($1, 'x', $2, 'application/pdf', $3)`,
			other, strings.Repeat("8", 31)+strconv.Itoa(i)+".pdf", CertMaxPDF)
	}
	body, ct = multipartCert(t, validFields, "a.pdf", pdfBytes())
	if w := send(t, "POST", "/api/portfolio/certificates", other, ct, body); w.Code != 409 || errCodeOf(w) != "cert_quota_bytes" {
		t.Fatalf("byte quota: %d %s", w.Code, w.Body)
	}
}

func TestCertRowLevelSecurityOwnerOnly(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	a, _ := newUser(t, "Иесі")
	b, _ := newUser(t, "Бөтен")
	c := addCert(t, a, "a.pdf", pdfBytes())

	// the API: B cannot see, edit, publish or delete A's certificate
	w := send(t, "GET", "/api/portfolio/certificates", b, "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), c.URL) {
		t.Fatalf("B's list: %d %s", w.Code, w.Body)
	}
	id := strconv.FormatInt(c.ID, 10)
	pub, _ := json.Marshal(map[string]any{"public": true})
	if w := send(t, "PATCH", "/api/portfolio/certificates/"+id, b, "application/json", pub); w.Code != 404 {
		t.Fatalf("B patch: %d %s", w.Code, w.Body)
	}
	if w := send(t, "DELETE", "/api/portfolio/certificates/"+id, b, "", nil); w.Code != 404 {
		t.Fatalf("B delete: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(CertDir(), fileName(c))); err != nil {
		t.Fatal("B's delete removed A's file")
	}

	// the database itself, as app_user B
	var seen int
	err := db.WithUser(ctx(), b, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx(), `select count(*) from portfolio_certificates where id = $1`, c.ID).Scan(&seen); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx(), `update portfolio_certificates set is_public = true where id = $1`, c.ID)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("B updated A's row: %v %v", err, tag)
		}
		return nil
	})
	if err != nil || seen != 0 {
		t.Fatalf("RLS read: %v %d", err, seen)
	}
	err = db.WithUser(ctx(), b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `insert into portfolio_certificates (user_id, title, file_name, mime, bytes) values ($1, 'x', $2, 'application/pdf', 1)`,
			a, strings.Repeat("7", 32)+".pdf")
		return err
	})
	if err == nil {
		t.Fatal("B inserted a row for A")
	}
	err = db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update portfolio_certificates set file_name = $2 where id = $1`, c.ID, strings.Repeat("6", 32)+".pdf")
		return err
	})
	if err == nil {
		t.Fatal("the owner could repoint the file name")
	}
}

func TestCertPrivateFileForbiddenForOthers(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	a, _ := newUser(t, "Жеке")
	b, _ := newUser(t, "Көрші")
	c := addCert(t, a, "IELTS.pdf", pdfBytes())

	w := send(t, "GET", c.URL, a, "", nil)
	h := w.Header()
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), pdfBytes()) {
		t.Fatalf("owner: %d %s", w.Code, w.Body)
	}
	if h.Get("Content-Type") != "application/pdf" || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(h.Get("Content-Disposition"), "inline; filename=") || !strings.Contains(h.Get("Content-Disposition"), "IELTS Academic 7.5.pdf") ||
		!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") ||
		h.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("owner headers: %v", h)
	}
	for name, uid := range map[string]int64{"other student": b, "anonymous": 0} {
		if w := send(t, "GET", c.URL, uid, "", nil); w.Code != 403 || bytes.Contains(w.Body.Bytes(), []byte("%PDF")) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// unknown or malformed names are 404, never a directory listing
	for _, p := range []string{"/api/portfolio/files/" + strings.Repeat("a", 32) + ".pdf", "/api/portfolio/files/..%2f..%2fetc%2fpasswd", "/api/portfolio/files/x.pdf"} {
		if w := send(t, "GET", p, a, "", nil); w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
}

func TestCertPublicOnlyWhenPortfolioPublic(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	a, _ := newUser(t, "Жария")
	b, _ := newUser(t, "Қонақ")
	c := addCert(t, a, "a.pdf", pdfBytes())
	slug := settingsOf(t, do(t, "GET", "/api/portfolio/me", a, nil)).Slug
	id := strconv.FormatInt(c.ID, 10)
	patch := func(v bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"public": v})
		w := send(t, "PATCH", "/api/portfolio/certificates/"+id, a, "application/json", body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"public":`+strconv.FormatBool(v)) {
			t.Fatalf("patch public=%v: %d %s", v, w.Code, w.Body)
		}
	}
	visible := func() bool {
		t.Helper()
		w := send(t, "GET", c.URL, 0, "", nil)
		w2 := send(t, "GET", c.URL, b, "", nil)
		if w.Code != w2.Code {
			t.Fatalf("anonymous %d vs signed-in %d", w.Code, w2.Code)
		}
		return w.Code == 200
	}
	onPage := func() bool {
		t.Helper()
		w := do(t, "GET", "/api/p/"+slug, 0, nil)
		if w.Code == 404 {
			return false
		}
		var p Page
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		for _, f := range p.SelfReported.Files {
			if f.URL == c.URL {
				if f.Verified || f.Title != validFields["title"] || f.Kind != "pdf" || strings.Contains(w.Body.String(), `"id"`) {
					t.Fatalf("public entry: %+v", f)
				}
				return true
			}
		}
		return false
	}

	patch(true) // public file, page still off
	if visible() || onPage() {
		t.Fatal("public file visible while the portfolio page is off")
	}
	if w := do(t, "PATCH", "/api/portfolio/settings", a, map[string]any{"enabled": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if !visible() || !onPage() {
		t.Fatal("public file hidden on an enabled page")
	}
	if w := send(t, "GET", c.URL, 0, "", nil); w.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("public headers: %v", w.Header())
	}
	// the certificates block off hides it too
	do(t, "PATCH", "/api/portfolio/settings", a, map[string]any{"showCerts": false})
	if visible() || onPage() {
		t.Fatal("visible with the certificates block off")
	}
	do(t, "PATCH", "/api/portfolio/settings", a, map[string]any{"showCerts": true})
	patch(false) // private again
	if visible() || onPage() {
		t.Fatal("private file still visible")
	}
	if w := send(t, "GET", c.URL, 0, "", nil); w.Code != 403 {
		t.Fatalf("private file for anonymous: %d", w.Code)
	}
	patch(true)
	do(t, "PATCH", "/api/portfolio/settings", a, map[string]any{"enabled": false})
	if visible() {
		t.Fatal("visible after the page was turned off")
	}
}

func TestCertEditAndDeleteRemovesFile(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	uid, _ := newUser(t, "Өшіру")
	c := addCert(t, uid, "a.pdf", pdfBytes())
	id := strconv.FormatInt(c.ID, 10)
	body, _ := json.Marshal(map[string]any{"title": "Олимпиада, 1-орын", "category": "olympiad", "issuedOn": nil, "credentialUrl": ""})
	w := send(t, "PATCH", "/api/portfolio/certificates/"+id, uid, "application/json", body)
	var out struct{ Certificate Cert }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Certificate.Title != "Олимпиада, 1-орын" ||
		out.Certificate.Category != "olympiad" || out.Certificate.IssuedOn != nil || out.Certificate.CredentialURL != "" || out.Certificate.Issuer != "British Council" {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	for _, bad := range []map[string]any{{"category": "money"}, {"title": ""}, {"issuedOn": "2099-01-01"}, {"public": "yes"}, {"fileName": "x.pdf"}, {"verified": true}} {
		body, _ := json.Marshal(bad)
		if w := send(t, "PATCH", "/api/portfolio/certificates/"+id, uid, "application/json", body); w.Code != 400 {
			t.Errorf("patch %v: %d %s", bad, w.Code, w.Body)
		}
	}
	path := filepath.Join(CertDir(), fileName(c))
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if w := send(t, "DELETE", "/api/portfolio/certificates/"+id, uid, "", nil); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file survived the delete: %v", err)
	}
	if w := send(t, "GET", c.URL, uid, "", nil); w.Code != 404 {
		t.Fatalf("deleted file served: %d", w.Code)
	}
	if w := send(t, "DELETE", "/api/portfolio/certificates/"+id, uid, "", nil); w.Code != 404 {
		t.Fatalf("second delete: %d", w.Code)
	}
}

func TestCertAccountDeletionRemovesFiles(t *testing.T) {
	testdb.Need(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	uid, _ := newUser(t, "Кету")
	keep, _ := newUser(t, "Қалу")
	c := addCert(t, uid, "a.pdf", pdfBytes())
	k := addCert(t, keep, "b.pdf", pdfBytes())
	body, _ := json.Marshal(map[string]any{"confirm": "DELETE"})
	if w := send(t, "DELETE", "/api/me", uid, "application/json", body); w.Code != 200 {
		t.Fatalf("delete account: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(CertDir(), fileName(c))); !os.IsNotExist(err) {
		t.Fatal("certificate file survived the account deletion")
	}
	if _, err := os.Stat(filepath.Join(CertDir(), fileName(k))); err != nil {
		t.Fatal("another student's file was removed")
	}
}

func TestSniffCert(t *testing.T) {
	for in, want := range map[string]string{
		"%PDF-1.7\n": "pdf", "%PDF-2.0\n": "pdf", "%PDF-x.y\n": "", "MZ\x90\x00\x03\x00\x00\x00": "", "<html><body>": "",
		"\xff\xd8\xff\xe0": "jpeg", "\x89PNG\r\n\x1a\n": "png", "RIFF\x00\x00\x00\x00WEBP": "webp", "GIF89a": "", "": "",
	} {
		if got := SniffCert([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if got := dispositionName("../../etc/passwd\r\nX: y", "pdf"); strings.ContainsAny(got, "/\r\n:") || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("disposition: %q", got)
	}
	if got := dispositionName("  ", "jpg"); got != "certificate.jpg" {
		t.Errorf("empty disposition: %q", got)
	}
}

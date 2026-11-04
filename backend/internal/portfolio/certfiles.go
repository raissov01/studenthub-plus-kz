package portfolio

// Certificate files (PDF, JPG, PNG) in the portfolio. The student uploads a file with a title,
// issuer, date, optional credential id / link and a category. Images are decoded and re-encoded
// as JPEG (≤1600 px) on the server, so EXIF, GPS, XMP and every other metadata block is gone;
// PDFs are accepted only when the bytes really are a PDF (magic bytes, not the file name or
// Content-Type) and are stored as they are. Files live under UPLOAD_DIR/certs with random
// names; portfolio_certificates is the registry (RLS: owner only).
//
// Every file is private until the student turns on its "public" flag; a public file is served
// to others only while the public portfolio page is enabled with the certificates block on.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
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
	"github.com/jackc/pgx/v5/pgconn"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
)

// Certificate file limits (the quota numbers are also enforced by the database trigger).
const (
	CertMaxPDF      = 10 << 20  // a PDF as uploaded
	CertMaxImage    = 8 << 20   // a JPG / PNG / WebP as uploaded (the client usually sends a ≤1600 px WebP)
	CertMaxFiles    = 30        // files per student
	CertMaxBytes    = 100 << 20 // stored bytes per student
	CertMaxSide     = 1600      // longest side of a stored image
	certMaxPixels   = 12_000_000
	certUploadsHour = 30
)

// CertCategories are the accepted categories, in display order.
var CertCategories = []string{"course", "olympiad", "competition", "volunteering", "language", "other"}

var certNameRe = regexp.MustCompile(`^[0-9a-f]{32}\.(pdf|jpg)$`)

// CertDir is where certificate files live: $UPLOAD_DIR/certs (default ./uploads/certs).
func CertDir() string { return filepath.Join(config.Str("UPLOAD_DIR", "./uploads"), "certs") }

var certMsgs = map[string]httpx.Msg{
	"cert_file_type":   {KK: "Тек PDF, JPG немесе PNG файл қосуға болады.", RU: "Можно добавить только PDF, JPG или PNG."},
	"cert_too_large":   {KK: "Файл тым үлкен: PDF — 10 МБ-қа дейін, сурет — 8 МБ-қа дейін.", RU: "Файл слишком большой: PDF — до 10 МБ, фото — до 8 МБ."},
	"cert_invalid":     {KK: "Файлды оқу мүмкін болмады. Басқа файл таңдаңыз.", RU: "Не удалось прочитать файл. Выберите другой."},
	"cert_dimensions":  {KK: "Суреттің өлшемі тым үлкен.", RU: "Слишком большое разрешение фото."},
	"cert_no_file":     {KK: "Сертификат файлын таңдаңыз.", RU: "Выберите файл сертификата."},
	"cert_quota_files": {KK: "Ең көбі 30 сертификат файлы сақталады. Ескісін өшіріңіз.", RU: "Можно хранить не больше 30 файлов. Удалите старый."},
	"cert_quota_bytes": {KK: "Файлдарға 100 МБ орын бөлінген, ол толды. Ескісін өшіріңіз.", RU: "Место для файлов (100 МБ) закончилось. Удалите старый."},
	"cert_not_found":   {KK: "Сертификат табылмады.", RU: "Сертификат не найден."},
}

func certFail(w http.ResponseWriter, r *http.Request, code string, status int) {
	m, ok := certMsgs[code]
	if !ok {
		m = MsgInvalid
	}
	httpx.Error(w, r, status, code, m)
}

// certRoutes wires the certificate endpoints (called from Routes, behind the same switch).
func certRoutes(h func(string, http.HandlerFunc)) {
	h("GET /api/portfolio/certificates", auth.Need(listCerts))
	h("POST /api/portfolio/certificates", auth.Write(auth.Need(uploadCert)))
	h("PATCH /api/portfolio/certificates/{id}", auth.Write(auth.Need(patchCert)))
	h("DELETE /api/portfolio/certificates/{id}", auth.Write(auth.Need(deleteCert)))
	h("GET /api/portfolio/files/{name}", serveCertFile)
}

// Cert is the owner's view of a certificate file.
type Cert struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	Issuer        string    `json:"issuer"`
	IssuedOn      *string   `json:"issuedOn"` // YYYY-MM-DD
	CredentialID  string    `json:"credentialId"`
	CredentialURL string    `json:"credentialUrl"`
	Category      string    `json:"category"`
	Kind          string    `json:"kind"` // pdf | image
	Mime          string    `json:"mime"`
	Bytes         int       `json:"bytes"`
	Width         *int      `json:"width"`
	Height        *int      `json:"height"`
	Public        bool      `json:"public"`
	URL           string    `json:"url"`
	CreatedAt     time.Time `json:"createdAt"`
}

const certCols = `id, title, issuer, to_char(issued_on, 'YYYY-MM-DD'), credential_id, credential_url, category, file_name, mime, bytes,
	width, height, is_public, created_at`

func scanCert(row pgx.Row) (Cert, string, error) {
	var c Cert
	var name string
	err := row.Scan(&c.ID, &c.Title, &c.Issuer, &c.IssuedOn, &c.CredentialID, &c.CredentialURL, &c.Category, &name, &c.Mime,
		&c.Bytes, &c.Width, &c.Height, &c.Public, &c.CreatedAt)
	c.Kind, c.URL = kindOf(c.Mime), certURL(name)
	return c, name, err
}

func kindOf(mime string) string {
	if mime == "application/pdf" {
		return "pdf"
	}
	return "image"
}

func certURL(name string) string { return "/api/portfolio/files/" + name }

// Usage is the student's quota state.
type Usage struct {
	Files    int   `json:"files"`
	Bytes    int64 `json:"bytes"`
	MaxFiles int   `json:"maxFiles"`
	MaxBytes int64 `json:"maxBytes"`
}

func usage(ctx context.Context, tx pgx.Tx) (Usage, error) {
	u := Usage{MaxFiles: CertMaxFiles, MaxBytes: CertMaxBytes}
	err := tx.QueryRow(ctx, `select count(*), coalesce(sum(bytes), 0) from portfolio_certificates where user_id = app_user_id()`).
		Scan(&u.Files, &u.Bytes)
	return u, err
}

// listCerts: GET /api/portfolio/certificates — the caller's files (newest first), the quota and
// whether the public page is on (a public file is visible only then).
func listCerts(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	certs := []Cert{}
	var use Usage
	pageOn := false
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+certCols+` from portfolio_certificates where user_id = app_user_id()
			order by created_at desc, id desc`)
		if err != nil {
			return err
		}
		certs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Cert, error) {
			c, _, err := scanCert(row)
			return c, err
		})
		if err != nil {
			return err
		}
		if use, err = usage(ctx, tx); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `select coalesce((select enabled and show_certs from portfolio_settings where user_id = app_user_id()), false)`).Scan(&pageOn)
		return err
	})
	if err != nil {
		fail(w, r, "certs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"certificates": certs, "usage": use, "pageVisible": pageOn})
}

// CertMeta is the descriptive part of a certificate (form fields or PATCH body).
type CertMeta struct {
	Title, Issuer, CredentialID, CredentialURL, Category string
	IssuedOn                                             *string
}

// cleanMeta validates the descriptive fields; get returns a field's raw value and whether it
// was sent. full = every required field must be present (upload).
func cleanMeta(get func(string) (any, bool), full bool) (m CertMeta, set map[string]bool, code string) {
	set = map[string]bool{}
	str := func(key string, max int, required bool, dst *string) bool {
		raw, ok := get(key)
		if !ok {
			return !(full && required)
		}
		v, good := cleanText(raw, max, false)
		if !good || (required && v == nil) {
			return false
		}
		*dst = ""
		if v != nil {
			*dst = *v
		}
		set[key] = true
		return true
	}
	if !str("title", 120, true, &m.Title) {
		return m, nil, "invalid_title"
	}
	if !str("issuer", 120, false, &m.Issuer) {
		return m, nil, "invalid_issuer"
	}
	if !str("credentialId", 100, false, &m.CredentialID) {
		return m, nil, "invalid_credentialId"
	}
	if !str("credentialUrl", 500, false, &m.CredentialURL) || (m.CredentialURL != "" &&
		(!strings.HasPrefix(strings.ToLower(m.CredentialURL), "https://") || strings.ContainsAny(m.CredentialURL, " <>\"'\\"))) {
		return m, nil, "invalid_credentialUrl"
	}
	m.Category = "other"
	if raw, ok := get("category"); ok {
		s, _ := raw.(string)
		if !contains(CertCategories, s) {
			return m, nil, "invalid_category"
		}
		m.Category = s
		set["category"] = true
	}
	if raw, ok := get("issuedOn"); ok {
		if raw != nil && raw != "" {
			s, _ := raw.(string)
			t, err := time.Parse("2006-01-02", s)
			if err != nil || t.Year() < 1950 || t.After(time.Now().AddDate(0, 0, 2)) {
				return m, nil, "invalid_issuedOn"
			}
			m.IssuedOn = &s
		}
		set["issuedOn"] = true
	}
	return m, set, ""
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

var (
	errCertType     = errors.New("cert_file_type")
	errCertLarge    = errors.New("cert_too_large")
	errCertInvalid  = errors.New("cert_invalid")
	errCertPixels   = errors.New("cert_dimensions")
	errCertNoFile   = errors.New("cert_no_file")
	errCertNotFound = errors.New("cert_not_found")
)

// SniffCert identifies an upload by its bytes: "pdf", "jpeg", "png", "webp" or "". The name and
// the declared Content-Type are never trusted: an .exe or .html renamed to .pdf is "".
func SniffCert(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.HasPrefix(b, []byte("%PDF-")) && (b[5] == '1' || b[5] == '2') && b[6] == '.':
		return "pdf"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// validPDF: the PDF header at byte 0 and an end-of-file marker near the end (a truncated or
// fake file fails). The content is never parsed or executed by the server.
func validPDF(b []byte) bool {
	if SniffCert(b) != "pdf" {
		return false
	}
	tail := b[max(0, len(b)-2048):]
	return bytes.Contains(tail, []byte("%%EOF"))
}

var certDecodeSem = make(chan struct{}, 1)

// ProcessCert validates an upload and returns what is stored: the PDF bytes as they are, or an
// image re-encoded as JPEG (≤ CertMaxSide px, metadata dropped).
func ProcessCert(ctx context.Context, data []byte) (out []byte, mimeType string, w, h int, err error) {
	kind := SniffCert(data)
	switch kind {
	case "pdf":
		if len(data) > CertMaxPDF {
			return nil, "", 0, 0, errCertLarge
		}
		if !validPDF(data) {
			return nil, "", 0, 0, errCertInvalid
		}
		return data, "application/pdf", 0, 0, nil
	case "jpeg", "png", "webp":
		if len(data) > CertMaxImage {
			return nil, "", 0, 0, errCertLarge
		}
	default:
		return nil, "", 0, 0, errCertType
	}
	var cfg image.Config
	switch kind {
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	default:
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, "", 0, 0, errCertInvalid
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > certMaxPixels {
		return nil, "", 0, 0, errCertPixels
	}
	select {
	case certDecodeSem <- struct{}{}:
		defer func() { <-certDecodeSem }()
	case <-ctx.Done():
		return nil, "", 0, 0, ctx.Err()
	}
	var src image.Image
	switch kind {
	case "jpeg":
		src, err = jpeg.Decode(bytes.NewReader(data))
	case "png":
		src, err = png.Decode(bytes.NewReader(data))
	default:
		src, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, "", 0, 0, errCertInvalid
	}
	b := src.Bounds()
	w, h = b.Dx(), b.Dy()
	if w > CertMaxSide || h > CertMaxSide {
		if w >= h {
			h, w = max(1, h*CertMaxSide/w), CertMaxSide
		} else {
			w, h = max(1, w*CertMaxSide/h), CertMaxSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, xdraw.Src)
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", 0, 0, err
	}
	return buf.Bytes(), "image/jpeg", w, h, nil
}

func certName(ext string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]) + "." + ext, nil
}

// saveCertFile writes data atomically (temp file + rename) under CertDir.
func saveCertFile(name string, data []byte) error {
	dir := CertDir()
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

// removeCertFile deletes a stored file; a missing file is fine.
func removeCertFile(name string) error {
	if !certNameRe.MatchString(name) {
		return fmt.Errorf("invalid certificate file name")
	}
	if err := os.Remove(filepath.Join(CertDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// readCertUpload reads multipart/form-data: the text fields (small) and the "file" part
// (at most CertMaxPDF + 1 bytes are read).
func readCertUpload(w http.ResponseWriter, r *http.Request) (map[string]any, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, CertMaxPDF+64<<10)
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, nil, errCertInvalid
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, nil, errCertInvalid
	}
	fields := map[string]any{}
	var data []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, certMaxBytes(err)
		}
		name := part.FormName()
		switch name {
		case "file":
			if data != nil {
				part.Close()
				return nil, nil, errCertInvalid
			}
			data, err = io.ReadAll(io.LimitReader(part, CertMaxPDF+1))
			if err != nil {
				return nil, nil, certMaxBytes(err)
			}
			if len(data) > CertMaxPDF {
				return nil, nil, errCertLarge
			}
		case "title", "issuer", "issuedOn", "credentialId", "credentialUrl", "category":
			v, err := io.ReadAll(io.LimitReader(part, 2048))
			if err != nil {
				return nil, nil, certMaxBytes(err)
			}
			if _, dup := fields[name]; dup {
				return nil, nil, errCertInvalid
			}
			fields[name] = string(v)
		default:
			// "public", "verified", ... are not accepted here: a new file is always private
			part.Close()
			return nil, nil, errUnknownField
		}
		part.Close()
	}
	if len(data) == 0 {
		return nil, nil, errCertNoFile
	}
	return fields, data, nil
}

var errUnknownField = errors.New("unknown_field")

func certMaxBytes(err error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return errCertLarge
	}
	return errCertInvalid
}

func certErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errCertLarge):
		certFail(w, r, "cert_too_large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, errCertType):
		certFail(w, r, "cert_file_type", http.StatusUnsupportedMediaType)
	case errors.Is(err, errCertPixels):
		certFail(w, r, "cert_dimensions", http.StatusBadRequest)
	case errors.Is(err, errCertNoFile):
		certFail(w, r, "cert_no_file", http.StatusBadRequest)
	case errors.Is(err, errUnknownField):
		httpx.Error(w, r, http.StatusBadRequest, "unknown_field", httpx.MsgBad)
	default:
		certFail(w, r, "cert_invalid", http.StatusBadRequest)
	}
}

func raisedCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "P0001" {
		return pe.Message
	}
	return ""
}

// uploadCert: POST /api/portfolio/certificates (multipart: file + fields) → 201 {certificate, usage}.
// The new file is private.
func uploadCert(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	if ok, retry := ratelimit.Allow(ctx, "pfc:u:"+strconv.FormatInt(u.ID, 10), config.Int("PORTFOLIO_CERTS_PER_HOUR", certUploadsHour), time.Hour); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	fields, data, err := readCertUpload(w, r)
	if err != nil {
		certErr(w, r, err)
		return
	}
	meta, _, code := cleanMeta(func(k string) (any, bool) { v, ok := fields[k]; return v, ok }, true)
	if code != "" {
		httpx.Error(w, r, http.StatusBadRequest, code, MsgInvalid)
		return
	}
	out, mimeType, width, height, err := ProcessCert(ctx, data)
	if err != nil {
		certErr(w, r, err)
		return
	}
	ext := "jpg"
	var wp, hp *int
	if mimeType == "application/pdf" {
		ext = "pdf"
	} else {
		wp, hp = &width, &height
	}
	name, err := certName(ext)
	if err != nil {
		fail(w, r, "cert name", err)
		return
	}
	var c Cert
	var use Usage
	saved := false
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		c, _, err = scanCert(tx.QueryRow(ctx, `insert into portfolio_certificates
			(user_id, title, issuer, issued_on, credential_id, credential_url, category, file_name, mime, bytes, width, height)
			values (app_user_id(), $1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11) returning `+certCols,
			meta.Title, meta.Issuer, meta.IssuedOn, meta.CredentialID, meta.CredentialURL, meta.Category, name, mimeType, len(out), wp, hp))
		if err != nil {
			return err
		}
		if use, err = usage(ctx, tx); err != nil {
			return err
		}
		// the file is written before the commit; a failed commit removes it below
		if err := saveCertFile(name, out); err != nil {
			return err
		}
		saved = true
		return nil
	})
	if err != nil {
		if saved {
			if rmErr := removeCertFile(name); rmErr != nil {
				log.Printf("portfolio: remove orphan cert %s: %v", name, rmErr)
			}
		}
		if q := raisedCode(err); q == "cert_quota_files" || q == "cert_quota_bytes" {
			certFail(w, r, q, http.StatusConflict)
			return
		}
		fail(w, r, "cert upload", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"certificate": c, "usage": use})
}

func certID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// patchCert: PATCH /api/portfolio/certificates/{id} — any subset of title, issuer, issuedOn,
// credentialId, credentialUrl, category and public. Someone else's id is 404 (RLS hides it).
func patchCert(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	id, ok := certID(r)
	var body map[string]any
	if !ok || httpx.Decode(r, &body, 8<<10) != nil || len(body) == 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	for k := range body {
		switch k {
		case "title", "issuer", "issuedOn", "credentialId", "credentialUrl", "category", "public":
		default:
			httpx.Error(w, r, http.StatusBadRequest, "unknown_field", httpx.MsgBad)
			return
		}
	}
	meta, set, code := cleanMeta(func(k string) (any, bool) { v, ok := body[k]; return v, ok }, false)
	if code != "" {
		httpx.Error(w, r, http.StatusBadRequest, code, MsgInvalid)
		return
	}
	var sets []string
	var args []any
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	for key, col := range map[string]string{"title": "title", "issuer": "issuer", "credentialId": "credential_id", "credentialUrl": "credential_url", "category": "category"} {
		if set[key] {
			add(col, map[string]string{"title": meta.Title, "issuer": meta.Issuer, "credentialId": meta.CredentialID,
				"credentialUrl": meta.CredentialURL, "category": meta.Category}[key])
		}
	}
	if set["issuedOn"] {
		args = append(args, meta.IssuedOn)
		sets = append(sets, "issued_on = $"+itoa(len(args))+"::date")
	}
	if v, ok := body["public"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_public", MsgInvalid)
			return
		}
		add("is_public", b)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	var c Cert
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		c, _, err = scanCert(tx.QueryRow(ctx, "update portfolio_certificates set "+strings.Join(sets, ", ")+
			" where id = $"+itoa(len(args))+" and user_id = app_user_id() returning "+certCols, args...))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		certFail(w, r, "cert_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		fail(w, r, "cert patch", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"certificate": c})
}

// deleteCert: DELETE /api/portfolio/certificates/{id} — the row and the file go.
func deleteCert(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := certID(r)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var name string
	var use Usage
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `delete from portfolio_certificates where id = $1 and user_id = app_user_id() returning file_name`, id).Scan(&name)
		if err != nil {
			return err
		}
		use, err = usage(ctx, tx)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		certFail(w, r, "cert_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		fail(w, r, "cert delete", err)
		return
	}
	if err := removeCertFile(name); err != nil {
		log.Printf("portfolio: remove cert file %s: %v", name, err)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true, "usage": use})
}

// dispositionName is the download name: the title without path or control characters.
func dispositionName(title, ext string) string {
	s := strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == ' ' || c == '-' || c == '_' || c == '.' {
			return c
		}
		return -1
	}, title)
	s = strings.Trim(strings.Join(strings.Fields(s), " "), ". ")
	if s == "" {
		s = "certificate"
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	return s + "." + ext
}

// serveCertFile: GET /api/portfolio/files/{name}. The owner always; others only when the file
// is public and the owner's public page is enabled with the certificates block on. A private
// file is 403 for everyone else; an unknown name is 404. The file is never rendered as HTML:
// a fixed Content-Type, nosniff and a sandboxing CSP.
func serveCertFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !certNameRe.MatchString(name) {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var owner int64
	var mimeType, title string
	var public bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select c.user_id, c.mime, c.title,
				c.is_public and coalesce(s.enabled and s.show_certs, false) and u.deleted_at is null and u.consent_at is not null
			from portfolio_certificates c join users u on u.id = c.user_id
			left join portfolio_settings s on s.user_id = c.user_id
			where c.file_name = $1`, name).Scan(&owner, &mimeType, &title, &public)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w, r)
		return
	}
	if err != nil {
		fail(w, r, "cert file", err)
		return
	}
	isOwner := false
	if !public {
		cu := auth.Current(r)
		isOwner = cu != nil && cu.ID == owner
		if !isOwner {
			httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
			return
		}
	}
	f, err := os.Open(filepath.Join(CertDir(), name))
	if err != nil {
		notFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		notFound(w, r)
		return
	}
	ext := "jpg"
	if mimeType == "application/pdf" {
		ext = "pdf"
	}
	h := w.Header()
	h.Set("Content-Type", mimeType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": dispositionName(title, ext)}))
	// a sandboxed, script-less document wherever it is opened; only our own pages may frame it
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	if public {
		// turning the flag (or the page) off must take effect at once: revalidate every time
		h.Set("Cache-Control", "no-cache")
	} else {
		h.Set("Cache-Control", "private, no-store")
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// deleteUserCertFiles is the account-deletion hook (the rows cascade with the users row).
func deleteUserCertFiles(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `select file_name from portfolio_certificates where user_id = $1`, userID)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := removeCertFile(n); err != nil {
			return fmt.Errorf("delete account certificate: %w", err)
		}
	}
	return nil
}

// PublicCert is a public certificate file on /p/<slug>: no ids, the URL carries the random name.
type PublicCert struct {
	Title         string  `json:"title"`
	Issuer        string  `json:"issuer"`
	IssuedOn      *string `json:"issuedOn"`
	Category      string  `json:"category"`
	CredentialURL string  `json:"credentialUrl"`
	Kind          string  `json:"kind"`
	Bytes         int     `json:"bytes"`
	Width         *int    `json:"width"`
	Height        *int    `json:"height"`
	URL           string  `json:"url"`
	Verified      bool    `json:"verified"` // always false: the student uploaded it
}

func publicCerts(ctx context.Context, tx pgx.Tx, userID int64) ([]PublicCert, error) {
	rows, err := tx.Query(ctx, `select title, issuer, to_char(issued_on, 'YYYY-MM-DD'), category, credential_url, mime, bytes,
			width, height, file_name
		from portfolio_certificates where user_id = $1 and is_public order by issued_on desc nulls last, created_at desc limit 30`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (PublicCert, error) {
		var c PublicCert
		var mimeType, name string
		err := row.Scan(&c.Title, &c.Issuer, &c.IssuedOn, &c.Category, &c.CredentialURL, &mimeType, &c.Bytes, &c.Width, &c.Height, &name)
		c.Kind, c.URL = kindOf(mimeType), certURL(name)
		return c, err
	})
}

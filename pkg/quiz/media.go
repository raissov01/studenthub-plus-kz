package quiz

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	"time"

	"github.com/jackc/pgx/v5"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

const (
	imageMaxSide = 1280
	maxPixels    = 16_000_000 // decompression-bomb guard: a small file may declare a huge canvas
)

var mediaNameRe = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|mp3)$`)

// UploadDir is where quiz media live: $UPLOAD_DIR/quiz (default ./uploads/quiz).
func UploadDir() string { return filepath.Join(config.Str("UPLOAD_DIR", "./uploads"), "quiz") }

func maxImageUpload() int64 { return int64(config.Int("QUIZ_IMAGE_MAX_KB", 900)) << 10 }
func maxAudioUpload() int64 { return int64(config.Int("QUIZ_AUDIO_MAX_KB", 900)) << 10 }

var (
	errType     = errors.New("media_type")
	errHEIC     = errors.New("file_heic")
	errTooLarge = errors.New("media_too_large")
	errPixels   = errors.New("media_dimensions")
	errDecode   = errors.New("media_invalid")
)

// sniffImage identifies the real format by magic bytes; the file name and Content-Type are ignored.
func sniffImage(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		switch string(b[8:12]) {
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1", "avif":
			return "heic"
		}
	}
	return ""
}

// decodeSem keeps one full-size decode in memory at a time (the API runs with MemoryMax=150M).
var decodeSem = make(chan struct{}, 1)

// processImage validates an image and re-encodes it as a JPEG of at most maxSide px on the long
// side. Decoding to pixels and encoding anew drops every metadata block (EXIF with GPS, XMP,
// ICC, comments): none of them survive into the new file.
func processImage(ctx context.Context, data []byte, maxBytes int64, maxSide, targetBytes int) ([]byte, int, int, error) {
	if int64(len(data)) > maxBytes {
		return nil, 0, 0, errTooLarge
	}
	kind := sniffImage(data)
	var cfg image.Config
	var err error
	switch kind {
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case "webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	case "heic":
		return nil, 0, 0, errHEIC
	default:
		return nil, 0, 0, errType
	}
	if err != nil {
		return nil, 0, 0, errDecode
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > maxPixels {
		return nil, 0, 0, errPixels
	}
	select {
	case decodeSem <- struct{}{}:
		defer func() { <-decodeSem }()
	case <-ctx.Done():
		return nil, 0, 0, ctx.Err()
	}
	var src image.Image
	switch kind {
	case "jpeg":
		src, err = jpeg.Decode(bytes.NewReader(data))
	case "png":
		src, err = png.Decode(bytes.NewReader(data))
	case "webp":
		src, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, 0, 0, errDecode
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxSide || h > maxSide {
		if w >= h {
			h, w = max(1, h*maxSide/w), maxSide
		} else {
			w, h = max(1, w*maxSide/h), maxSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, xdraw.Src)
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var buf bytes.Buffer
	for _, q := range []int{82, 72, 62} {
		buf.Reset()
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q}); err != nil {
			return nil, 0, 0, err
		}
		if buf.Len() <= targetBytes {
			break
		}
	}
	return buf.Bytes(), w, h, nil
}

// processMP3 accepts MPEG audio Layer III only. ID3v2 (start) and ID3v1 (end) tags are cut
// off — they may carry names, cover pictures and comments — and the remaining bytes must start
// with at least three consecutive valid frames.
func processMP3(data []byte, maxBytes int64) ([]byte, error) {
	if int64(len(data)) > maxBytes {
		return nil, errTooLarge
	}
	b := data
	for len(b) >= 10 && string(b[:3]) == "ID3" {
		size := int(b[6]&0x7f)<<21 | int(b[7]&0x7f)<<14 | int(b[8]&0x7f)<<7 | int(b[9]&0x7f)
		total := 10 + size
		if b[5]&0x10 != 0 {
			total += 10 // footer
		}
		if total > len(b) {
			return nil, errDecode
		}
		b = b[total:]
	}
	if len(b) >= 128 && string(b[len(b)-128:len(b)-125]) == "TAG" {
		b = b[:len(b)-128]
	}
	off, frames := 0, 0
	for frames < 3 && off < len(b) {
		n := mp3FrameLen(b[off:])
		if n <= 0 {
			break
		}
		off += n
		frames++
	}
	if frames == 0 {
		return nil, errType
	}
	if frames < 3 && off < len(b) {
		return nil, errDecode
	}
	return append([]byte(nil), b...), nil
}

// mp3FrameLen returns the byte length of the Layer III frame starting at b, or 0.
func mp3FrameLen(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return 0
	}
	version := (b[1] >> 3) & 3 // 3 = MPEG1, 2 = MPEG2, 0 = MPEG2.5
	layer := (b[1] >> 1) & 3   // 1 = Layer III
	bi := int(b[2] >> 4)
	si := int((b[2] >> 2) & 3)
	pad := int((b[2] >> 1) & 1)
	if version == 1 || layer != 1 || bi == 0 || bi == 15 || si == 3 {
		return 0
	}
	v1 := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	v2 := []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	rates := map[byte][]int{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
	sr := rates[version][si]
	if version == 3 {
		return 144*v1[bi]*1000/sr + pad
	}
	return 72*v2[bi]*1000/sr + pad
}

func randomName(ext string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]) + ext, nil
}

// saveFile writes data atomically (temp file in the same directory + rename).
func saveFile(name string, data []byte) error {
	dir := UploadDir()
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

// removeFiles deletes media files; missing files are fine (idempotent).
func removeFiles(names []string) {
	for _, n := range names {
		if !mediaNameRe.MatchString(n) {
			continue
		}
		if err := os.Remove(filepath.Join(UploadDir(), n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("quiz: remove media %s: %v", n, err)
		}
	}
}

func mediaErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errTooLarge):
		fail(w, r, "media_too_large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, errHEIC):
		fail(w, r, "file_heic", http.StatusUnsupportedMediaType)
	case errors.Is(err, errType):
		fail(w, r, "media_type", http.StatusUnsupportedMediaType)
	case errors.Is(err, errPixels):
		fail(w, r, "media_dimensions", http.StatusBadRequest)
	default:
		fail(w, r, "media_invalid", http.StatusBadRequest)
	}
}

// readOne reads a single upload: multipart/form-data field `field`, or the raw body.
// At most limit+1 bytes are read.
func readOne(w http.ResponseWriter, r *http.Request, field string, limit int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+64<<10)
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var src io.Reader = r.Body
	if mt == "multipart/form-data" {
		mr, err := r.MultipartReader()
		if err != nil || params["boundary"] == "" {
			return nil, errDecode
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				return nil, wrapMaxBytes(err, errDecode)
			}
			if part.FormName() == field {
				src = part
				break
			}
			part.Close()
		}
	}
	data, err := io.ReadAll(io.LimitReader(src, limit+1))
	if err != nil {
		return nil, wrapMaxBytes(err, errDecode)
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

func wrapMaxBytes(err, def error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return errTooLarge
	}
	return def
}

// uploadMedia: POST /api/quiz/media?kind=image|audio (multipart field "file" or raw body)
// → 201 {name, url, kind, width?, height?, bytes}. The name then goes into a question's
// "image"/"audio"; files never attached to a quiz are removed by the cron after 24 h.
func uploadMedia(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !needConsent(w, r, u) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "image"
	}
	if kind != "image" && kind != "audio" {
		bad(w, r)
		return
	}
	if ok, _ := ratelimit.Allow(r.Context(), "quiz:m:"+itoa(u.ID), config.Int("QUIZ_MEDIA_PER_HOUR", 60), time.Hour); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	limit := maxImageUpload()
	if kind == "audio" {
		limit = maxAudioUpload()
	}
	data, err := readOne(w, r, "file", limit)
	if err != nil {
		mediaErr(w, r, err)
		return
	}
	resp := map[string]any{"kind": kind}
	var out []byte
	ext := ".jpg"
	if kind == "image" {
		var width, height int
		out, width, height, err = processImage(r.Context(), data, limit, imageMaxSide, 400<<10)
		resp["width"], resp["height"] = width, height
	} else {
		ext = ".mp3"
		out, err = processMP3(data, limit)
	}
	if err != nil {
		mediaErr(w, r, err)
		return
	}
	name, err := registerMedia(r.Context(), u.ID, kind, ext, out)
	if err != nil {
		internal(w, r, "save media", err)
		return
	}
	resp["name"], resp["url"], resp["bytes"] = name, mediaURL(name), len(out)
	httpx.JSON(w, http.StatusCreated, resp)
}

func registerMedia(ctx context.Context, uid int64, kind, ext string, data []byte) (string, error) {
	name, err := randomName(ext)
	if err != nil {
		return "", err
	}
	if err := saveFile(name, data); err != nil {
		return "", err
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into quiz_media (name, user_id, kind, bytes) values ($1, $2, $3, $4)`, name, uid, kind, len(data))
		return err
	})
	if err != nil {
		removeFiles([]string{name})
		return "", err
	}
	return name, nil
}

// copyMedia duplicates files for a clone; returns old → new name.
func copyMedia(ctx context.Context, uid int64, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, n := range names {
		if !mediaNameRe.MatchString(n) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(UploadDir(), n))
		if err != nil {
			return nil, err
		}
		kind, ext := "image", ".jpg"
		if filepath.Ext(n) == ".mp3" {
			kind, ext = "audio", ".mp3"
		}
		nn, err := registerMedia(ctx, uid, kind, ext, data)
		if err != nil {
			return nil, err
		}
		out[n] = nn
	}
	return out, nil
}

// serveMedia: GET /api/quiz/media/{name}. Media of published public/unlisted quizzes and of
// quizzes in an open live game are public (the name is 128 random bits); others only for the
// uploader or an admin.
func serveMedia(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !mediaNameRe.MatchString(name) {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var owner int64
	var public bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select m.user_id, coalesce(
			(q.visibility <> 'private' and not q.is_draft and q.hidden_at is null)
			or exists (select 1 from quiz_live_sessions s where s.quiz_id = q.id and s.status in ('lobby', 'active')), false)
			from quiz_media m left join quizzes q on q.id = m.quiz_id where m.name = $1`, name).Scan(&owner, &public)
	})
	if isNoRows(err) {
		notFound(w, r)
		return
	}
	if err != nil {
		internal(w, r, "media", err)
		return
	}
	if !public {
		u := auth.Current(r)
		if u == nil || (u.ID != owner && u.Role != "admin") {
			notFound(w, r)
			return
		}
	}
	f, err := os.Open(filepath.Join(UploadDir(), name))
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
	h := w.Header()
	if filepath.Ext(name) == ".mp3" {
		h.Set("Content-Type", "audio/mpeg")
	} else {
		h.Set("Content-Type", "image/jpeg")
	}
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if public {
		h.Set("Cache-Control", "public, max-age=86400")
	} else {
		h.Set("Cache-Control", "private, no-store")
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// deleteUserFiles is the account-deletion hook: every media file the student uploaded goes
// (the rows cascade with the users row). Live games they host are closed.
func deleteUserFiles(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `select name from quiz_media where user_id = $1`, userID)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	removeFiles(names)
	closeLiveForHost(userID)
	return nil
}

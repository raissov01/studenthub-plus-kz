package market

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
	// MaxSide is the longest side of a stored photo.
	MaxSide = 1280
	// maxPixels guards against decompression bombs: a small file may declare a huge canvas.
	maxPixels = 12_000_000
	// targetBytes is the size the re-encoded JPEG aims for (spec: ~500 KB).
	targetBytes = 500 << 10
)

var nameRe = regexp.MustCompile(`^[0-9a-f]{32}\.jpg$`)

// UploadDir is where market photos live: $UPLOAD_DIR/market (default ./uploads/market).
func UploadDir() string {
	return filepath.Join(config.Str("UPLOAD_DIR", "./uploads"), "market")
}

// maxUpload is the accepted upload size (MARKET_PHOTO_MAX_KB, default 600 KB as in spec 08).
func maxUpload() int64 { return int64(config.Int("MARKET_PHOTO_MAX_KB", 600)) << 10 }

var (
	errType     = errors.New("photo_type")
	errTooLarge = errors.New("photo_too_large")
	errPixels   = errors.New("photo_dimensions")
	errDecode   = errors.New("photo_invalid")
)

// sniff identifies the real format by magic bytes; the client's Content-Type and file name
// are ignored. Only JPEG, PNG and WebP pass (no SVG, HTML, GIF, HEIC, ...).
func sniff(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// decodeSem keeps at most one full-size decode in memory at a time (the API runs with
// MemoryMax=150M; a 12 MP RGBA image alone is ~48 MB).
var decodeSem = make(chan struct{}, 1)

// Process validates an upload and re-encodes it as a JPEG of at most MaxSide px on the long
// side. Decoding to pixels and encoding anew drops every metadata block — EXIF (with GPS),
// XMP, ICC, comments — because none of them survive into the new file.
func Process(ctx context.Context, data []byte) (out []byte, w, h int, err error) {
	if int64(len(data)) > maxUpload() {
		return nil, 0, 0, errTooLarge
	}
	kind := sniff(data)
	var cfg image.Config
	switch kind {
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case "webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
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
	w, h = b.Dx(), b.Dy()
	if w > MaxSide || h > MaxSide {
		if w >= h {
			h, w = max(1, h*MaxSide/w), MaxSide
		} else {
			w, h = max(1, w*MaxSide/h), MaxSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// white under transparent PNG/WebP pixels (JPEG has no alpha)
	xdraw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, xdraw.Src)
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	src = nil
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

func randomName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]) + ".jpg", nil
}

// saveFile writes data atomically (temp file + rename) under UploadDir.
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

// removeFiles deletes photo files; missing files are fine (idempotent).
func removeFiles(names []string) {
	for _, n := range names {
		if !nameRe.MatchString(n) {
			continue
		}
		if err := os.Remove(filepath.Join(UploadDir(), n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("market: remove photo %s: %v", n, err)
		}
	}
}

// dropPhotos clears an item's photos inside tx: registry rows go, the photos array is
// emptied, and the names are returned so the caller deletes the files after the commit.
func dropPhotos(ctx context.Context, tx pgx.Tx, itemID int64) ([]string, error) {
	var listed []string
	err := tx.QueryRow(ctx, `select photos from market_items where id = $1 for update`, itemID).Scan(&listed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := tx.Query(ctx, `delete from market_photos where item_id = $1 or name = any($2) returning name`, itemID, listed)
	if err != nil {
		return nil, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `update market_items set photos = '{}' where id = $1`, itemID); err != nil {
		return nil, err
	}
	return append(names, listed...), nil
}

func photoErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errTooLarge):
		fail(w, r, "photo_too_large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, errType):
		fail(w, r, "photo_type", http.StatusUnsupportedMediaType)
	case errors.Is(err, errPixels):
		fail(w, r, "photo_dimensions", http.StatusBadRequest)
	default:
		fail(w, r, "photo_invalid", http.StatusBadRequest)
	}
}

// readUpload accepts multipart/form-data (field "photo", as FormData sends it) or the raw
// image as the request body. At most maxUpload()+1 bytes are read.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	limit := maxUpload()
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
			if part.FormName() == "photo" {
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

// upload: POST /api/market/photos — returns {name, url, width, height, bytes}. The name is
// then passed in "photos" when creating the item (within 24 h, after that it is cleaned up).
func upload(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	if ok, _ := ratelimit.Allow(r.Context(), "mp:u:"+itoa(u.ID), config.Int("MARKET_PHOTOS_PER_HOUR", 30), time.Hour); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	data, err := readUpload(w, r)
	if err != nil {
		photoErr(w, r, err)
		return
	}
	out, width, height, err := Process(r.Context(), data)
	if err != nil {
		photoErr(w, r, err)
		return
	}
	name, err := randomName()
	if err != nil {
		internal(w, r, "name", err)
		return
	}
	if err := saveFile(name, out); err != nil {
		internal(w, r, "save photo", err)
		return
	}
	err = db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `insert into market_photos (name, user_id, bytes) values ($1, $2, $3)`, name, u.ID, len(out))
		return err
	})
	if err != nil {
		removeFiles([]string{name})
		internal(w, r, "register photo", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"name": name, "url": photoURL(name), "width": width, "height": height, "bytes": len(out)})
}

func photoURL(name string) string { return "/api/market/photos/" + name }

// servePhoto: GET /api/market/photos/{name}. Photos of active/reserved items are public and
// cacheable; others only for the uploader or an admin. Never a directory listing: the name
// must match the random-name pattern before the disk is touched.
func servePhoto(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var owner int64
	var status *string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select p.user_id, i.status from market_photos p left join market_items i on i.id = p.item_id
			where p.name = $1`, name).Scan(&owner, &status)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w, r)
		return
	}
	if err != nil {
		internal(w, r, "photo", err)
		return
	}
	public := status != nil && (*status == "active" || *status == "reserved")
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
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if public {
		// the name is random and the content never changes; a day keeps hidden items from
		// lingering in shared caches for long
		h.Set("Cache-Control", "public, max-age=86400")
	} else {
		h.Set("Cache-Control", "private, no-store")
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// deleteUserFiles is the account-deletion hook: every photo file the user uploaded goes
// (the rows cascade with the users row).
func deleteUserFiles(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `select name from market_photos where user_id = $1
		union select unnest(photos) from market_items where user_id = $1`, userID)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, name := range names {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("invalid registered photo name")
		}
		if err := os.Remove(filepath.Join(UploadDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete account photo: %w", err)
		}
	}
	return nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

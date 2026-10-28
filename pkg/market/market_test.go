package market

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

var tg = struct {
	sync.Mutex
	msgs map[int64][]string
}{msgs: map[int64][]string{}}

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("k", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "10000")
	os.Setenv("FEATURE_MARKET", "on")
	os.Setenv("CRON_SECRET", "cron-secret")
	dir, err := os.MkdirTemp("", "market-uploads-")
	if err != nil {
		panic(err)
	}
	os.Setenv("UPLOAD_DIR", dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		tg.Lock()
		tg.msgs[body.ChatID] = append(tg.msgs[body.ChatID], body.Text)
		tg.Unlock()
		io.WriteString(w, `{"ok":true}`)
	}))
	os.Setenv("BOT_TOKEN", "123:test")
	os.Setenv("TELEGRAM_API_BASE", srv.URL)
	code := testdb.Main(m, "market")
	srv.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

func ctx() context.Context { return context.Background() }

var seq atomic.Int64

type userOpt struct {
	role       string
	university string
	adult      *bool
	username   string // "-" = none
	young      bool   // account younger than 7 days
	noRules    bool
}

var yes, no = true, false

func newUser(t *testing.T, o userOpt) (int64, int64) {
	t.Helper()
	if o.role == "" {
		o.role = "student"
	}
	if o.university == "" {
		o.university = "ENU"
	}
	if o.adult == nil {
		o.adult = &yes
	}
	n := seq.Add(1)
	if o.username == "" {
		o.username = "giver" + strconv.FormatInt(n, 10)
	}
	age := 30
	if o.young {
		age = 1
	}
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx(), `insert into users (telegram_id, tg_username, name, university, role, consent_at, is_adult, created_at)
			values ($1, nullif($2, '-'), $3, $4, $5::user_role, now(), $6, now() - make_interval(days => $7)) returning id`,
			700000+n, o.username, "Giver "+strconv.FormatInt(n, 10), o.university, o.role, *o.adult, age).Scan(&id)
		if err != nil {
			return err
		}
		// delivery independent of the wall clock: no quiet hours, high daily cap (pkg/notify tests cover both)
		if _, err = tx.Exec(ctx(), `insert into notify_prefs (user_id, quiet_enabled, daily_cap) values ($1, false, 20)`, id); err != nil || o.noRules {
			return err
		}
		_, err = tx.Exec(ctx(), `insert into market_rules (user_id) values ($1)`, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, 700000 + n
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return tx.QueryRow(ctx(), sql, args...).Scan(&v) }); err != nil {
		t.Fatal(err)
	}
	return v
}

type resp struct{ *httptest.ResponseRecorder }

func (r resp) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Fatalf("json: %v %s", err, r.Body)
	}
	return m
}

func (r resp) code() string {
	var m struct{ Error struct{ Code string } }
	json.Unmarshal(r.Body.Bytes(), &m)
	return m.Error.Code
}

func send(t *testing.T, method, path string, uid int64, contentType string, body []byte) resp {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	if strings.HasPrefix(path, "/api/cron/") {
		r.Header.Set("Authorization", "Bearer cron-secret")
	}
	m := http.NewServeMux()
	Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return resp{w}
}

func do(t *testing.T, method, path string, uid int64, body any) resp {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	return send(t, method, path, uid, "application/json", b)
}

func sentTo(tgID int64) []string {
	tg.Lock()
	defer tg.Unlock()
	return append([]string(nil), tg.msgs[tgID]...)
}

// ---- images ----

func picture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	return img
}

const gpsMarker = "GPSLatitude=51.1694N;GPSLongitude=71.4491E"

// jpegWithExif returns a JPEG with an APP1 Exif segment carrying (fake) GPS coordinates.
func jpegWithExif(t *testing.T, w, h int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, picture(w, h), &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	exif := append([]byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08"), gpsMarker...)
	seg := []byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	out = append(out, exif...)
	return append(out, b[2:]...)
}

func pngBytes(t *testing.T, w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, picture(w, h)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngHeaderOnly declares a huge canvas in a tiny file (decompression bomb shape).
func pngHeaderOnly(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6
	chunk := append([]byte("IHDR"), ihdr...)
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func multipartBody(t *testing.T, field string, data []byte) ([]byte, string) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile(field, "../../etc/passwd.jpg")
	fw.Write(data)
	mw.Close()
	return b.Bytes(), mw.FormDataContentType()
}

func uploadPhoto(t *testing.T, uid int64) string {
	t.Helper()
	body, ct := multipartBody(t, "photo", jpegWithExif(t, 300, 200))
	w := send(t, "POST", "/api/market/photos", uid, ct, body)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	return w.json(t)["name"].(string)
}

func newItem(t *testing.T, uid int64, photos ...string) int64 {
	t.Helper()
	if photos == nil {
		photos = []string{}
	}
	w := do(t, "POST", "/api/market/items", uid, map[string]any{"title": "Физика оқулығы", "desc": "1-курсқа", "category": "books",
		"condition": "good", "pickup": "4-корпус, кіреберіс", "photos": photos})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	return int64(w.json(t)["item"].(map[string]any)["id"].(float64))
}

func fileExists(name string) bool {
	_, err := os.Stat(filepath.Join(UploadDir(), name))
	return err == nil
}

// ---- tests ----

func TestPhotoValidationAndExifStripped(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t, userOpt{})
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)
	html := []byte(`<!doctype html><script>alert(1)</script>`)
	for name, c := range map[string]struct {
		ct     string
		body   []byte
		status int
		code   string
	}{
		"svg as image/jpeg": {"image/jpeg", svg, 415, "photo_type"},
		"html":              {"image/png", html, 415, "photo_type"},
		"gif":               {"image/gif", []byte("GIF89a\x01\x00\x01\x00"), 415, "photo_type"},
		"too large":         {"image/jpeg", append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, 700<<10)...), 413, "photo_too_large"},
		"bomb":              {"image/png", pngHeaderOnly(20000, 20000), 400, "photo_dimensions"},
		"broken webp":       {"image/webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 garbage"), 400, "photo_invalid"},
		"empty":             {"image/jpeg", nil, 415, "photo_type"},
	} {
		w := send(t, "POST", "/api/market/photos", uid, c.ct, c.body)
		if w.Code != c.status || w.code() != c.code {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// multipart too
	body, ct := multipartBody(t, "photo", svg)
	if w := send(t, "POST", "/api/market/photos", uid, ct, body); w.Code != 415 {
		t.Errorf("multipart svg: %d %s", w.Code, w.Body)
	}
	if n := scalar[int](t, `select count(*) from market_photos where user_id = $1`, uid); n != 0 {
		t.Fatalf("rejected uploads registered: %d", n)
	}

	// a real JPEG with EXIF/GPS: accepted, re-encoded, metadata gone, long side capped
	src := jpegWithExif(t, 2000, 1000)
	if !bytes.Contains(src, []byte("Exif")) || !bytes.Contains(src, []byte(gpsMarker)) {
		t.Fatal("test image has no EXIF")
	}
	body, ct = multipartBody(t, "photo", src)
	w := send(t, "POST", "/api/market/photos", uid, ct, body)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	out := w.json(t)
	name := out["name"].(string)
	if !nameRe.MatchString(name) || strings.Contains(name, "passwd") || out["width"].(float64) != 1280 || out["height"].(float64) != 640 {
		t.Fatalf("upload result: %v", out)
	}
	stored, err := os.ReadFile(filepath.Join(UploadDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("Exif")) || bytes.Contains(stored, []byte("GPS")) || bytes.Contains(stored, []byte{0xFF, 0xE1}) {
		t.Fatal("EXIF/GPS survived re-encoding")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil || format != "jpeg" || cfg.Width != 1280 || cfg.Height != 640 || len(stored) > targetBytes {
		t.Fatalf("stored file: %v %s %+v %d bytes", err, format, cfg, len(stored))
	}
	// raw PNG body works as well, stored as JPEG
	if w := send(t, "POST", "/api/market/photos", uid, "image/png", pngBytes(t, 64, 64)); w.Code != 201 {
		t.Fatalf("png: %d %s", w.Code, w.Body)
	}
	if w := send(t, "POST", "/api/market/photos", 0, "image/png", pngBytes(t, 8, 8)); w.Code != 401 {
		t.Fatalf("anonymous upload: %d", w.Code)
	}
}

func TestPublishRequirementsAndDailyLimit(t *testing.T) {
	testdb.Need(t)
	noName, _ := newUser(t, userOpt{username: "-"})
	minor, _ := newUser(t, userOpt{adult: &no})
	noRules, _ := newUser(t, userOpt{noRules: true})
	for uid, code := range map[int64]string{noName: "no_username", minor: "minor", noRules: "no_rules"} {
		w := do(t, "POST", "/api/market/items", uid, map[string]any{"title": "Шәйнек", "category": "dorm", "condition": "used"})
		if w.Code != 403 || w.code() != code {
			t.Errorf("want %s: %d %s", code, w.Code, w.Body)
		}
		if st := do(t, "GET", "/api/market/me", uid, nil).json(t); st["canPublish"] != false {
			t.Errorf("status for %s: %v", code, st)
		}
	}
	if w := do(t, "POST", "/api/market/rules/accept", noRules, nil); w.Code != 200 {
		t.Fatalf("accept rules: %d", w.Code)
	}
	if w := do(t, "POST", "/api/market/items", noRules, map[string]any{"title": "Шәйнек", "category": "dorm", "condition": "used"}); w.Code != 201 {
		t.Fatalf("after rules: %d %s", w.Code, w.Body)
	}
	// no price field, ever
	if w := do(t, "POST", "/api/market/items", noRules, map[string]any{"title": "x", "category": "dorm", "condition": "used", "price": 5000}); w.Code != 400 {
		t.Fatalf("price accepted: %d", w.Code)
	}

	uid, _ := newUser(t, userOpt{})
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, newItem(t, uid))
	}
	// deleting does not give the slot back
	if w := do(t, "DELETE", "/api/market/items/"+strconv.FormatInt(ids[0], 10), uid, nil); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	w := do(t, "POST", "/api/market/items", uid, map[string]any{"title": "Төртінші", "category": "other", "condition": "new"})
	if w.Code != 429 || w.code() != "daily_limit" {
		t.Fatalf("4th item: %d %s", w.Code, w.Body)
	}
	if st := do(t, "GET", "/api/market/me", uid, nil).json(t); st["today"].(float64) != 3 || st["canPublish"] != false {
		t.Fatalf("status: %v", st)
	}
	// someone else's photo cannot be attached
	other, _ := newUser(t, userOpt{})
	photo := uploadPhoto(t, other)
	w = do(t, "POST", "/api/market/items", noRules, map[string]any{"title": "x", "category": "dorm", "condition": "used", "photos": []string{photo}})
	if w.Code != 400 || w.code() != "bad_photo" {
		t.Fatalf("foreign photo: %d %s", w.Code, w.Body)
	}
}

func TestYoungAccountFirstItemModerated(t *testing.T) {
	testdb.Need(t)
	uid, tgID := newUser(t, userOpt{young: true})
	admin, _ := newUser(t, userOpt{role: "admin"})
	w := do(t, "POST", "/api/market/items", uid, map[string]any{"title": "Матрас", "category": "dorm", "condition": "good"})
	if w.Code != 201 || w.json(t)["pending"] != true {
		t.Fatalf("young first item: %d %s", w.Code, w.Body)
	}
	id := int64(w.json(t)["item"].(map[string]any)["id"].(float64))
	if body := do(t, "GET", "/api/market/items?university=all", 0, nil).Body.String(); strings.Contains(body, "Матрас") {
		t.Fatal("pending item is public")
	}
	sub := scalar[int64](t, `select id from submissions where kind = 'market_item' and (payload->>'itemId')::bigint = $1`, id)
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := moderation.Review(ctx(), tx, sub, admin, true, ""); return err }); err != nil {
		t.Fatal(err)
	}
	if s := scalar[string](t, `select status from market_items where id = $1`, id); s != "active" {
		t.Fatalf("after approve: %s", s)
	}
	if len(sentTo(tgID)) == 0 {
		t.Fatal("owner not told")
	}
	// second item of a young account is not moderated any more
	w = do(t, "POST", "/api/market/items", uid, map[string]any{"title": "Шам", "category": "dorm", "condition": "good"})
	if w.json(t)["pending"] != false {
		t.Fatalf("second item: %s", w.Body)
	}
}

func TestAnonymousSeesNoContacts(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t, userOpt{university: "Visible Uni", username: "secret_giver"})
	viewer, _ := newUser(t, userOpt{university: "Visible Uni"})
	photo := uploadPhoto(t, uid)
	id := newItem(t, uid, photo)

	anon := do(t, "GET", "/api/market/items?university=Visible+Uni", 0, nil)
	body := anon.Body.String()
	if anon.Code != 200 || !strings.Contains(body, "Физика") || strings.Contains(body, "secret_giver") ||
		strings.Contains(body, "4-корпус") || !strings.Contains(body, `"pickup":null`) || !strings.Contains(body, `"author":null`) {
		t.Fatalf("anonymous browse: %s", body)
	}
	if b := do(t, "GET", "/api/market/items/"+strconv.FormatInt(id, 10), 0, nil).Body.String(); strings.Contains(b, "secret_giver") || strings.Contains(b, "4-корпус") {
		t.Fatalf("anonymous item: %s", b)
	}
	signed := do(t, "GET", "/api/market/items", viewer, nil).Body.String() // default: own university
	if !strings.Contains(signed, "https://t.me/secret_giver") || !strings.Contains(signed, "4-корпус") {
		t.Fatalf("signed-in browse: %s", signed)
	}
	if b := do(t, "GET", "/api/market/items?q=физика&category=books&university=all", 0, nil).Body.String(); !strings.Contains(b, "Физика") {
		t.Fatalf("search: %s", b)
	}

	// photo serving: public for active items, cache headers, never a listing / traversal
	w := do(t, "GET", "/api/market/photos/"+photo, 0, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || !strings.Contains(w.Header().Get("Cache-Control"), "max-age") {
		t.Fatalf("photo: %d %v", w.Code, w.Header())
	}
	for _, p := range []string{"/api/market/photos/", "/api/market/photos/..%2f..%2fgo.mod", "/api/market/photos/" + strings.Repeat("0", 32) + ".jpg"} {
		if w := do(t, "GET", p, 0, nil); w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	// an unattached upload is visible to its uploader only
	loose := uploadPhoto(t, uid)
	if w := do(t, "GET", "/api/market/photos/"+loose, 0, nil); w.Code != 404 {
		t.Fatalf("loose photo anonymous: %d", w.Code)
	}
	if w := do(t, "GET", "/api/market/photos/"+loose, uid, nil); w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("loose photo owner: %d %v", w.Code, w.Header())
	}
}

func TestOwnerOnlyEdits(t *testing.T) {
	testdb.Need(t)
	owner, _ := newUser(t, userOpt{})
	other, _ := newUser(t, userOpt{})
	id := newItem(t, owner)
	p := "/api/market/items/" + strconv.FormatInt(id, 10)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"PATCH", p, map[string]any{"title": "hacked"}},
		{"PATCH", p, map[string]any{"status": "reserved"}},
		{"DELETE", p, nil},
		{"POST", p + "/given", nil},
		{"POST", p + "/extend", nil},
	} {
		if w := do(t, c.method, c.path, other, c.body); w.Code != 404 {
			t.Errorf("%s %s by stranger: %d %s", c.method, c.path, w.Code, w.Body)
		}
	}
	if s := scalar[string](t, `select title || '/' || status from market_items where id = $1`, id); s != "Физика оқулығы/active" {
		t.Fatalf("stranger changed the item: %s", s)
	}
	// RLS itself, bypassing the handlers
	err := db.WithUser(ctx(), other, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx(), `update market_items set title = 'x' where id = $1`, id)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("RLS update: %v %d", err, tag.RowsAffected())
		}
		tag, err = tx.Exec(ctx(), `delete from market_items where id = $1`, id)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("RLS delete: %v %d", err, tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithUser(ctx(), owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update market_items set photos = '{}' where id = $1`, id)
		return err
	}); err == nil {
		t.Fatal("owner may rewrite the photos column directly")
	}
	// owner: reserve, extend (capped at 30 days), cannot hide/unhide
	if w := do(t, "PATCH", p, owner, map[string]any{"status": "reserved", "pickup": "кітапхана"}); w.Code != 200 {
		t.Fatalf("reserve: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", p, owner, map[string]any{"status": "hidden"}); w.Code != 400 {
		t.Fatalf("owner sets hidden: %d", w.Code)
	}
	if err := db.WithUser(ctx(), owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update market_items set expires_at = now() + interval '1 year' where id = $1`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if d := scalar[float64](t, `select extract(epoch from expires_at - now()) / 86400 from market_items where id = $1`, id); d > 30.01 {
		t.Fatalf("expires in %.1f days", d)
	}
	exec(t, `update market_items set status = 'hidden', hidden_reason = 'reports' where id = $1`, id)
	if w := do(t, "PATCH", p, owner, map[string]any{"status": "active"}); w.Code != 404 {
		t.Fatalf("owner unhides: %d", w.Code)
	}
}

func TestReportsHideAndAdminDismissRestores(t *testing.T) {
	testdb.Need(t)
	owner, _ := newUser(t, userOpt{})
	admin, adminTG := newUser(t, userOpt{role: "admin"})
	photo := uploadPhoto(t, owner)
	id := newItem(t, owner, photo)
	p := "/api/market/items/" + strconv.FormatInt(id, 10)
	adminBefore := len(sentTo(adminTG))

	if w := do(t, "POST", p+"/report", owner, map[string]any{"reason": "selling"}); w.Code != 400 {
		t.Fatalf("own report: %d", w.Code)
	}
	// young accounts' reports are recorded but not counted
	for i := 0; i < 3; i++ {
		y, _ := newUser(t, userOpt{young: true})
		if w := do(t, "POST", p+"/report", y, map[string]any{"reason": "selling"}); w.Code != 200 || w.json(t)["hidden"] != false {
			t.Fatalf("young report: %d %s", w.Code, w.Body)
		}
	}
	if len(sentTo(adminTG)) != adminBefore+1 {
		t.Fatal("admins should hear about the first report only once")
	}
	r1, _ := newUser(t, userOpt{})
	r2, _ := newUser(t, userOpt{})
	r3, _ := newUser(t, userOpt{})
	do(t, "POST", p+"/report", r1, map[string]any{"reason": "selling", "text": "5000 тг сұрады"})
	if w := do(t, "POST", p+"/report", r1, map[string]any{"reason": "scam"}); w.json(t)["alreadyReported"] != true {
		t.Fatalf("duplicate report: %s", w.Body)
	}
	do(t, "POST", p+"/report", r2, map[string]any{"reason": "prohibited"})
	if s := scalar[string](t, `select status from market_items where id = $1`, id); s != "active" {
		t.Fatalf("hidden after 2 counted reporters: %s", s)
	}
	if w := do(t, "POST", p+"/report", r3, map[string]any{"reason": "other"}); w.json(t)["hidden"] != true {
		t.Fatalf("third counted report: %s", w.Body)
	}
	if body := do(t, "GET", "/api/market/items?university=all", 0, nil).Body.String(); strings.Contains(body, `"id":`+strconv.FormatInt(id, 10)+",") {
		t.Fatal("hidden item still listed")
	}
	if n := scalar[int](t, `select count(*) from submissions where kind = 'market_report' and status = 'pending' and (payload->>'itemId')::bigint = $1`, id); n != 1 {
		t.Fatalf("pending report submissions: %d", n)
	}
	if !fileExists(photo) {
		t.Fatal("photos must survive an automatic hide (it may be restored)")
	}
	list := do(t, "GET", "/api/admin/market/items", admin, nil)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"countedReports":3`) {
		t.Fatalf("admin list: %s", list.Body)
	}
	if w := do(t, "GET", "/api/admin/market/items", owner, nil); w.Code != 403 {
		t.Fatalf("student on admin list: %d", w.Code)
	}
	// admin finds the reports groundless: item comes back, reports dismissed, queue closed
	if w := do(t, "POST", "/api/admin/market/items/"+strconv.FormatInt(id, 10)+"/restore", admin, map[string]any{"reason": "сату емес, тегін"}); w.Code != 200 {
		t.Fatalf("restore: %d %s", w.Code, w.Body)
	}
	if s := scalar[string](t, `select status from market_items where id = $1`, id); s != "active" {
		t.Fatalf("after restore: %s", s)
	}
	if n := scalar[int](t, `select count(*) from reports where target_id = $1 and not dismissed`, strconv.FormatInt(id, 10)); n != 0 {
		t.Fatalf("undismissed reports: %d", n)
	}
	if s := scalar[string](t, `select status::text from submissions where kind = 'market_report' and (payload->>'itemId')::bigint = $1`, id); s != "rejected" {
		t.Fatalf("submission: %s", s)
	}
	if n := scalar[int](t, `select count(*) from audit_log where action = 'market_restore' and actor_id = $1`, admin); n != 1 {
		t.Fatal("restore not audited")
	}
	// admin hides for good: photos deleted
	if w := do(t, "POST", "/api/admin/market/items/"+strconv.FormatInt(id, 10)+"/hide", admin, map[string]any{"reason": "тыйым салынған зат"}); w.Code != 200 {
		t.Fatalf("hide: %d %s", w.Code, w.Body)
	}
	if s := scalar[string](t, `select status || '/' || hidden_reason from market_items where id = $1`, id); s != "hidden/admin" {
		t.Fatalf("after hide: %s", s)
	}
	if fileExists(photo) {
		t.Fatal("photo file kept after admin hide")
	}
}

func TestGivenDeletesPhotosAndAwardsBadge(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t, userOpt{})
	var photos []string
	for i := 0; i < 3; i++ {
		ph := uploadPhoto(t, uid)
		photos = append(photos, ph)
		id := newItem(t, uid, ph)
		w := do(t, "POST", "/api/market/items/"+strconv.FormatInt(id, 10)+"/given", uid, nil)
		if w.Code != 200 || (w.json(t)["badgeAwarded"] == true) != (i == 2) {
			t.Fatalf("given %d: %d %s", i, w.Code, w.Body)
		}
	}
	for _, ph := range photos {
		if fileExists(ph) {
			t.Fatal("photo kept after given")
		}
	}
	if n := scalar[int](t, `select count(*) from user_badges where user_id = $1 and badge = 'generous' and not verified`, uid); n != 1 {
		t.Fatal("generous badge missing")
	}
	if n := scalar[int](t, `select count(*) from market_photos where user_id = $1`, uid); n != 0 {
		t.Fatalf("photo rows left: %d", n)
	}
}

func TestAccountDeletionRemovesRowsAndFiles(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t, userOpt{})
	attached := uploadPhoto(t, uid)
	loose := uploadPhoto(t, uid)
	newItem(t, uid, attached)
	other, _ := newUser(t, userOpt{})
	keep := uploadPhoto(t, other)
	if !fileExists(attached) || !fileExists(loose) {
		t.Fatal("setup")
	}
	if err := me.Delete(ctx(), uid); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"market_items", "market_photos", "market_daily", "market_rules"} {
		if n := scalar[int](t, "select count(*) from "+tbl+" where user_id = $1", uid); n != 0 {
			t.Errorf("%s keeps %d rows", tbl, n)
		}
	}
	if fileExists(attached) || fileExists(loose) {
		t.Fatal("photo files survived account deletion")
	}
	if !fileExists(keep) {
		t.Fatal("another user's photo was deleted")
	}
}

func TestCronExpiryReminderOrphansSeason(t *testing.T) {
	testdb.Need(t)
	uid, tgID := newUser(t, userOpt{})
	photo := uploadPhoto(t, uid)
	old := newItem(t, uid, photo)
	soon := newItem(t, uid)
	loose := uploadPhoto(t, uid)
	exec(t, `update market_items set expires_at = now() - interval '1 minute' where id = $1`, old)
	exec(t, `update market_items set expires_at = now() + interval '2 days' where id = $1`, soon)
	exec(t, `update market_photos set created_at = now() - interval '2 days' where name = $1`, loose)
	now := time.Now()
	res, err := RunCron(ctx(), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Expired < 1 || res.Reminded < 1 || res.OrphanPhotos < 1 {
		t.Fatalf("cron: %+v", res)
	}
	if s := scalar[string](t, `select status from market_items where id = $1`, old); s != "expired" {
		t.Fatalf("old item: %s", s)
	}
	if fileExists(photo) || fileExists(loose) {
		t.Fatal("expired/orphan photo files kept")
	}
	msgs := sentTo(tgID)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "3 күн") {
		t.Fatalf("reminder: %q", msgs)
	}
	// already claimed today: the reminder is not repeated and nothing else goes out today
	exec(t, `update market_items set reminded_at = null where id = $1`, soon)
	if res, _ := RunCron(ctx(), now); res.Reminded != 0 || res.Postponed < 1 {
		t.Fatalf("second run: %+v", res)
	}
	// season: once per student per season
	day := notify.Day(now.Add(48 * time.Hour))
	t.Setenv("MARKET_SEASON", day+".."+day)
	res, err = RunCron(ctx(), now.Add(48*time.Hour))
	if err != nil || res.SeasonSent < 1 {
		t.Fatalf("season: %+v %v", res, err)
	}
	if n := scalar[int](t, `select count(*) from market_season_sends where user_id = $1`, uid); n != 1 {
		t.Fatal("season digest not recorded")
	}
	before := len(sentTo(tgID))
	exec(t, `delete from bot_daily_sends where user_id = $1`, uid)
	RunCron(ctx(), now.Add(48*time.Hour))
	for _, m := range sentTo(tgID)[before:] {
		if strings.Contains(m, "📦") {
			t.Fatal("season digest sent twice")
		}
	}
	if w := send(t, "GET", "/api/cron/market", 0, "", nil); w.Code != 200 {
		t.Fatalf("cron endpoint: %d %s", w.Code, w.Body)
	}
}

func TestAdminBan(t *testing.T) {
	testdb.Need(t)
	admin, _ := newUser(t, userOpt{role: "admin"})
	uid, _ := newUser(t, userOpt{})
	if w := do(t, "POST", "/api/admin/market/bans", admin, map[string]any{"userId": uid, "days": 7}); w.Code != 400 {
		t.Fatalf("ban without reason: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/market/bans", uid, map[string]any{"userId": admin, "days": 7, "reason": "test ban"}); w.Code != 403 {
		t.Fatalf("student bans: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/market/bans", admin, map[string]any{"userId": uid, "days": 7, "reason": "сатып жатыр"}); w.Code != 200 {
		t.Fatalf("ban: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/market/items", uid, map[string]any{"title": "x", "category": "other", "condition": "used"}); w.Code != 403 || w.code() != "banned" {
		t.Fatalf("banned publish: %d %s", w.Code, w.Body)
	}
	if n := scalar[int](t, `select count(*) from audit_log where action = 'market_ban' and target_id = $1 and reason = 'сатып жатыр'`, uid); n != 1 {
		t.Fatal("ban not audited")
	}
	if w := do(t, "DELETE", "/api/admin/market/bans/"+strconv.FormatInt(uid, 10), admin, map[string]any{"reason": "кешірім"}); w.Code != 200 {
		t.Fatalf("unban: %d %s", w.Code, w.Body)
	}
	newItem(t, uid)
}

func TestFeatureOff(t *testing.T) {
	testdb.Need(t)
	uid, _ := newUser(t, userOpt{})
	photo := uploadPhoto(t, uid)
	newItem(t, uid, photo)
	t.Setenv("FEATURE_MARKET", "off")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/market/items"}, {"POST", "/api/market/items"}, {"GET", "/api/market/photos/" + photo},
		{"POST", "/api/market/photos"}, {"GET", "/api/admin/market/items"}, {"GET", "/api/cron/market"},
	} {
		if w := do(t, c.method, c.path, uid, map[string]any{}); w.Code != 404 {
			t.Errorf("%s %s with feature off: %d", c.method, c.path, w.Code)
		}
	}
}

package community

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

var sent = struct {
	sync.Mutex
	msgs map[int64][]string
}{msgs: map[int64][]string{}}

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("c", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "10000")
	for _, f := range []string{"LOSTFOUND", "COURSEQA", "ROOMMATES", "POINTS"} {
		os.Setenv("FEATURE_"+f, "on")
	}
	dir, err := os.MkdirTemp("", "community-uploads-")
	if err != nil {
		panic(err)
	}
	os.Setenv("UPLOAD_DIR", dir)
	Notify = func(ctx context.Context, uid int64, text string) error {
		sent.Lock()
		sent.msgs[uid] = append(sent.msgs[uid], text)
		sent.Unlock()
		return nil
	}
	code := testdb.Main(m, "community")
	os.RemoveAll(dir)
	os.Exit(code)
}

func ctx() context.Context { return context.Background() }

var seq atomic.Int64

type userOpt struct {
	university, faculty, role string
	noContact                 bool
	gender                    string
}

func newUser(t *testing.T, o userOpt) int64 {
	t.Helper()
	testdb.Need(t)
	if o.university == "" {
		o.university = "KATU"
	}
	if o.role == "" {
		o.role = "student"
	}
	n := seq.Add(1)
	username := "cm_user" + strconv.FormatInt(n, 10)
	if o.noContact {
		username = ""
	}
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx(), `insert into users (telegram_id, tg_username, name, university, faculty, role, consent_at, is_adult, created_at, course)
			values ($1, nullif($2, ''), $3, $4, nullif($5, ''), $6::user_role, now(), true, now() - interval '30 days', 2) returning id`,
			900000+n, username, "Студент Тестов"+strconv.FormatInt(n, 10), o.university, o.faculty, o.role).Scan(&id); err != nil {
			return err
		}
		if o.gender != "" {
			_, err := tx.Exec(ctx(), `insert into roommate_genders (user_id, gender) values ($1, $2)`, id, o.gender)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
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

func must(t *testing.T, r resp, status int) map[string]any {
	t.Helper()
	if r.Code != status {
		t.Fatalf("want %d, got %d: %s", status, r.Code, r.Body)
	}
	return r.json(t)
}

func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return tx.QueryRow(ctx(), sql, args...).Scan(&v) }); err != nil {
		t.Fatal(err)
	}
	return v
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

// waitSent waits for a background notification to reach uid containing substr.
func waitSent(t *testing.T, uid int64, substr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sent.Lock()
		for _, m := range sent.msgs[uid] {
			if strings.Contains(m, substr) {
				sent.Unlock()
				return
			}
		}
		sent.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no notification for %d containing %q", uid, substr)
}

func id(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	obj, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("no %s in %v", key, m)
	}
	return int64(obj["id"].(float64))
}

func todayStr() string { return time.Now().In(almaty).Format("2006-01-02") }

// ---------- pure rules ----------

func TestTextRules(t *testing.T) {
	for _, s := range []string{"жаз: +7 701 123 45 67", "@aru_kz", "t.me/aru", "https://x.kz", "мейл aru@mail.ru", "8(701)1234567", "instagram.com/aru"} {
		if !HasContact(s) {
			t.Errorf("contact not caught: %q", s)
		}
	}
	for _, s := range []string{"Сары түсті рюкзак, 2-корпус", "3 бөлмелі пәтер, 1-қабат", "бюджет 60 000"} {
		if HasContact(s) {
			t.Errorf("false contact: %q", s)
		}
	}
	if !HasPhoneOrHandle("call 87011234567") || !HasPhoneOrHandle("жаз @aru_kz") || HasPhoneOrHandle("see https://docs.python.org/3/") {
		t.Error("Q&A contact rule")
	}
	if !HasLongNumber("ЖСН 010203 456789") || !HasLongNumber("карта 4400 4301 2345 6789") || HasLongNumber("1-305 аудитория") {
		t.Error("long number rule")
	}
	if NormKey("  Математика-1 ") != "математика 1" || NormKey("МАТЕМАТИКА 1") != "математика 1" || NormKey("Ёлка") != "елка" {
		t.Errorf("norm key: %q", NormKey("  Математика-1 "))
	}
	if ShortName("Аружан Тестова") != "Аружан Т." || ShortName("Аружан") != "Аружан" {
		t.Error("short name")
	}
}

// ---------- lost & found ----------

func lfPostBody(over map[string]any) map[string]any {
	b := map[string]any{"kind": "found", "category": "keys", "title": "Кілттер табылды", "desc": "Көк брелокпен", "building": "b1",
		"room": "1-305", "happenedOn": todayStr()}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func TestLostFoundClaimRevealAndReturn(t *testing.T) {
	finder := newUser(t, userOpt{})
	owner := newUser(t, userOpt{})
	stranger := newUser(t, userOpt{})
	p := must(t, do(t, "POST", "/api/lostfound/posts", finder, lfPostBody(nil)), 201)
	pid := id(t, p, "post")

	// anonymous list and detail: no author identity anywhere
	list := do(t, "GET", "/api/lostfound/posts?university=all", 0, nil)
	if list.Code != 200 || strings.Contains(list.Body.String(), "cm_user") || strings.Contains(list.Body.String(), "Студент Тестов") {
		t.Fatalf("anonymous list leaks identity: %s", list.Body)
	}
	if !strings.Contains(list.Body.String(), "Кілттер табылды") {
		t.Fatal("post missing from list")
	}

	// contact data in the private message is refused (contacts open only after consent)
	if r := do(t, "POST", "/api/lostfound/posts/"+itoa(pid)+"/claims", owner, map[string]any{"message": "Менікі, жаз @owner_kz"}); r.code() != "has_contact" {
		t.Fatalf("contact in claim: %d %s", r.Code, r.Body)
	}
	if r := do(t, "POST", "/api/lostfound/posts/"+itoa(pid)+"/claims", finder, map[string]any{"message": "Өзімдікі сияқты еді ғой"}); r.code() != "own_item" {
		t.Fatalf("own claim: %s", r.Body)
	}
	c := must(t, do(t, "POST", "/api/lostfound/posts/"+itoa(pid)+"/claims", owner, map[string]any{"message": "Брелокта кішкентай мысық суреті бар"}), 201)
	cid := id(t, c, "claim")
	if r := do(t, "POST", "/api/lostfound/posts/"+itoa(pid)+"/claims", owner, map[string]any{"message": "Брелокта кішкентай мысық суреті бар"}); r.code() != "already" {
		t.Fatalf("duplicate claim: %s", r.Body)
	}
	waitSent(t, finder, "иесі хабарласты")

	// before confirmation: nobody sees a contact; the stranger never sees the claim or its secret
	g := must(t, do(t, "GET", "/api/lostfound/posts/"+itoa(pid), owner, nil), 200)
	if g["myClaim"].(map[string]any)["contact"] != nil {
		t.Fatal("contact before confirmation")
	}
	for _, uid := range []int64{0, stranger} {
		r := do(t, "GET", "/api/lostfound/posts/"+itoa(pid), uid, nil)
		if r.Code != 200 || strings.Contains(r.Body.String(), "мысық") || strings.Contains(r.Body.String(), "cm_user") {
			t.Fatalf("claim leaked to %d: %s", uid, r.Body)
		}
	}
	// only the poster decides
	if r := do(t, "POST", "/api/lostfound/claims/"+itoa(cid)+"/decide", stranger, map[string]any{"accept": true}); r.Code != 404 {
		t.Fatalf("stranger decided: %d", r.Code)
	}
	if r := do(t, "POST", "/api/lostfound/claims/"+itoa(cid)+"/returned", owner, nil); r.code() != "bad_state" {
		t.Fatalf("returned before confirm: %s", r.Body)
	}
	must(t, do(t, "POST", "/api/lostfound/claims/"+itoa(cid)+"/decide", finder, map[string]any{"accept": true}), 200)
	waitSent(t, owner, "расталды")

	// after: both parties see each other's Telegram, the stranger still nothing
	finderName := scalar[string](t, `select tg_username from users where id = $1`, finder)
	ownerName := scalar[string](t, `select tg_username from users where id = $1`, owner)
	if r := do(t, "GET", "/api/lostfound/posts/"+itoa(pid), owner, nil); !strings.Contains(r.Body.String(), finderName) {
		t.Fatalf("owner lacks finder contact: %s", r.Body)
	}
	if r := do(t, "GET", "/api/lostfound/posts/"+itoa(pid), finder, nil); !strings.Contains(r.Body.String(), ownerName) {
		t.Fatalf("finder lacks owner contact: %s", r.Body)
	}
	if r := do(t, "GET", "/api/lostfound/mine", owner, nil); !strings.Contains(r.Body.String(), finderName) {
		t.Fatalf("mine lacks contact: %s", r.Body)
	}
	if r := do(t, "GET", "/api/lostfound/posts/"+itoa(pid), stranger, nil); strings.Contains(r.Body.String(), finderName) || strings.Contains(r.Body.String(), ownerName) {
		t.Fatal("contact leaked to a stranger")
	}

	// return needs both marks; the finder earns once
	r1 := must(t, do(t, "POST", "/api/lostfound/claims/"+itoa(cid)+"/returned", owner, nil), 200)
	if r1["returned"] != false {
		t.Fatal("one mark resolved the claim")
	}
	r2 := must(t, do(t, "POST", "/api/lostfound/claims/"+itoa(cid)+"/returned", finder, nil), 200)
	if r2["returned"] != true || r2["award"] == nil {
		t.Fatalf("return: %v", r2)
	}
	if scalar[string](t, `select status from lostfound_posts where id = $1`, pid) != "resolved" {
		t.Fatal("post not resolved")
	}
	if n := scalar[int](t, `select count(*) from xp_events where user_id = $1 and reason = 'lostfound_return'`, finder); n != 1 {
		t.Fatalf("finder awards: %d", n)
	}
	if n := scalar[int](t, `select count(*) from xp_events where user_id = $1`, owner); n != 0 {
		t.Fatal("the owner was paid")
	}
	// the resolved post is gone for strangers
	if r := do(t, "GET", "/api/lostfound/posts/"+itoa(pid), stranger, nil); r.Code != 404 {
		t.Fatalf("resolved post visible: %d", r.Code)
	}
}

func TestLostFoundSensitiveDocuments(t *testing.T) {
	u := newUser(t, userOpt{})
	cases := []struct {
		over map[string]any
		code string
	}{
		{map[string]any{"category": "cards", "photo": strings.Repeat("a", 32) + ".jpg", "handedTo": "dean"}, "sensitive_photo"},
		{map[string]any{"category": "documents", "title": "Жеке куәлік табылды"}, "handed_required"},
		{map[string]any{"category": "documents", "handedTo": "dean", "desc": "ЖСН 990101300123"}, "has_number"},
		{map[string]any{"title": "Кілт +77011234567"}, "has_contact"},
		{map[string]any{"happenedOn": "2020-01-01"}, "bad_date"},
	}
	for _, c := range cases {
		if r := do(t, "POST", "/api/lostfound/posts", u, lfPostBody(c.over)); r.code() != c.code {
			t.Errorf("%v: want %s, got %d %s", c.over, c.code, r.Code, r.Body)
		}
	}
	p := must(t, do(t, "POST", "/api/lostfound/posts", u, lfPostBody(map[string]any{"category": "cards", "title": "Студенттік билет", "handedTo": "security"})), 201)
	if p["post"].(map[string]any)["handedTo"] != "security" {
		t.Fatal("handedTo lost")
	}
	// lost documents need no hand-over, but still no photo
	must(t, do(t, "POST", "/api/lostfound/posts", u, lfPostBody(map[string]any{"kind": "lost", "category": "documents", "title": "Жеке куәлік жоғалды"})), 201)
}

func TestLostFoundLimitsAndChannel(t *testing.T) {
	t.Setenv("LOSTFOUND_DAILY_CLAIMS", "2")
	t.Setenv("LOSTFOUND_DAILY_POSTS", "3")
	poster := newUser(t, userOpt{})
	claimant := newUser(t, userOpt{})
	silent := newUser(t, userOpt{noContact: true})
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, id(t, must(t, do(t, "POST", "/api/lostfound/posts", poster, lfPostBody(nil)), 201), "post"))
	}
	if r := do(t, "POST", "/api/lostfound/posts", poster, lfPostBody(nil)); r.Code != 429 || r.code() != "daily_limit" {
		t.Fatalf("post limit: %d %s", r.Code, r.Body)
	}
	if r := do(t, "POST", "/api/lostfound/posts", silent, lfPostBody(nil)); r.code() != "no_contact" {
		t.Fatalf("no channel: %s", r.Body)
	}
	if r := do(t, "POST", "/api/lostfound/posts/"+itoa(ids[0])+"/claims", silent, map[string]any{"message": "Бұл менікі болуы керек"}); r.code() != "no_contact" {
		t.Fatalf("no channel claim: %s", r.Body)
	}
	for i := 0; i < 2; i++ {
		must(t, do(t, "POST", "/api/lostfound/posts/"+itoa(ids[i])+"/claims", claimant, map[string]any{"message": "Бұл менікі болуы керек"}), 201)
	}
	if r := do(t, "POST", "/api/lostfound/posts/"+itoa(ids[2])+"/claims", claimant, map[string]any{"message": "Бұл менікі болуы керек"}); r.Code != 429 {
		t.Fatalf("claim limit: %d %s", r.Code, r.Body)
	}
	// signed-out writes
	if r := do(t, "POST", "/api/lostfound/posts", 0, lfPostBody(nil)); r.Code != 401 {
		t.Fatalf("anonymous post: %d", r.Code)
	}
}

func jpegWithExif(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	exif := append([]byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08"), "GPSLatitude=51.1694N"...)
	seg := []byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	out = append(out, exif...)
	return append(out, b[2:]...)
}

func TestLostFoundPhotoDropsExifAndIsPrivateUntilPosted(t *testing.T) {
	u := newUser(t, userOpt{})
	other := newUser(t, userOpt{})
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("photo", "p.jpg")
	fw.Write(jpegWithExif(t))
	mw.Close()
	up := must(t, send(t, "POST", "/api/lostfound/photos", u, mw.FormDataContentType(), body.Bytes()), 201)
	name := up["name"].(string)
	stored, err := os.ReadFile(filepath.Join(PhotoDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("GPSLatitude")) || bytes.Contains(stored, []byte("Exif")) {
		t.Fatal("EXIF survived")
	}
	if r := do(t, "GET", "/api/lostfound/photos/"+name, other, nil); r.Code != 404 {
		t.Fatalf("unattached photo public: %d", r.Code)
	}
	if r := do(t, "GET", "/api/lostfound/photos/"+name, u, nil); r.Code != 200 {
		t.Fatalf("uploader cannot see: %d", r.Code)
	}
	// someone else cannot attach my upload
	if r := do(t, "POST", "/api/lostfound/posts", other, lfPostBody(map[string]any{"photo": name})); r.code() != "bad_photo" {
		t.Fatalf("foreign photo: %s", r.Body)
	}
	must(t, do(t, "POST", "/api/lostfound/posts", u, lfPostBody(map[string]any{"photo": name, "category": "bag", "title": "Қара сөмке"})), 201)
	if r := do(t, "GET", "/api/lostfound/photos/"+name, 0, nil); r.Code != 200 {
		t.Fatalf("posted photo not public: %d", r.Code)
	}
}

// ---------- RLS ----------

func TestRLSKeepsPrivateRowsPrivate(t *testing.T) {
	poster := newUser(t, userOpt{gender: "f"})
	claimant := newUser(t, userOpt{gender: "f"})
	third := newUser(t, userOpt{gender: "m"})
	pid := scalar[int64](t, `insert into lostfound_posts (user_id, kind, category, title, happened_on, university) values ($1, 'found', 'keys', 'RLS keys', current_date, 'KATU') returning id`, poster)
	exec(t, `insert into lostfound_claims (post_id, claimant_id, message) values ($1, $2, 'secret detail for RLS')`, pid, claimant)
	lid := scalar[int64](t, `insert into roommate_listings (user_id, kind, housing, district, budget_min, budget_max, move_in, university, same_gender)
		values ($1, 'have_room', 'flat', 'RLS', 0, 50000, current_date, 'KATU', true) returning id`, poster)
	exec(t, `insert into roommate_requests (listing_id, from_user) values ($1, $2)`, lid, claimant)
	qid := scalar[int64](t, `insert into qa_questions (user_id, university, subject_key, subject_name, title, status) values ($1, 'KATU', 'rls', 'RLS', 'Hidden RLS question', 'hidden') returning id`, poster)

	count := func(uid int64, q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.WithUser(ctx(), uid, func(tx pgx.Tx) error { return tx.QueryRow(ctx(), q, args...).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	claims := `select count(*) from lostfound_claims where post_id = $1`
	if count(poster, claims, pid) != 1 || count(claimant, claims, pid) != 1 || count(third, claims, pid) != 0 {
		t.Error("lostfound_claims RLS")
	}
	reqs := `select count(*) from roommate_requests where listing_id = $1`
	if count(poster, reqs, lid) != 1 || count(claimant, reqs, lid) != 1 || count(third, reqs, lid) != 0 {
		t.Error("roommate_requests RLS")
	}
	if count(third, `select count(*) from roommate_listings where id = $1`, lid) != 0 || count(poster, `select count(*) from roommate_listings where id = $1`, lid) != 1 {
		t.Error("roommate_listings RLS")
	}
	if count(third, `select count(*) from roommate_genders`) != 1 || count(third, `select count(*) from roommate_genders where user_id = $1`, poster) != 0 {
		t.Error("roommate_genders RLS: only my own row")
	}
	if count(third, `select count(*) from qa_questions where id = $1`, qid) != 0 || count(poster, `select count(*) from qa_questions where id = $1`, qid) != 1 {
		t.Error("qa_questions RLS")
	}
	// contacts: app_user reads its own users row only
	if count(claimant, `select count(*) from users where id = $1`, poster) != 0 {
		t.Error("users RLS")
	}
	// writes: app_user has no write grant on any community table
	err := db.WithUser(ctx(), third, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `update lostfound_claims set status = 'confirmed'`)
		return err
	})
	if err == nil {
		t.Error("app_user could write lostfound_claims")
	}
}

// ---------- Q&A ----------

func TestCourseQAFlow(t *testing.T) {
	asker := newUser(t, userOpt{faculty: "IT факультеті"})
	helper := newUser(t, userOpt{faculty: "IT факультеті"})
	other := newUser(t, userOpt{faculty: "Агро"})
	adminID := newUser(t, userOpt{role: "admin"})
	q := map[string]any{"subject": "  Математика-1 ", "title": "Туындыны қалай табамыз?", "body": "x^2 туындысы", "tags": []string{"#Туынды", "емтихан"},
		"anonymous": true, "scope": "faculty"}
	if r := do(t, "POST", "/api/qa/questions", asker, q); r.code() != "honesty" {
		t.Fatalf("honesty: %s", r.Body)
	}
	q["honesty"] = true
	if r := do(t, "POST", "/api/qa/questions", asker, map[string]any{"subject": "Физика", "title": "Маған жазыңыз +77011234567", "honesty": true}); r.code() != "has_contact" {
		t.Fatalf("phone in question: %s", r.Body)
	}
	qid := id(t, must(t, do(t, "POST", "/api/qa/questions", asker, q), 201), "question")

	// the board key: another spelling of the subject finds it; another faculty does not by default
	l := must(t, do(t, "GET", "/api/qa/questions?subject="+urlq("МАТЕМАТИКА 1"), helper, nil), 200)
	if len(l["questions"].([]any)) != 1 {
		t.Fatalf("board: %v", l)
	}
	if l := must(t, do(t, "GET", "/api/qa/questions?subject="+urlq("математика 1"), other, nil), 200); len(l["questions"].([]any)) != 0 {
		t.Fatal("faculty-only question shown to another faculty")
	}
	if l := must(t, do(t, "GET", "/api/qa/questions?subject="+urlq("математика 1")+"&faculty=all", other, nil), 200); len(l["questions"].([]any)) != 1 {
		t.Fatal("faculty=all")
	}
	// anonymous to classmates, known to moderators
	g := must(t, do(t, "GET", "/api/qa/questions/"+itoa(qid), helper, nil), 200)["question"].(map[string]any)
	if g["author"] != nil || g["authorId"] != nil {
		t.Fatalf("anonymous author leaked: %v", g)
	}
	ga := must(t, do(t, "GET", "/api/qa/questions/"+itoa(qid), adminID, nil), 200)["question"].(map[string]any)
	if int64(ga["authorId"].(float64)) != asker {
		t.Fatal("admin cannot see the anonymous author")
	}
	if tags := g["tags"].([]any); len(tags) != 2 || tags[0] != "туынды" {
		t.Fatalf("tags: %v", tags)
	}

	a1 := id(t, must(t, do(t, "POST", "/api/qa/questions/"+itoa(qid)+"/answers", helper, map[string]any{"body": "2x болады"}), 201), "answer")
	waitSent(t, asker, "жаңа жауап")
	a2 := id(t, must(t, do(t, "POST", "/api/qa/questions/"+itoa(qid)+"/answers", other, map[string]any{"body": "Анықтама бойынша шек арқылы"}), 201), "answer")
	// signed-in viewers see a short name of non-anonymous authors; signed-out viewers see none
	if r := do(t, "GET", "/api/qa/questions/"+itoa(qid), 0, nil); strings.Contains(r.Body.String(), "Студент") {
		t.Fatalf("names to signed-out: %s", r.Body)
	}
	if r := do(t, "GET", "/api/qa/questions/"+itoa(qid), asker, nil); !strings.Contains(r.Body.String(), "Студент Т.") {
		t.Fatalf("short name: %s", r.Body)
	}

	if r := do(t, "POST", "/api/qa/vote", helper, map[string]any{"kind": "answer", "id": a1, "up": true}); r.code() != "own_item" {
		t.Fatalf("own vote: %s", r.Body)
	}
	v := must(t, do(t, "POST", "/api/qa/vote", asker, map[string]any{"kind": "answer", "id": a1, "up": true}), 200)
	must(t, do(t, "POST", "/api/qa/vote", asker, map[string]any{"kind": "answer", "id": a1, "up": true}), 200)
	if v["score"].(float64) != 1 || scalar[int](t, `select score from qa_answers where id = $1`, a1) != 1 {
		t.Fatal("vote is not idempotent")
	}

	// only the asker accepts; the answerer is paid; switching takes it back
	if r := do(t, "POST", "/api/qa/questions/"+itoa(qid)+"/accept", helper, map[string]any{"answerId": a1}); r.Code != 404 {
		t.Fatalf("non-asker accepted: %d", r.Code)
	}
	acc := must(t, do(t, "POST", "/api/qa/questions/"+itoa(qid)+"/accept", asker, map[string]any{"answerId": a1}), 200)
	if acc["award"] == nil || scalar[int](t, `select coalesce(sum(delta), 0) from xp_events where user_id = $1`, helper) != 10 {
		t.Fatalf("accepted award: %v", acc)
	}
	waitSent(t, helper, "шешілді")
	must(t, do(t, "POST", "/api/qa/questions/"+itoa(qid)+"/accept", asker, map[string]any{"answerId": a2}), 200)
	if scalar[int](t, `select coalesce(sum(delta), 0) from xp_events where user_id = $1`, helper) != 0 {
		t.Fatal("award not reversed after switching")
	}
	got := must(t, do(t, "GET", "/api/qa/questions/"+itoa(qid), asker, nil), 200)
	if first := got["answers"].([]any)[0].(map[string]any); int64(first["id"].(float64)) != a2 || first["accepted"] != true {
		t.Fatalf("accepted answer first: %v", first)
	}

	// exam: a moderator locks the thread
	if r := do(t, "POST", "/api/admin/qa/questions/"+itoa(qid)+"/lock", asker, map[string]any{"locked": true, "reason": "Емтихан жүріп жатыр"}); r.Code != 403 {
		t.Fatalf("student locked: %d", r.Code)
	}
	must(t, do(t, "POST", "/api/admin/qa/questions/"+itoa(qid)+"/lock", adminID, map[string]any{"locked": true, "reason": "Емтихан жүріп жатыр"}), 200)
	if r := do(t, "POST", "/api/qa/questions/"+itoa(qid)+"/answers", helper, map[string]any{"body": "Жауабы: 42"}); r.code() != "locked" {
		t.Fatalf("answer to a locked thread: %s", r.Body)
	}
	if r := do(t, "GET", "/api/qa/questions/"+itoa(qid), 0, nil); !strings.Contains(r.Body.String(), "Емтихан жүріп жатыр") {
		t.Fatal("lock reason not shown")
	}
}

func TestReportsHideAndQueueWithRealAuthor(t *testing.T) {
	t.Setenv("REPORT_HIDE_AFTER", "2")
	author := newUser(t, userOpt{})
	qid := id(t, must(t, do(t, "POST", "/api/qa/questions", author, map[string]any{"subject": "Тарих", "title": "Емтихан жауаптарын кім береді?", "anonymous": true, "honesty": true}), 201), "question")
	for i := 0; i < 2; i++ {
		rep := newUser(t, userOpt{})
		if _, err := moderation.Report(ctx(), reportTarget(TargetQuestion), TargetQuestion, itoa(qid), rep, "Емтихан жауаптары"); err != nil {
			t.Fatal(err)
		}
	}
	if scalar[string](t, `select status from qa_questions where id = $1`, qid) != "hidden" {
		t.Fatal("not hidden after reports")
	}
	var sid int64
	var by int64
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select id, submitted_by from submissions where kind = $1 and payload->>'id' = $2`, KindHidden, itoa(qid)).Scan(&sid, &by)
	}); err != nil {
		t.Fatal(err)
	}
	if by != author {
		t.Fatal("queue lost the anonymous author")
	}
	if r := do(t, "GET", "/api/qa/questions/"+itoa(qid), 0, nil); r.Code != 404 {
		t.Fatal("hidden question visible")
	}
	admin := newUser(t, userOpt{role: "admin"})
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		_, err := moderation.Review(ctx(), tx, sid, admin, false, "Ереже бұзылмаған")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if scalar[string](t, `select status from qa_questions where id = $1`, qid) != "active" {
		t.Fatal("rejecting the queue item did not restore")
	}
}

// reportTarget fetches the registered moderation target through a report-only round trip.
func reportTarget(kind string) moderation.ReportTarget {
	t := targets[kind]
	return moderation.ReportTarget{
		Exists: func(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
			var ok bool
			err := tx.QueryRow(ctx, `select exists (select 1 from `+t.table+` where id = $1::bigint and status = 'active')`, id).Scan(&ok)
			return ok, err
		},
		SetHidden: func(ctx context.Context, tx pgx.Tx, id string, hidden bool) error {
			return setHidden(ctx, tx, kind, id, hidden)
		},
	}
}

// ---------- roommates ----------

func rmBody(over map[string]any) map[string]any {
	b := map[string]any{"kind": "have_room", "housing": "flat", "district": "Сарыарқа ауданы", "budgetMin": 40000, "budgetMax": 80000,
		"moveIn": todayStr(), "habits": []string{"no_smoking", "quiet", "tidy"}, "about": "Тыныш, таза студентпін"}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func TestRoommatesStrictGenderAndConsent(t *testing.T) {
	her := newUser(t, userOpt{university: "GENDERU", gender: "f"})
	sister := newUser(t, userOpt{university: "GENDERU", gender: "f"})
	him := newUser(t, userOpt{university: "GENDERU", gender: "m"})
	unset := newUser(t, userOpt{university: "GENDERU"})

	if r := do(t, "GET", "/api/roommates/listings", 0, nil); r.Code != 401 {
		t.Fatalf("signed-out read: %d", r.Code)
	}
	if r := do(t, "POST", "/api/roommates/listings", unset, rmBody(map[string]any{"sameGender": true})); r.code() != "need_gender" {
		t.Fatalf("strict without gender: %s", r.Body)
	}
	for _, about := range []string{"жазыңыз @her_kz", "тел 8 701 123 45 67", "https://krisha.kz/a/1"} {
		if r := do(t, "POST", "/api/roommates/listings", her, rmBody(map[string]any{"about": about})); r.code() != "has_contact" {
			t.Fatalf("contact %q: %s", about, r.Body)
		}
	}
	lid := id(t, must(t, do(t, "POST", "/api/roommates/listings", her, rmBody(map[string]any{"sameGender": true})), 201), "listing")
	if r := do(t, "POST", "/api/roommates/listings", her, rmBody(nil)); r.code() != "one_active" {
		t.Fatalf("one active: %s", r.Body)
	}
	if r := do(t, "POST", "/api/roommates/gender", her, map[string]any{"gender": "m"}); r.code() != "bad_state" {
		t.Fatal("gender changed behind a strict listing")
	}

	// strict: invisible to him and to an unset gender, also by id and for requests
	for _, uid := range []int64{him, unset} {
		l := must(t, do(t, "GET", "/api/roommates/listings", uid, nil), 200)
		if strings.Contains(do(t, "GET", "/api/roommates/listings", uid, nil).Body.String(), `"id":`+itoa(lid)+`,`) || len(l["listings"].([]any)) != 0 {
			t.Fatalf("strict listing shown to %d: %v", uid, l)
		}
		if r := do(t, "GET", "/api/roommates/listings/"+itoa(lid), uid, nil); r.Code != 404 {
			t.Fatalf("strict by id: %d", r.Code)
		}
		if r := do(t, "POST", "/api/roommates/listings/"+itoa(lid)+"/requests", uid, nil); r.Code != 404 {
			t.Fatalf("strict request: %d", r.Code)
		}
	}
	l := must(t, do(t, "GET", "/api/roommates/listings", sister, nil), 200)
	raw, _ := json.Marshal(l)
	if len(l["listings"].([]any)) != 1 || strings.Contains(string(raw), "cm_user") || strings.Contains(string(raw), "Студент") {
		t.Fatalf("sister's view: %s", raw)
	}

	rid := id(t, must(t, do(t, "POST", "/api/roommates/listings/"+itoa(lid)+"/requests", sister, nil), 201), "request")
	waitSent(t, her, "қызығушылық")
	if r := do(t, "POST", "/api/roommates/listings/"+itoa(lid)+"/requests", sister, nil); r.code() != "already" {
		t.Fatal("duplicate request")
	}
	herName := scalar[string](t, `select tg_username from users where id = $1`, her)
	sisterName := scalar[string](t, `select tg_username from users where id = $1`, sister)
	reqs := do(t, "GET", "/api/roommates/requests", her, nil)
	if strings.Contains(reqs.Body.String(), sisterName) || !strings.Contains(reqs.Body.String(), `"course":2`) {
		t.Fatalf("before consent: %s", reqs.Body)
	}
	if r := do(t, "POST", "/api/roommates/requests/"+itoa(rid)+"/decide", sister, map[string]any{"accept": true}); r.Code != 404 {
		t.Fatal("the requester decided her own request")
	}
	must(t, do(t, "POST", "/api/roommates/requests/"+itoa(rid)+"/decide", her, map[string]any{"accept": true}), 200)
	waitSent(t, sister, "қабылданды")
	if r := do(t, "GET", "/api/roommates/requests", her, nil); !strings.Contains(r.Body.String(), sisterName) {
		t.Fatalf("owner lacks contact: %s", r.Body)
	}
	if r := do(t, "GET", "/api/roommates/requests", sister, nil); !strings.Contains(r.Body.String(), herName) {
		t.Fatalf("requester lacks contact: %s", r.Body)
	}
	for _, uid := range []int64{him, unset} {
		r := do(t, "GET", "/api/roommates/requests", uid, nil)
		if strings.Contains(r.Body.String(), herName) || strings.Contains(r.Body.String(), sisterName) {
			t.Fatal("contact leaked to a third party")
		}
	}
}

func TestRoommatesMatchAndDailyCap(t *testing.T) {
	t.Setenv("ROOMMATES_DAILY_REQUESTS", "2")
	me := newUser(t, userOpt{university: "MATCHU", gender: "m"})
	if r := must(t, do(t, "GET", "/api/roommates/listings?match=1", me, nil), 200); r["needListing"] != true {
		t.Fatal("match without a listing")
	}
	must(t, do(t, "POST", "/api/roommates/listings", me, rmBody(map[string]any{"kind": "looking", "habits": []string{"quiet", "tidy", "early_bird"}})), 201)
	best := newUser(t, userOpt{university: "MATCHU"})
	some := newUser(t, userOpt{university: "MATCHU"})
	far := newUser(t, userOpt{university: "MATCHU"})
	dorm := newUser(t, userOpt{university: "MATCHU"})
	b := id(t, must(t, do(t, "POST", "/api/roommates/listings", best, rmBody(map[string]any{"habits": []string{"quiet", "tidy", "early_bird"}})), 201), "listing")
	s := id(t, must(t, do(t, "POST", "/api/roommates/listings", some, rmBody(map[string]any{"habits": []string{"quiet"}})), 201), "listing")
	f := id(t, must(t, do(t, "POST", "/api/roommates/listings", far, rmBody(map[string]any{"budgetMin": 200000, "budgetMax": 300000})), 201), "listing")
	must(t, do(t, "POST", "/api/roommates/listings", dorm, rmBody(map[string]any{"housing": "dorm"})), 201)
	m := must(t, do(t, "GET", "/api/roommates/listings?match=1", me, nil), 200)["listings"].([]any)
	if len(m) != 2 || int64(m[0].(map[string]any)["id"].(float64)) != b || int64(m[1].(map[string]any)["id"].(float64)) != s {
		t.Fatalf("match order: %v", m)
	}
	for _, lid := range []int64{b, s} {
		must(t, do(t, "POST", "/api/roommates/listings/"+itoa(lid)+"/requests", me, nil), 201)
	}
	if r := do(t, "POST", "/api/roommates/listings/"+itoa(f)+"/requests", me, nil); r.Code != 429 {
		t.Fatalf("daily cap: %d %s", r.Code, r.Body)
	}
	if r := must(t, do(t, "GET", "/api/roommates/me", me, nil), 200); r["requestsToday"].(float64) != 2 {
		t.Fatalf("me: %v", r)
	}
}

// ---------- 30-day archive ----------

func TestArchiveAfterThirtyDays(t *testing.T) {
	u := newUser(t, userOpt{university: "ARCH"})
	pid := id(t, must(t, do(t, "POST", "/api/lostfound/posts", u, lfPostBody(nil)), 201), "post")
	lid := id(t, must(t, do(t, "POST", "/api/roommates/listings", u, rmBody(nil)), 201), "listing")
	exec(t, `update lostfound_posts set expires_at = now() - interval '1 minute' where id = $1`, pid)
	exec(t, `update roommate_listings set expires_at = now() - interval '1 minute' where id = $1`, lid)
	// expired but not yet archived: already out of every list
	if r := do(t, "GET", "/api/lostfound/posts?university=ARCH", 0, nil); strings.Contains(r.Body.String(), `"id":`+itoa(pid)+`,`) {
		t.Fatal("expired post listed")
	}
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return Archive(ctx(), tx) }); err != nil {
		t.Fatal(err)
	}
	if scalar[string](t, `select status from lostfound_posts where id = $1`, pid) != "archived" || scalar[string](t, `select status from roommate_listings where id = $1`, lid) != "closed" {
		t.Fatal("not archived")
	}
	// a new listing is allowed again
	must(t, do(t, "POST", "/api/roommates/listings", u, rmBody(nil)), 201)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func urlq(s string) string { return url.QueryEscape(s) }

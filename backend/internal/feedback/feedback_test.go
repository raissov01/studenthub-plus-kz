package feedback_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feedback"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/server"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Main(m, "feedback")) }

var bg = context.Background()

func env(t *testing.T) {
	t.Helper()
	testdb.Need(t)
	t.Setenv("FEATURE_FEEDBACK", "true")
	t.Setenv("ALLOWED_HOSTS", "localhost")
	t.Setenv("SESSION_SECRET", strings.Repeat("feedback-test-secret", 3))
	t.Setenv("RATE_WRITE_PER_MIN", "10000")
	t.Setenv("UPLOAD_DIR", t.TempDir())
}

var seq int

func user(t *testing.T, role, univ string) int64 {
	t.Helper()
	seq++
	var id int64
	err := db.Tx(bg, func(tx pgx.Tx) error {
		return tx.QueryRow(bg, `insert into users (telegram_id, name, tg_username, role, university, consent_at, is_adult, bot_inactive)
			values ((select coalesce(max(telegram_id), 70000) + 1 from users), $1, $2, $3, $4, now(), true, true) returning id`,
			fmt.Sprintf("Sender Person %d", seq), fmt.Sprintf("sender_%d", seq), role, univ).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	if role == "university_staff" {
		exec(t, `insert into university_staff (user_id, university, granted_by) values ($1, $2, $1)`, id, univ)
	}
	return id
}

func exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if err := db.Tx(bg, func(tx pgx.Tx) error { _, err := tx.Exec(bg, q, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(bg, func(tx pgx.Tx) error { return tx.QueryRow(bg, q, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func send(t *testing.T, method, path, ctype string, body []byte, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Origin", "http://localhost")
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func call(t *testing.T, method, path string, body any, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	return send(t, method, path, "application/json", b, uid)
}

type fixture struct {
	admin, staff, other, stud int64
	rec                       int64
}

func setup(t *testing.T, univ string) fixture {
	t.Helper()
	f := fixture{admin: user(t, "admin", ""), staff: user(t, "university_staff", univ), other: user(t, "university_staff", univ), stud: user(t, "student", univ)}
	if w := call(t, "POST", "/api/admin/feedback/recipients", map[string]any{"university": univ, "defaults": true}, f.admin); w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	f.rec = int64(count(t, `select id from feedback_recipients where university = $1 and key = 'dorm'`, univ))
	// students only see recipients that have staff
	if w := call(t, "GET", "/api/feedback/recipients", nil, f.stud); !strings.Contains(w.Body.String(), `"recipients":[]`) {
		t.Fatalf("recipient without staff listed: %s", w.Body)
	}
	if w := call(t, "PUT", fmt.Sprintf("/api/admin/feedback/recipients/%d/staff", f.rec), map[string]any{"userIds": []int64{f.staff}}, f.admin); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	return f
}

type created struct {
	ID     int64
	Token  string
	Status string
	Held   bool
}

func ticket(t *testing.T, f fixture, body string, anonymous, ack bool) (created, *httptest.ResponseRecorder) {
	t.Helper()
	w := call(t, "POST", "/api/feedback/tickets", map[string]any{"recipientId": f.rec, "category": "complaint", "body": body, "anonymous": anonymous, "ack": ack}, f.stud)
	var c created
	_ = json.Unmarshal(w.Body.Bytes(), &c)
	return c, w
}

func TestAnonymousTicketReplyLoop(t *testing.T) {
	env(t)
	f := setup(t, "FB-A")
	c, w := ticket(t, f, "Жатақханада ыстық су жоқ, үш күн болды.", true, false)
	if w.Code != 201 || len(c.Token) != 43 || c.ID == 0 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	// stored without any author; only the hash of the token
	if count(t, `select count(*) from feedback_tickets where id = $1 and author_id is null and token_hash is not null and anonymous
		and created_at = date_trunc('hour', created_at)`, c.ID) != 1 {
		t.Fatal("anonymous ticket stored with an author or exact time")
	}
	if count(t, `select count(*) from feedback_tickets where token_hash = convert_to($1, 'UTF8')`, c.Token) != 0 {
		t.Fatal("raw token stored")
	}
	// the schema itself refuses an anonymous ticket with an author
	err := db.Tx(bg, func(tx pgx.Tx) error {
		_, err := tx.Exec(bg, `insert into feedback_tickets (recipient_id, category, body, anonymous, author_id, token_hash, sla_due_at, created_at, updated_at)
			values ($1, 'question', 'Body text here', true, $2, decode(repeat('ee', 32), 'hex'), now(), now(), now())`, f.rec, f.stud)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("anonymous + author accepted: %v", err)
	}

	// staff inbox: the ticket, never the sender
	inbox := call(t, "GET", "/api/staff/feedback", nil, f.staff)
	body := inbox.Body.String()
	if inbox.Code != 200 || !strings.Contains(body, "ыстық су") {
		t.Fatalf("inbox: %d %s", inbox.Code, body)
	}
	name := fmt.Sprintf("Sender Person %d", seq) // the student is the last fixture user
	if strings.Contains(body, name) || strings.Contains(body, "sender_") || strings.Contains(body, `"author"`) || strings.Contains(body, strconv.FormatInt(f.stud, 10)+`,`) {
		t.Fatalf("staff can see the anonymous sender: %s", body)
	}
	detail := call(t, "GET", fmt.Sprintf("/api/staff/feedback/%d", c.ID), nil, f.staff).Body.String()
	if strings.Contains(detail, name) || strings.Contains(detail, `"author"`) {
		t.Fatalf("detail: %s", detail)
	}
	// unassigned staff of the same university: 404
	if w := call(t, "GET", fmt.Sprintf("/api/staff/feedback/%d", c.ID), nil, f.other); w.Code != 404 {
		t.Fatalf("unassigned staff: %d", w.Code)
	}
	if strings.Contains(call(t, "GET", "/api/staff/feedback", nil, f.other).Body.String(), "ыстық су") {
		t.Fatal("unassigned inbox shows ticket")
	}

	// staff reply → the device sees it with its token (no session needed)
	if w := call(t, "POST", fmt.Sprintf("/api/staff/feedback/%d/reply", c.ID), map[string]any{"body": "Ертең жөндейміз."}, f.staff); w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	look := call(t, "POST", "/api/feedback/lookup", map[string]any{"tokens": []string{c.Token, strings.Repeat("A", 43)}}, 0)
	var res struct {
		Tickets []struct {
			Token  string
			Ticket feedback.Ticket
		}
	}
	if err := json.Unmarshal(look.Body.Bytes(), &res); err != nil || len(res.Tickets) != 1 {
		t.Fatalf("lookup: %d %s", look.Code, look.Body)
	}
	tk := res.Tickets[0].Ticket
	if tk.Status != "answered" || !tk.Unread || len(tk.Messages) != 1 || !tk.Messages[0].FromStaff || tk.Messages[0].Body != "Ертең жөндейміз." {
		t.Fatalf("ticket: %+v", tk)
	}
	if strings.Contains(look.Body.String(), "staff_id") || strings.Contains(look.Body.String(), `"staff`) || strings.Contains(look.Body.String(), "Sender Person") {
		t.Fatalf("lookup names staff: %s", look.Body)
	}
	// the student answers back with the token; a wrong token gets 404
	if w := call(t, "POST", fmt.Sprintf("/api/feedback/tickets/%d/messages", c.ID), map[string]any{"token": strings.Repeat("B", 43), "body": "hi"}, 0); w.Code != 404 {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w := call(t, "POST", fmt.Sprintf("/api/feedback/tickets/%d/messages", c.ID), map[string]any{"token": c.Token, "body": "Рақмет!"}, 0); w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(t, "POST", fmt.Sprintf("/api/feedback/tickets/%d/read", c.ID), map[string]any{"token": c.Token}, 0); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if count(t, `select count(*) from feedback_tickets where id = $1 and status = 'in_progress' and not student_unread`, c.ID) != 1 {
		t.Fatal("status after student reply")
	}
	// even the signed-in sender cannot reach the anonymous ticket without the token
	if w := call(t, "POST", fmt.Sprintf("/api/feedback/tickets/%d/read", c.ID), map[string]any{}, f.stud); w.Code != 404 {
		t.Fatalf("anonymous ticket linked to its sender's session: %d", w.Code)
	}
	if strings.Contains(call(t, "GET", "/api/feedback/mine", nil, f.stud).Body.String(), "ыстық су") {
		t.Fatal("anonymous ticket listed as mine")
	}
}

func TestSignedTicketAndRLS(t *testing.T) {
	env(t)
	f := setup(t, "FB-S")
	signed, w := ticket(t, f, "Асханада кешкі ас ерте бітеді.", false, false)
	if w.Code != 201 || signed.Token != "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	anon, _ := ticket(t, f, "Анонимді өтініш мәтіні осында.", true, false)
	inbox := call(t, "GET", "/api/staff/feedback", nil, f.staff).Body.String()
	if !strings.Contains(inbox, `"author":{`) {
		t.Fatalf("signed sender not shown to staff: %s", inbox)
	}
	call(t, "POST", fmt.Sprintf("/api/staff/feedback/%d/reply", signed.ID), map[string]any{"body": "Уақытты ұзартамыз."}, f.staff)
	if count(t, `select count(*) from notifications where user_id = $1 and link = '/feedback'`, f.stud) != 1 {
		t.Fatal("signed sender not notified of the reply")
	}
	// app_user: own signed ticket and its messages only; anonymous tickets have no author to match
	var tickets, msgs, recs int
	err := db.WithUser(bg, f.stud, func(tx pgx.Tx) error {
		if err := tx.QueryRow(bg, `select count(*) from feedback_tickets`).Scan(&tickets); err != nil {
			return err
		}
		if err := tx.QueryRow(bg, `select count(*) from feedback_messages`).Scan(&msgs); err != nil {
			return err
		}
		return tx.QueryRow(bg, `select count(*) from feedback_recipients`).Scan(&recs)
	})
	if err != nil || tickets != 1 || msgs != 1 || recs == 0 {
		t.Fatalf("RLS: tickets %d msgs %d: %v", tickets, msgs, err)
	}
	err = db.WithUser(bg, f.stud, func(tx pgx.Tx) error {
		var n int
		return tx.QueryRow(bg, `select count(*) from feedback_recipient_staff`).Scan(&n)
	})
	if err == nil {
		t.Fatal("app_user reads staff assignments")
	}
	_ = anon
	exp := call(t, "GET", "/api/me/export", nil, f.stud).Body.String()
	if !strings.Contains(exp, "кешкі ас") || strings.Contains(exp, "Анонимді өтініш") {
		t.Fatalf("export: %s", exp)
	}
	// account deletion removes the signed ticket, the anonymous one stays (it is nobody's)
	if w := call(t, "DELETE", "/api/me", map[string]string{"confirm": "DELETE"}, f.stud); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if count(t, `select count(*) from feedback_tickets where id = $1`, signed.ID) != 0 || count(t, `select count(*) from feedback_tickets where id = $1`, anon.ID) != 1 {
		t.Fatal("deletion")
	}
}

func TestThreatEscalationAndProfanityHold(t *testing.T) {
	env(t)
	f := setup(t, "FB-T")
	c, w := ticket(t, f, "Мен оны өлтіремін деп қорқытты, көмектесіңіз.", true, false)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "escalation_notice") || !strings.Contains(w.Body.String(), "112") {
		t.Fatalf("threat without notice: %d %s", w.Code, w.Body)
	}
	c, w = ticket(t, f, "Мен оны өлтіремін деп қорқытты, көмектесіңіз.", true, true)
	if w.Code != 201 || !c.Held || c.Status != "review" {
		t.Fatalf("threat with ack: %d %s", w.Code, w.Body)
	}
	if strings.Contains(call(t, "GET", "/api/staff/feedback", nil, f.staff).Body.String(), "өлтіремін") {
		t.Fatal("held ticket reached the recipient")
	}
	var subID int64
	_ = db.Tx(bg, func(tx pgx.Tx) error {
		return tx.QueryRow(bg, `select id from submissions where kind = 'feedback' and payload->>'ticketId' = $1 and submitted_by is null`, strconv.FormatInt(c.ID, 10)).Scan(&subID)
	})
	if subID == 0 {
		t.Fatal("not in the moderation queue (or names the sender)")
	}
	if count(t, `select count(*) from notifications n join users u on u.id = n.user_id where u.role = 'admin' and n.link = '/admin/queue'`) == 0 {
		t.Fatal("admins not alerted")
	}
	if w := call(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", subID), map[string]any{}, f.admin); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if !strings.Contains(call(t, "GET", "/api/staff/feedback", nil, f.staff).Body.String(), "өлтіремін") {
		t.Fatal("approved ticket not delivered")
	}
	// profanity: held, rejected → closed with a note the student can read
	p, w := ticket(t, f, "Бұл сука асхана мүлде нашар жұмыс істейді.", true, false)
	if w.Code != 201 || !p.Held {
		t.Fatalf("profanity: %d %s", w.Code, w.Body)
	}
	_ = db.Tx(bg, func(tx pgx.Tx) error {
		return tx.QueryRow(bg, `select id from submissions where kind = 'feedback' and payload->>'ticketId' = $1`, strconv.FormatInt(p.ID, 10)).Scan(&subID)
	})
	if w := call(t, "POST", fmt.Sprintf("/api/admin/queue/%d/reject", subID), map[string]any{"reason": "Сыпайы жазыңыз"}, f.admin); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	look := call(t, "POST", "/api/feedback/lookup", map[string]any{"tokens": []string{p.Token}}, 0).Body.String()
	if !strings.Contains(look, `"status":"closed"`) || !strings.Contains(look, "Сыпайы жазыңыз") {
		t.Fatalf("rejected ticket: %s", look)
	}
}

func TestRateLimitAndRoles(t *testing.T) {
	env(t)
	t.Setenv("FEEDBACK_PER_DAY", "2")
	f := setup(t, "FB-R")
	for i := 0; i < 2; i++ {
		if _, w := ticket(t, f, "Ұсыныс мәтіні нөмір "+strconv.Itoa(i), true, false); w.Code != 201 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if _, w := ticket(t, f, "Үшінші өтініш мәтіні.", true, false); w.Code != 429 {
		t.Fatalf("limit: %d", w.Code)
	}
	foreign := user(t, "student", "FB-OTHER")
	if w := call(t, "POST", "/api/feedback/tickets", map[string]any{"recipientId": f.rec, "category": "question", "body": "Басқа университеттен.", "anonymous": true}, foreign); w.Code != 404 {
		t.Fatalf("foreign university: %d", w.Code)
	}
	if w := call(t, "GET", "/api/staff/feedback", nil, f.stud); w.Code != 403 {
		t.Fatalf("student inbox: %d", w.Code)
	}
	// staff of another university cannot be assigned
	stranger := user(t, "university_staff", "FB-OTHER")
	if w := call(t, "PUT", fmt.Sprintf("/api/admin/feedback/recipients/%d/staff", f.rec), map[string]any{"userIds": []int64{stranger}}, f.admin); w.Code != 400 {
		t.Fatalf("foreign staff assigned: %d", w.Code)
	}
}

func TestSLAReminder(t *testing.T) {
	env(t)
	f := setup(t, "FB-SLA")
	c, _ := ticket(t, f, "Мерзімі өткен өтініш мәтіні.", true, false)
	exec(t, `update feedback_tickets set sla_due_at = now() - interval '1 hour' where id = $1`, c.ID)
	st, err := feedback.RemindOverdue(bg, time.Now())
	if err != nil || st.Overdue != 1 || st.Reminded != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	if st, _ := feedback.RemindOverdue(bg, time.Now()); st.Overdue != 0 {
		t.Fatal("reminded twice within a day")
	}
	if !strings.Contains(call(t, "GET", "/api/staff/feedback", nil, f.staff).Body.String(), `"overdue":true`) {
		t.Fatal("overdue flag")
	}
}

func TestPhotoEXIFStripped(t *testing.T) {
	env(t)
	f := setup(t, "FB-P")
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := 0; x < 40; x++ {
		img.Set(x, 5, color.RGBA{200, 10, 10, 255})
	}
	var plain bytes.Buffer
	_ = jpeg.Encode(&plain, img, nil)
	// splice an APP1 Exif segment with a fake GPS marker right after SOI
	exif := append([]byte("Exif\x00\x00"), []byte("GPSLatitude=51.18")...)
	seg := append([]byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	withExif := append(append(append([]byte{}, plain.Bytes()[:2]...), seg...), plain.Bytes()[2:]...)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	data, _ := mw.CreateFormField("data")
	_ = json.NewEncoder(data).Encode(map[string]any{"recipientId": f.rec, "category": "complaint", "body": "Фото қоса жібердім, қараңыз.", "anonymous": true})
	part, _ := mw.CreateFormFile("photo", "a.jpg")
	part.Write(withExif)
	mw.Close()
	w := send(t, "POST", "/api/feedback/tickets", mw.FormDataContentType(), buf.Bytes(), f.stud)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	files, _ := filepath.Glob(filepath.Join(feedback.PhotoDir(), "*.jpg"))
	if len(files) != 1 {
		t.Fatalf("files: %v", files)
	}
	stored, _ := os.ReadFile(files[0])
	if bytes.Contains(stored, []byte("Exif")) || bytes.Contains(stored, []byte("GPSLatitude")) {
		t.Fatal("EXIF survived")
	}
	name := filepath.Base(files[0])
	if w := call(t, "GET", "/api/feedback/photos/"+name, nil, f.staff); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("staff photo: %d", w.Code)
	}
	for _, u := range []int64{f.stud, f.other} {
		if w := call(t, "GET", "/api/feedback/photos/"+name, nil, u); w.Code != 404 {
			t.Fatalf("photo for %d: %d", u, w.Code)
		}
	}
}

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"Жатақханада ыстық су жоқ":            "",
		"Я убью его если ещё раз":             feedback.FlagThreat,
		"не хочу жить, всё плохо":             feedback.FlagThreat,
		"Өмір сүргім келмейді":                feedback.FlagThreat,
		"Это блядство какое-то":               feedback.FlagProfanity,
		"Ректорат пен деканат жауап бермейді": "",
		"Учебный план и расписание":           "",
	}
	for in, want := range cases {
		if got := feedback.Detect(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

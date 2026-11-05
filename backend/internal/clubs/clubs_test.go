package clubs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/events"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

var (
	sentMu sync.Mutex
	sent   = map[int64][]string{}
	seq    atomic.Int64
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("c", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("FEATURE_CLUBS", "true")
	os.Setenv("FEATURE_EVENTS", "true")
	os.Setenv("DB_MAX_CONNS", "8")
	Notify = func(_ context.Context, uid int64, text string) error {
		sentMu.Lock()
		defer sentMu.Unlock()
		sent[uid] = append(sent[uid], text)
		return nil
	}
	events.Notify = func(context.Context, int64, string) error { return nil }
	events.ClubManager = ManagedUniversity
	os.Exit(testdb.Main(m, "clubs"))
}

func messages(uid int64) []string {
	sentMu.Lock()
	defer sentMu.Unlock()
	return append([]string(nil), sent[uid]...)
}

// waitFor polls for an async notification (sent from a goroutine after the response).
func waitFor(t *testing.T, uid int64, part string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		for _, m := range messages(uid) {
			if strings.Contains(m, part) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("user %d: no message with %q in %q", uid, part, messages(uid))
}

var ctx = context.Background()

var mux = func() *http.ServeMux { m := http.NewServeMux(); Routes(m); events.Routes(m); return m }()

func newUser(t *testing.T, role string) int64 {
	t.Helper()
	testdb.Need(t)
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `insert into users (telegram_id, name, role, university, consent_at)
			values ($1, $2, $3::user_role, 'Сейфуллин университеті', now()) returning id`, 70000+seq.Add(1), "User "+role, role).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newClub(t *testing.T, name, mode string, admins ...int64) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		link := any(nil)
		if mode == "link" {
			link = "https://t.me/club_group"
		}
		if err := tx.QueryRow(ctx, `insert into clubs (name_kk, direction, university, join_mode, join_link)
			values ($1, 'debate', 'Сейфуллин университеті', $2, $3) returning id`, name, mode, link).Scan(&id); err != nil {
			return err
		}
		for _, a := range admins {
			if _, err := tx.Exec(ctx, `insert into club_admins (club_id, user_id) values ($1, $2)`, id, a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body, err)
	}
	return out
}

func want(t *testing.T, w *httptest.ResponseRecorder, code int) map[string]any {
	t.Helper()
	if w.Code != code {
		t.Fatalf("want %d, got %d: %s", code, w.Code, w.Body)
	}
	return decode(t, w)
}

func TestJoinRequestFlow(t *testing.T) {
	leader, student := newUser(t, "student"), newUser(t, "student")
	club := newClub(t, "Дебат клубы", "request", leader)
	p := fmt.Sprintf("/api/clubs/%d", club)

	out := want(t, do(t, "POST", p+"/join", student, map[string]string{"message": "1-курс, дебатқа қызығамын"}), 200)
	if out["status"] != "pending" || out["created"] != true {
		t.Fatalf("join: %v", out)
	}
	waitFor(t, leader, "жаңа өтінім")
	// repeated request is idempotent
	if out = want(t, do(t, "POST", p+"/join", student, nil), 200); out["created"] != false {
		t.Fatalf("second join: %v", out)
	}
	d := want(t, do(t, "GET", p, student, nil), 200)
	if my := d["club"].(map[string]any)["my"].(map[string]any); my["status"] != "pending" || my["admin"] != false {
		t.Fatalf("my: %v", my)
	}
	if _, ok := d["pending_requests"]; ok {
		t.Fatal("a student must not see the pending count")
	}
	// the leader sees and approves
	d = want(t, do(t, "GET", p, leader, nil), 200)
	if d["can_manage"] != true || d["pending_requests"].(float64) != 1 {
		t.Fatalf("leader detail: %v", d)
	}
	want(t, do(t, "POST", fmt.Sprintf("%s/requests/%d", p, student), leader, map[string]string{"decision": "approve"}), 200)
	waitFor(t, student, "қабылдандыңыз")
	d = want(t, do(t, "GET", p, 0, nil), 200)
	if d["club"].(map[string]any)["members"].(float64) != 1 {
		t.Fatalf("members count: %v", d["club"])
	}
	// approving again: nothing pending
	want(t, do(t, "POST", fmt.Sprintf("%s/requests/%d", p, student), leader, map[string]string{"decision": "approve"}), 404)
	// leaving
	want(t, do(t, "DELETE", p+"/join", student, nil), 200)
	want(t, do(t, "DELETE", p+"/join", student, nil), 404)

	// decline deletes the request and tells the student
	want(t, do(t, "POST", p+"/join", student, nil), 200)
	want(t, do(t, "POST", fmt.Sprintf("%s/requests/%d", p, student), leader, map[string]string{"decision": "decline"}), 200)
	waitFor(t, student, "қабылданбады")
	// someone who is not the leader cannot decide
	other := newUser(t, "student")
	want(t, do(t, "POST", p+"/join", other, nil), 200)
	want(t, do(t, "POST", fmt.Sprintf("%s/requests/%d", p, other), student, map[string]string{"decision": "approve"}), 403)
}

func TestJoinViaLinkClub(t *testing.T) {
	student := newUser(t, "student")
	club := newClub(t, "Arrow crew", "link")
	out := want(t, do(t, "POST", fmt.Sprintf("/api/clubs/%d/join", club), student, nil), 409)
	if out["join_link"] != "https://t.me/club_group" {
		t.Fatalf("link: %v", out)
	}
	// RLS refuses the row even when the Go check is bypassed
	err := db.WithUser(ctx, student, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into club_members (club_id, user_id) values ($1, app_user_id())`, club)
		return err
	})
	if pgCode(err) != "42501" {
		t.Fatalf("rls insert into link club: %v", err)
	}
}

func TestMembersListHiddenFromNonAdmins(t *testing.T) {
	leader, a, b := newUser(t, "student"), newUser(t, "student"), newUser(t, "student")
	club := newClub(t, "Privacy club", "request", leader)
	for _, u := range []int64{a, b} {
		want(t, do(t, "POST", fmt.Sprintf("/api/clubs/%d/join", club), u, nil), 200)
	}
	want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d/members", club), a, nil), 403)
	want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d/members", club), 0, nil), 401)
	out := want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d/members", club), leader, nil), 200)
	if n := len(out["members"].([]any)); n != 2 {
		t.Fatalf("leader sees %d members", n)
	}
	// RLS: a member reading the table directly sees only their own row
	var n int
	err := db.WithUser(ctx, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from club_members where club_id = $1`, club).Scan(&n)
	})
	if err != nil || n != 1 {
		t.Fatalf("rls own rows: %d %v", n, err)
	}
	err = db.WithUser(ctx, leader, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from club_members where club_id = $1`, club).Scan(&n)
	})
	if err != nil || n != 2 {
		t.Fatalf("rls leader rows: %d %v", n, err)
	}
	// and cannot approve themselves
	var changed int64
	err = db.WithUser(ctx, a, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update club_members set status = 'member' where club_id = $1`, club)
		changed = tag.RowsAffected()
		return err
	})
	if err == nil && changed != 0 {
		t.Fatal("a student approved their own request")
	}
	// the public detail never carries names
	if body := do(t, "GET", fmt.Sprintf("/api/clubs/%d", club), a, nil).Body.String(); strings.Contains(body, "User student") {
		t.Fatalf("names leaked: %s", body)
	}
}

func TestClubAdminCannotEditOtherClubs(t *testing.T) {
	leader := newUser(t, "student")
	mine := newClub(t, "Мой клуб", "request", leader)
	other := newClub(t, "Чужой клуб", "request")
	want(t, do(t, "PATCH", fmt.Sprintf("/api/clubs/%d", mine), leader, map[string]any{
		"schedule_kk": "Сейсенбі 18:00", "room": "3-корпус, 214", "recruiting": true, "telegram_url": "https://t.me/my_club",
		"leader_contact": "@leader", "leader_contact_public": false,
	}), 200)
	want(t, do(t, "PATCH", fmt.Sprintf("/api/clubs/%d", other), leader, map[string]any{"room": "1"}), 403)
	// admin-only fields stay with the site admin
	out := want(t, do(t, "PATCH", fmt.Sprintf("/api/clubs/%d", mine), leader, map[string]any{"name_kk": "Жаңа ат"}), 400)
	if out["error"].(map[string]any)["field"] != "name_kk" {
		t.Fatalf("field: %v", out)
	}
	want(t, do(t, "PATCH", fmt.Sprintf("/api/clubs/%d", mine), leader, map[string]any{"status": "archived"}), 400)
	want(t, do(t, "PATCH", fmt.Sprintf("/api/clubs/%d", mine), leader, map[string]any{"telegram_url": "http://evil"}), 400)
	// link mode needs a link (DB check)
	want(t, do(t, "PATCH", fmt.Sprintf("/api/clubs/%d", mine), leader, map[string]any{"join_mode": "link"}), 400)
	// RLS directly: zero rows for another club
	var n int64
	err := db.WithUser(ctx, leader, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update clubs set room = 'x' where id = $1`, other)
		n = tag.RowsAffected()
		return err
	})
	if err != nil || n != 0 {
		t.Fatalf("rls other club: %d %v", n, err)
	}
	// app_user has no grant on name_kk at all
	err = db.WithUser(ctx, leader, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update clubs set name_kk = 'x' where id = $1`, mine)
		return err
	})
	if pgCode(err) != "42501" {
		t.Fatalf("name_kk grant: %v", err)
	}
	// the hidden contact is not public, but the leader sees it
	pub := want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", mine), 0, nil), 200)["club"].(map[string]any)
	if pub["leader_contact"] != nil || pub["recruiting"] != true {
		t.Fatalf("public club: %v", pub)
	}
	own := want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", mine), leader, nil), 200)["club"].(map[string]any)
	if own["leader_contact"] != "@leader" {
		t.Fatalf("leader view: %v", own)
	}
}

func TestLeaderApplicationAndModeratedNews(t *testing.T) {
	student, admin := newUser(t, "student"), newUser(t, "admin")
	club := newClub(t, "Formula Student", "request")
	app := want(t, do(t, "POST", "/api/clubs/leader-applications", student, map[string]any{
		"club_id": club, "proof_text": "Мен клубтың төрағасымын, 2025 жылдан бері.", "proof_url": "https://instagram.com/p/x",
	}), 201)
	want(t, do(t, "POST", "/api/clubs/leader-applications", student, map[string]any{
		"club_id": club, "proof_text": "Мен клубтың төрағасымын, 2025 жылдан бері.", "proof_url": "https://instagram.com/p/x",
	}), 409)
	want(t, do(t, "POST", "/api/clubs/leader-applications", student, map[string]any{"club_id": club, "proof_text": "short", "proof_url": "https://x.kz"}), 400)
	// before approval: no rights
	want(t, do(t, "POST", fmt.Sprintf("/api/clubs/%d/posts", club), student, map[string]any{"body_kk": "Жиналыс сейсенбіде"}), 403)
	review := func(id int64, approve bool) {
		t.Helper()
		reason := ""
		if !approve {
			reason = "дәлел жоқ"
		}
		if err := db.Tx(ctx, func(tx pgx.Tx) error { _, err := moderation.Review(ctx, tx, id, admin, approve, reason); return err }); err != nil {
			t.Fatal(err)
		}
	}
	review(int64(app["id"].(float64)), true)
	if ok, _ := IsAdmin(ctx, db0(t), club, student); !ok {
		t.Fatal("approved applicant is not a club admin")
	}

	// a new club through the application
	app2 := want(t, do(t, "POST", "/api/clubs/leader-applications", student, map[string]any{
		"name_kk": "Робототехника клубы", "direction": "it", "proof_text": "Клубты биыл құрдық, 15 мүшеміз бар.", "proof_url": "https://t.me/robo_kz",
	}), 201)
	review(int64(app2["id"].(float64)), true)
	var newID int64
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select c.id from clubs c join club_admins a on a.club_id = c.id where c.name_kk = 'Робототехника клубы' and a.user_id = $1`, student).Scan(&newID)
	}); err != nil {
		t.Fatalf("new club: %v", err)
	}

	// news: pending until moderation, visible to the leader, then public
	post := want(t, do(t, "POST", fmt.Sprintf("/api/clubs/%d/posts", club), student, map[string]any{"body_kk": "Жарыс көлігінің жаңа шассиі дайын"}), 201)
	pub := want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", club), 0, nil), 200)
	if len(pub["posts"].([]any)) != 0 {
		t.Fatal("pending post is public")
	}
	own := want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", club), student, nil), 200)
	if len(own["posts"].([]any)) != 1 {
		t.Fatal("leader does not see own pending post")
	}
	review(int64(post["submission_id"].(float64)), true)
	pub = want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", club), 0, nil), 200)
	if ps := pub["posts"].([]any); len(ps) != 1 || ps[0].(map[string]any)["status"] != "published" {
		t.Fatalf("published: %v", ps)
	}
	post2 := want(t, do(t, "POST", fmt.Sprintf("/api/clubs/%d/posts", club), student, map[string]any{"body_kk": "Жарнама сілтемесі"}), 201)
	review(int64(post2["submission_id"].(float64)), false)
	pub = want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", club), 0, nil), 200)
	if len(pub["posts"].([]any)) != 1 {
		t.Fatal("rejected post is public")
	}
	// another club's leader cannot post here
	other := newUser(t, "student")
	newClub(t, "Other", "request", other)
	want(t, do(t, "POST", fmt.Sprintf("/api/clubs/%d/posts", club), other, map[string]any{"body_kk": "Спам мәтін"}), 403)
}

func db0(t *testing.T) pgx.Tx {
	t.Helper()
	p, err := db.Pool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func TestClubEvents(t *testing.T) {
	leader, outsider, student := newUser(t, "student"), newUser(t, "student"), newUser(t, "student")
	club := newClub(t, "Event club", "request", leader)
	newClub(t, "Outsider club", "request", outsider)
	starts := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Minute)
	ev := map[string]any{"title_kk": "Клуб кездесуі", "cat": "youth", "starts_at": starts, "ends_at": starts.Add(2 * time.Hour),
		"status": "published", "club_id": club}
	out := want(t, do(t, "POST", "/api/events/create", leader, ev), 201)
	e := out["event"].(map[string]any)
	if e["club_id"].(float64) != float64(club) || e["university"] != "Сейфуллин университеті" || e["can_manage"] != true {
		t.Fatalf("event: %v", e)
	}
	id := int64(e["id"].(float64))
	want(t, do(t, "POST", "/api/events/create", outsider, ev), 403)
	// listed for the club, with the manage flag for the leader only
	list := want(t, do(t, "GET", fmt.Sprintf("/api/events?club=%d", club), leader, nil), 200)["events"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["can_manage"] != true {
		t.Fatalf("club list: %v", list)
	}
	list = want(t, do(t, "GET", fmt.Sprintf("/api/events?club=%d", club), student, nil), 200)["events"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["can_manage"] != false {
		t.Fatalf("club list student: %v", list)
	}
	// the normal events flow: edit, registration, registrations list for the leader only
	want(t, do(t, "PATCH", fmt.Sprintf("/api/events/%d", id), leader, map[string]any{"place_text": "Акт залы"}), 200)
	want(t, do(t, "PATCH", fmt.Sprintf("/api/events/%d", id), outsider, map[string]any{"place_text": "x"}), 403)
	want(t, do(t, "POST", fmt.Sprintf("/api/events/%d/register", id), student, nil), 200)
	want(t, do(t, "GET", fmt.Sprintf("/api/events/%d/regs", id), leader, nil), 200)
	want(t, do(t, "GET", fmt.Sprintf("/api/events/%d/regs", id), outsider, nil), 403)
	manage := want(t, do(t, "GET", "/api/events/manage", leader, nil), 200)["events"].([]any)
	if len(manage) != 1 {
		t.Fatalf("manage list: %d", len(manage))
	}
	// RLS: an ambassador cannot tag another club's event
	amb := newUser(t, "ambassador")
	err := db.WithUser(ctx, amb, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into events (title_kk, cat, starts_at, ends_at, university, status, created_by, club_id)
			values ('Тег', 'youth', now() + interval '1 day', now() + interval '2 days', 'Сейфуллин университеті', 'published', app_user_id(), $1)`, club)
		return err
	})
	if pgCode(err) != "42501" {
		t.Fatalf("restrictive club tag: %v", err)
	}
}

func TestSiteAdminCRUD(t *testing.T) {
	admin, student := newUser(t, "admin"), newUser(t, "student")
	want(t, do(t, "POST", "/api/admin/clubs", student, map[string]any{"name_kk": "X"}), 403)
	c := want(t, do(t, "POST", "/api/admin/clubs", admin, map[string]any{"name_kk": "Шахмат клубы", "direction": "sport",
		"source_url": "https://kazatu.edu.kz/x", "verified_at": "2026-11-05"}), 201)["club"].(map[string]any)
	id := int64(c["id"].(float64))
	want(t, do(t, "POST", "/api/admin/clubs", admin, map[string]any{"name_kk": "Y", "direction": "chess"}), 400)
	want(t, do(t, "POST", fmt.Sprintf("/api/admin/clubs/%d/admins", id), admin, map[string]any{"userId": student}), 200)
	if ok, _ := IsAdmin(ctx, db0(t), id, student); !ok {
		t.Fatal("grant")
	}
	all := want(t, do(t, "GET", "/api/admin/clubs", admin, nil), 200)["clubs"].([]any)
	found := false
	for _, x := range all {
		if m := x.(map[string]any); int64(m["id"].(float64)) == id {
			found = len(m["admins"].([]any)) == 1
		}
	}
	if !found {
		t.Fatal("admin list")
	}
	want(t, do(t, "PATCH", fmt.Sprintf("/api/admin/clubs/%d", id), admin, map[string]any{"status": "archived"}), 200)
	want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", id), 0, nil), 404)
	want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", id), student, nil), 200) // its club admin still sees it
	want(t, do(t, "DELETE", fmt.Sprintf("/api/admin/clubs/%d/admins/%d", id, student), admin, nil), 200)
	want(t, do(t, "GET", fmt.Sprintf("/api/clubs/%d", id), student, nil), 404)
	var n int
	_ = db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from audit_log where actor_id = $1 and action like 'club_%'`, admin).Scan(&n)
	})
	if n < 4 {
		t.Fatalf("audit rows: %d", n)
	}
}

func TestSeedIsIdempotentAndValid(t *testing.T) {
	testdb.Need(t)
	recs, err := SeedRecords()
	if err != nil || len(recs) == 0 {
		t.Fatalf("seed file: %d %v", len(recs), err)
	}
	for _, r := range recs {
		if !strings.HasPrefix(r.SourceURL, "https://kazatu.edu.kz/") || r.VerifiedAt == "" {
			t.Errorf("%s: unverified source %q", r.SeedKey, r.SourceURL)
		}
	}
	n, err := Seed(ctx)
	if err != nil || n != len(recs) {
		t.Fatalf("first seed: %d %v", n, err)
	}
	if n, err = Seed(ctx); err != nil || n != 0 {
		t.Fatalf("second seed: %d %v", n, err)
	}
}

func TestAttachFeedItem(t *testing.T) {
	club := newClub(t, "Media", "request")
	var id1, id2 int64
	var c1, c2 bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if id1, c1, err = AttachFeedItem(ctx, tx, club, FeedItem{Ref: "tg:media/1", BodyKK: "Кастинг ашық", URL: "https://t.me/media/1"}); err != nil {
			return err
		}
		id2, c2, err = AttachFeedItem(ctx, tx, club, FeedItem{Ref: "tg:media/1", BodyKK: "Кастинг ашық"})
		return err
	})
	if err != nil || !c1 || c2 || id1 != id2 {
		t.Fatalf("attach: %d %v %d %v %v", id1, c1, id2, c2, err)
	}
	var kind string
	_ = db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select kind from submissions where payload->>'post_id' = $1`, fmt.Sprint(id1)).Scan(&kind)
	})
	if kind != KindPost {
		t.Fatalf("feed item not queued: %q", kind)
	}
}

package portfolio

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("p", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "10000")
	os.Setenv("FEATURE_PORTFOLIO", "on")
	os.Exit(testdb.Main(m, "portfolio"))
}

func ctx() context.Context { return context.Background() }

var seq atomic.Int64

func newUser(t *testing.T, name string) (int64, int64) {
	t.Helper()
	n := seq.Add(1)
	tg := 910000 + n
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, tg_username, name, university, faculty, course, consent_at, is_adult)
			values ($1, $2, $3, 'ENU', 'IT', 3, now(), true) returning id`, tg, "user"+strconv.FormatInt(n, 10), name).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, tg
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	r.Header.Set("X-Real-Ip", "10.9."+strconv.FormatInt(seq.Add(1)%250, 10)+".1")
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

func settingsOf(t *testing.T, w *httptest.ResponseRecorder) Settings {
	t.Helper()
	var out struct{ Settings Settings }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Settings.Slug == "" {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	return out.Settings
}

// setup: a student with a checked-in event, an event certificate, an ambassador certificate,
// a verified mentor badge and an unverified "generous" badge.
func setup(t *testing.T) (int64, int64) {
	uid, tg := newUser(t, "Айгерім")
	var ev int64
	if err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into events (title_kk, cat, starts_at, ends_at, university, status, gives_cert)
			values ('Хакатон', 'career', now() - interval '3 days', now() - interval '2 days', 'ENU', 'finished', true) returning id`).Scan(&ev)
	}); err != nil {
		t.Fatal(err)
	}
	exec(t, `insert into event_regs (event_id, user_id, ticket, checked_in_at, checkin_method) values ($1, $2, $3, now() - interval '3 days', 'qr')`,
		ev, uid, "T"+strconv.FormatInt(uid, 10))
	exec(t, `insert into event_certificates (event_id, user_id) values ($1, $2)`, ev, uid)
	exec(t, `insert into ambassador_terms (user_id, university, term_start, term_end, hours_per_week, ref_code, status, cert_code, cert_issued_at)
		values ($1, 'ENU', current_date - 100, current_date - 1, 3, $2, 'ended', $3, now())`, uid, "code"+strconv.FormatInt(uid, 10), "cert"+strconv.FormatInt(uid, 10)+"xyz")
	exec(t, `insert into user_badges (user_id, badge, verified) values ($1, 'mentor', true), ($1, 'generous', false)`, uid)
	return uid, tg
}

func TestDisabledAndMissingAreIndistinguishable(t *testing.T) {
	testdb.Need(t)
	uid, _ := setup(t)
	w := do(t, "GET", "/api/portfolio/me", uid, nil)
	s := settingsOf(t, w)
	if s.Enabled || !ValidSlug(s.Slug) || s.Slug == "user1" {
		t.Fatalf("defaults: %+v", s)
	}
	disabled := do(t, "GET", "/api/p/"+s.Slug, 0, nil)
	missing := do(t, "GET", "/api/p/zzzzzzzzzz", 0, nil)
	badShape := do(t, "GET", "/api/p/AB", 0, nil)
	unknown := do(t, "GET", "/api/nope", 0, nil)
	for _, x := range []*httptest.ResponseRecorder{disabled, missing, badShape} {
		if x.Code != 404 || x.Body.String() != unknown.Body.String() {
			t.Fatalf("not neutral: %d %s vs %s", x.Code, x.Body, unknown.Body)
		}
	}
	// switch off: the route is a plain unknown route
	t.Setenv("FEATURE_PORTFOLIO", "off")
	if off := do(t, "GET", "/api/p/"+s.Slug, 0, nil); off.Code != 404 || off.Body.String() != unknown.Body.String() {
		t.Fatalf("feature off: %d %s", off.Code, off.Body)
	}
	if off := do(t, "GET", "/api/portfolio/me", uid, nil); off.Code != 404 {
		t.Fatalf("feature off me: %d", off.Code)
	}
}

func TestPublicPage(t *testing.T) {
	testdb.Need(t)
	uid, tg := setup(t)
	s := settingsOf(t, do(t, "PATCH", "/api/portfolio/settings", uid, map[string]any{
		"enabled": true, "headline": "Backend стажер", "bio": `<script>alert(1)</script> "x" & y`, "showContact": true,
	}))
	// self-declared items need the explicit sync consent
	items := map[string]any{"items": []map[string]any{
		{"kind": "cert", "title": "Coursera Go", "url": "https://coursera.org/x"},
		{"kind": "experience", "title": "Кафе", "org": "Coffee", "period": "2025"},
		{"kind": "skill", "title": "Go"},
	}}
	if w := do(t, "PUT", "/api/portfolio/items", uid, items); w.Code != 409 {
		t.Fatalf("items without consent: %d %s", w.Code, w.Body)
	}
	items["consent"] = true
	if w := do(t, "PUT", "/api/portfolio/items", uid, items); w.Code != 200 {
		t.Fatalf("items: %d %s", w.Code, w.Body)
	}
	w := do(t, "GET", "/api/p/"+s.Slug, 0, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("public: %d %v", w.Code, w.Header())
	}
	body := w.Body.String()
	var p Page
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !p.NoIndex || p.Profile.Name != "Айгерім" || *p.Profile.University != "ENU" || *p.Profile.Course != 3 {
		t.Fatalf("profile: %+v", p)
	}
	if len(p.Verified.Events) != 1 || !p.Verified.Events[0].Verified || !strings.Contains(p.Verified.Events[0].VerifyURL, "/#/event/") {
		t.Fatalf("verified events: %+v", p.Verified.Events)
	}
	if len(p.Verified.Certificates) != 2 || len(p.Verified.Badges) != 1 || p.Verified.Badges[0].Badge != "mentor" {
		t.Fatalf("verified certs/badges: %+v", p.Verified)
	}
	if len(p.SelfReported.Certificates) != 1 || p.SelfReported.Certificates[0].Verified ||
		len(p.SelfReported.Experience) != 1 || len(p.SelfReported.Skills) != 1 ||
		len(p.SelfReported.Badges) != 1 || p.SelfReported.Badges[0].Verified {
		t.Fatalf("self-reported: %+v", p.SelfReported)
	}
	if p.Contact == nil || !strings.HasPrefix(p.Contact.TelegramURL, "https://t.me/user") {
		t.Fatalf("contact: %+v", p.Contact)
	}
	if !strings.Contains(p.OG.Title, "Student Hub портфолиосы") {
		t.Fatalf("og: %+v", p.OG)
	}
	// XSS: the raw response never carries markup, and no ids / hidden fields leak
	if strings.Contains(body, "<script>") || strings.Contains(body, `"id"`) || strings.Contains(body, "userId") ||
		strings.Contains(body, strconv.FormatInt(tg, 10)) || strings.Contains(strings.ToLower(body), "gpa") ||
		strings.Contains(body, "phone") || strings.Contains(body, "email") || strings.Contains(body, "slug") {
		t.Fatalf("leak in public body: %s", body)
	}
	if p.Profile.Bio == nil || !strings.Contains(*p.Profile.Bio, "<script>") {
		t.Fatalf("bio text should round-trip as data: %v", p.Profile.Bio)
	}

	// switched-off blocks disappear
	do(t, "PATCH", "/api/portfolio/settings", uid, map[string]any{"showEvents": false, "showCerts": false, "showContact": false,
		"showUniversity": false, "showSkills": false})
	json.Unmarshal(do(t, "GET", "/api/p/"+s.Slug, 0, nil).Body.Bytes(), &p)
	if len(p.Verified.Events) != 0 || len(p.Verified.Certificates) != 0 || len(p.SelfReported.Certificates) != 0 ||
		len(p.SelfReported.Skills) != 0 || p.Contact != nil || p.Profile.University != nil || len(p.SelfReported.Experience) != 1 {
		t.Fatalf("blocks not hidden: %+v", p)
	}

	// GPA / phone / email have no field and no toggle; nobody can mark an item verified
	for _, bad := range []map[string]any{{"gpa": 3.9}, {"showPhone": true}, {"email": "a@b.c"}, {"verified": true}} {
		if w := do(t, "PATCH", "/api/portfolio/settings", uid, bad); w.Code != 400 {
			t.Errorf("patch %v: %d", bad, w.Code)
		}
	}
	if w := do(t, "PUT", "/api/portfolio/items", uid, map[string]any{"items": []map[string]any{{"kind": "cert", "title": "Fake", "verified": true}}}); w.Code != 400 {
		t.Fatalf("verified flag accepted: %d", w.Code)
	}
	// ... not even with crafted SQL as the student: no column, no grant on system tables
	for _, q := range []string{
		"update portfolio_items set verified = true",
		"insert into user_badges (user_id, badge, verified) values (app_user_id(), 'mentor2', true)",
		"update event_regs set checked_in_at = now() where user_id = app_user_id()",
		"insert into event_certificates (event_id, user_id) values (1, app_user_id())",
	} {
		err := db.WithUser(ctx(), uid, func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), q); return err })
		if err == nil {
			t.Errorf("%q succeeded", q)
		}
	}
	// turning the sync consent off deletes the synced items
	do(t, "PATCH", "/api/portfolio/settings", uid, map[string]any{"syncConsent": false})
	var n int
	db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `select count(*) from portfolio_items where user_id = $1`, uid).Scan(&n)
	})
	if n != 0 {
		t.Fatalf("items survived consent withdrawal: %d", n)
	}
}

func TestSlugRotationAndAccountDeletion(t *testing.T) {
	testdb.Need(t)
	uid, _ := setup(t)
	old := settingsOf(t, do(t, "PATCH", "/api/portfolio/settings", uid, map[string]any{"enabled": true}))
	if w := do(t, "GET", "/api/p/"+old.Slug, 0, nil); w.Code != 200 {
		t.Fatalf("before rotation: %d", w.Code)
	}
	fresh := settingsOf(t, do(t, "POST", "/api/portfolio/slug", uid, nil))
	if fresh.Slug == old.Slug || fresh.SlugRotatedAt == nil {
		t.Fatalf("not rotated: %+v", fresh)
	}
	if w := do(t, "GET", "/api/p/"+old.Slug, 0, nil); w.Code != 404 {
		t.Fatalf("old slug: %d", w.Code)
	}
	if w := do(t, "GET", "/api/p/"+fresh.Slug, 0, nil); w.Code != 200 {
		t.Fatalf("new slug: %d", w.Code)
	}
	// disabling hides it at once
	do(t, "PATCH", "/api/portfolio/settings", uid, map[string]any{"enabled": false})
	if w := do(t, "GET", "/api/p/"+fresh.Slug, 0, nil); w.Code != 404 {
		t.Fatalf("disabled: %d", w.Code)
	}
	do(t, "PATCH", "/api/portfolio/settings", uid, map[string]any{"enabled": true})
	if w := do(t, "DELETE", "/api/me", uid, map[string]string{"confirm": "DELETE"}); w.Code != 200 {
		t.Fatalf("delete account: %d %s", w.Code, w.Body)
	}
	if w := do(t, "GET", "/api/p/"+fresh.Slug, 0, nil); w.Code != 404 {
		t.Fatalf("after deletion: %d", w.Code)
	}
}

func TestRLSOwnSettingsOnly(t *testing.T) {
	testdb.Need(t)
	a, _ := newUser(t, "A")
	b, _ := newUser(t, "B")
	sb := settingsOf(t, do(t, "PATCH", "/api/portfolio/settings", b, map[string]any{"enabled": true, "syncConsent": true}))
	do(t, "PUT", "/api/portfolio/items", b, map[string]any{"items": []map[string]any{{"kind": "skill", "title": "B skill"}}})
	err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx(), `select count(*) from portfolio_settings where user_id = $1`, b).Scan(&n); err != nil || n != 0 {
			t.Errorf("A reads B's settings: %d %v", n, err)
		}
		if err := tx.QueryRow(ctx(), `select count(*) from portfolio_items where user_id = $1`, b).Scan(&n); err != nil || n != 0 {
			t.Errorf("A reads B's items: %d %v", n, err)
		}
		tag, err := tx.Exec(ctx(), `update portfolio_settings set enabled = false where user_id = $1`, b)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("A updates B: %v %d", err, tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), `insert into portfolio_items (user_id, kind, title) values ($1, 'skill', 'x')`, b)
		return err
	}); err == nil {
		t.Fatal("A inserted an item for B")
	}
	if w := do(t, "GET", "/api/p/"+sb.Slug, 0, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "B skill") {
		t.Fatalf("B's page: %d %s", w.Code, w.Body)
	}
	// export carries the student's own portfolio sections
	if w := do(t, "GET", "/api/me/export", b, nil); !strings.Contains(w.Body.String(), "portfolioSettings") || !strings.Contains(w.Body.String(), "B skill") {
		t.Fatalf("export: %s", w.Body)
	}
}

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("a", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "1000")
	os.Exit(testdb.Main(m, "admin"))
}

var seq atomic.Int64

func ctx() context.Context { return context.Background() }

func newUser(t *testing.T, role, name string) (id, tg int64) {
	t.Helper()
	tg = 7000 + seq.Add(1)
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, role, consent_at) values ($1, $2, $3::user_role, now()) returning id`,
			tg, name, role).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, tg
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	m := http.NewServeMux()
	Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func TestNonAdminForbidden(t *testing.T) {
	testdb.Need(t)
	student, _ := newUser(t, "student", "Студент")
	amb, _ := newUser(t, "ambassador", "Амбассадор")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/admin/summary"}, {"GET", "/api/admin/users?q=a"}, {"POST", "/api/admin/role"},
	} {
		body := map[string]any{"userId": student, "role": "admin", "reason": "self promotion"}
		if w := do(t, c.method, c.path, 0, body); w.Code != 401 {
			t.Errorf("anonymous %s %s: %d", c.method, c.path, w.Code)
		}
		for _, uid := range []int64{student, amb} {
			if w := do(t, c.method, c.path, uid, body); w.Code != 403 {
				t.Errorf("non-admin %s %s: %d", c.method, c.path, w.Code)
			}
		}
	}
}

func TestRoleChangeAuditedWithReason(t *testing.T) {
	testdb.Need(t)
	admin, _ := newUser(t, "admin", "Админ")
	target, targetTG := newUser(t, "student", "Мақсат")

	if w := do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": target, "role": "ambassador"}); w.Code != 400 {
		t.Fatalf("no reason: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": target, "role": "king", "reason": "valid reason"}); w.Code != 400 {
		t.Fatalf("bad role: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": admin, "role": "student", "reason": "oops mistake"}); w.Code != 400 {
		t.Fatalf("self demotion: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": 999999, "role": "admin", "reason": "missing user"}); w.Code != 404 {
		t.Fatalf("missing user: %d", w.Code)
	}
	w := do(t, "POST", "/api/admin/role", admin, map[string]any{"telegramId": targetTG, "role": "ambassador", "reason": "ЕНУ амбассадоры"})
	if w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body)
	}
	w = do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": target, "role": "student", "reason": "семестр бітті"})
	if w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	type row struct {
		action, reason, from, to string
		actor, target            int64
	}
	var rows []row
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		rs, err := tx.Query(ctx(), `select action, reason, meta->>'from', meta->>'to', actor_id, target_id
			from audit_log where target_id = $1 order by id`, target)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.action, &r.reason, &r.from, &r.to, &r.actor, &r.target); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return rs.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []row{
		{"role_grant", "ЕНУ амбассадоры", "student", "ambassador", admin, target},
		{"role_revoke", "семестр бітті", "ambassador", "student", admin, target},
	}
	if len(rows) != 2 || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("audit rows: %+v", rows)
	}

	// Revoking admin takes effect on the very next request.
	other, _ := newUser(t, "admin", "Екінші админ")
	if w := do(t, "GET", "/api/admin/summary", other, nil); w.Code != 200 {
		t.Fatalf("second admin: %d", w.Code)
	}
	do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": other, "role": "student", "reason": "rotation"})
	if w := do(t, "GET", "/api/admin/summary", other, nil); w.Code != 403 {
		t.Fatalf("revoked admin still allowed: %d", w.Code)
	}
}

func TestSummaryAndSearch(t *testing.T) {
	testdb.Need(t)
	t.Setenv("FEATURE_MARKET", "true")
	admin, _ := newUser(t, "admin", "Бас админ")
	_, tg := newUser(t, "student", "Ерасыл_100%")
	w := do(t, "GET", "/api/admin/summary", admin, nil)
	var s struct {
		Users struct {
			Total       int
			ByRole      map[string]int
			WithConsent int
		}
		ActiveReminders int
		Features        map[string]bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || w.Code != 200 {
		t.Fatalf("summary: %d %s", w.Code, w.Body)
	}
	if s.Users.Total < 2 || s.Users.ByRole["admin"] < 1 || len(s.Users.ByRole) != 5 || !s.Features["market"] || s.Features["events"] {
		t.Fatalf("summary: %+v", s)
	}
	// "%" and "_" are matched literally, not as LIKE wildcards.
	for q, want := range map[string]int{"ерасыл": 1, "100%": 1, "_": 1, "%": 1, "": -1, "zzz-nobody": 0} {
		w := do(t, "GET", "/api/admin/users?q="+url.QueryEscape(q), admin, nil)
		var out struct{ Users []map[string]any }
		json.Unmarshal(w.Body.Bytes(), &out)
		if want >= 0 && len(out.Users) != want {
			t.Errorf("q=%q: %d results, want %d", q, len(out.Users), want)
		}
	}
	w = do(t, "GET", "/api/admin/users?q="+strconv.FormatInt(tg, 10), admin, nil)
	if !strings.Contains(w.Body.String(), "Ерасыл") {
		t.Fatalf("search by telegram id: %s", w.Body)
	}
}

// Email-only accounts (telegram_id null) show up in search (also by address) with
// telegramId null, and roles can be changed by user id.
func TestEmailOnlyUserInAdmin(t *testing.T) {
	testdb.Need(t)
	admin, _ := newUser(t, "admin", "Email админ")
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (email, email_verified_at, name, consent_at)
			values ('dana.admintest@example.kz', now(), 'Дана Email', now()) returning id`).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"dana.admintest", "Дана Email", strconv.FormatInt(id, 10), ""} {
		w := do(t, "GET", "/api/admin/users?q="+url.QueryEscape(q), admin, nil)
		var out struct{ Users []map[string]any }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
			t.Fatalf("q=%q: %d %s", q, w.Code, w.Body)
		}
		found := false
		for _, u := range out.Users {
			if int64(u["id"].(float64)) == id {
				found = true
				if u["telegramId"] != nil || u["email"] != "dana.admintest@example.kz" {
					t.Fatalf("row: %v", u)
				}
			}
		}
		if !found {
			t.Fatalf("q=%q did not find the email-only user: %s", q, w.Body)
		}
	}
	w := do(t, "POST", "/api/admin/role", admin, map[string]any{"userId": id, "role": "ambassador", "reason": "email-only test"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"telegramId":null`) {
		t.Fatalf("role: %d %s", w.Code, w.Body)
	}
	if w := do(t, "GET", "/api/admin/summary", admin, nil); w.Code != 200 {
		t.Fatalf("summary with email-only user: %d", w.Code)
	}
}

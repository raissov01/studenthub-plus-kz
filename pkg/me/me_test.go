package me

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
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("m", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "1000")
	os.Exit(testdb.Main(m, "me"))
}

var tgSeq atomic.Int64

func ctx() context.Context { return context.Background() }

// newUser inserts a user as the server would and returns its id.
func newUser(t *testing.T, consent bool) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, consent_at, city)
			values ($1, $2, case when $3 then now() end, 'Астана') returning id`,
			5000+tgSeq.Add(1), "Студент "+strconv.FormatInt(tgSeq.Load(), 10), consent).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func addReminder(t *testing.T, uid int64, opp string) {
	t.Helper()
	exec(t, `insert into reminders (user_id, opportunity_id, title, deadline) values ($1, $2, 'Грант', current_date + 7)`, uid, opp)
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return tx.QueryRow(ctx(), sql, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		var ver int
		if err := db.Tx(ctx(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx(), "select session_version from users where id = $1", uid).Scan(&ver)
		}); err != nil {
			t.Fatal(err)
		}
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: ver, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	m := http.NewServeMux()
	Routes(m)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func TestRLSIsolatesStudents(t *testing.T) {
	testdb.Need(t)
	a, b := newUser(t, true), newUser(t, true)
	addReminder(t, a, "a1")
	addReminder(t, b, "b1")
	err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		var n int
		// Crafted reads that name B explicitly still see nothing.
		for q, want := range map[string]int{
			"select count(*) from users where id = " + strconv.FormatInt(b, 10):          0,
			"select count(*) from users":                                                 1, // only A's own row
			"select count(*) from reminders where user_id = " + strconv.FormatInt(b, 10): 0,
			"select count(*) from reminders where true or user_id <> app_user_id()":      1,
		} {
			if err := tx.QueryRow(ctx(), q).Scan(&n); err != nil {
				return err
			}
			if n != want {
				t.Errorf("%q: %d rows, want %d", q, n, want)
			}
		}
		tag, err := tx.Exec(ctx(), "update users set name = 'hacked' where id = $1", b)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("update B's user: %v %d", err, tag.RowsAffected())
		}
		tag, err = tx.Exec(ctx(), "update reminders set title = 'hacked' where user_id = $1", b)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("update B's reminder: %v %d", err, tag.RowsAffected())
		}
		tag, err = tx.Exec(ctx(), "delete from reminders where user_id = $1", b)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("delete B's reminder: %v %d", err, tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Writes that would move a row to B or give A admin are refused outright.
	for _, q := range []string{
		"insert into reminders (user_id, opportunity_id, title, deadline) values (" + strconv.FormatInt(b, 10) + ", 'x', 'x', current_date)",
		"update reminders set user_id = " + strconv.FormatInt(b, 10) + " where user_id = app_user_id()",
		"update users set role = 'admin' where id = app_user_id()",
		"update users set telegram_id = 1 where id = app_user_id()",
		"select * from audit_log where actor_id is null",
		"insert into audit_log (action) values ('forged')",
		"delete from users where id = " + strconv.FormatInt(b, 10) + " returning id",
	} {
		err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx(), q)
			if err != nil {
				return err
			}
			n := 0
			for rows.Next() {
				n++
			}
			rows.Close()
			if rows.Err() != nil {
				return rows.Err()
			}
			if n > 0 {
				t.Errorf("%q returned %d rows", q, n)
			}
			return errNoop
		})
		if err == errNoop && !strings.HasPrefix(q, "select") && !strings.HasPrefix(q, "delete") {
			t.Errorf("%q succeeded", q)
		}
	}
	if n := count(t, "select count(*) from users where id = $1 and name like 'Студент%'", b); n != 1 {
		t.Fatal("B's row changed")
	}
	if n := count(t, "select count(*) from reminders where user_id = $1 and title = 'Грант'", b); n != 1 {
		t.Fatal("B's reminder changed")
	}
	if n := count(t, "select count(*) from users where id = $1 and role = 'student'", a); n != 1 {
		t.Fatal("A escalated role")
	}
}

type noopErr struct{}

func (noopErr) Error() string { return "noop" }

var errNoop error = noopErr{}

func TestMeAndConsentRequired(t *testing.T) {
	testdb.Need(t)
	if w := do(t, "GET", "/api/me", 0, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"user":null`) {
		t.Fatalf("anonymous /api/me: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", "/api/me", 0, map[string]string{"name": "x"}); w.Code != 401 {
		t.Fatalf("anonymous patch: %d", w.Code)
	}
	uid := newUser(t, false)
	if w := do(t, "GET", "/api/me", uid, nil); !strings.Contains(w.Body.String(), `"needsConsent":true`) {
		t.Fatalf("needsConsent: %s", w.Body)
	}
	if w := do(t, "PATCH", "/api/me", uid, map[string]string{"name": "Жаңа"}); w.Code != 403 || !strings.Contains(w.Body.String(), "consent_required") {
		t.Fatalf("patch before consent: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/me/consent", uid, map[string]bool{"isAdult": true}); w.Code != 200 || !strings.Contains(w.Body.String(), `"isAdult":true`) {
		t.Fatalf("consent: %d %s", w.Code, w.Body)
	}
	w := do(t, "PATCH", "/api/me", uid, map[string]any{"name": "  Жаңа Аты ", "lang": "ru", "course": 3, "university": "", "faculty": "IT"})
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body)
	}
	var out struct{ User auth.User }
	json.Unmarshal(w.Body.Bytes(), &out)
	if *out.User.Name != "Жаңа Аты" || out.User.Lang != "ru" || *out.User.Course != 3 || out.User.University != nil || *out.User.Faculty != "IT" {
		t.Fatalf("patched user: %+v", out.User)
	}
	for _, bad := range []map[string]any{
		{"gpa": 3.5}, {"phone": "+77001234567"}, {"role": "admin"}, {"course": 9}, {"lang": "en"},
		{"name": strings.Repeat("я", 81)}, {"city": "a\u0000b"}, {},
	} {
		if w := do(t, "PATCH", "/api/me", uid, bad); w.Code != 400 {
			t.Errorf("patch %v: %d", bad, w.Code)
		}
	}
	if n := count(t, "select count(*) from users where id = $1 and role = 'student'", uid); n != 1 {
		t.Fatal("role changed via patch")
	}
}

func TestExportOwnDataOnly(t *testing.T) {
	testdb.Need(t)
	a, b := newUser(t, true), newUser(t, true)
	addReminder(t, a, "mine")
	addReminder(t, b, "theirs")
	exec(t, "insert into bot_daily_sends (user_id, day) values ($1, current_date), ($2, current_date)", a, b)
	exec(t, "insert into bot_events (kind, user_id) values ('submitted', $1), ('submitted', $2)", a, b)
	exec(t, "insert into audit_log (actor_id, action, target_id, reason) values (null, 'role_grant', $1, 'mine'), (null, 'role_grant', $2, 'theirs')", a, b)
	w := do(t, "GET", "/api/me/export", a, nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", w.Code, w.Header())
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"user", "reminders", "botDailySends", "botEvents", "auditLog"} {
		if out[k] == nil {
			t.Errorf("missing section %s", k)
		}
	}
	body := w.Body.String()
	if !strings.Contains(body, `"mine"`) || strings.Contains(body, "theirs") {
		t.Fatalf("export leaks or misses data: %s", body)
	}
	var arr []any
	for _, k := range []string{"reminders", "botDailySends", "botEvents", "auditLog"} {
		json.Unmarshal(out[k], &arr)
		if len(arr) != 1 {
			t.Errorf("%s: %d rows, want 1", k, len(arr))
		}
	}
	var user map[string]any
	json.Unmarshal(out["user"], &user)
	if int64(user["id"].(float64)) != a {
		t.Fatalf("export user id %v", user["id"])
	}
}

// userRefs lists every column with a foreign key to users(id) and its delete rule.
func userRefs(t *testing.T) (refs [][3]string) {
	t.Helper()
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx(), `select cl.relname, a.attname, c.confdeltype::text
			from pg_constraint c
			join pg_class cl on cl.oid = c.conrelid
			join pg_attribute a on a.attrelid = c.conrelid and a.attnum = any(c.conkey)
			where c.contype = 'f' and c.confrelid = 'public.users'::regclass`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r [3]string
			if err := rows.Scan(&r[0], &r[1], &r[2]); err != nil {
				return err
			}
			refs = append(refs, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

// Every table pointing at users must cascade (personal rows) or set null (authorship),
// otherwise account deletion would fail or leave data behind. Future migrations are
// checked automatically.
func TestEveryUserReferenceHasDeleteRule(t *testing.T) {
	testdb.Need(t)
	refs := userRefs(t)
	if len(refs) < 5 {
		t.Fatalf("expected at least 5 references to users, got %v", refs)
	}
	for _, r := range refs {
		if r[2] != "c" && r[2] != "n" {
			t.Errorf("%s.%s references users without on delete cascade / set null (rule %q)", r[0], r[1], r[2])
		}
	}
}

func TestDeleteAccount(t *testing.T) {
	testdb.Need(t)
	uid, other, admin := newUser(t, true), newUser(t, true), newUser(t, true)
	addReminder(t, uid, "r1")
	addReminder(t, uid, "r2")
	addReminder(t, other, "r1")
	exec(t, "insert into bot_daily_sends (user_id, day) values ($1, current_date)", uid)
	exec(t, "insert into bot_events (kind, user_id) values ('registered', $1)", uid)
	exec(t, "insert into audit_log (actor_id, action, target_id, reason) values ($1, 'role_grant', $2, 'test')", admin, uid)
	exec(t, "insert into audit_log (actor_id, action, target_id, reason) values ($1, 'moderation', $2, 'test')", uid, other)
	var hookRan int64
	RegisterDeleteHook(func(_ context.Context, _ pgx.Tx, id int64) error { hookRan = id; return nil })
	t.Cleanup(func() { mu.Lock(); deleteHooks = nil; mu.Unlock() })

	if w := do(t, "DELETE", "/api/me", uid, map[string]string{"confirm": "delete"}); w.Code != 400 {
		t.Fatalf("wrong confirm: %d", w.Code)
	}
	before := count(t, "select count(*) from audit_log where action = 'account_delete'")
	w := do(t, "DELETE", "/api/me", uid, map[string]string{"confirm": "DELETE"})
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	var cleared bool
	for _, c := range w.Result().Cookies() {
		cleared = cleared || (c.Name == auth.CookieName && c.MaxAge < 0)
	}
	if !cleared || hookRan != uid {
		t.Fatalf("cookie cleared %v, hook ran for %d", cleared, hookRan)
	}
	if n := count(t, "select count(*) from users where id = $1", uid); n != 0 {
		t.Fatal("users row survived")
	}
	// Query every table that references users: nothing points at the deleted id.
	for _, r := range userRefs(t) {
		if n := count(t, "select count(*) from "+pgx.Identifier{r[0]}.Sanitize()+" where "+pgx.Identifier{r[1]}.Sanitize()+" = $1", uid); n != 0 {
			t.Errorf("%s.%s still has %d rows for the deleted user", r[0], r[1], n)
		}
	}
	if n := count(t, "select count(*) from reminders where user_id = $1", other); n != 1 {
		t.Fatal("other student's reminder removed")
	}
	if n := count(t, "select count(*) from audit_log where action = 'account_delete'"); n != before+1 {
		t.Fatalf("account_delete audit rows: %d", n)
	}
	if n := count(t, `select count(*) from audit_log where action = 'account_delete'
		and actor_id is null and target_id is null and reason is null and meta = '{}'::jsonb`); n != before+1 {
		t.Fatal("deletion audit entry is not anonymous")
	}
	// The old cookie no longer works.
	if w := do(t, "GET", "/api/me", other, nil); !strings.Contains(w.Body.String(), `"id":`+strconv.FormatInt(other, 10)) {
		t.Fatal("other user's session broken")
	}
	tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
	r := httptest.NewRequest("GET", "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	if auth.Current(r) != nil {
		t.Fatal("deleted user's token still valid")
	}
}

// newEmailUser inserts an email-only account (no Telegram) with a password and a live code.
func newEmailUser(t *testing.T, consent bool) (int64, string) {
	t.Helper()
	email := "me" + strconv.FormatInt(tgSeq.Add(1), 10) + "@example.kz"
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx(), `insert into users (email, email_verified_at, name, consent_at)
			values ($1, now(), 'Email Студент', case when $2 then now() end) returning id`, email, consent).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx(), `insert into user_passwords (user_id, hash) values ($1, '$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA')`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx(), `insert into auth_codes (user_id, purpose, email, code_hash, expires_at)
			values ($1, 'reset_password', $2, '\x00', now() + interval '15 minutes')`, id, email)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, email
}

func TestEmailUserConsentExportDelete(t *testing.T) {
	testdb.Need(t)
	uid, email := newEmailUser(t, false)
	// same consent gate as a Telegram sign-up
	w := do(t, "GET", "/api/me", uid, nil)
	if !strings.Contains(w.Body.String(), `"needsConsent":true`) || !strings.Contains(w.Body.String(), `"telegramId":null`) ||
		!strings.Contains(w.Body.String(), `"email":"`+email+`"`) || !strings.Contains(w.Body.String(), `"emailVerified":true`) {
		t.Fatalf("/api/me: %s", w.Body)
	}
	if w := do(t, "PATCH", "/api/me", uid, map[string]string{"name": "x"}); w.Code != 403 {
		t.Fatalf("patch before consent: %d", w.Code)
	}
	if w := do(t, "POST", "/api/me/consent", uid, map[string]bool{"isAdult": true}); w.Code != 200 {
		t.Fatalf("consent: %d %s", w.Code, w.Body)
	}
	// export: the address is there, the password hash and codes are not
	w = do(t, "GET", "/api/me/export", uid, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), email) || strings.Contains(w.Body.String(), "argon2") || strings.Contains(w.Body.String(), "code_hash") {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	if w := do(t, "DELETE", "/api/me", uid, map[string]string{"confirm": "DELETE"}); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	for _, q := range []string{
		"select count(*) from users where id = $1",
		"select count(*) from user_passwords where user_id = $1",
		"select count(*) from auth_codes where user_id = $1",
	} {
		if n := count(t, q, uid); n != 0 {
			t.Errorf("%s: %d rows left", q, n)
		}
	}
	if n := count(t, "select count(*) from users where email = $1", email); n != 0 {
		t.Fatal("address survived deletion")
	}
}

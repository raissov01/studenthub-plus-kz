package discounts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("d", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "1000")
	for _, k := range []string{"SHEET_OPPORTUNITIES_CSV", "SHEET_JOBS_CSV", "SHEET_DISCOUNTS_CSV"} {
		os.Unsetenv(k) // data/discounts.json: d1, d2, ...
	}
	content.Default.DataDir = filepath.Join("..", "..", "data") // tests run in pkg/discounts
	os.Exit(testdb.Main(m, "discounts"))
}

var tgSeq atomic.Int64

func ctx() context.Context { return context.Background() }

func newUser(t *testing.T, consent bool) int64 {
	t.Helper()
	var id int64
	err := db.Tx(ctx(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx(), `insert into users (telegram_id, name, consent_at) values ($1, 'D', case when $2 then now() end) returning id`,
			770000+tgSeq.Add(1), consent).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
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
	var m struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &m)
	return m.Error.Code
}

func do(t *testing.T, method, path string, uid int64, body any) resp {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	if uid != 0 {
		var ver int
		_ = db.Tx(ctx(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx(), "select session_version from users where id = $1", uid).Scan(&ver)
		})
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
	return resp{w}
}

func vote(t *testing.T, uid int64, id string, worked bool) resp {
	t.Helper()
	return do(t, "POST", "/api/discounts/"+id+"/check", uid, map[string]bool{"worked": worked})
}

func stats(t *testing.T) map[string]any {
	t.Helper()
	w := do(t, "GET", "/api/discounts/checks", 0, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, s-maxage=300" {
		t.Fatalf("checks: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	return w.json(t)["checks"].(map[string]any)
}

func TestKnownFromContent(t *testing.T) {
	if !Known(ctx(), "d1") || Known(ctx(), "nope") {
		t.Fatal("discount ids not read from data/discounts.json")
	}
}

func TestVoteOncePer30DaysAndCounts(t *testing.T) {
	testdb.Need(t)
	a, b, c := newUser(t, true), newUser(t, true), newUser(t, true)
	w := vote(t, a, "d1", true)
	if w.Code != 201 {
		t.Fatalf("vote: %d %s", w.Code, w.Body)
	}
	st := w.json(t)["stats"].(map[string]any)
	if st["worked"] != float64(1) || st["notWorked"] != float64(0) || st["lastConfirmedAt"] == nil {
		t.Fatalf("stats after vote: %v", st)
	}
	w = vote(t, a, "d1", false)
	if w.Code != 409 || w.code() != "already_checked" {
		t.Fatalf("second vote: %d %s", w.Code, w.Body)
	}
	if _, ok := w.json(t)["error"].(map[string]any)["nextAllowedAt"]; !ok {
		t.Fatal("no nextAllowedAt")
	}
	if w := vote(t, a, "d2", false); w.Code != 201 {
		t.Fatalf("other discount: %d", w.Code)
	}
	vote(t, b, "d1", false)
	vote(t, c, "d1", true)

	all := stats(t)
	d1 := all["d1"].(map[string]any)
	if d1["worked"] != float64(2) || d1["notWorked"] != float64(1) {
		t.Fatalf("d1: %v", d1)
	}
	if d2 := all["d2"].(map[string]any); d2["worked"] != float64(0) || d2["notWorked"] != float64(1) || d2["lastConfirmedAt"] != nil {
		t.Fatalf("d2: %v", d2)
	}

	// 31 days later the student may vote again
	exec(t, "update discount_checks set created_at = now() - interval '31 days' where user_id = $1 and discount_id = 'd1'", a)
	if w := vote(t, a, "d1", false); w.Code != 201 {
		t.Fatalf("vote after 30 days: %d %s", w.Code, w.Body)
	}
	// votes older than 60 days leave the counts, the last confirmation stays
	exec(t, "update discount_checks set created_at = now() - interval '61 days' where discount_id = 'd1' and worked")
	d1 = stats(t)["d1"].(map[string]any)
	if d1["worked"] != float64(0) || d1["notWorked"] != float64(2) || d1["lastConfirmedAt"] == nil {
		t.Fatalf("d1 after 60 days: %v", d1)
	}
}

func TestCheckErrors(t *testing.T) {
	testdb.Need(t)
	uid := newUser(t, true)
	if w := vote(t, uid, "nope", true); w.Code != 404 || w.code() != "not_found" {
		t.Fatalf("unknown id: %d %s", w.Code, w.Body)
	}
	if w := vote(t, uid, "bad%20id", true); w.Code != 404 {
		t.Fatalf("bad id: %d", w.Code)
	}
	if w := do(t, "POST", "/api/discounts/d1/check", uid, map[string]any{}); w.Code != 400 {
		t.Fatalf("missing worked: %d", w.Code)
	}
	if w := vote(t, 0, "d1", true); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := vote(t, newUser(t, false), "d1", true); w.Code != 403 || w.code() != "consent_required" {
		t.Fatalf("no consent: %d %s", w.Code, w.Body)
	}
	t.Setenv("DISCOUNT_CHECKS_PER_HOUR", "2")
	busy := newUser(t, true)
	codes := []int{vote(t, busy, "d1", true).Code, vote(t, busy, "d2", true).Code, vote(t, busy, "d3", true).Code}
	if codes[0] != 201 || codes[1] != 201 || codes[2] != 429 {
		t.Fatalf("rate limit: %v", codes)
	}
}

func TestRLSAndDeletionCascade(t *testing.T) {
	testdb.Need(t)
	a, b := newUser(t, true), newUser(t, true)
	vote(t, a, "d1", true)
	vote(t, b, "d1", true)
	err := db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx(), "select count(*) from discount_checks").Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Errorf("a sees %d rows, want only their own", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithUser(ctx(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx(), "insert into discount_checks (discount_id, user_id, worked) values ('d2', $1, true)", b)
		return err
	})
	if err == nil {
		t.Fatal("a voted as b")
	}
	if err := me.Delete(ctx(), a); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "select count(*) from discount_checks where user_id = $1", a); n != 0 {
		t.Fatalf("votes survived deletion: %d", n)
	}
	if n := count(t, "select count(*) from discount_checks where user_id = $1", b); n != 1 {
		t.Fatal("other student's vote deleted")
	}
}

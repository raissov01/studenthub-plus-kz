package fairs

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
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

type sentMsg struct {
	user      int64
	key, text string
}

var (
	outMu sync.Mutex
	out   []sentMsg
	seq   atomic.Int64
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("f", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("FEATURE_EVENTS", "true")
	os.Setenv("FEATURE_FAIRS", "true")
	os.Setenv("CRON_SECRET", "cron-secret")
	os.Setenv("ALLOWED_HOSTS", "localhost")
	seen := map[string]bool{}
	Send = func(_ context.Context, userID int64, key, text, _ string) error {
		outMu.Lock()
		defer outMu.Unlock()
		if !seen[key] { // the dispatcher's delivery key: a rerun is a no-op
			seen[key] = true
			out = append(out, sentMsg{userID, key, text})
		}
		return nil
	}
	os.Exit(testdb.Main(m, "fairs"))
}

func bg() context.Context { return context.Background() }

func q(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := db.Tx(bg(), func(tx pgx.Tx) error { return tx.QueryRow(bg(), sql, args...).Scan(&id) }); err != nil {
		t.Fatal(err)
	}
	return id
}

func user(t *testing.T, role, name string) int64 {
	t.Helper()
	testdb.Need(t)
	n := seq.Add(1)
	return q(t, `insert into users (telegram_id, name, role, consent_at, university, faculty, course, tg_username, bot_inactive)
		values ($1, $2, $3, now(), 'KATU', 'IT', 2, $4, true) returning id`, 770000+n, name, role, fmt.Sprintf("tg%d", n))
}

func employer(t *testing.T, owner int64, name, bin string) int64 {
	t.Helper()
	id := q(t, `insert into employers (owner_user_id, name, bin, website, contact_name, status) values ($1, $2, $3, 'https://example.kz', 'HR', 'verified') returning id`, owner, name, bin)
	q(t, `insert into employer_members (employer_id, user_id, role) values ($1, $2, 'owner') returning employer_id`, id, owner)
	return id
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "http://localhost")
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	m := http.NewServeMux()
	Routes(m)
	m.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", w.Body, err)
	}
	return v
}

func errCode(w *httptest.ResponseRecorder) string {
	var v struct{ Error struct{ Code string } }
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return v.Error.Code
}

func TestFairBooths(t *testing.T) {
	org := user(t, "ambassador", "Ұйымдастырушы Тест")
	stud := user(t, "student", "Аружан Тестова")
	quiet := user(t, "student", "Дана Жасырын")
	ownerA := user(t, "student", "A HR")
	ownerB := user(t, "student", "B HR")
	empA := employer(t, ownerA, "Alpha Agro", "111111111111")
	empB := employer(t, ownerB, "Beta Bank", "222222222222")
	start := time.Now().Add(-time.Hour)
	ev := q(t, `insert into events (title_kk, cat, starts_at, ends_at, university, status) values ('Мансап жәрмеңкесі', 'career', $1, $2, 'KATU', 'published') returning id`,
		start, start.Add(3*time.Hour))
	other := q(t, `insert into events (title_kk, cat, starts_at, ends_at, university, status) values ('Спорт күні', 'sport', $1, $2, 'KATU', 'published') returning id`,
		start, start.Add(3*time.Hour))

	// organizer adds the two companies; students and outsiders cannot, and a sport event takes none
	if w := do(t, "POST", fmt.Sprintf("/api/events/%d/exhibitors", ev), stud, map[string]string{"bin": "111111111111"}); w.Code != 403 {
		t.Fatalf("student added an exhibitor: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/events/%d/exhibitors", other), org, map[string]string{"bin": "111111111111"}); w.Code != 400 || errCode(w) != "not_career" {
		t.Fatalf("sport event: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/events/%d/exhibitors", ev), org, map[string]string{"bin": "999999999999"}); w.Code != 404 {
		t.Fatalf("unknown BIN: %d", w.Code)
	}
	do(t, "POST", fmt.Sprintf("/api/events/%d/exhibitors", ev), org, map[string]string{"bin": "111111111111", "booth": "A1"})
	w := do(t, "POST", fmt.Sprintf("/api/events/%d/exhibitors", ev), org, map[string]string{"bin": "222222222222", "booth": "B2"})
	list := decode[struct{ Exhibitors []Exhibitor }](t, w).Exhibitors
	if w.Code != 200 || len(list) != 2 || list[0].Token == nil || list[0].Booth != "A1" {
		t.Fatalf("exhibitors: %d %s", w.Code, w.Body)
	}
	tokA, tokB := *list[0].Token, *list[1].Token
	if len(tokA) > 64 || !strings.HasPrefix(tokA, TokenPrefix) {
		t.Fatalf("token %q", tokA)
	}
	// the public list has no tokens
	pub := do(t, "GET", fmt.Sprintf("/api/events/%d/exhibitors", ev), 0, nil)
	if pub.Code != 200 || strings.Contains(pub.Body.String(), "token") {
		t.Fatalf("public list: %d %s", pub.Code, pub.Body)
	}

	// 1. scan booth A twice: one row, then "already"
	r1 := do(t, "POST", "/api/events/booth-checkin", stud, map[string]any{"token": tokA})
	if r1.Code != 200 || decode[BoothResult](t, r1).Status != "ok" || decode[BoothResult](t, r1).Company != "Alpha Agro" {
		t.Fatalf("first scan: %d %s", r1.Code, r1.Body)
	}
	r2 := do(t, "POST", "/api/events/booth-checkin", stud, map[string]any{"token": tokA, "share": []string{"name", "telegram"}})
	if res := decode[BoothResult](t, r2); r2.Code != 200 || res.Status != "already" || len(res.Shared) != 2 {
		t.Fatalf("second scan: %d %s", r2.Code, r2.Body)
	}
	if n := q(t, `select count(*) from booth_checkins where user_id = $1`, stud); n != 1 {
		t.Fatalf("rows after two scans: %d", n)
	}
	do(t, "POST", "/api/events/booth-checkin", stud, map[string]any{"token": tokB})
	do(t, "POST", "/api/events/booth-checkin", quiet, map[string]any{"token": tokA})
	// a forged token (right ids, wrong MAC) is refused
	forged := tokA[:len(tokA)-2] + "AA"
	if forged == tokA {
		forged = tokA[:len(tokA)-2] + "BB"
	}
	if w := do(t, "POST", "/api/events/booth-checkin", stud, map[string]any{"token": forged}); w.Code != 404 {
		t.Fatalf("forged token: %d", w.Code)
	}
	if w := do(t, "POST", "/api/events/booth-checkin", stud, map[string]any{"token": tokA, "share": []string{"phone"}}); w.Code != 400 {
		t.Fatalf("unknown field: %d", w.Code)
	}
	mine := decode[struct{ Booths []MyBooth }](t, do(t, "GET", fmt.Sprintf("/api/events/%d/my-booths", ev), stud, nil)).Booths
	if len(mine) != 2 || mine[0].Company != "Alpha Agro" || mine[1].Company != "Beta Bank" {
		t.Fatalf("my booths: %+v", mine)
	}

	// 3. the company sees only shared fields; the silent visitor is a count
	if w := do(t, "GET", fmt.Sprintf("/api/employer/fairs/%d/checkins", ev), stud, nil); w.Code != 403 {
		t.Fatalf("student read employer list: %d", w.Code)
	}
	fa := decode[struct{ Fairs []Fair }](t, do(t, "GET", "/api/employer/fairs", ownerA, nil)).Fairs
	if len(fa) != 1 || fa[0].Token != tokA || fa[0].Checkins != 2 {
		t.Fatalf("employer fairs: %+v", fa)
	}
	ck := decode[struct {
		Total, Anonymous int
		Visitors         []Visitor
	}](t, do(t, "GET", fmt.Sprintf("/api/employer/fairs/%d/checkins", ev), ownerA, nil))
	if ck.Total != 2 || ck.Anonymous != 1 || len(ck.Visitors) != 1 {
		t.Fatalf("checkins: %+v", ck)
	}
	v := ck.Visitors[0]
	if v.Name == nil || *v.Name != "Аружан Тестова" || v.Telegram == nil || v.Course != nil || v.Faculty != nil {
		t.Fatalf("visitor fields: %+v", v)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/employer/fairs/%d/checkins", ev), ownerB, nil); !strings.Contains(w.Body.String(), `"anonymous":1`) {
		t.Fatalf("B sees: %s", w.Body) // the student shared nothing with B
	}

	// 4. a scan after ends_at + 1 h is refused with a clear code; before start − 30 min too
	prev := Now
	Now = func() time.Time { return start.Add(4*time.Hour + time.Minute) }
	if w := do(t, "POST", "/api/events/booth-checkin", quiet, map[string]any{"token": tokB}); w.Code != 409 || errCode(w) != "fair_over" {
		t.Fatalf("late scan: %d %s", w.Code, w.Body)
	}
	Now = func() time.Time { return start.Add(-31 * time.Minute) }
	if w := do(t, "POST", "/api/events/booth-checkin", quiet, map[string]any{"token": tokB}); w.Code != 409 || errCode(w) != "fair_not_started" {
		t.Fatalf("early scan: %d %s", w.Code, w.Body)
	}
	Now = prev

	// 2. the morning after: one student message listing A and B, one message per company
	nextDay := start.Add(3*time.Hour).Add(24 * time.Hour)
	for range 2 { // a rerun sends nothing new
		if _, err := Summaries(bg(), nextDay); err != nil {
			t.Fatal(err)
		}
	}
	outMu.Lock()
	defer outMu.Unlock()
	var toStud, toQuiet, toA, toB int
	for _, m := range out {
		switch m.user {
		case stud:
			toStud++
			if !strings.Contains(m.text, "Alpha Agro") || !strings.Contains(m.text, "Beta Bank") {
				t.Fatalf("student summary: %s", m.text)
			}
		case quiet:
			toQuiet++
		case ownerA:
			toA++
			if !strings.Contains(m.text, "2") {
				t.Fatalf("A summary: %s", m.text)
			}
		case ownerB:
			toB++
		}
	}
	if toStud != 1 || toQuiet != 1 || toA != 1 || toB != 1 {
		t.Fatalf("summaries: student %d, quiet %d, A %d, B %d", toStud, toQuiet, toA, toB)
	}

	// removing the company voids its printed QR
	do(t, "DELETE", fmt.Sprintf("/api/events/%d/exhibitors/%d", ev, empA), org, nil)
	if w := do(t, "POST", "/api/events/booth-checkin", stud, map[string]any{"token": tokA}); w.Code != 404 {
		t.Fatalf("removed booth: %d", w.Code)
	}
	_ = empB
}

func TestSwitchOff(t *testing.T) {
	t.Setenv("FEATURE_FAIRS", "false")
	if w := do(t, "GET", "/api/events/1/exhibitors", 0, nil); w.Code != 404 {
		t.Fatalf("switch off: %d", w.Code)
	}
}

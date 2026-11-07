package wellbeing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/server"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Main(m, "wellbeing")) }

func call(t *testing.T, method, path string, body any, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
	r.Header.Set("Origin", "http://localhost")
	if uid != 0 {
		tok, _ := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func user(t *testing.T, role, univ string) int64 {
	var id int64
	err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), `insert into users (telegram_id, name, role, university, consent_at, bot_inactive)
			values ((select coalesce(max(telegram_id), 90000) + 1 from users), 'W', $1, $2, now(), true) returning id`, role, univ).Scan(&id); err != nil {
			return err
		}
		if role == "university_staff" {
			_, err := tx.Exec(context.Background(), `insert into university_staff (user_id, university, granted_by) values ($1, $2, $1)`, id, univ)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestContactsEmptyUntilFilledAndScoped(t *testing.T) {
	testdb.Need(t)
	t.Setenv("FEATURE_WELLBEING", "true")
	t.Setenv("ALLOWED_HOSTS", "localhost")
	t.Setenv("SESSION_SECRET", strings.Repeat("wellbeing-test-secret", 3))
	if w := call(t, "GET", "/api/wellbeing/contacts?university=W-A", nil, 0); w.Code != 200 || !strings.Contains(w.Body.String(), `"contacts":[]`) {
		t.Fatalf("empty state: %d %s", w.Code, w.Body)
	}
	staff, stud := user(t, "university_staff", "W-A"), user(t, "student", "W-A")
	list := map[string]any{"university": "W-B", "contacts": []map[string]any{{"name": "Психолог кабинеті", "phone": "+7 7172 00-00-00", "place": "Бас корпус, 101", "hours": "Дс–Жм 9:00–17:00"}}}
	if w := call(t, "PUT", "/api/staff/wellbeing/contacts", list, stud); w.Code != 403 {
		t.Fatalf("student edit: %d", w.Code)
	}
	w := call(t, "PUT", "/api/staff/wellbeing/contacts", list, staff)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"university":"W-A"`) {
		t.Fatalf("staff edit is bound to own university: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(call(t, "GET", "/api/wellbeing/contacts?university=W-A", nil, 0).Body.String(), "Психолог кабинеті") {
		t.Fatal("contact not listed")
	}
	bad := map[string]any{"contacts": []map[string]any{{"name": "No way to reach"}}}
	if w := call(t, "PUT", "/api/staff/wellbeing/contacts", bad, staff); w.Code != 400 {
		t.Fatalf("contact without phone/email/place: %d", w.Code)
	}
	// an address the pattern allows but the column (≤ 120) does not was a 500
	long := map[string]any{"contacts": []map[string]any{{"name": "Психолог", "email": strings.Repeat("a", 64) + "@" + strings.Repeat("b", 90) + ".kz"}}}
	if w := call(t, "PUT", "/api/staff/wellbeing/contacts", long, staff); w.Code != 400 {
		t.Fatalf("157-character email: %d %s", w.Code, w.Body)
	}
}

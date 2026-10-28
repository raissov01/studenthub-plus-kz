package quiz

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
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
	os.Setenv("SESSION_SECRET", strings.Repeat("q", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("FEATURE_QUIZ", "true")
	os.Setenv("QUIZ_LIVE_JOINS_PER_MIN", "1000")
	os.Setenv("QUIZ_LIVE_WS_PER_MIN", "1000")
	dir, _ := os.MkdirTemp("", "quiz-upload-")
	os.Setenv("UPLOAD_DIR", dir)
	code := testdb.Main(m, "quiz")
	os.RemoveAll(dir)
	os.Exit(code)
}

var seq atomic.Int64

func ctxb() context.Context { return context.Background() }

// newUser inserts a consented student and returns the id.
func newUser(t *testing.T) int64 {
	t.Helper()
	testdb.Need(t)
	var id int64
	n := seq.Add(1)
	err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctxb(), `insert into users (telegram_id, name, consent_at, university)
			values ($1, $2, now(), 'TEST') returning id`, 700000+n, "Айгерім Студентова "+strconv.FormatInt(n, 10)).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func userName(t *testing.T, uid int64) string {
	var s string
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctxb(), "select name from users where id = $1", uid).Scan(&s)
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error { return tx.QueryRow(ctxb(), sql, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func handler() http.Handler {
	m := http.NewServeMux()
	Routes(m)
	return m
}

func sign(t *testing.T, r *http.Request, uid int64) {
	t.Helper()
	if uid == 0 {
		return
	}
	var ver int
	if err := db.Tx(ctxb(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctxb(), "select session_version from users where id = $1", uid).Scan(&ver)
	}); err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Sign(auth.Claims{UserID: uid, Version: ver, Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader = bytes.NewReader(nil)
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	sign(t, r, uid)
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
	return w
}

type part struct {
	field, name string
	data        []byte
}

func doMultipart(t *testing.T, path string, uid int64, parts []part) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p.name != "" {
			w, err = mw.CreateFormFile(p.field, p.name)
		} else {
			w, err = mw.CreateFormField(p.field)
		}
		if err != nil {
			t.Fatal(err)
		}
		w.Write(p.data)
	}
	mw.Close()
	r := httptest.NewRequest("POST", path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
	sign(t, r, uid)
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
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
	var v struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	return v.Error.Code
}

// sampleQuiz has one question of each simple type.
func sampleQuiz(vis string) map[string]any {
	return map[string]any{
		"title": "Биология негіздері", "visibility": vis, "timePerQuestion": 20,
		"questions": []map[string]any{
			{"type": "mcq", "prompt": "2+2?", "options": []string{"3", "4", "5"}, "correctIndex": 1, "explanation": "Арифметика"},
			{"type": "true_false", "prompt": "Жер дөңгелек", "correctBool": true},
			{"type": "fill_blank", "prompt": "H2O — ...", "answers": []string{"су", "water"}},
			{"type": "mcq_multi", "prompt": "Жай сандар", "options": []string{"2", "4", "7"}, "correctIndexes": []int{0, 2}},
			{"type": "poll", "prompt": "Ұнады ма?", "options": []string{"иә", "жоқ"}},
		},
	}
}

func createQuizT(t *testing.T, uid int64, body any) (int64, map[string]any) {
	t.Helper()
	w := do(t, "POST", "/api/quiz/quizzes", uid, body)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	v := decode[struct {
		ID   int64          `json:"id"`
		Quiz map[string]any `json:"quiz"`
	}](t, w)
	return v.ID, v.Quiz
}

type playResp struct {
	Quiz      Meta           `json:"quiz"`
	Questions []PlayQuestion `json:"questions"`
}

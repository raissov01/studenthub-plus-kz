package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

const day = 24 * time.Hour

func needMux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /need", Need(func(w http.ResponseWriter, r *http.Request, u *User) { w.Write([]byte("ok")) }))
	return m
}

// callNeed sends tok as a cookie (or a Bearer header) to a Need-guarded endpoint.
func callNeed(tok string, bearer bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/need", nil)
	if bearer {
		r.Header.Set("Authorization", "Bearer "+tok)
	} else {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	needMux().ServeHTTP(w, r)
	return w
}

func signedAt(t *testing.T, uid int64, version int, issued, auth time.Time, ttl time.Duration) string {
	t.Helper()
	tok, err := Sign(Claims{UserID: uid, Version: version, Issued: issued.Unix(), Auth: auth.Unix(), Exp: issued.Add(ttl).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// signLegacy builds a token the way it was signed before "i"/"a" existed.
func signLegacy(t *testing.T, uid int64, exp time.Time) string {
	t.Helper()
	key, err := secret()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]int64{"u": uid, "v": 1, "e": exp.Unix()})
	p := base64.RawURLEncoding.EncodeToString(body)
	return p + "." + base64.RawURLEncoding.EncodeToString(hmacSHA256(key, []byte(p)))
}

func refreshed(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	return nil
}

func TestSessionLifetimeDependsOnRole(t *testing.T) {
	testdb.Need(t)
	w, out := loginWebApp(t, 7000+ipSeq.Add(1))
	c := sessionCookie(t, w)
	if c.MaxAge != int(StudentSessionTTL.Seconds()) {
		t.Fatalf("student cookie max-age %d", c.MaxAge)
	}
	cl, err := Parse(c.Value, time.Now())
	if err != nil || cl.Issued == 0 || cl.Auth != cl.Issued || time.Until(time.Unix(cl.Exp, 0)) < 89*day {
		t.Fatalf("student claims %+v %v", cl, err)
	}
	if out["token"] == nil {
		t.Fatal("mini app token missing")
	}

	tg := 7000 + ipSeq.Add(1)
	t.Setenv("ADMIN_TELEGRAM_IDS", strconv.FormatInt(tg, 10))
	w, _ = loginWebApp(t, tg)
	c = sessionCookie(t, w)
	if c.MaxAge != int(StaffSessionTTL.Seconds()) {
		t.Fatalf("admin cookie max-age %d", c.MaxAge)
	}
}

func TestSlidingSessionRefreshesActiveStudent(t *testing.T) {
	testdb.Need(t)
	_, out := loginWebApp(t, 7100+ipSeq.Add(1))
	uid := int64(out["user"].(map[string]any)["id"].(float64))
	now := time.Now()

	// Younger than half the lifetime: no new cookie.
	young := signedAt(t, uid, 1, now.Add(-10*day), now.Add(-10*day), StudentSessionTTL)
	w := callNeed(young, false)
	if w.Code != 200 || refreshed(t, w) != nil || w.Header().Get(TokenHeader) != "" {
		t.Fatalf("young session refreshed: %d %v", w.Code, w.Result().Cookies())
	}

	// Older than half: a fresh 90-day cookie, same user and version, original sign-in kept.
	signIn := now.Add(-200 * day)
	old := signedAt(t, uid, 1, now.Add(-50*day), signIn, StudentSessionTTL)
	w = callNeed(old, false)
	nc := refreshed(t, w)
	if w.Code != 200 || nc == nil {
		t.Fatalf("old session not refreshed: %d", w.Code)
	}
	cl, err := Parse(nc.Value, time.Now())
	if err != nil || cl.UserID != uid || cl.Version != 1 || cl.Auth != signIn.Unix() ||
		time.Until(time.Unix(cl.Exp, 0)) < 89*day || nc.MaxAge != int(StudentSessionTTL.Seconds()) || !nc.HttpOnly {
		t.Fatalf("refreshed claims %+v %v %+v", cl, err, nc)
	}
	if w.Header().Get(TokenHeader) != "" {
		t.Fatal("cookie session also got a header token")
	}
	// The refreshed token is young: the next request does not refresh again (once a day at most).
	if w2 := callNeed(nc.Value, false); w2.Code != 200 || refreshed(t, w2) != nil {
		t.Fatal("refreshed twice")
	}

	// Mini App (Bearer): the fresh token comes back in X-Session-Token, no cookie is set.
	w = callNeed(old, true)
	if w.Code != 200 || refreshed(t, w) != nil {
		t.Fatalf("bearer refresh: %d %v", w.Code, w.Result().Cookies())
	}
	ht := w.Header().Get(TokenHeader)
	if cl, err := Parse(ht, time.Now()); err != nil || cl.UserID != uid || time.Until(time.Unix(cl.Exp, 0)) < 89*day {
		t.Fatalf("header token %q %+v %v", ht, cl, err)
	}

	// session_version semantics survive: "log out everywhere" ends refreshed tokens too.
	exec(t, "update users set session_version = session_version + 1 where id = $1", uid)
	if w := callNeed(ht, true); w.Code != 401 {
		t.Fatalf("refreshed token outlived log-out-everywhere: %d", w.Code)
	}
}

func TestStaffSessionIsAbsoluteFourteenDays(t *testing.T) {
	testdb.Need(t)
	_, out := loginWebApp(t, 7200+ipSeq.Add(1))
	uid := int64(out["user"].(map[string]any)["id"].(float64))
	now := time.Now()
	for _, role := range []string{"admin", "university_staff", "employer"} {
		exec(t, "update users set role = $2::user_role where id = $1", uid, role)
		fresh := signedAt(t, uid, 1, now.Add(-13*day), now.Add(-13*day), StaffSessionTTL)
		w := callNeed(fresh, false)
		if w.Code != 200 || refreshed(t, w) != nil {
			t.Fatalf("%s: 13-day session %d / refreshed", role, w.Code)
		}
		// A token from the student days (or the old 30-day rule) is not a way around 14 days.
		stale := signedAt(t, uid, 1, now.Add(-15*day), now.Add(-15*day), StudentSessionTTL)
		if w := callNeed(stale, false); w.Code != 401 {
			t.Fatalf("%s: 15-day-old sign-in accepted: %d", role, w.Code)
		}
		legacy := signLegacy(t, uid, now.Add(10*day)) // issued 20 days ago under the 30-day rule
		if w := callNeed(legacy, false); w.Code != 401 {
			t.Fatalf("%s: legacy 20-day-old token accepted: %d", role, w.Code)
		}
	}
	exec(t, "update users set role = 'student' where id = $1", uid)
	legacy := signLegacy(t, uid, now.Add(10*day))
	if w := callNeed(legacy, false); w.Code != 200 {
		t.Fatalf("legacy student token rejected: %d", w.Code)
	}
}

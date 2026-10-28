package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func allFeatures(t *testing.T) {
	t.Helper()
	for _, f := range features.Names {
		t.Setenv("FEATURE_"+strings.ToUpper(f), "true")
	}
	t.Setenv("ALLOWED_HOSTS", "localhost")
	t.Setenv("SESSION_SECRET", strings.Repeat("launch-test-secret", 3))
	t.Setenv("RATE_WRITE_PER_MIN", "10000")
}

var wildcard = regexp.MustCompile(`\{[^}]+\}`)

func requestRoute(route Route) *http.Request {
	method, path, ok := strings.Cut(route.Pattern, " ")
	if !ok {
		method, path = "GET", route.Pattern
	}
	return httptest.NewRequest(method, wildcard.ReplaceAllString(path, "1"), strings.NewReader(`{}`))
}
func TestSecurityDocumentMatchesRouter(t *testing.T) {
	doc, err := SecurityDocument()
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile("../../docs/SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc != string(disk) {
		t.Fatal("route audit drift: run go run ./cmd/routes > docs/SECURITY.md")
	}
}

// Every write is exercised against the real router. A guard must reject a hostile origin
// AND an exhausted shared write budget; implementing just one check cannot pass this test.
func TestEveryMutationHasWriteGuard(t *testing.T) {
	allFeatures(t)
	t.Setenv("RATE_WRITE_PER_MIN", "0")
	exceptions := map[string]bool{"POST /api/telegram": true}
	seen := map[string]bool{}
	for i, route := range Inventory() {
		method, _, explicit := strings.Cut(route.Pattern, " ")
		if !explicit && route.Pattern != "/api/" {
			t.Errorf("route must declare its HTTP method: %s", route.Pattern)
		}
		if method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
			continue
		}
		if exceptions[route.Pattern] {
			seen[route.Pattern] = true
			continue
		}
		t.Run(route.Pattern, func(t *testing.T) {
			r := requestRoute(route)
			r.Header.Set("Origin", "https://hostile.invalid")
			w := httptest.NewRecorder()
			Handler().ServeHTTP(w, r)
			if w.Code != 403 || !strings.Contains(w.Body.String(), "bad_origin") {
				t.Fatalf("missing origin guard: %d %s", w.Code, w.Body)
			}
			ip := fmt.Sprintf("198.18.0.%d", i+1)
			r = requestRoute(route)
			r.Header.Set("Origin", "http://localhost")
			r.Header.Set("X-Real-IP", ip)
			w = httptest.NewRecorder()
			Handler().ServeHTTP(w, r)
			if w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limited") {
				t.Fatalf("missing shared rate guard: %d %s", w.Code, w.Body)
			}
		})
	}
	for p := range exceptions {
		if !seen[p] {
			t.Errorf("obsolete write exception %s", p)
		}
	}
}

func TestRouterAuthenticationAndSwitches(t *testing.T) {
	allFeatures(t)
	for i, route := range Inventory() {
		if route.Policy.Callers == "anonymous" || strings.HasPrefix(route.Policy.Callers, "anonymous (") || strings.Contains(route.Policy.Callers, "secret") || strings.HasPrefix(route.Policy.Callers, "cron") {
			continue
		}
		t.Run(route.Pattern, func(t *testing.T) {
			r := requestRoute(route)
			r.Header.Set("Origin", "http://localhost")
			r.Header.Set("X-Real-IP", fmt.Sprintf("198.19.0.%d", i+1))
			w := httptest.NewRecorder()
			Handler().ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("anonymous: %d %s", w.Code, w.Body)
			}
		})
	}
	for _, f := range features.Names {
		t.Setenv("FEATURE_"+strings.ToUpper(f), "false")
	}
	for _, route := range Inventory() {
		if route.Policy.Feature == "—" {
			continue
		}
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, requestRoute(route))
		if w.Code != 404 {
			t.Errorf("disabled %s: %d", route.Pattern, w.Code)
		}
	}
}

func launchUser(t *testing.T, role string) int64 {
	t.Helper()
	testdb.Need(t)
	var id int64
	err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `insert into users (telegram_id,name,role,university,consent_at,is_adult,bot_inactive)
   values ((select coalesce(max(telegram_id),10000)+1 from users),'Launch fixture',$1,'LAUNCH',now(),true,true) returning id`, role).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func launchRequest(t *testing.T, method, path, body string, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost")
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, r)
	return w
}
func TestEveryAdminRouteRejectsOtherRoles(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	for _, role := range []string{"student", "ambassador", "university_staff", "employer"} {
		id := launchUser(t, role)
		for _, route := range Inventory() {
			if route.Policy.Callers != "admin" {
				continue
			}
			method, path, _ := strings.Cut(route.Pattern, " ")
			w := launchRequest(t, method, wildcard.ReplaceAllString(path, "1"), `{}`, id)
			if w.Code != 403 {
				t.Errorf("%s %s: %d %s", role, route.Pattern, w.Code, w.Body)
			}
		}
	}
}

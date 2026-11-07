package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

// consoleRequest is launchRequest from its own client IP: these tests make many writes, and the shared per-IP
// write budget (Postgres counters) must stay free for the other tests of the package.
func consoleRequest(t *testing.T, method, path, body string, uid int64) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost")
	r.RemoteAddr = "198.51.100.77:4000"
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

// isAdminOnly: the route is for admins and does not open to moderators (policy text starts with "admin" and
// does not name the moderator scope).
func isAdminOnly(p RoutePolicy) bool {
	return (p.Callers == "admin" || strings.HasPrefix(p.Callers, "admin (") || strings.HasPrefix(p.Callers, "admin;")) &&
		!strings.Contains(p.Callers, "moderator")
}

// A moderator is a student with an admin_grants row: every admin-only route still answers 403, every
// moderation route lets them in (anything but 401/403).
func TestModeratorScopeOnEveryAdminRoute(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	mod := launchUser(t, "student")
	launchExec(t, `insert into admin_grants (user_id, scope, reason) values ($1, 'moderator', 'test grant')`, mod)
	student := launchUser(t, "student")
	adminOnly, modRoutes := 0, 0
	for _, route := range Inventory() {
		method, path, _ := strings.Cut(route.Pattern, " ")
		p := wildcard.ReplaceAllString(path, "1")
		switch {
		case isAdminOnly(route.Policy):
			adminOnly++
			if w := consoleRequest(t, method, p, `{}`, mod); w.Code != 403 {
				t.Errorf("moderator %s: %d %s", route.Pattern, w.Code, w.Body)
			}
		case strings.HasPrefix(route.Policy.Callers, "admin, moderator"):
			modRoutes++
			if w := consoleRequest(t, method, p, `{}`, mod); w.Code == 401 || w.Code == 403 {
				t.Errorf("moderator refused on %s: %d %s", route.Pattern, w.Code, w.Body)
			}
			if w := consoleRequest(t, method, p, `{}`, student); w.Code != 403 {
				t.Errorf("student %s: %d %s", route.Pattern, w.Code, w.Body)
			}
		}
	}
	if adminOnly < 40 || modRoutes < 20 {
		t.Fatalf("policy scan looks wrong: %d admin-only, %d moderator routes", adminOnly, modRoutes)
	}
}

// Role-granting queue kinds stay with admins even for a moderator.
func TestModeratorQueueSkipsRoleGrantingKinds(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	mod := launchUser(t, "student")
	launchExec(t, `insert into admin_grants (user_id, scope, reason) values ($1, 'moderator', 'test grant')`, mod)
	var amb, opp int64
	amb = int64(launchCount(t, `with s as (insert into submissions (kind, payload) values ('ambassador_application', '{}') returning id) select id from s`))
	opp = int64(launchCount(t, `with s as (insert into submissions (kind, payload) values ('opportunity', '{"title":"Queue fixture"}') returning id) select id from s`))
	w := consoleRequest(t, "GET", "/api/admin/queue?limit=200", "", mod)
	if w.Code != 200 {
		t.Fatalf("queue: %d %s", w.Code, w.Body)
	}
	var q struct {
		Items         []struct{ ID int64 } `json:"items"`
		PendingByKind map[string]int       `json:"pendingByKind"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	seen := map[int64]bool{}
	for _, it := range q.Items {
		seen[it.ID] = true
	}
	if seen[amb] || !seen[opp] || q.PendingByKind["ambassador_application"] != 0 {
		t.Fatalf("moderator queue: %s", w.Body)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/queue/%d/reject", amb), `{"reason":"not for moderators"}`, mod); w.Code != 403 {
		t.Fatalf("moderator decided an ambassador application: %d %s", w.Code, w.Body)
	}
}

func TestGrantBlockAndAudit(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	admin := launchUser(t, "admin")
	target := launchUser(t, "student")
	// a grant needs a reason and is audited
	if w := consoleRequest(t, "POST", "/api/admin/grants", fmt.Sprintf(`{"userId":%d,"scope":"moderator","reason":""}`, target), admin); w.Code != 400 {
		t.Fatalf("grant without reason: %d", w.Code)
	}
	if w := consoleRequest(t, "POST", "/api/admin/grants", fmt.Sprintf(`{"userId":%d,"scope":"moderator","reason":"runs the queue"}`, target), admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body)
	}
	if n := launchCount(t, `select count(*) from audit_log where action = 'scope_grant' and actor_id = $1 and target_id = $2 and reason = 'runs the queue'`, admin, target); n != 1 {
		t.Fatalf("grant audit rows: %d", n)
	}
	w := consoleRequest(t, "GET", "/api/me", "", target)
	if !strings.Contains(w.Body.String(), `"scopes":["moderator"]`) {
		t.Fatalf("me after grant: %s", w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/admin/reports", "", target); w.Code != 200 {
		t.Fatalf("moderator reports: %d", w.Code)
	}
	// revoke takes effect on the next request
	if w := consoleRequest(t, "POST", "/api/admin/grants/revoke", fmt.Sprintf(`{"userId":%d,"scope":"moderator","reason":"term over"}`, target), admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/admin/reports", "", target); w.Code != 403 {
		t.Fatalf("revoked moderator: %d", w.Code)
	}
	// block: writes refused (except account deletion / sign-out), reads still work; audited; admins cannot be blocked
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/users/%d/block", admin), `{"reason":"self block"}`, admin); w.Code != 400 {
		t.Fatalf("self block: %d", w.Code)
	}
	other := launchUser(t, "admin")
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/users/%d/block", other), `{"reason":"admin block"}`, admin); w.Code != 409 {
		t.Fatalf("admin block: %d", w.Code)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/users/%d/block", target), `{"reason":"spam in Q&A"}`, admin); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body)
	}
	w = consoleRequest(t, "PATCH", "/api/me", `{"name":"Blocked writer"}`, target)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "account_restricted") {
		t.Fatalf("blocked write: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/me", "", target); w.Code != 200 || !strings.Contains(w.Body.String(), `"blocked":true`) {
		t.Fatalf("blocked read: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "POST", "/api/auth/logout", `{}`, target); w.Code != 200 {
		t.Fatalf("blocked logout: %d", w.Code)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/users/%d/unblock", target), `{"reason":"appeal accepted"}`, admin); w.Code != 200 {
		t.Fatalf("unblock: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "PATCH", "/api/me", `{"name":"Writer again"}`, target); w.Code == 403 {
		t.Fatalf("unblocked write: %d %s", w.Code, w.Body)
	}
	if n := launchCount(t, `select count(*) from audit_log where action in ('user_block', 'user_unblock', 'scope_revoke') and target_id = $1`, target); n != 3 {
		t.Fatalf("block audit rows: %d", n)
	}
	// the audit screen lists it with names
	w = consoleRequest(t, "GET", fmt.Sprintf("/api/admin/audit?target=%d", target), "", admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "spam in Q\\u0026A") && !strings.Contains(w.Body.String(), "spam in Q&A") {
		t.Fatalf("audit: %d %s", w.Code, w.Body)
	}
	// detail and list
	if w := consoleRequest(t, "GET", fmt.Sprintf("/api/admin/users/%d", target), "", admin); w.Code != 200 || !strings.Contains(w.Body.String(), `"reportsAgainst"`) {
		t.Fatalf("detail: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/admin/users?role=student&active=30&sort=active&page=1&limit=10", "", admin); w.Code != 200 || !strings.Contains(w.Body.String(), `"total"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/admin/users?active=5", "", admin); w.Code != 400 {
		t.Fatalf("bad filter: %d", w.Code)
	}
	if w := consoleRequest(t, "GET", "/api/admin/dashboard", "", admin); w.Code != 200 || !strings.Contains(w.Body.String(), `"dau"`) {
		t.Fatalf("dashboard: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/admin/system", "", admin); w.Code != 200 || !strings.Contains(w.Body.String(), `"onlineNow"`) {
		t.Fatalf("system: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "GET", "/api/admin/voice", "", admin); w.Code != 200 {
		t.Fatalf("voice: %d %s", w.Code, w.Body)
	}
}

func TestExportsAreExcelFriendlyAndAudited(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	admin := launchUser(t, "admin")
	launchExec(t, `update users set name = '=HYPERLINK("x")' where id = $1`, launchUser(t, "student"))
	for _, kind := range []string{"users", "audit", "cafe-orders", "print-jobs", "feedback", "surveys"} {
		w := consoleRequest(t, "GET", "/api/admin/export/"+kind+".csv", "", admin)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", kind, w.Code, w.Body)
		}
		body := w.Body.String()
		if !strings.HasPrefix(body, "\xEF\xBB\xBF") || !strings.Contains(body, "\r\n") || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
			t.Fatalf("%s: not Excel-friendly: %q", kind, body[:min(len(body), 80)])
		}
		if kind == "users" && (strings.Contains(body, `,=HYPERLINK`) || !strings.Contains(body, `'=HYPERLINK`)) {
			t.Fatalf("formula cell not neutralised: %s", body)
		}
	}
	if n := launchCount(t, `select count(*) from audit_log where action = 'export' and actor_id = $1`, admin); n != 6 {
		t.Fatalf("export audit rows: %d", n)
	}
	if w := consoleRequest(t, "GET", "/api/admin/export/secrets.csv", "", admin); w.Code != 404 {
		t.Fatalf("unknown export: %d", w.Code)
	}
}

// The shop workspace: a barista sees today's numbers of their own café only; the manager also sees revenue and
// the team and may add baristas, not managers; another café's staff get 403; every change is audited.
func TestShopWorkspaceScope(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	manager, barista, outsider, newbie := launchUser(t, "student"), launchUser(t, "student"), launchUser(t, "student"), launchUser(t, "student")
	cafe := int64(launchCount(t, `with c as (insert into cafes (name_kk, name_ru) values ('Тест кофе', 'Тест кофе') returning id) select id from c`))
	other := int64(launchCount(t, `with c as (insert into cafes (name_kk, name_ru) values ('Басқа', 'Другая') returning id) select id from c`))
	shop := int64(launchCount(t, `with c as (insert into print_shops (name_kk, name_ru) values ('Тест принт', 'Тест принт') returning id) select id from c`))
	launchExec(t, `insert into cafe_staff (cafe_id, user_id, role) values ($1, $2, 'manager'), ($1, $3, 'barista'), ($4, $5, 'barista')`, cafe, manager, barista, other, outsider)
	launchExec(t, `insert into cafe_orders (cafe_id, user_id, items, total, pickup_at, pickup_code, payment_method, idem_key, status, closed_at)
		values ($1, $2, '[{"itemId":1}]', 150000, now(), '123', 'pickup', 'consoletest1', 'picked_up', now())`, cafe, newbie)
	stats := func(uid int64, id int64) (int, map[string]any) {
		w := consoleRequest(t, "GET", fmt.Sprintf("/api/console/cafe/%d/stats", id), "", uid)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if code, out := stats(barista, cafe); code != 200 || out["team"] != nil || out["days"] != nil || out["today"].(map[string]any)["revenue"] != nil {
		t.Fatalf("barista stats: %d %v", code, out)
	}
	code, out := stats(manager, cafe)
	if code != 200 || out["today"].(map[string]any)["revenue"] != float64(150000) || len(out["team"].([]any)) != 2 {
		t.Fatalf("manager stats: %d %v", code, out)
	}
	if code, _ := stats(outsider, cafe); code != 403 {
		t.Fatalf("other café's barista: %d", code)
	}
	if w := consoleRequest(t, "GET", fmt.Sprintf("/api/console/print/%d/stats", shop), "", manager); w.Code != 403 {
		t.Fatalf("café manager on the print shop: %d", w.Code)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/console/cafe/%d/team", cafe), fmt.Sprintf(`{"user":"%d"}`, newbie), barista); w.Code != 403 {
		t.Fatalf("barista added staff: %d", w.Code)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/console/cafe/%d/team", cafe), fmt.Sprintf(`{"user":"%d","role":"manager"}`, newbie), manager); w.Code != 403 {
		t.Fatalf("manager added a manager: %d", w.Code)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/console/cafe/%d/team", cafe), fmt.Sprintf(`{"user":"%d","reason":"new shift"}`, newbie), manager); w.Code != 200 {
		t.Fatalf("manager added a barista: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "DELETE", fmt.Sprintf("/api/console/cafe/%d/team/%d", other, outsider), "", manager); w.Code != 403 {
		t.Fatalf("manager removed staff of another café: %d", w.Code)
	}
	if w := consoleRequest(t, "DELETE", fmt.Sprintf("/api/console/cafe/%d/team/%d", cafe, barista), "", manager); w.Code != 200 {
		t.Fatalf("remove: %d %s", w.Code, w.Body)
	}
	if n := launchCount(t, `select count(*) from audit_log where actor_id = $1 and action in ('staff_grant', 'staff_revoke') and meta->>'shop' = 'cafe'`, manager); n != 2 {
		t.Fatalf("team audit rows: %d", n)
	}
	// the panel list names the café for its manager and nothing for the outsider's moderation
	w := consoleRequest(t, "GET", "/api/console/me", "", manager)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"manager"`) || !strings.Contains(w.Body.String(), `"moderation":null`) {
		t.Fatalf("console me: %d %s", w.Code, w.Body)
	}
}

// A moderator does not judge their own case: their own submission (a hidden-by-reports post files one in the
// author's name), their own market ban, or an admin's final hide; nor bans an admin.
func TestModeratorCannotJudgeOwnCaseOrUndoAdmins(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	mod := launchUser(t, "student")
	launchExec(t, `insert into admin_grants (user_id, scope, reason) values ($1, 'moderator', 'test grant')`, mod)
	admin := launchUser(t, "admin")
	own := int64(launchCount(t, `with s as (insert into submissions (kind, payload, submitted_by) values ('opportunity', '{"title":"Own fixture"}', $1) returning id) select id from s`, mod))
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", own), `{}`, mod); w.Code != 403 {
		t.Fatalf("moderator approved their own submission: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "PATCH", fmt.Sprintf("/api/admin/queue/%d", own), `{"payload":{"title":"Edited own"}}`, mod); w.Code != 403 {
		t.Fatalf("moderator edited their own submission: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "POST", fmt.Sprintf("/api/admin/queue/%d/approve", own), `{}`, admin); w.Code != 200 {
		t.Fatalf("admin decides it: %d %s", w.Code, w.Body)
	}
	// banned by an admin, the moderator cannot lift their own ban
	if w := consoleRequest(t, "POST", "/api/admin/market/bans", fmt.Sprintf(`{"userId":%d,"days":30,"reason":"spam listings"}`, mod), admin); w.Code != 200 {
		t.Fatalf("admin ban: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "DELETE", fmt.Sprintf("/api/admin/market/bans/%d", mod), `{"reason":"my own ban"}`, mod); w.Code != 403 {
		t.Fatalf("moderator lifted their own ban: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "POST", "/api/admin/market/bans", fmt.Sprintf(`{"userId":%d,"days":30,"reason":"revenge ban"}`, admin), mod); w.Code != 403 {
		t.Fatalf("moderator banned an admin: %d %s", w.Code, w.Body)
	}
	// an admin's final hide stays for moderators
	q := int64(launchCount(t, `with q as (insert into qa_questions (user_id, university, subject_key, subject_name, title, status, hidden_reason)
		values ($1, 'KazATU', 'math', 'Math', 'Upheld abusive question', 'hidden', 'admin') returning id) select id from q`, mod))
	if w := consoleRequest(t, "POST", "/api/admin/qa/moderate", fmt.Sprintf(`{"kind":"question","id":%d,"hidden":false,"reason":"bring it back"}`, q), mod); w.Code != 403 {
		t.Fatalf("moderator restored an admin hide: %d %s", w.Code, w.Body)
	}
	if w := consoleRequest(t, "POST", "/api/admin/qa/moderate", fmt.Sprintf(`{"kind":"question","id":%d,"hidden":false,"reason":"appeal upheld"}`, q), admin); w.Code != 200 {
		t.Fatalf("admin restores: %d %s", w.Code, w.Body)
	}
}

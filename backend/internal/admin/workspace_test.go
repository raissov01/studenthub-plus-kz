package admin

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { _, err := tx.Exec(ctx(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func scalar(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Tx(ctx(), func(tx pgx.Tx) error { return tx.QueryRow(ctx(), sql, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

// Notes: admin only, never the text in the audit log; orders tab lists the account's café orders.
func TestNotesAndOrders(t *testing.T) {
	testdb.Need(t)
	admin, _ := newUser(t, "admin", "Ноут админ")
	mod, _ := newUser(t, "student", "Ноут модератор")
	mustExec(t, `insert into admin_grants (user_id, scope, reason) values ($1, 'moderator', 'test')`, mod)
	target, _ := newUser(t, "student", "Ноут студент")
	path := "/api/admin/users/" + strconv.FormatInt(target, 10) + "/notes"
	if w := do(t, "POST", path, mod, map[string]any{"body": "x"}); w.Code != 403 {
		t.Fatalf("moderator note: %d", w.Code)
	}
	if w := do(t, "POST", path, admin, map[string]any{"body": "  "}); w.Code != 400 {
		t.Fatalf("empty note: %d", w.Code)
	}
	w := do(t, "POST", path, admin, map[string]any{"body": "Телефонмен хабарласты"})
	if w.Code != 200 {
		t.Fatalf("note: %d %s", w.Code, w.Body)
	}
	var created struct{ ID int64 }
	json.Unmarshal(w.Body.Bytes(), &created)
	if w := do(t, "GET", path, admin, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Телефонмен хабарласты") || !strings.Contains(w.Body.String(), "Ноут админ") {
		t.Fatalf("notes: %d %s", w.Code, w.Body)
	}
	if n := scalar(t, `select count(*) from audit_log where action = 'note_add' and target_id = $1 and meta::text not like '%Телефон%'`, target); n != 1 {
		t.Fatalf("note_add audit: %d", n)
	}
	if w := do(t, "DELETE", path+"/"+strconv.FormatInt(created.ID, 10), admin, nil); w.Code != 200 {
		t.Fatalf("delete note: %d", w.Code)
	}
	if w := do(t, "DELETE", path+"/"+strconv.FormatInt(created.ID, 10), admin, nil); w.Code != 404 {
		t.Fatalf("delete twice: %d", w.Code)
	}
	cafe := scalar(t, `insert into cafes (name_kk, name_ru) values ('Тест кофе', 'Тест кофе') returning id`)
	mustExec(t, `insert into cafe_orders (cafe_id, user_id, items, total, pickup_at, pickup_code, payment_method, idem_key)
		values ($1, $2, '[{"id":1}]', 90000, now(), '123', 'pickup', 'idem-orders-1')`, cafe, target)
	w = do(t, "GET", "/api/admin/users/"+strconv.FormatInt(target, 10)+"/orders", admin, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total": 90000`) && !strings.Contains(w.Body.String(), `"total":90000`) {
		t.Fatalf("orders: %d %s", w.Code, w.Body)
	}
	if w := do(t, "GET", "/api/admin/users/999999999/orders", admin, nil); w.Code != 404 {
		t.Fatalf("orders of nobody: %d", w.Code)
	}
}

// Payments: admin sees every shop; a manager only their own (another shop 403); a barista and a student 403.
// Saved filters follow the same right and belong to their owner.
func TestPaymentsScopeAndSavedViews(t *testing.T) {
	testdb.Need(t)
	t.Setenv("FEATURE_CAFE", "true")
	admin, _ := newUser(t, "admin", "Төлем админ")
	manager, _ := newUser(t, "student", "Төлем менеджер")
	barista, _ := newUser(t, "student", "Төлем бариста")
	buyer, _ := newUser(t, "student", "Төлем сатып алушы")
	a := scalar(t, `insert into cafes (name_kk, name_ru) values ('Кофе А', 'Кофе А') returning id`)
	b := scalar(t, `insert into cafes (name_kk, name_ru) values ('Кофе Б', 'Кофе Б') returning id`)
	mustExec(t, `insert into cafe_staff (cafe_id, user_id, role) values ($1, $2, 'manager'), ($1, $3, 'barista')`, a, manager, barista)
	mustExec(t, `insert into cafe_orders (cafe_id, user_id, items, total, pickup_at, pickup_code, payment_method, payment_status, paid_amount, status, closed_at, idem_key, refund_manual)
		values ($1, $3, '[{"id":1}]', 50000, now(), '111', 'kaspi', 'paid', 50000, 'picked_up', now(), 'idem-pay-a', false),
		       ($2, $3, '[{"id":1}]', 70000, now(), '222', 'kaspi', 'paid', 70000, 'cancelled', now(), 'idem-pay-b', true)`, a, b, buyer)
	w := do(t, "GET", "/api/admin/payments", admin, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Кофе Б") || !strings.Contains(w.Body.String(), `"scope":"all"`) {
		t.Fatalf("admin: %d %s", w.Code, w.Body)
	}
	w = do(t, "GET", "/api/admin/payments", manager, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Кофе Б") || !strings.Contains(w.Body.String(), "Кофе А") {
		t.Fatalf("manager: %d %s", w.Code, w.Body)
	}
	var out struct {
		Totals map[string]int64
		Rows   []map[string]any
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Totals["revenue"] != 50000 || len(out.Rows) != 1 {
		t.Fatalf("manager totals: %v rows %d", out.Totals, len(out.Rows))
	}
	if w := do(t, "GET", "/api/admin/payments?kind=cafe&shop="+strconv.FormatInt(b, 10), manager, nil); w.Code != 403 {
		t.Fatalf("manager, other shop: %d", w.Code)
	}
	w = do(t, "GET", "/api/admin/payments?payment=refund_manual", admin, nil)
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Totals["toReturn"] < 1 || out.Totals["toReturnSum"] < 70000 {
		t.Fatalf("refund filter: %s", w.Body)
	}
	for _, uid := range []int64{barista, buyer} {
		if w := do(t, "GET", "/api/admin/payments", uid, nil); w.Code != 403 {
			t.Fatalf("not a manager: %d", w.Code)
		}
		if w := do(t, "POST", "/api/admin/views", uid, map[string]any{"screen": "payments", "name": "x", "query": "kind=cafe"}); w.Code != 403 {
			t.Fatalf("view by a non-manager: %d", w.Code)
		}
	}
	if w := do(t, "GET", "/api/admin/payments?from=bad", admin, nil); w.Code != 400 {
		t.Fatalf("bad date: %d", w.Code)
	}
	// saved filters
	if w := do(t, "POST", "/api/admin/views", manager, map[string]any{"screen": "users", "name": "x", "query": "role=admin"}); w.Code != 403 {
		t.Fatalf("manager users view: %d", w.Code)
	}
	w = do(t, "POST", "/api/admin/views", manager, map[string]any{"screen": "payments", "name": "Қайтарылатындар", "query": "?payment=refund_manual"})
	if w.Code != 200 {
		t.Fatalf("save view: %d %s", w.Code, w.Body)
	}
	var v struct{ ID int64 }
	json.Unmarshal(w.Body.Bytes(), &v)
	if w := do(t, "GET", "/api/admin/views?screen=payments", manager, nil); !strings.Contains(w.Body.String(), `"query":"payment=refund_manual"`) {
		t.Fatalf("views: %s", w.Body)
	}
	if w := do(t, "GET", "/api/admin/views?screen=payments", admin, nil); strings.Contains(w.Body.String(), "Қайтарылатындар") {
		t.Fatal("someone else's saved filter is visible")
	}
	if w := do(t, "DELETE", "/api/admin/views/"+strconv.FormatInt(v.ID, 10), admin, nil); w.Code != 404 {
		t.Fatalf("delete someone else's view: %d", w.Code)
	}
	if w := do(t, "DELETE", "/api/admin/views/"+strconv.FormatInt(v.ID, 10), manager, nil); w.Code != 200 {
		t.Fatalf("delete own view: %d", w.Code)
	}
	if n := scalar(t, `select count(*) from audit_log where actor_id = $1 and action in ('view_save', 'view_delete')`, manager); n != 2 {
		t.Fatalf("view audit: %d", n)
	}
}

// Settings: admin only, audited with the changed field names; the public endpoint shows the contacts.
func TestSupportSettings(t *testing.T) {
	testdb.Need(t)
	admin, _ := newUser(t, "admin", "Баптау админ")
	student, _ := newUser(t, "student", "Баптау студент")
	body := map[string]any{"email": "Help@Example.kz", "telegram": "@studenthub_help", "hoursKk": "Дс–Жм 9:00–18:00"}
	if w := do(t, "PUT", "/api/admin/config/support", student, body); w.Code != 403 {
		t.Fatalf("student: %d", w.Code)
	}
	if w := do(t, "PUT", "/api/admin/config/support", admin, map[string]any{"phone": "<script>"}); w.Code != 400 {
		t.Fatalf("bad: %d", w.Code)
	}
	if w := do(t, "PUT", "/api/admin/config/support", admin, body); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	w := do(t, "GET", "/api/admin/config", admin, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"email":"help@example.kz"`) || !strings.Contains(w.Body.String(), `"telegram":"studenthub_help"`) || !strings.Contains(w.Body.String(), `"features"`) {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	if w := do(t, "GET", "/api/support", 0, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Дс–Жм") {
		t.Fatalf("public: %d %s", w.Code, w.Body)
	}
	before := scalar(t, `select count(*) from audit_log where action = 'settings_update'`)
	do(t, "PUT", "/api/admin/config/support", admin, body) // no change, no audit row
	if n := scalar(t, `select count(*) from audit_log where action = 'settings_update'`); n != before {
		t.Fatal("unchanged save audited")
	}
	if n := scalar(t, `select count(*) from audit_log where action = 'settings_update' and actor_id = $1 and meta->'fields' ? 'email'`, admin); n != 1 {
		t.Fatalf("settings audit: %d", n)
	}
}

// The dashboard carries the new chart series.
func TestDashboardSeries(t *testing.T) {
	testdb.Need(t)
	admin, _ := newUser(t, "admin", "Чарт админ")
	w := do(t, "GET", "/api/admin/dashboard", admin, nil)
	var d struct {
		Orders   []map[string]any
		Checkins []map[string]any
		Active   struct{ Days []map[string]any }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != 200 {
		t.Fatalf("dashboard: %d %s", w.Code, w.Body)
	}
	if len(d.Orders) != 30 || len(d.Checkins) != 12 || len(d.Active.Days) != 30 {
		t.Fatalf("series: orders %d checkins %d active %d", len(d.Orders), len(d.Checkins), len(d.Active.Days))
	}
}

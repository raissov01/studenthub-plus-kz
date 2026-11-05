package printshop

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestFreeCodeFindsTheLastFreeCodeAndNeverAUsedOne(t *testing.T) {
	used := map[string]bool{}
	for i := 1000; i <= 9999; i++ {
		if i != 7342 {
			used[strconv.Itoa(i)] = true
		}
	}
	if c, ok := shopkit.FreeCode(used, 4); !ok || c != "7342" {
		t.Fatalf("last free code: %q %v", c, ok)
	}
	used["7342"] = true
	if c, ok := shopkit.FreeCode(used, 4); ok {
		t.Fatalf("all taken but got %q", c)
	}
	for i := 0; i < 500; i++ {
		c, ok := shopkit.FreeCode(map[string]bool{}, 3)
		if n, _ := strconv.Atoi(c); !ok || len(c) != 3 || n < 100 || n > 999 {
			t.Fatalf("three digits: %q", c)
		}
	}
	if _, ok := shopkit.FreeCode(nil, 0); ok {
		t.Fatal("zero digits")
	}
}

// placeGoods: a counter job with one folder and no file.
func placeGoods(t *testing.T, f fixture, uid int64) Job {
	t.Helper()
	w := do(t, "POST", "/api/print/jobs", uid, map[string]any{"shopId": f.shop, "payment": "pickup", "key": key(), "goods": []map[string]any{{"id": f.folder, "qty": 1}}})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	return decode[jobReply](t, w).Job
}

func TestPickupCodesAreUniqueAmongOpenJobs(t *testing.T) {
	testdb.Need(t)
	f := newShop(t, "pickup")
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	// many students at once: the shop row lock orders the inserts, so no two open jobs share a code
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := placeGoods(t, f, newUser(t))
			mu.Lock()
			defer mu.Unlock()
			if len(j.PickupCode) != 4 || j.PickupCode[0] == '0' || seen[j.PickupCode] {
				t.Errorf("code %q (seen %v)", j.PickupCode, seen[j.PickupCode])
			}
			seen[j.PickupCode] = true
		}()
	}
	wg.Wait()
	// the database refuses a duplicate among open jobs, and allows it once the other is closed
	codes := []string{}
	for c := range seen {
		codes = append(codes, c)
	}
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		_, err := tx.Exec(bg(), `update print_jobs set pickup_code = $2 where shop_id = $1 and pickup_code = $3`, f.shop, codes[0], codes[1])
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "print_jobs_open_code") {
		t.Fatalf("duplicate open code allowed: %v", err)
	}
	exec(t, `update print_jobs set status = 'picked_up', closed_at = now() where shop_id = $1 and pickup_code = $2`, f.shop, codes[1])
	exec(t, `update print_jobs set pickup_code = $2 where shop_id = $1 and pickup_code = $3 and status = 'picked_up'`, f.shop, codes[0], codes[1])
	// another shop may use the same code
	g := newShop(t, "pickup")
	exec(t, `update print_jobs set pickup_code = $2 where id = $1`, placeGoods(t, g, newUser(t)).ID, codes[2])
}

type lookupReply struct {
	Jobs    []Job `json:"jobs"`
	Valid   bool  `json:"valid"`
	ByToken bool  `json:"byToken"`
}

func lookup(t *testing.T, f fixture, uid int64, q string) lookupReply {
	t.Helper()
	w := do(t, "GET", fmt.Sprintf("/api/print/staff/lookup?shop=%d&q=%s", f.shop, url.QueryEscape(q)), uid, nil)
	if w.Code != 200 {
		t.Fatalf("lookup %q: %d %s", q, w.Code, w.Body)
	}
	return decode[lookupReply](t, w)
}

func TestCounterLookupByCodeOrQRAndAMatchingHandover(t *testing.T) {
	testdb.Need(t)
	f := newShop(t, "pickup")
	student, other := newUser(t), newUser(t)
	j := placeGoods(t, f, student)
	k := placeGoods(t, f, other)
	if j.PickupToken == "" || !strings.HasPrefix(j.PickupToken, fmt.Sprintf("PJ-%d-%s-", j.ID, j.PickupCode)) {
		t.Fatalf("the student's own job carries its QR: %q", j.PickupToken)
	}
	// only the job's student sees the QR: staff reading the job do not
	if w := do(t, "GET", fmt.Sprintf("/api/print/jobs/%d", j.ID), f.operator, nil); w.Code != 200 || strings.Contains(w.Body.String(), "pickupToken") {
		t.Fatalf("staff job view: %d %s", w.Code, w.Body)
	}
	for _, a := range []string{"printing", "ready"} {
		if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/%s", j.ID, a), f.operator, nil); w.Code != 200 {
			t.Fatalf("%s: %d %s", a, w.Code, w.Body)
		}
	}
	// by the spoken code (with or without «№»): the one open job, the first name to ask
	for _, q := range []string{j.PickupCode, "№" + j.PickupCode, " " + j.PickupCode + " "} {
		res := lookup(t, f, f.operator, q)
		if len(res.Jobs) != 1 || res.Jobs[0].ID != j.ID || res.Jobs[0].Customer == nil || !strings.HasPrefix(*res.Jobs[0].Customer, "Студент") || res.ByToken {
			t.Fatalf("lookup %q: want job %d: %+v", q, j.ID, res)
		}
	}
	// by the QR
	res := lookup(t, f, f.operator, j.PickupToken)
	if len(res.Jobs) != 1 || res.Jobs[0].ID != j.ID || !res.ByToken {
		t.Fatalf("lookup by QR: %+v", res)
	}
	// a forged or altered QR, nonsense: nothing
	bad := j.PickupToken[:len(j.PickupToken)-1] + "0"
	if bad == j.PickupToken {
		bad = j.PickupToken[:len(j.PickupToken)-1] + "1"
	}
	forged := fmt.Sprintf("PJ-%d-%s-%s", k.ID, k.PickupCode, strings.Repeat("a", 20))
	for _, q := range []string{bad, forged, "hello", "12", ""} {
		if res := lookup(t, f, f.operator, q); len(res.Jobs) != 0 {
			t.Fatalf("lookup %q found %+v", q, res.Jobs)
		}
	}
	// a student, and another shop's staff, cannot look up
	if w := do(t, "GET", fmt.Sprintf("/api/print/staff/lookup?shop=%d&q=%s", f.shop, j.PickupCode), student, nil); w.Code != 403 {
		t.Fatalf("student lookup: %d", w.Code)
	}
	g := newShop(t, "pickup")
	if w := do(t, "GET", fmt.Sprintf("/api/print/staff/lookup?shop=%d&q=%s", f.shop, j.PickupCode), g.operator, nil); w.Code != 403 {
		t.Fatalf("foreign staff lookup: %d", w.Code)
	}
	if res := lookup(t, g, g.operator, j.PickupToken); len(res.Jobs) != 0 {
		t.Fatalf("foreign shop found the job by QR: %+v", res.Jobs)
	}
	// the handover: another job's code or QR is refused, this job's QR works
	for _, body := range []map[string]any{{"code": k.PickupCode}, {"token": k.PickupToken}, {"token": bad}, {"code": "№"}} {
		if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/picked-up", j.ID), f.operator, body); w.Code != 409 || !strings.Contains(w.Body.String(), "code_mismatch") {
			t.Fatalf("handover with %v: %d %s", body, w.Code, w.Body)
		}
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/picked-up", j.ID), f.operator, map[string]any{"token": j.PickupToken}); w.Code != 200 {
		t.Fatalf("handover by QR: %d %s", w.Code, w.Body)
	}
	// today's closed job is still found by its code (the operator sees it was already handed over)
	res = lookup(t, f, f.operator, j.PickupCode)
	if len(res.Jobs) != 1 || res.Jobs[0].Status != "picked_up" {
		t.Fatalf("closed job lookup: %+v", res)
	}
	// and the closed job's screen no longer carries a QR
	if w := do(t, "GET", fmt.Sprintf("/api/print/jobs/%d", j.ID), student, nil); strings.Contains(w.Body.String(), "pickupToken") {
		t.Fatalf("closed job still has a QR: %s", w.Body)
	}
}

func TestBulkMovesABatchInQueueOrder(t *testing.T) {
	testdb.Need(t)
	f := newShop(t, "pickup")
	a, b, c := placeGoods(t, f, newUser(t)), placeGoods(t, f, newUser(t)), placeGoods(t, f, newUser(t))
	foreign := placeGoods(t, newShop(t, "pickup"), newUser(t))
	type bulk struct {
		Results []bulkResult `json:"results"`
	}
	// asked out of order, moved in queue order; another shop's job looks missing
	w := do(t, "POST", "/api/print/staff/jobs/bulk", f.operator, map[string]any{"action": "printing", "ids": []int64{c.ID, foreign.ID, a.ID, b.ID}})
	if w.Code != 200 {
		t.Fatalf("bulk: %d %s", w.Code, w.Body)
	}
	got := decode[bulk](t, w).Results
	if len(got) != 4 || got[0].ID != a.ID || got[1].ID != b.ID || got[2].ID != c.ID || !got[0].OK || !got[2].OK || got[3].ID != foreign.ID || got[3].OK || got[3].Code != "not_found" {
		t.Fatalf("bulk results: %+v", got)
	}
	if st := scalar[string](t, `select status from print_jobs where id = $1`, foreign.ID); st != "new" {
		t.Fatalf("foreign job moved: %s", st)
	}
	w = do(t, "POST", "/api/print/staff/jobs/bulk", f.operator, map[string]any{"action": "ready", "ids": []int64{a.ID, b.ID}})
	if got := decode[bulk](t, w).Results; len(got) != 2 || !got[0].OK || !got[1].OK {
		t.Fatalf("bulk ready: %+v", got)
	}
	// never a batch handover, never an unknown action, at most maxBulk jobs
	for _, body := range []map[string]any{{"action": "picked-up", "ids": []int64{a.ID}}, {"action": "printing", "ids": []int64{}}, {"action": "printing", "ids": make([]int64, maxBulk+1)}} {
		if w := do(t, "POST", "/api/print/staff/jobs/bulk", f.operator, body); w.Code != 400 {
			t.Fatalf("bulk %v: %d", body["action"], w.Code)
		}
	}
	if w := do(t, "POST", "/api/print/staff/jobs/bulk", newUser(t), map[string]any{"action": "printing", "ids": []int64{c.ID}}); decode[bulk](t, w).Results[0].OK {
		t.Fatal("a student moved a job")
	}
	// the board counts every column
	type board struct {
		Counts map[string]int `json:"counts"`
		Jobs   []Job          `json:"jobs"`
	}
	bd := decode[board](t, do(t, "GET", fmt.Sprintf("/api/print/staff/jobs?shop=%d", f.shop), f.operator, nil))
	if bd.Counts["ready"] != 2 || bd.Counts["printing"] != 1 || len(bd.Jobs) != 3 || bd.Jobs[0].ID != a.ID {
		t.Fatalf("board: %+v", bd.Counts)
	}
}

func TestShopSeparatorSetting(t *testing.T) {
	testdb.Need(t)
	f := newShop(t, "pickup")
	admin := newUser(t)
	exec(t, `update users set role = 'admin' where id = $1`, admin)
	body := map[string]any{"nameKk": "Тест полиграфия", "nameRu": "Тест полиграфия", "openTime": "08:00", "closeTime": "20:00", "openDays": []int{1, 2, 3, 4, 5, 6, 7},
		"slotMinutes": 15, "leadMinutes": 0, "minOrder": 0, "accepts": []string{"pdf"}, "retentionDays": 3, "active": true, "paymentMode": "pickup"}
	type shopReply struct {
		Shop Shop `json:"shop"`
	}
	for _, sep := range []string{"slip", "full", "", "none"} {
		body["separator"] = sep
		if w := do(t, "PUT", fmt.Sprintf("/api/admin/print/shops/%d", f.shop), admin, body); w.Code != 200 {
			t.Fatalf("save %q: %d %s", sep, w.Code, w.Body)
		}
		want := sep
		if want == "" {
			want = "none"
		}
		if got := decode[shopReply](t, do(t, "GET", fmt.Sprintf("/api/print/shops/%d", f.shop), 0, nil)).Shop.Separator; got != want {
			t.Fatalf("separator %q: got %q", sep, got)
		}
	}
	body["separator"] = "codes-in-margins"
	if w := do(t, "PUT", fmt.Sprintf("/api/admin/print/shops/%d", f.shop), admin, body); w.Code != 400 {
		t.Fatalf("bad separator: %d", w.Code)
	}
}

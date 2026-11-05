package printshop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

func TestEstimateUsesTheQuoteRules(t *testing.T) {
	cat := testCatalog()
	// 10 pages duplex b/w × 2 copies + binding (5 sheets → 500 ₸ a copy)
	q, err := Estimate(cat, []EstimateItem{{ServiceID: 1, Size: "A4", Duplex: true, Copies: 2, Pages: 10, BindingID: 2}})
	if err != nil || q.Total != 10*1500*2+2*50000 || q.Review {
		t.Fatalf("estimate: %+v %v", q, err)
	}
	for _, bad := range [][]EstimateItem{
		nil,
		{{ServiceID: 1, Size: "A4", Copies: 1, Pages: 0}},
		{{ServiceID: 1, Size: "A4", Copies: 1, Pages: maxDeclared + 1}},
		{{ServiceID: 6, Size: "A4", Copies: 1, Pages: 3}},              // inactive service
		{{ServiceID: 1, Size: "A3", Color: true, Copies: 1, Pages: 3}}, // no colour at A3
		{{ServiceID: 1, Size: "A4", Copies: 0, Pages: 3}},
		make([]EstimateItem, maxJobFiles+1),
	} {
		if _, err := Estimate(cat, bad); err == nil {
			t.Fatalf("estimate accepted %+v", bad)
		}
	}
}

func TestEtaMinutes(t *testing.T) {
	// 4 s a page, 60 s a job, rounded up to five minutes, never under five
	for _, c := range []struct{ sides, jobs, own, want int }{
		{0, 0, 1, 5},
		{0, 0, 60, 5},    // 240 + 60 s
		{300, 4, 20, 30}, // 1280 + 300 s = 26.3 min
		{1000, 9, 0, 80}, // 4000 + 600 s = 76.7 min
	} {
		if got := EtaMinutes(c.sides, c.jobs, c.own); got != c.want {
			t.Fatalf("eta(%d, %d, %d) = %d, want %d", c.sides, c.jobs, c.own, got, c.want)
		}
	}
}

func TestEstimateEndpointEtaReminderAndReport(t *testing.T) {
	testdb.Need(t)
	realClock(t)
	f := newShop(t, "pickup")
	exec(t, `update print_shops set open_time = '00:00', close_time = '23:59' where id = $1`, f.shop) // open whenever the test runs
	// the estimate: public, the shop's own prices, nothing stored
	w := do(t, "POST", "/api/print/estimate", 0, map[string]any{"shopId": f.shop, "items": []map[string]any{{"serviceId": f.print, "size": "A4", "copies": 3, "pages": 12}}})
	if q := decode[struct{ Quote Quoted }](t, w).Quote; w.Code != 200 || q.Total != 12*2000*3 {
		t.Fatalf("estimate: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/print/estimate", 0, map[string]any{"shopId": f.shop, "items": []map[string]any{{"serviceId": f.print, "size": "A4", "copies": 1, "pages": 0}}}); w.Code != 400 {
		t.Fatalf("estimate without pages: %d", w.Code)
	}
	if w := do(t, "POST", "/api/print/estimate", 0, map[string]any{"shopId": 999999, "items": []map[string]any{{"serviceId": f.print, "size": "A4", "copies": 1, "pages": 1}}}); w.Code != 404 {
		t.Fatalf("estimate of a missing shop: %d", w.Code)
	}
	if n := scalar[int](t, `select count(*)::int from print_jobs where shop_id = $1`, f.shop); n != 0 {
		t.Fatalf("estimate stored %d jobs", n)
	}

	// two jobs in the queue: the second waits for the first and sees a rough time
	student, late := newUser(t), newUser(t)
	file := func(uid int64, pages int) map[string]any {
		u := uploaded(t, uid, "x.pdf", makePDF(pages))
		return map[string]any{"fileId": u.ID, "serviceId": f.print, "size": "A4", "copies": 1}
	}
	first := place(t, f, student, "pickup", file(student, 50))
	second := place(t, f, late, "pickup", file(late, 2))
	got := decode[jobReply](t, do(t, "GET", fmt.Sprintf("/api/print/jobs/%d", second.ID), late, nil)).Job
	if got.Ahead == nil || *got.Ahead != 1 || got.EtaMin == nil || *got.EtaMin != 10 { // (50 + 2 pages) × 4 s + 2 jobs × 60 s = 5.5 min → 10
		t.Fatalf("eta: ahead %v eta %v", got.Ahead, got.EtaMin)
	}

	// ready, then forgotten: one reminder after half the no-show grace, never two
	for _, a := range []string{"printing", "ready"} {
		if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/%s", first.ID, a), f.operator, nil); w.Code != 200 {
			t.Fatalf("%s: %d %s", a, w.Code, w.Body)
		}
	}
	if n, err := remindReady(bg()); err != nil || n != 0 {
		t.Fatalf("early reminder: %d %v", n, err)
	}
	exec(t, `update print_jobs set ready_at = now() - interval '61 minutes', queued_at = now() - interval '70 minutes' where id = $1`, first.ID)
	if n, err := remindReady(bg()); err != nil || n != 1 {
		t.Fatalf("reminder: %d %v", n, err)
	}
	if n, _ := remindReady(bg()); n != 0 {
		t.Fatalf("second reminder: %d", n)
	}
	eventually(t, "the reminder", func() bool { return sentTo(student, fmt.Sprintf("print:reminder:%d", first.ID)) == 1 })
	sentMu.Lock()
	var text string
	for _, m := range messages {
		if m.user == student && strings.HasPrefix(m.key, "print:reminder:") {
			text = m.text
		}
	}
	sentMu.Unlock()
	if !strings.Contains(text, first.PickupCode) || strings.Contains(text, "x.pdf") {
		t.Fatalf("reminder text: %q", text)
	}

	// the day: one handed over at the counter (50 b/w pages), one still open; the student cannot see it
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/picked-up", first.ID), f.operator, map[string]any{"code": first.PickupCode}); w.Code != 200 {
		t.Fatalf("picked up: %d %s", w.Code, w.Body)
	}
	type reportReply struct {
		Report Report `json:"report"`
	}
	// the report is of one Almaty day: pin both jobs to yesterday so the test never straddles midnight
	day := shopkit.DayStart(time.Now()).AddDate(0, 0, -1)
	exec(t, `update print_jobs set created_at = $2, queued_at = $2 where shop_id = $1`, f.shop, day.Add(10*time.Hour))
	exec(t, `update print_jobs set ready_at = $2, closed_at = $3 where id = $1`, first.ID, day.Add(10*time.Hour+30*time.Minute), day.Add(11*time.Hour))
	rep := decode[reportReply](t, do(t, "GET", fmt.Sprintf("/api/print/staff/report?shop=%d&day=%s", f.shop, day.Format("2006-01-02")), f.operator, nil)).Report
	if rep.Received != 2 || rep.PickedUp != 1 || rep.Open != 1 || rep.RevenueCash != first.Total || rep.RevenueKaspi != 0 || rep.PagesBW != 50 || rep.PagesColor != 0 ||
		rep.Sheets != 50 || rep.WaitMedianMin == nil || *rep.WaitMedianMin != 30 || rep.Day != day.Format("2006-01-02") {
		t.Fatalf("report: %+v", rep)
	}
	// today: nothing came in or went out yet, the open job still counts
	if today := decode[reportReply](t, do(t, "GET", fmt.Sprintf("/api/print/staff/report?shop=%d", f.shop), f.operator, nil)).Report; today.Received != 0 || today.PickedUp != 0 || today.Open != 1 {
		t.Fatalf("today: %+v", today)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/print/staff/report?shop=%d", f.shop), student, nil); w.Code != 403 {
		t.Fatalf("student report: %d", w.Code)
	}
	for _, d := range []string{"2026-13-01", "2099-01-01", "yesterday"} {
		if w := do(t, "GET", fmt.Sprintf("/api/print/staff/report?shop=%d&day=%s", f.shop, d), f.operator, nil); w.Code != 400 {
			t.Fatalf("report day %s: %d", d, w.Code)
		}
	}
}

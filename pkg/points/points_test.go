package points

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
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("p", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("POINTS_REDEEMS_PER_HOUR", "100000")
	os.Setenv("POINTS_CLAIMS_PER_HOUR", "100000")
	os.Setenv("FEATURE_POINTS", "true")
	os.Setenv("ALLOWED_HOSTS", "localhost")
	os.Exit(testdb.Main(m, "points"))
}

var seq atomic.Int64

func bg() context.Context { return context.Background() }

func newUser(t *testing.T, role, university, faculty string) int64 {
	t.Helper()
	testdb.Need(t)
	var id int64
	n := seq.Add(1)
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `insert into users (telegram_id, name, role, consent_at, university, faculty, bot_inactive)
			values ($1, $2, $3, now(), nullif($4, ''), nullif($5, ''), true) returning id`,
			880000+n, fmt.Sprintf("Студент%d Тестов", n), role, university, faculty).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(bg(), func(tx pgx.Tx) error { _, err := tx.Exec(bg(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Tx(bg(), func(tx pgx.Tx) error { return tx.QueryRow(bg(), sql, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func handler() http.Handler {
	m := http.NewServeMux()
	Routes(m)
	return m
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
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return v.Error.Code
}

func grantAt(t *testing.T, e Earn, at time.Time) *Award {
	t.Helper()
	var a *Award
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		var err error
		a, err = award(bg(), tx, e, at)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func balance(t *testing.T, uid int64) (coins, ledger int) {
	t.Helper()
	coins = count(t, `select coalesce((select coins from points_wallets where user_id = $1), 0)`, uid)
	ledger = count(t, `select coalesce(sum(delta), 0) from coin_ledger where user_id = $1`, uid)
	if coins != ledger {
		t.Fatalf("cached balance %d != ledger sum %d", coins, ledger)
	}
	return coins, ledger
}

func setCoins(t *testing.T, uid int64, coins int) {
	t.Helper()
	exec(t, `insert into points_wallets (user_id) values ($1) on conflict do nothing`, uid)
	exec(t, `insert into coin_ledger (user_id, delta, reason, ref_type, ref_id, day) values ($1, $2, 'admin_adjust', 'admin_adjust', $3, current_date)`,
		uid, coins, fmt.Sprintf("seed-%d-%d", uid, seq.Add(1)))
	exec(t, `update points_wallets set coins = coins + $2 where user_id = $1`, uid, coins)
}

func TestLevelDayWeek(t *testing.T) {
	for _, c := range []struct {
		xp          int64
		level       int
		floor, next int64
	}{{0, 1, 0, 100}, {99, 1, 0, 100}, {100, 2, 100, 300}, {299, 2, 100, 300}, {300, 3, 300, 600}, {1000, 5, 1000, 1500}} {
		l, f, n := Level(c.xp)
		if l != c.level || f != c.floor || n != c.next {
			t.Errorf("Level(%d) = %d %d %d", c.xp, l, f, n)
		}
	}
	// 2026-11-08 is a Sunday; 20:30 UTC is already Monday 01:30 in Almaty (UTC+5)
	sun := time.Date(2026, 11, 8, 20, 30, 0, 0, time.UTC)
	if d := Day(sun).Format("2006-01-02"); d != "2026-11-09" {
		t.Fatalf("day %s", d)
	}
	if ws := WeekStart(sun); ws.UTC() != time.Date(2026, 11, 8, 19, 0, 0, 0, time.UTC) {
		t.Fatalf("week start %s", ws.UTC())
	}
	if ws := WeekStart(sun.Add(-2 * time.Hour)); ws.UTC() != time.Date(2026, 11, 1, 19, 0, 0, 0, time.UTC) {
		t.Fatalf("previous week start %s", ws.UTC())
	}
}

func TestStreakAndFreeze(t *testing.T) {
	d := func(n int) time.Time { return time.Date(2026, 11, n, 0, 0, 0, 0, time.UTC) }
	w := &wallet{}
	w.streakAfter(d(1))
	w.streakAfter(d(1))
	w.streakAfter(d(2))
	if w.Streak != 2 || w.Best != 2 {
		t.Fatalf("consecutive: %+v", w)
	}
	w.Freezes = 1
	if !w.streakAfter(d(4)) || w.Streak != 3 || w.Freezes != 0 { // the 3rd was missed: the freeze covers it
		t.Fatalf("freeze: %+v", w)
	}
	w.streakAfter(d(6)) // missed the 5th with no freeze left
	if w.Streak != 1 || w.Best != 3 {
		t.Fatalf("reset: %+v", w)
	}
	last := d(6)
	for _, c := range []struct {
		today          time.Time
		freezes, wants int
	}{{d(6), 0, 1}, {d(7), 0, 1}, {d(8), 0, 0}, {d(8), 1, 1}, {d(9), 1, 0}} {
		if got := CurrentStreak(&last, 1, c.freezes, c.today); got != c.wants {
			t.Errorf("CurrentStreak(today %s, freezes %d) = %d", c.today.Format("02"), c.freezes, got)
		}
	}
}

func TestAwardIdempotentCappedAndDeviceOnly(t *testing.T) {
	uid := newUser(t, "student", "", "")
	now := time.Now()
	e := Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "1"}
	if a := grantAt(t, e, now); a.XP != 30 || a.Coins != 15 || a.Duplicate {
		t.Fatalf("first: %+v", a)
	}
	if a := grantAt(t, e, now); !a.Duplicate || a.XP != 0 || a.Coins != 0 {
		t.Fatalf("same action paid twice: %+v", a)
	}
	// per-reason daily cap: 3 check-ins per day
	grantAt(t, Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "2"}, now)
	grantAt(t, Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "3"}, now)
	if a := grantAt(t, Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "4"}, now); !a.Capped || a.XP != 0 {
		t.Fatalf("4th check-in not capped: %+v", a)
	}
	// daily coin cap 30: two check-ins already paid 30 coins, the third only XP
	if c, _ := balance(t, uid); c != DailyCoinCap {
		t.Fatalf("coins %d, want the daily cap %d", c, DailyCoinCap)
	}
	// the capped action pays on another day
	if a := grantAt(t, Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "4"}, now.Add(24*time.Hour)); a.XP != 30 || a.Coins != 15 {
		t.Fatalf("next day: %+v", a)
	}
	// device-only claims never mint coins, even when asked to
	if a := grantAt(t, Earn{UserID: uid, Reason: ReasonRoadmapStep, RefType: "roadmap", RefID: "cv:0", Coins: 50}, now); a.Coins != 0 || a.XP != 5 {
		t.Fatalf("device claim: %+v", a)
	}
	if a := grantAt(t, Earn{UserID: uid, Reason: ReasonRoadmapStep, RefType: "roadmap", RefID: "cv:1"}, now); !a.Capped {
		t.Fatalf("2nd roadmap step today not capped: %+v", a)
	}
	balance(t, uid)
	if count(t, `select streak_days from points_wallets where user_id = $1`, uid) != 2 {
		t.Fatal("streak not kept across the two active days")
	}
	// the ledger is append-only
	if err := db.Tx(bg(), func(tx pgx.Tx) error {
		_, err := tx.Exec(bg(), `update coin_ledger set delta = 1000 where user_id = $1`, uid)
		return err
	}); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("ledger update allowed: %v", err)
	}
	if err := db.Tx(bg(), func(tx pgx.Tx) error {
		_, err := tx.Exec(bg(), `delete from xp_events where user_id = $1`, uid)
		return err
	}); err == nil {
		t.Fatal("xp_events delete allowed")
	}
}

func TestFeatureOffAwardsNothing(t *testing.T) {
	uid := newUser(t, "student", "", "")
	t.Setenv("FEATURE_POINTS", "false")
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		a, err := Grant(bg(), tx, Earn{UserID: uid, Reason: ReasonQuiz, RefType: "quiz", RefID: "9"})
		if a != nil {
			t.Errorf("award while off: %+v", a)
		}
		return err
	})
	if err != nil || count(t, `select count(*) from xp_events where user_id = $1`, uid) != 0 {
		t.Fatal(err)
	}
	if w := do(t, "GET", "/api/points/me", uid, nil); w.Code != 404 {
		t.Fatalf("route while off: %d", w.Code)
	}
}

func TestReverseClampsToBalance(t *testing.T) {
	uid := newUser(t, "student", "", "")
	now := time.Now()
	grantAt(t, Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "77"}, now)
	exec(t, `insert into coin_ledger (user_id, delta, reason, ref_type, ref_id, day) values ($1, -10, 'redeem', 'redemption', 'x', current_date)`, uid)
	exec(t, `update points_wallets set coins = coins - 10 where user_id = $1`, uid)
	for range 2 {
		if err := db.Tx(bg(), func(tx pgx.Tx) error { return Reverse(bg(), tx, uid, ReasonEventCheckin, "event", "77") }); err != nil {
			t.Fatal(err)
		}
	}
	if c, _ := balance(t, uid); c != 0 {
		t.Fatalf("coins %d", c)
	}
	if xp := count(t, `select xp from points_wallets where user_id = $1`, uid); xp != 0 {
		t.Fatalf("xp %d", xp)
	}
	// re-checking-in never pays again
	if a := grantAt(t, Earn{UserID: uid, Reason: ReasonEventCheckin, RefType: "event", RefID: "77"}, now); !a.Duplicate {
		t.Fatalf("paid again after revoke: %+v", a)
	}
}

type meResp struct {
	Award *Award   `json:"award"`
	Me    *Summary `json:"me"`
}

func TestDailyClaimFreezeSettingsAPI(t *testing.T) {
	uid := newUser(t, "student", "U1", "IT")
	if w := do(t, "GET", "/api/points/me", 0, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	w := do(t, "POST", "/api/points/daily", uid, nil)
	r := decode[meResp](t, w)
	if w.Code != 200 || r.Award.XP != 5 || r.Award.Coins != 1 || r.Me.Coins != 1 || r.Me.Streak.Days != 1 || !r.Me.Streak.ActiveToday {
		t.Fatalf("daily: %d %s", w.Code, w.Body)
	}
	if r := decode[meResp](t, do(t, "POST", "/api/points/daily", uid, nil)); !r.Award.Duplicate || r.Me.Coins != 1 {
		t.Fatalf("daily twice: %+v", r.Award)
	}
	if r.Me.Tasks[0].ID != ReasonDailyOpen || r.Me.Tasks[0].Done != 1 || len(r.Me.Recent) != 1 {
		t.Fatalf("tasks: %+v", r.Me)
	}
	// client cannot name an amount or an arbitrary reason
	for _, body := range []map[string]any{{"action": "event_checkin", "ref": "1"}, {"action": "quiz", "ref": "1"}, {"action": "roadmap_step", "ref": "../x"}, {"action": "deadline_done", "ref": "dl-1"}} {
		if w := do(t, "POST", "/api/points/claim", uid, body); w.Code != 400 {
			t.Errorf("claim %v: %d", body, w.Code)
		}
	}
	w = do(t, "POST", "/api/points/claim", uid, map[string]any{"action": "roadmap_step", "ref": "cv:0", "xp": 1000, "coins": 1000})
	if r := decode[meResp](t, w); w.Code != 200 || r.Award.XP != 5 || r.Award.Coins != 0 {
		t.Fatalf("claim: %d %s", w.Code, w.Body)
	}
	// freeze: costs 20, at most one
	if w := do(t, "POST", "/api/points/freeze", uid, map[string]string{"key": "freeze-key-1"}); w.Code != 409 || errCode(w) != "not_enough_coins" {
		t.Fatalf("freeze without coins: %d %s", w.Code, w.Body)
	}
	setCoins(t, uid, 50)
	if w := do(t, "POST", "/api/points/freeze", uid, map[string]string{"key": "freeze-key-2"}); w.Code != 200 || decode[meResp](t, w).Me.Streak.Freezes != 1 {
		t.Fatalf("freeze: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/points/freeze", uid, map[string]string{"key": "freeze-key-2"}); w.Code != 200 { // retry of the same request
		t.Fatalf("freeze retry: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/points/freeze", uid, map[string]string{"key": "freeze-key-3"}); w.Code != 409 || errCode(w) != "freeze_owned" {
		t.Fatalf("second freeze: %d %s", w.Code, w.Body)
	}
	if c, _ := balance(t, uid); c != 31 {
		t.Fatalf("coins after freeze %d", c)
	}
	// settings
	if w := do(t, "PATCH", "/api/points/settings", uid, map[string]any{"studyGroup": "  ис-21  "}); w.Code != 200 || *decode[meResp](t, w).Me.Settings.StudyGroup != "ИС-21" {
		t.Fatalf("group: %d %s", w.Code, w.Body)
	}
	if w := do(t, "PATCH", "/api/points/settings", uid, map[string]any{"studyGroup": "<script>"}); w.Code != 400 {
		t.Fatalf("bad group: %d", w.Code)
	}
	if w := do(t, "PATCH", "/api/points/settings", uid, map[string]any{"leaderboardOptOut": true}); !decode[meResp](t, w).Me.Settings.LeaderboardOptOut {
		t.Fatalf("opt out: %s", w.Body)
	}
}

func TestLeaderboardPrivacy(t *testing.T) {
	uni, fac := fmt.Sprintf("LB%d", seq.Add(1)), "ECON"
	var ids []int64
	for i := range 6 {
		id := newUser(t, "student", uni, fac)
		ids = append(ids, id)
		grantAt(t, Earn{UserID: id, Reason: ReasonEventCheckin, RefType: "event", RefID: fmt.Sprint(i)}, time.Now())
		if i < 4 {
			exec(t, `update points_wallets set study_group = 'G-1' where user_id = $1`, id)
		}
	}
	other := newUser(t, "student", "OTHER", fac) // another university never shows up
	grantAt(t, Earn{UserID: other, Reason: ReasonEventCheckin, RefType: "event", RefID: "1"}, time.Now())
	type lb struct {
		Status  string   `json:"status"`
		Members int      `json:"members"`
		Leaders []Leader `json:"leaders"`
	}
	if r := decode[lb](t, do(t, "GET", "/api/points/leaderboard?scope=group", ids[0], nil)); r.Status != "too_small" || r.Leaders != nil {
		t.Fatalf("group of 4 shown: %+v", r)
	}
	r := decode[lb](t, do(t, "GET", "/api/points/leaderboard?scope=faculty", ids[0], nil))
	if r.Status != "ok" || len(r.Leaders) != 6 || !r.Leaders[0].Me && r.Leaders[0].XP != 30 {
		t.Fatalf("faculty: %+v", r)
	}
	for _, l := range r.Leaders {
		if strings.Contains(l.Name, "Тестов") {
			t.Fatalf("full surname exposed: %q", l.Name)
		}
	}
	// opting out: hidden from others, and the faculty drops to 5 visible members (still shown)
	exec(t, `update points_wallets set leaderboard_opt_out = true where user_id = $1`, ids[5])
	r = decode[lb](t, do(t, "GET", "/api/points/leaderboard?scope=faculty", ids[0], nil))
	if r.Status != "ok" || len(r.Leaders) != 5 {
		t.Fatalf("after opt-out: %+v", r)
	}
	exec(t, `update points_wallets set leaderboard_opt_out = true where user_id = $1`, ids[4])
	if r := decode[lb](t, do(t, "GET", "/api/points/leaderboard?scope=faculty", ids[0], nil)); r.Status != "too_small" {
		t.Fatalf("4 visible members shown: %+v", r)
	}
	if w := do(t, "GET", "/api/points/leaderboard?scope=all", ids[0], nil); w.Code != 400 {
		t.Fatalf("unknown scope: %d", w.Code)
	}
	noScope := newUser(t, "student", "", "")
	if r := decode[lb](t, do(t, "GET", "/api/points/leaderboard?scope=faculty", noScope, nil)); r.Status != "no_scope" {
		t.Fatalf("no profile: %+v", r)
	}
}

func newReward(t *testing.T, admin int64, body map[string]any) Reward {
	t.Helper()
	w := do(t, "POST", "/api/admin/points/rewards", admin, body)
	if w.Code != 201 {
		t.Fatalf("create reward: %d %s", w.Code, w.Body)
	}
	return decode[struct{ Reward Reward }](t, w).Reward
}

// The concurrency proof: many parallel redeems against one wallet never overspend, and the
// cached balance always equals the ledger sum.
func TestParallelRedeemsCannotOverspend(t *testing.T) {
	admin := newUser(t, "admin", "", "")
	uid := newUser(t, "student", "", "")
	rw := newReward(t, admin, map[string]any{"kind": "merch", "titleKk": "Футболка", "titleRu": "Футболка", "cost": 30, "perUserLimit": 50})
	setCoins(t, uid, 100)
	const n = 12
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": rw.ID, "key": fmt.Sprintf("parallel-key-%02d", i)}).Code
		}()
	}
	wg.Wait()
	ok, refused := 0, 0
	for _, c := range codes {
		switch c {
		case 201:
			ok++
		case 409:
			refused++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if ok != 3 || refused != n-3 {
		t.Fatalf("created %d, refused %d: %v", ok, refused, codes)
	}
	if c, _ := balance(t, uid); c != 10 {
		t.Fatalf("balance %d, want 10", c)
	}
	// the same idempotency key in parallel creates one redemption
	setCoins(t, uid, 100)
	codes = make([]int, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": rw.ID, "key": "same-key-for-all"}).Code
		}()
	}
	wg.Wait()
	if count(t, `select count(*) from reward_redemptions where user_id = $1 and idem_key = 'same-key-for-all'`, uid) != 1 {
		t.Fatalf("idempotency key paid twice: %v", codes)
	}
	if c, _ := balance(t, uid); c != 80 {
		t.Fatalf("balance %d, want 80", c)
	}
	// stock is respected under parallel load too
	limited := newReward(t, admin, map[string]any{"kind": "merch", "titleKk": "Блокнот", "titleRu": "Блокнот", "cost": 1, "stock": 2, "perUserLimit": 50})
	codes = make([]int, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": limited.ID, "key": fmt.Sprintf("stock-key-%02d", i)}).Code
		}()
	}
	wg.Wait()
	if count(t, `select count(*) from reward_redemptions where reward_id = $1`, limited.ID) != 2 || count(t, `select stock from rewards where id = $1`, limited.ID) != 0 {
		t.Fatalf("stock oversold: %v", codes)
	}
	balance(t, uid)
}

func TestRedeemModerationFlow(t *testing.T) {
	admin := newUser(t, "admin", "", "")
	uid := newUser(t, "student", "", "")
	student := newUser(t, "student", "", "")
	rw := newReward(t, admin, map[string]any{"kind": "print_quota", "titleKk": "Қосымша басып шығару", "titleRu": "Доп. печать", "cost": 10, "stock": 5,
		"perUserLimit": 2, "addCodes": []string{"PRINT-AAA", " ", "PRINT-BBB"}})
	if *rw.CodesLeft != 2 {
		t.Fatalf("codes: %+v", rw)
	}
	// students cannot manage rewards; validation names the field
	if w := do(t, "POST", "/api/admin/points/rewards", student, map[string]any{}); w.Code != 403 {
		t.Fatalf("student created reward: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/points/rewards", admin, map[string]any{"kind": "cash", "titleKk": "Ақша", "titleRu": "Деньги", "cost": 1, "perUserLimit": 1}); w.Code != 400 || !strings.Contains(w.Body.String(), `"field":"kind"`) {
		t.Fatalf("cash reward accepted: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": rw.ID, "key": "flow-key-01"}); w.Code != 409 || errCode(w) != "not_enough_coins" {
		t.Fatalf("redeem with 0 coins: %d %s", w.Code, w.Body)
	}
	setCoins(t, uid, 25)
	type redResp struct {
		Redemption Redemption `json:"redemption"`
		Me         Summary    `json:"me"`
	}
	w := do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": rw.ID, "key": "flow-key-02"})
	first := decode[redResp](t, w)
	if w.Code != 201 || first.Redemption.Status != "pending" || first.Me.Coins != 15 {
		t.Fatalf("redeem: %d %s", w.Code, w.Body)
	}
	second := decode[redResp](t, do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": rw.ID, "key": "flow-key-03"}))
	if w := do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": rw.ID, "key": "flow-key-04"}); w.Code != 409 || errCode(w) != "limit_reached" {
		t.Fatalf("per-user limit: %d %s", w.Code, w.Body)
	}
	queue := decode[struct{ Redemptions []Redemption }](t, do(t, "GET", "/api/admin/points/redemptions", admin, nil))
	if len(queue.Redemptions) < 2 || queue.Redemptions[0].UserBalance == nil {
		t.Fatalf("queue: %+v", queue)
	}
	// approve hands out the next pool code; the student sees it only now
	w = do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/approve", first.Redemption.ID), admin, map[string]any{})
	if w.Code != 200 || *decode[redResp](t, w).Redemption.Code != "PRINT-AAA" {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/approve", first.Redemption.ID), admin, map[string]any{}); w.Code != 409 {
		t.Fatalf("approve twice: %d", w.Code)
	}
	// reject needs a reason and refunds in the same transaction, restoring the stock
	if w := do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/reject", second.Redemption.ID), admin, map[string]any{}); w.Code != 400 {
		t.Fatalf("reject without reason: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/reject", second.Redemption.ID), admin, map[string]any{"note": "Қор таусылды"}); w.Code != 200 {
		t.Fatalf("reject: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/reject", second.Redemption.ID), admin, map[string]any{"note": "again"}); w.Code != 409 {
		t.Fatalf("double refund: %d", w.Code)
	}
	if c, _ := balance(t, uid); c != 15 {
		t.Fatalf("balance after refund %d", c)
	}
	if count(t, `select stock from rewards where id = $1`, rw.ID) != 4 {
		t.Fatal("stock not restored")
	}
	mine := decode[struct{ Redemptions []Redemption }](t, do(t, "GET", "/api/points/redemptions", uid, nil)).Redemptions
	if len(mine) != 2 || mine[0].Status != "rejected" || mine[0].Code != nil || mine[1].Code == nil || *mine[1].Code != "PRINT-AAA" {
		t.Fatalf("my redemptions: %+v", mine)
	}
	if count(t, `select count(*) from reward_redemptions where user_id = $1`, student) != 0 ||
		len(decode[struct{ Redemptions []Redemption }](t, do(t, "GET", "/api/points/redemptions", student, nil)).Redemptions) != 0 {
		t.Fatal("another student's redemptions visible")
	}
	// both decisions reached the inbox through the shared dispatcher, without the code
	if n := count(t, `select count(*) from notifications where user_id = $1 and category = 'moderation' and link = '/points'`, uid); n != 2 {
		t.Fatalf("inbox notifications: %d", n)
	}
	if count(t, `select count(*) from notifications where user_id = $1 and text like '%PRINT-AAA%'`, uid) != 0 {
		t.Fatal("promo code sent in a notification")
	}
	if count(t, `select count(*) from audit_log where actor_id = $1 and action in ('points_redeem_approve', 'points_redeem_reject')`, admin) != 2 {
		t.Fatal("decisions not audited")
	}
	// a discount reward with no code anywhere cannot be approved silently
	exec(t, `insert into rewards (kind, title_kk, title_ru, cost, discount_id) values ('discount', 'Жеңілдік', 'Скидка', 1, 'd1')`)
	var did int64
	_ = db.Tx(bg(), func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `select id from rewards where kind = 'discount' order by id desc limit 1`).Scan(&did)
	})
	red := decode[redResp](t, do(t, "POST", "/api/points/redeem", uid, map[string]any{"rewardId": did, "key": "flow-key-05"}))
	if w := do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/approve", red.Redemption.ID), admin, map[string]any{}); w.Code != 400 || errCode(w) != "code_required" {
		t.Fatalf("discount approve without code: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/admin/points/redemptions/%d/approve", red.Redemption.ID), admin, map[string]any{"code": "PARTNER10"}); w.Code != 200 {
		t.Fatalf("discount approve: %d %s", w.Code, w.Body)
	}
}

func TestAdminAdjustAudited(t *testing.T) {
	admin := newUser(t, "admin", "", "")
	uid := newUser(t, "student", "", "")
	path := fmt.Sprintf("/api/admin/points/users/%d/adjust", uid)
	if w := do(t, "POST", path, admin, map[string]any{"delta": 10, "key": "adjust-key-1"}); w.Code != 400 || errCode(w) != "reason_required" {
		t.Fatalf("adjust without reason: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", path, admin, map[string]any{"delta": -10, "reason": "Қате есептелген", "key": "adjust-key-2"}); w.Code != 409 {
		t.Fatalf("adjust below zero: %d %s", w.Code, w.Body)
	}
	for range 2 { // retried with the same key: applied once
		if w := do(t, "POST", path, admin, map[string]any{"delta": 10, "reason": "Олимпиада жеңімпазы", "key": "adjust-key-3"}); w.Code != 200 {
			t.Fatalf("adjust: %d %s", w.Code, w.Body)
		}
	}
	if c, _ := balance(t, uid); c != 10 {
		t.Fatalf("balance %d", c)
	}
	if count(t, `select count(*) from audit_log where actor_id = $1 and target_id = $2 and action = 'points_adjust' and reason = 'Олимпиада жеңімпазы'`, admin, uid) != 1 {
		t.Fatal("adjust not audited")
	}
	led := decode[struct {
		User   map[string]any
		Ledger []map[string]any
	}](t, do(t, "GET", fmt.Sprintf("/api/admin/points/users/%d", uid), admin, nil))
	if len(led.Ledger) != 1 || led.Ledger[0]["note"] != "Олимпиада жеңімпазы" || led.User["ledgerSum"] != float64(10) {
		t.Fatalf("ledger view: %+v", led)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/admin/points/users/%d", uid), uid, nil); w.Code != 403 {
		t.Fatalf("student read ledger: %d", w.Code)
	}
	if w := do(t, "POST", "/api/admin/points/users/999999999/adjust", admin, map[string]any{"delta": 1, "reason": "nobody here", "key": "adjust-key-4"}); w.Code != 404 {
		t.Fatalf("unknown user: %d", w.Code)
	}
}

func TestRLSOwnRowsOnly(t *testing.T) {
	a, b := newUser(t, "student", "", ""), newUser(t, "student", "", "")
	grantAt(t, Earn{UserID: a, Reason: ReasonDailyOpen, RefType: "day", RefID: "2026-01-01"}, time.Now())
	grantAt(t, Earn{UserID: b, Reason: ReasonDailyOpen, RefType: "day", RefID: "2026-01-01"}, time.Now())
	err := db.WithUser(bg(), a, func(tx pgx.Tx) error {
		var n int
		for _, table := range []string{"points_wallets", "xp_events", "coin_ledger"} {
			if err := tx.QueryRow(bg(), `select count(*) from `+table+` where user_id <> app_user_id()`).Scan(&n); err != nil || n != 0 {
				return fmt.Errorf("%s: %d %v", table, n, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithUser(bg(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(bg(), `update points_wallets set coins = 1000`)
		return err
	}); err == nil {
		t.Fatal("app_user wrote its wallet")
	}
	if err := db.WithUser(bg(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(bg(), `select count(*) from reward_codes`)
		return err
	}); err == nil {
		t.Fatal("app_user can read reward codes")
	}
}

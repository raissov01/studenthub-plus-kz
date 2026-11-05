package points

import (
	"context"
	"sync"
	"testing"
	"time"
)

// almatyAt is an Almaty wall-clock time on 2026-11-<day>.
func almatyAt(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, almaty)
}

func withNow(t *testing.T, at time.Time) {
	t.Helper()
	prev := Now
	Now = func() time.Time { return at }
	t.Cleanup(func() { Now = prev })
}

func TestStreakPureRules(t *testing.T) {
	d := func(n int) time.Time { return time.Date(2026, 11, n, 0, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		day  time.Time
		want string
	}{{d(5), "2026-fall"}, {time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC), "2026-fall"}, {time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), "2027-spring"}, {time.Date(2027, 7, 31, 0, 0, 0, 0, time.UTC), "2027-spring"}} {
		if got := Semester(c.day); got != c.want {
			t.Errorf("Semester(%s) = %s", c.day.Format(ymd), got)
		}
	}
	if NextMilestone(0) != 3 || NextMilestone(3) != 7 || NextMilestone(364) != 365 || NextMilestone(365) != 0 {
		t.Fatal("next milestone")
	}
	from, until := d(5), d(14)
	// 10-day pause: the gap 4 → 15 has 10 missed days, all covered
	if n := missedDays(d(4), d(15), &from, &until); n != 0 {
		t.Fatalf("covered gap: %d", n)
	}
	if n := missedDays(d(4), d(16), &from, &until); n != 1 {
		t.Fatalf("one day after the pause: %d", n)
	}
	if got := CurrentStreakPaused(ptr(d(4)), 9, 0, d(15), &from, &until); got != 9 {
		t.Fatalf("paused streak lost: %d", got)
	}
	if got := CurrentStreakPaused(ptr(d(4)), 9, 0, d(16), &from, &until); got != 0 {
		t.Fatalf("streak kept a day past the pause without a freeze: %d", got)
	}
	// two freezes cover two missed days
	w := &wallet{Streak: 4, Best: 4, LastDay: ptr(d(1)), Freezes: 2}
	if !w.streakAfter(d(4)) || w.Streak != 5 || w.Freezes != 0 {
		t.Fatalf("two freezes: %+v", w)
	}
}

func ptr[T any](v T) *T { return &v }

func TestStreakReminderOncePerEvening(t *testing.T) {
	t.Setenv("FEATURE_STREAK", "true")
	var mu sync.Mutex
	var got []Reminder
	prev := SendReminder
	SendReminder = func(_ context.Context, r Reminder, _ time.Time) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r)
		return true, nil
	}
	t.Cleanup(func() { SendReminder = prev })

	five := newUser(t, "student", "", "")
	two := newUser(t, "student", "", "")
	off := newUser(t, "student", "", "")
	exec(t, `insert into points_wallets (user_id, streak_days, streak_best, last_active_day, streak_reminder) values
		($1, 5, 5, '2026-11-09', true), ($2, 2, 2, '2026-11-09', true), ($3, 8, 8, '2026-11-09', false)`, five, two, off)
	mine := func() []Reminder {
		mu.Lock()
		defer mu.Unlock()
		var out []Reminder
		for _, r := range got {
			if r.UserID == five || r.UserID == two || r.UserID == off {
				out = append(out, r)
			}
		}
		return out
	}

	if _, err := RunReminders(bg(), almatyAt(10, 19, 50)); err != nil {
		t.Fatal(err)
	}
	if n := len(mine()); n != 0 {
		t.Fatalf("before the reminder time: %d", n)
	}
	if _, err := RunReminders(bg(), almatyAt(10, 20, 0)); err != nil {
		t.Fatal(err)
	}
	rs := mine()
	if len(rs) != 1 || rs[0].UserID != five || rs[0].Key != "streak:"+itoa(five)+":2026-11-10" || rs[0].Streak != 5 {
		t.Fatalf("20:00 run: %+v", rs)
	}
	if _, err := RunReminders(bg(), almatyAt(10, 20, 10)); err != nil {
		t.Fatal(err)
	}
	if n := len(mine()); n != 1 {
		t.Fatalf("20:10 run sent again: %d", n)
	}
	// a pause covering today silences it; past 22:00 nothing goes out
	exec(t, `update points_wallets set streak_reminded_on = null, session_pause_from = '2026-11-10', session_pause_until = '2026-11-12' where user_id = $1`, five)
	if _, err := RunReminders(bg(), almatyAt(10, 20, 30)); err != nil {
		t.Fatal(err)
	}
	exec(t, `update points_wallets set session_pause_from = null, session_pause_until = null where user_id = $1`, five)
	if _, err := RunReminders(bg(), almatyAt(10, 22, 0)); err != nil {
		t.Fatal(err)
	}
	if n := len(mine()); n != 1 {
		t.Fatalf("paused / late run sent: %d", n)
	}
	// switch off: nothing at all
	t.Setenv("FEATURE_STREAK", "false")
	if _, err := RunReminders(bg(), almatyAt(10, 20, 40)); err != nil {
		t.Fatal(err)
	}
	if n := len(mine()); n != 1 {
		t.Fatalf("switch off sent: %d", n)
	}
}

func TestStreakMilestoneOnce(t *testing.T) {
	t.Setenv("FEATURE_STREAK", "true")
	uid := newUser(t, "student", "", "")
	now := almatyAt(7, 12, 0)
	exec(t, `insert into points_wallets (user_id, streak_days, streak_best, last_active_day, last_milestone) values ($1, 6, 6, '2026-11-06', 3)`, uid)
	a := grantAt(t, Earn{UserID: uid, Reason: ReasonDailyOpen, RefType: "day", RefID: "2026-11-07"}, now)
	if a.Milestone != 7 {
		t.Fatalf("day 7: %+v", a)
	}
	b := grantAt(t, Earn{UserID: uid, Reason: ReasonQuiz, RefType: "quiz", RefID: "1"}, now)
	if b.Milestone != 0 {
		t.Fatalf("second action the same day celebrated again: %+v", b)
	}
	if n := count(t, `select count(*) from coin_ledger where user_id = $1 and reason = $2`, uid, ReasonMilestone); n != 1 {
		t.Fatalf("milestone rows: %d", n)
	}
	if n := count(t, `select delta from coin_ledger where user_id = $1 and reason = $2`, uid, ReasonMilestone); n != MilestoneCoins[7] {
		t.Fatalf("milestone coins: %d", n)
	}
	balance(t, uid)
	// switch off: no milestone coins
	t.Setenv("FEATURE_STREAK", "false")
	other := newUser(t, "student", "", "")
	exec(t, `insert into points_wallets (user_id, streak_days, streak_best, last_active_day) values ($1, 2, 2, '2026-11-06')`, other)
	if a := grantAt(t, Earn{UserID: other, Reason: ReasonDailyOpen, RefType: "day", RefID: "2026-11-07"}, now); a.Milestone != 0 {
		t.Fatalf("switch off paid a milestone: %+v", a)
	}
}

func TestStreakPauseAndFreezes(t *testing.T) {
	t.Setenv("FEATURE_STREAK", "true")
	uid := newUser(t, "student", "", "")
	exec(t, `insert into points_wallets (user_id, streak_days, streak_best, last_active_day, coins) values ($1, 9, 9, '2026-11-10', 100)`, uid)
	withNow(t, almatyAt(10, 12, 0))

	if w := do(t, "POST", "/api/points/pause", uid, map[string]int{"days": 15}); w.Code != 400 {
		t.Fatalf("15 days: %d", w.Code)
	}
	w := do(t, "POST", "/api/points/pause", uid, map[string]int{"days": 10})
	if w.Code != 200 {
		t.Fatalf("pause: %d %s", w.Code, w.Body)
	}
	s := decode[struct{ Me Summary }](t, w).Me
	if s.Streak.Pause == nil || !s.Streak.Pause.Active || *s.Streak.Pause.From != "2026-11-11" || *s.Streak.Pause.Until != "2026-11-20" || !s.Streak.Pause.Used {
		t.Fatalf("pause state: %+v", s.Streak.Pause)
	}
	if w := do(t, "POST", "/api/points/pause", uid, map[string]int{"days": 3}); w.Code != 409 || errCode(w) != "pause_used" {
		t.Fatalf("second pause: %d %s", w.Code, w.Body)
	}
	// ten days later with no activity the streak is unchanged
	withNow(t, almatyAt(21, 12, 0))
	me := decode[Summary](t, do(t, "GET", "/api/points/me", uid, nil))
	if me.Streak.Days != 9 || me.Streak.Pause.Active {
		t.Fatalf("after the pause: %+v", me.Streak)
	}
	// acting on day 21 continues it without spending a freeze
	if a := grantAt(t, Earn{UserID: uid, Reason: ReasonDailyOpen, RefType: "day", RefID: "2026-11-21"}, almatyAt(21, 12, 0)); a == nil {
		t.Fatal("no award")
	}
	if n := count(t, `select streak_days from points_wallets where user_id = $1`, uid); n != 10 {
		t.Fatalf("streak after the pause: %d", n)
	}

	// two freezes in hand, a third is refused
	for i, want := range []int{200, 200, 409} {
		w := do(t, "POST", "/api/points/freeze", uid, map[string]string{"key": "freezekey" + itoa(int64(i))})
		if w.Code != want {
			t.Fatalf("freeze %d: %d %s", i+1, w.Code, w.Body)
		}
	}
	if n := count(t, `select freezes from points_wallets where user_id = $1`, uid); n != 2 {
		t.Fatalf("freezes: %d", n)
	}

	// reminder settings: on/off and a time inside 19:00–21:00 only
	if w := do(t, "PATCH", "/api/points/streak", uid, map[string]any{"time": "22:30"}); w.Code != 400 {
		t.Fatalf("22:30: %d", w.Code)
	}
	w = do(t, "PATCH", "/api/points/streak", uid, map[string]any{"reminder": false, "time": "19:30"})
	if w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	if r := decode[struct{ Me Summary }](t, w).Me.Streak.Reminder; r == nil || r.On || r.Time != "19:30" {
		t.Fatalf("reminder settings: %+v", r)
	}

	// the switch off: the routes are gone and one freeze is the limit again
	t.Setenv("FEATURE_STREAK", "false")
	if w := do(t, "POST", "/api/points/pause", uid, map[string]int{"days": 1}); w.Code != 404 {
		t.Fatalf("switch off pause: %d", w.Code)
	}
	if me := decode[Summary](t, do(t, "GET", "/api/points/me", uid, nil)); me.Streak.MaxFreezes != 1 || me.Streak.Pause != nil {
		t.Fatalf("switch off summary: %+v", me.Streak)
	}
}

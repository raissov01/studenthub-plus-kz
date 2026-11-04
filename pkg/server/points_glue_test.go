package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

type pointsAward struct {
	XP        int  `json:"xp"`
	Coins     int  `json:"coins"`
	Duplicate bool `json:"duplicate"`
}

// The quiz and events modules pay through the glue hooks, inside their own transactions.
func TestPointsGlueQuizAttempt(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	owner, player := launchUser(t, "student"), launchUser(t, "student")
	quiz := map[string]any{"title": "Points glue quiz", "visibility": "public", "timePerQuestion": 20,
		"questions": []map[string]any{
			{"type": "mcq", "prompt": "2+2?", "options": []string{"3", "4"}, "correctIndex": 1},
			{"type": "true_false", "prompt": "Earth is round", "correctBool": true},
			{"type": "true_false", "prompt": "Water is dry", "correctBool": false},
		}}
	body, _ := json.Marshal(quiz)
	w := launchRequest(t, "POST", "/api/quiz/quizzes", string(body), owner)
	if w.Code != 201 {
		t.Fatalf("create quiz: %d %s", w.Code, w.Body)
	}
	var created struct{ ID int64 }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	attempt := func(uid int64) *pointsAward {
		w := launchRequest(t, "POST", fmt.Sprintf("/api/quiz/quizzes/%d/attempts", created.ID), `{"answers":[]}`, uid)
		if w.Code != 201 {
			t.Fatalf("attempt: %d %s", w.Code, w.Body)
		}
		var res struct{ Points *pointsAward }
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return res.Points
	}
	if p := attempt(player); p == nil || p.XP != 10 || p.Coins != 0 { // 0 %: XP only
		t.Fatalf("first attempt: %+v", p)
	}
	if p := attempt(player); p == nil || !p.Duplicate || p.XP != 0 {
		t.Fatalf("second attempt paid again: %+v", p)
	}
	if p := attempt(owner); p != nil {
		t.Fatalf("own quiz paid: %+v", p)
	}
	if launchCount(t, `select count(*) from xp_events where user_id = $1`, owner) != 0 {
		t.Fatal("owner earned XP on own quiz")
	}
	t.Setenv("FEATURE_POINTS", "false")
	other := launchUser(t, "student")
	if p := attempt(other); p != nil {
		t.Fatalf("paid while switched off: %+v", p)
	}
}

func TestPointsGlueCheckinAndRevoke(t *testing.T) {
	allFeatures(t)
	testdb.Need(t)
	admin, student := launchUser(t, "admin"), launchUser(t, "student")
	var event int64
	event = int64(launchCount(t, `insert into events (title_kk, cat, starts_at, ends_at, university, status)
		values ('Points event', 'other', now() - interval '5 minutes', now() + interval '1 hour', 'LAUNCH', 'published') returning id`))
	launchExec(t, `insert into event_regs (event_id, user_id, ticket) values ($1, $2, 'points-glue-ticket')`, event, student)
	checkin := func() *pointsAward {
		w := launchRequest(t, "POST", fmt.Sprintf("/api/events/%d/checkin/manual", event), fmt.Sprintf(`{"user_id":%d}`, student), admin)
		if w.Code != 200 {
			t.Fatalf("manual check-in: %d %s", w.Code, w.Body)
		}
		var res struct{ Points *pointsAward }
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return res.Points
	}
	if p := checkin(); p == nil || p.XP != 30 || p.Coins != 15 {
		t.Fatalf("check-in award: %+v", p)
	}
	if launchCount(t, `select coins from points_wallets where user_id = $1`, student) != 15 {
		t.Fatal("wallet not credited")
	}
	w := launchRequest(t, "POST", fmt.Sprintf("/api/events/%d/checkin/revoke", event), fmt.Sprintf(`{"user_id":%d}`, student), admin)
	if w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if launchCount(t, `select coins + xp from points_wallets where user_id = $1`, student) != 0 {
		t.Fatal("revoked check-in kept its award")
	}
	if p := checkin(); p == nil || !p.Duplicate {
		t.Fatalf("re-check-in after revoke paid again: %+v", p)
	}
	if launchCount(t, `select coalesce(sum(delta), 0) from coin_ledger where user_id = $1`, student) !=
		launchCount(t, `select coins from points_wallets where user_id = $1`, student) {
		t.Fatal("ledger and cached balance disagree")
	}
}

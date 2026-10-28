package quiz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type wsMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type client struct {
	t  *testing.T
	ws *websocket.Conn
}

func dial(t *testing.T, srv *httptest.Server, token, origin string) (*client, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/quiz/live/ws?token=" + token
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	ws, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return nil, resp, err
	}
	t.Cleanup(func() { ws.CloseNow() })
	return &client{t, ws}, resp, nil
}

func (c *client) send(v any) {
	c.t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// until reads messages until one of type typ arrives (others are skipped).
func (c *client) until(typ string) map[string]any {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := c.ws.Read(ctx)
		if err != nil {
			c.t.Fatalf("waiting for %s: %v", typ, err)
		}
		var m wsMsg
		json.Unmarshal(b, &m)
		if m.Type == typ {
			var d map[string]any
			json.Unmarshal(m.Data, &d)
			return d
		}
	}
}

func liveQuiz() map[string]any {
	return map[string]any{
		"title": "Live", "visibility": "private", "timePerQuestion": 30,
		"questions": []map[string]any{
			{"type": "mcq", "prompt": "Q1", "options": []string{"a", "b", "c"}, "correctIndex": 2},
			{"type": "true_false", "prompt": "Q2", "correctBool": false, "timeLimit": 10},
			{"type": "fill_blank", "prompt": "Q3", "answers": []string{"Astana"}},
		},
	}
}

func TestLiveGameEndToEnd(t *testing.T) {
	oldUnit, oldGrace := liveUnit, lateGrace
	liveUnit, lateGrace = 50*time.Millisecond, 0 // Q2: 10 → 500ms
	t.Cleanup(func() { liveUnit, lateGrace = oldUnit, oldGrace })
	t.Setenv("QUIZ_LIVE_MAX_PLAYERS", "2")
	srv := httptest.NewServer(handler())
	defer srv.Close()
	const origin = "https://studenthub-plus-kz.vercel.app"

	host := newUser(t)
	qid, _ := createQuizT(t, host, liveQuiz())
	if w := do(t, "POST", "/api/quiz/live/sessions", newUser(t), map[string]any{"quizId": qid}); w.Code != 404 {
		t.Fatalf("hosting someone's private quiz: %d", w.Code)
	}
	w := do(t, "POST", "/api/quiz/live/sessions", host, map[string]any{"quizId": qid})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	sess := decode[struct {
		Code      string `json:"code"`
		SessionID int64  `json:"sessionId"`
		HostToken string `json:"hostToken"`
		WsURL     string `json:"wsUrl"`
	}](t, w)
	if len(sess.Code) != 6 || !strings.HasSuffix(sess.WsURL, "/api/quiz/live/ws") {
		t.Fatalf("session: %+v", sess)
	}
	info := decode[map[string]any](t, do(t, "GET", "/api/quiz/live/sessions/"+sess.Code, 0, nil))
	if info["status"] != "lobby" || info["questionCount"].(float64) != 3 {
		t.Fatalf("info: %v", info)
	}

	join := func(nick string, uid int64) (string, string) {
		w := do(t, "POST", "/api/quiz/live/join", uid, map[string]any{"code": sess.Code, "nickname": nick})
		if w.Code != 200 {
			t.Fatalf("join %s: %d %s", nick, w.Code, w.Body)
		}
		j := decode[map[string]any](t, w)
		return j["playerId"].(string), j["token"].(string)
	}
	signedIn := newUser(t)
	p1ID, p1Tok := join("Алма", signedIn) // signed in: result linked to the account
	p2ID, p2Tok := join("Bota", 0)        // anonymous
	if w := do(t, "POST", "/api/quiz/live/join", 0, map[string]any{"code": sess.Code, "nickname": "Third"}); w.Code != 409 || errCode(w) != "live_full" {
		t.Fatalf("cap: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/quiz/live/join", 0, map[string]any{"code": "abcdef", "nickname": "Nope"}); w.Code != 404 || errCode(w) != "live_not_found" {
		t.Fatalf("unknown code: %d", w.Code)
	}

	// handshake checks
	if _, resp, err := dial(t, srv, p1Tok, "https://evil.example"); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("foreign origin accepted: %v", err)
	}
	if _, resp, err := dial(t, srv, p1Tok, ""); err == nil || resp.StatusCode != 403 {
		t.Fatalf("missing origin accepted: %v", err)
	}
	if _, resp, err := dial(t, srv, p1Tok+"x", origin); err == nil || resp.StatusCode != 401 {
		t.Fatalf("forged token accepted: %v", err)
	}
	hc, _, err := dial(t, srv, sess.HostToken, origin)
	if err != nil {
		t.Fatal(err)
	}
	if hello := hc.until("hello"); hello["role"] != "host" || hello["status"] != "lobby" {
		t.Fatalf("host hello: %v", hello)
	}
	c1, _, err := dial(t, srv, p1Tok, origin)
	if err != nil {
		t.Fatal(err)
	}
	c2, _, err := dial(t, srv, p2Tok, origin)
	if err != nil {
		t.Fatal(err)
	}
	c1.until("hello")
	c2.until("hello")
	for {
		l := hc.until("lobby")
		if l["count"].(float64) == 2 && strings.Count(fmt.Sprint(l["players"]), "online:true") == 2 {
			break
		}
	}

	// players cannot drive the game
	c1.send(map[string]any{"type": "start"})
	if e := c1.until("error"); e["code"] != "unknown_type" {
		t.Fatalf("player start: %v", e)
	}

	// Q1: p1 right, p2 wrong; all answered → reveal at once
	hc.send(map[string]any{"type": "start"})
	q := c1.until("question")
	c2.until("question")
	if q["index"].(float64) != 0 || q["deadline"].(float64) <= q["serverTime"].(float64) || strings.Contains(fmt.Sprint(q["question"]), "correct") {
		t.Fatalf("question: %v", q)
	}
	c1.send(map[string]any{"type": "answer", "index": 0, "answer": 2, "isCorrect": true})
	c1.until("answer_ack")
	c1.send(map[string]any{"type": "answer", "index": 0, "answer": 1})
	if r := c1.until("answer_rejected"); r["reason"] != "duplicate" {
		t.Fatalf("duplicate: %v", r)
	}
	if p := hc.until("progress"); p["answered"].(float64) != 1 {
		t.Fatalf("progress: %v", p)
	}
	c2.send(map[string]any{"type": "answer", "index": 0, "answer": 0, "points": 5000, "isCorrect": true})
	r1 := c1.until("reveal")
	r2 := c2.until("reveal")
	you1, you2 := r1["you"].(map[string]any), r2["you"].(map[string]any)
	if you1["isCorrect"] != true || you1["rank"].(float64) != 1 || you2["isCorrect"] != false || you2["points"].(float64) != 0 || r1["correct"].(float64) != 2 {
		t.Fatalf("reveal: %v / %v", r1, r2)
	}
	if lb := r1["leaderboard"].([]any); lb[0].(map[string]any)["id"] != p1ID {
		t.Fatalf("leaderboard: %v", lb)
	}
	hr := hc.until("reveal")
	if hr["correctCount"].(float64) != 1 || hr["answered"].(float64) != 2 {
		t.Fatalf("host reveal: %v", hr)
	}

	// Q2 (500ms): p2 right, p1 waits past the server deadline → late
	hc.send(map[string]any{"type": "next"})
	c1.until("question")
	c2.until("question")
	c2.send(map[string]any{"type": "answer", "index": 1, "answer": false})
	c2.until("answer_ack")
	c1.until("reveal") // the server timer ends the question
	c1.send(map[string]any{"type": "answer", "index": 1, "answer": false})
	if r := c1.until("answer_rejected"); r["reason"] != "late" {
		t.Fatalf("late answer: %v", r)
	}
	// answers for a question that is not running
	c1.send(map[string]any{"type": "answer", "index": 2, "answer": "Astana"})
	if r := c1.until("answer_rejected"); r["reason"] != "not_active" {
		t.Fatalf("future answer: %v", r)
	}

	// Q3: p1 right (case-insensitive), p2 wrong
	hc.send(map[string]any{"type": "next"})
	c1.until("question")
	c2.until("question")
	c1.send(map[string]any{"type": "answer", "index": 2, "answer": " astana "})
	c2.send(map[string]any{"type": "answer", "index": 2, "answer": "Almaty"})
	if r := c2.until("reveal"); r["last"] != true {
		t.Fatalf("last flag: %v", r)
	}
	hc.send(map[string]any{"type": "next"})
	e1 := c1.until("ended")
	e2 := c2.until("ended")
	lb := e1["leaderboard"].([]any)
	if len(lb) != 2 || lb[0].(map[string]any)["id"] != p1ID || lb[1].(map[string]any)["id"] != p2ID ||
		e1["you"].(map[string]any)["rank"].(float64) != 1 || e2["you"].(map[string]any)["rank"].(float64) != 2 ||
		lb[0].(map[string]any)["correct"].(float64) != 2 {
		t.Fatalf("final: %v", e1)
	}
	hc.until("ended")
	// results persisted
	deadline := time.Now().Add(3 * time.Second)
	for count(t, "select count(*) from quiz_live_results where session_id = $1", sess.SessionID) != 2 {
		if time.Now().After(deadline) {
			t.Fatal("results not persisted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count(t, "select count(*) from quiz_live_results where session_id = $1 and rank = 1 and user_id = $2", sess.SessionID, signedIn) != 1 ||
		count(t, "select count(*) from quiz_live_sessions where id = $1 and status = 'finished' and players = 2", sess.SessionID) != 1 {
		t.Fatal("persisted results wrong")
	}
	h := decode[struct{ Sessions []map[string]any }](t, do(t, "GET", "/api/quiz/live/history", host, nil))
	if len(h.Sessions) != 1 || len(h.Sessions[0]["results"].([]any)) != 2 {
		t.Fatalf("history: %v", h)
	}
	// finished game: joining and tokens stop working
	if w := do(t, "POST", "/api/quiz/live/join", 0, map[string]any{"code": sess.Code, "nickname": "Late"}); w.Code != 404 {
		t.Fatalf("join finished: %d", w.Code)
	}
}

func TestLiveRateLimitAndKick(t *testing.T) {
	srv := httptest.NewServer(handler())
	defer srv.Close()
	const origin = "http://localhost"
	host := newUser(t)
	qid, _ := createQuizT(t, host, liveQuiz())
	sess := decode[struct {
		Code      string `json:"code"`
		HostToken string `json:"hostToken"`
	}](t, do(t, "POST", "/api/quiz/live/sessions", host, map[string]any{"quizId": qid}))
	j := decode[map[string]any](t, do(t, "POST", "/api/quiz/live/join", 0, map[string]any{"code": sess.Code, "nickname": "Spammer"}))
	c, _, err := dial(t, srv, j["token"].(string), origin)
	if err != nil {
		t.Fatal(err)
	}
	c.until("hello")
	for range 60 {
		b, _ := json.Marshal(map[string]string{"type": "ping"})
		if err := c.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
			break
		}
	}
	if e := c.until("error"); e["code"] != "rate_limited" {
		t.Fatalf("no rate limit: %v", e)
	}
	// after too many violations the server closes the socket
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var closeErr error
	for closeErr == nil {
		_, _, closeErr = c.ws.Read(ctx)
	}
	if websocket.CloseStatus(closeErr) != websocket.StatusPolicyViolation {
		t.Fatalf("socket not closed for flooding: %v", closeErr)
	}
	// rejoin with the key restores the same player; kicked players cannot come back
	j2 := decode[map[string]any](t, do(t, "POST", "/api/quiz/live/join", 0, map[string]any{"code": sess.Code, "rejoinKey": j["rejoinKey"]}))
	if j2["playerId"] != j["playerId"] {
		t.Fatalf("rejoin: %v", j2)
	}
	if w := do(t, "POST", "/api/quiz/live/join", 0, map[string]any{"code": sess.Code, "nickname": "spammer"}); w.Code != 409 {
		t.Fatalf("nickname reuse: %d", w.Code)
	}
	hc, _, _ := dial(t, srv, sess.HostToken, origin)
	hc.until("hello")
	pc, _, _ := dial(t, srv, j2["token"].(string), origin)
	pc.until("hello")
	hc.send(map[string]any{"type": "kick", "playerId": j["playerId"]})
	pc.until("kicked")
	if _, resp, err := dial(t, srv, j2["token"].(string), origin); err == nil || resp.StatusCode != 404 {
		t.Fatalf("kicked player reconnected: %v", err)
	}
	// another student cannot get a host token
	if w := do(t, "POST", "/api/quiz/live/sessions/"+sess.Code+"/host-token", newUser(t), nil); w.Code != 403 {
		t.Fatalf("foreign host token: %d", w.Code)
	}
	// an expired token is refused
	old := tokenTTL
	tokenTTL = -time.Second
	expired, _, _ := hostToken(sess.Code)
	tokenTTL = old
	if _, resp, err := dial(t, srv, expired, origin); err == nil || resp.StatusCode != 401 {
		t.Fatalf("expired token: %v", err)
	}
	// idle sweep: a lobby left alone is closed and forgotten
	rm := theHub.get(sess.Code)
	rm.mu.Lock()
	rm.activity = time.Now().Add(-2 * lobbyIdle)
	rm.mu.Unlock()
	theHub.sweep(time.Now())
	hc.until("ended")
	theHub.sweep(time.Now().Add(2 * finishedKeep))
	if theHub.get(sess.Code) != nil {
		t.Fatal("idle room not removed")
	}
}

package quiz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

// Live mode (Kahoot-style). Rooms live in this process's memory (the API is one Go process
// on the VPS); session headers and final results are written to Postgres. The server owns
// the clock: it sends each question with an absolute deadline, grades answers itself and
// rejects anything that arrives after the deadline.
//
// The site proxies /api/* through Vercel rewrites, which do not carry WebSockets, so browsers
// connect straight to the API origin (QUIZ_LIVE_WS_URL). Cookies do not reach that origin:
// the socket authenticates with a short-lived HMAC join token fetched over normal HTTPS, and
// the Origin header must be one of ALLOWED_HOSTS.

// Tunables; tests shrink the time unit.
var (
	liveUnit     = time.Second            // one "second" of question time
	lateGrace    = 300 * time.Millisecond // network slack after the deadline
	tokenTTL     = 2 * time.Minute
	janitorEvery = 30 * time.Second
	lobbyIdle    = 30 * time.Minute
	activeIdle   = 5 * time.Minute // no connected client at all
	finishedKeep = 2 * time.Minute
	maxRoomAge   = 3 * time.Hour
	pingEvery    = 25 * time.Second
)

func maxPlayers() int   { return config.Int("QUIZ_LIVE_MAX_PLAYERS", 100) }
func maxRooms() int     { return config.Int("QUIZ_LIVE_MAX_SESSIONS", 50) }
func maxHostRooms() int { return 2 }

const (
	msgBurst      = 10  // token bucket per connection
	msgPerSecond  = 5.0 // refill
	maxViolations = 20  // rate-limit hits before the socket is closed
	sendBuffer    = 64
)

const defaultHosts = "studenthub-plus-kz.vercel.app,studenthub-platform.vercel.app,localhost,127.0.0.1"

// --- join tokens -----------------------------------------------------------------------

type liveClaims struct {
	Code string `json:"c"`
	Role string `json:"r"` // host | player
	PID  string `json:"p,omitempty"`
	Exp  int64  `json:"e"`
}

func tokenKey() ([]byte, error) {
	s := config.Str("SESSION_SECRET", "")
	if len(s) < 32 {
		return nil, errors.New("SESSION_SECRET missing")
	}
	m := hmac.New(sha256.New, []byte(s))
	m.Write([]byte("quiz-live-token-v1")) // domain separation from session cookies
	return m.Sum(nil), nil
}

func signLive(c liveClaims) (string, error) {
	key, err := tokenKey()
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(body)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

func parseLive(tok string, now time.Time) (liveClaims, bool) {
	key, err := tokenKey()
	if err != nil || len(tok) > 512 {
		return liveClaims{}, false
	}
	p, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return liveClaims{}, false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(p))
	if err != nil || !hmac.Equal(got, m.Sum(nil)) {
		return liveClaims{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(p)
	var c liveClaims
	if err != nil || json.Unmarshal(body, &c) != nil || now.Unix() >= c.Exp || (c.Role != "host" && c.Role != "player") {
		return liveClaims{}, false
	}
	return c, true
}

// --- hub and rooms ---------------------------------------------------------------------

type hub struct {
	mu      sync.Mutex
	rooms   map[string]*room
	started bool
}

var theHub = &hub{rooms: map[string]*room{}}

type player struct {
	ID       string
	Nickname string
	UserID   *int64
	rejoin   [32]byte
	score    int
	correct  int
	streak   int
	fastMs   int64 // sum of answer times of correct answers (tie-break)
	joined   int   // join order
	kicked   bool
	conn     *conn
	// current question
	answered bool
	credit   float64
	points   int
	given    json.RawMessage
}

type room struct {
	mu        sync.Mutex
	code      string
	sessionID int64
	quizID    int64
	hostID    int64
	title     string
	timePer   int
	questions []Question
	views     []PlayQuestion
	status    string // lobby | question | reveal | finished
	current   int
	qStart    time.Time
	deadline  time.Time
	timer     *time.Timer
	players   map[string]*player
	joinSeq   int
	host      *conn
	created   time.Time
	activity  time.Time
	finished  time.Time
	stats     []map[string]any
}

type conn struct {
	ws         *websocket.Conn
	send       chan []byte
	role       string
	pid        string
	tokens     float64
	last       time.Time
	violations int
	once       sync.Once
	done       chan struct{}
	endCode    websocket.StatusCode // for closeAfterFlush
	endReason  string
}

// closeAfterFlush lets the writer send everything already queued, then close.
func (c *conn) closeAfterFlush(code websocket.StatusCode, reason string) {
	if c == nil {
		return
	}
	select {
	case <-c.done:
		return
	default:
	}
	c.endCode, c.endReason = code, reason
	select {
	case c.send <- nil:
	default:
		c.close(code, reason)
	}
}

func (c *conn) close(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		close(c.done)
		go c.ws.Close(code, reason)
	})
}

// push queues a message; a client that cannot keep up is disconnected.
func (c *conn) push(b []byte) {
	if c == nil {
		return
	}
	select {
	case <-c.done:
	case c.send <- b:
	default:
		c.close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

// allow is a token bucket: msgBurst messages at once, msgPerSecond sustained.
func (c *conn) allow(now time.Time) bool {
	if c.last.IsZero() {
		c.tokens, c.last = msgBurst, now
	}
	c.tokens = math.Min(msgBurst, c.tokens+now.Sub(c.last).Seconds()*msgPerSecond)
	c.last = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

func out(typ string, data any) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "data": data})
	return b
}

func (rm *room) broadcast(b []byte) {
	rm.host.push(b)
	for _, p := range rm.players {
		if !p.kicked {
			p.conn.push(b)
		}
	}
}

func (rm *room) online() int {
	n := 0
	for _, p := range rm.players {
		if !p.kicked && p.conn != nil {
			n++
		}
	}
	return n
}

func (rm *room) active() []*player {
	var ps []*player
	for _, p := range rm.players {
		if !p.kicked {
			ps = append(ps, p)
		}
	}
	return ps
}

type lbRow struct {
	Rank     int    `json:"rank"`
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Score    int    `json:"score"`
	Correct  int    `json:"correct"`
}

// ranking: score desc, then faster correct answers, then join order.
func (rm *room) ranking() []*player {
	ps := rm.active()
	sort.Slice(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.fastMs != b.fastMs {
			return a.fastMs < b.fastMs
		}
		return a.joined < b.joined
	})
	return ps
}

func (rm *room) leaderboard(n int) ([]lbRow, map[string]int) {
	ranks := map[string]int{}
	rows := []lbRow{}
	for i, p := range rm.ranking() {
		ranks[p.ID] = i + 1
		if i < n {
			rows = append(rows, lbRow{i + 1, p.ID, p.Nickname, p.score, p.correct})
		}
	}
	return rows, ranks
}

func (rm *room) lobbyMsg() []byte {
	type pl struct {
		ID       string `json:"id"`
		Nickname string `json:"nickname"`
		Online   bool   `json:"online"`
	}
	list := []pl{}
	for _, p := range rm.active() {
		list = append(list, pl{p.ID, p.Nickname, p.conn != nil})
	}
	sort.Slice(list, func(i, j int) bool { return rm.players[list[i].ID].joined < rm.players[list[j].ID].joined })
	return out("lobby", map[string]any{"players": list, "count": len(list), "max": maxPlayers()})
}

func (rm *room) timeLimit(i int) time.Duration {
	s := rm.views[i].TimeLimit
	if s <= 0 {
		s = rm.timePer
	}
	return time.Duration(s) * liveUnit
}

func (rm *room) questionMsg() []byte {
	v := rm.views[rm.current]
	return out("question", map[string]any{
		"index": rm.current, "total": len(rm.views), "question": v,
		"deadline": rm.deadline.UnixMilli(), "serverTime": time.Now().UnixMilli(),
		"timeLimitMs": rm.deadline.Sub(rm.qStart).Milliseconds(),
	})
}

// state is what a (re)connecting client gets first.
func (rm *room) stateFor(c *conn) []byte {
	d := map[string]any{"code": rm.code, "status": rm.status, "title": rm.title, "total": len(rm.views),
		"index": rm.current, "role": c.role}
	if c.role == "player" {
		if p := rm.players[c.pid]; p != nil {
			_, ranks := rm.leaderboard(0)
			d["you"] = map[string]any{"id": p.ID, "nickname": p.Nickname, "score": p.score, "rank": ranks[p.ID],
				"answered": rm.status == "question" && p.answered}
		}
	}
	return out("hello", d)
}

// --- game flow (all called with rm.mu held) -------------------------------------------

func (rm *room) start() {
	if rm.status != "lobby" || len(rm.active()) == 0 {
		return
	}
	go setSessionStatus(rm.sessionID, "active")
	rm.show(0)
}

func (rm *room) show(i int) {
	rm.current, rm.status = i, "question"
	rm.qStart = time.Now()
	rm.deadline = rm.qStart.Add(rm.timeLimit(i))
	for _, p := range rm.players {
		p.answered, p.credit, p.points, p.given = false, 0, 0, nil
	}
	rm.broadcast(rm.questionMsg())
	idx := i
	rm.timer = time.AfterFunc(time.Until(rm.deadline)+lateGrace, func() {
		rm.mu.Lock()
		defer rm.mu.Unlock()
		if rm.status == "question" && rm.current == idx {
			rm.reveal()
		}
	})
}

func (rm *room) answer(p *player, index int, raw json.RawMessage) {
	reject := func(reason string) {
		p.conn.push(out("answer_rejected", map[string]any{"index": index, "reason": reason}))
	}
	now := time.Now()
	switch {
	case rm.status != "question" || index != rm.current:
		if index == rm.current && (rm.status == "reveal" || rm.status == "finished") {
			reject("late")
		} else {
			reject("not_active")
		}
		return
	case now.After(rm.deadline.Add(lateGrace)):
		reject("late")
		return
	case p.answered:
		reject("duplicate")
		return
	}
	q := rm.questions[rm.current]
	credit, _, stored := Grade(q, raw)
	if stored == nil {
		reject("bad_answer")
		return
	}
	elapsed := min(now.Sub(rm.qStart), rm.deadline.Sub(rm.qStart))
	limit := rm.deadline.Sub(rm.qStart)
	points := 0
	if Graded(q) && credit > 0 {
		speed := 1 - float64(elapsed)/float64(limit)/2 // 1.0 instant … 0.5 at the deadline
		points = int(math.Round(1000 * credit * speed))
	}
	p.answered, p.credit, p.points, p.given = true, credit, points, stored
	p.score += points
	if Graded(q) && credit == 1 {
		p.correct++
		p.streak++
		p.fastMs += elapsed.Milliseconds()
	} else if Graded(q) {
		p.streak = 0
	}
	p.conn.push(out("answer_ack", map[string]any{"index": index}))
	answered, total := 0, 0
	for _, x := range rm.active() {
		total++
		if x.answered {
			answered++
		}
	}
	rm.host.push(out("progress", map[string]any{"index": index, "answered": answered, "total": total}))
	if answered == total {
		rm.reveal()
	}
}

func (rm *room) reveal() {
	if rm.status != "question" {
		return
	}
	if rm.timer != nil {
		rm.timer.Stop()
	}
	rm.status = "reveal"
	q := rm.questions[rm.current]
	var key any
	if Graded(q) {
		key = RevealKey(q)
	}
	dist := map[string]int{}
	answered, right := 0, 0
	for _, p := range rm.active() {
		if p.given != nil {
			answered++
			dist[string(p.given)]++
		}
		if p.credit == 1 && Graded(q) {
			right++
		}
	}
	rm.stats = append(rm.stats, map[string]any{"position": rm.current + 1, "answered": answered, "correct": right})
	top, ranks := rm.leaderboard(10)
	base := map[string]any{"index": rm.current, "correct": key, "explanation": q.Explanation,
		"answered": answered, "correctCount": right, "distribution": dist, "leaderboard": top,
		"last": rm.current == len(rm.questions)-1}
	rm.host.push(out("reveal", base))
	for _, p := range rm.active() {
		d := map[string]any{"index": rm.current, "correct": key, "explanation": q.Explanation, "leaderboard": top,
			"last": base["last"],
			"you": map[string]any{"answered": p.answered, "credit": p.credit, "isCorrect": p.credit == 1, "points": p.points,
				"score": p.score, "rank": ranks[p.ID], "streak": p.streak}}
		p.conn.push(out("reveal", d))
	}
}

func (rm *room) next() {
	switch rm.status {
	case "question":
		rm.reveal()
	case "reveal":
		if rm.current+1 < len(rm.questions) {
			rm.show(rm.current + 1)
		} else {
			rm.finish("completed")
		}
	}
}

func (rm *room) finish(reason string) {
	if rm.status == "finished" {
		return
	}
	if rm.timer != nil {
		rm.timer.Stop()
	}
	completed := rm.status != "lobby" && reason == "completed"
	rm.status, rm.finished = "finished", time.Now()
	top, ranks := rm.leaderboard(50)
	rm.host.push(out("ended", map[string]any{"reason": reason, "leaderboard": top}))
	for _, p := range rm.active() {
		p.conn.push(out("ended", map[string]any{"reason": reason, "leaderboard": top,
			"you": map[string]any{"rank": ranks[p.ID], "score": p.score, "correct": p.correct}}))
	}
	// persist outside the lock
	var results []lbRow
	var users []*int64
	for i, p := range rm.ranking() {
		results = append(results, lbRow{i + 1, p.ID, p.Nickname, p.score, p.correct})
		users = append(users, p.UserID)
	}
	status := "finished"
	if !completed && rm.stats == nil {
		status = "abandoned"
	}
	go persistResults(rm.sessionID, status, rm.stats, results, users)
}

// --- persistence -----------------------------------------------------------------------

func setSessionStatus(id int64, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update quiz_live_sessions set status = $2,
			started_at = case when $2 = 'active' then now() else started_at end where id = $1`, id, status)
		return err
	})
	if err != nil {
		log.Printf("quiz: live status: %v", err)
	}
}

func persistResults(id int64, status string, stats []map[string]any, rows []lbRow, users []*int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if stats == nil {
		stats = []map[string]any{}
	}
	st, _ := json.Marshal(stats)
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update quiz_live_sessions set status = $2, finished_at = now(), players = $3,
			question_stats = $4 where id = $1`, id, status, len(rows), string(st)); err != nil {
			return err
		}
		for i, r := range rows {
			if _, err := tx.Exec(ctx, `insert into quiz_live_results (session_id, rank, nickname, user_id, score, correct)
				values ($1, $2, $3, $4, $5, $6)`, id, r.Rank, r.Nickname, users[i], r.Score, r.Correct); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, db.ErrNoDB) {
		log.Printf("quiz: live results: %v", err)
	}
}

// --- janitor ---------------------------------------------------------------------------

func (h *hub) ensureJanitor() {
	if h.started {
		return
	}
	h.started = true
	// rooms from a previous process are gone from memory
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update quiz_live_sessions set status = 'abandoned', finished_at = now()
				where status in ('lobby', 'active') and created_at < now() - interval '1 minute'`)
			return err
		})
	}()
	go func() {
		for {
			time.Sleep(janitorEvery)
			h.sweep(time.Now())
		}
	}()
}

// sweep closes idle rooms and forgets finished ones.
func (h *hub) sweep(now time.Time) {
	h.mu.Lock()
	rooms := make([]*room, 0, len(h.rooms))
	for _, rm := range h.rooms {
		rooms = append(rooms, rm)
	}
	h.mu.Unlock()
	for _, rm := range rooms {
		rm.mu.Lock()
		drop := false
		switch {
		case rm.status == "finished":
			drop = now.Sub(rm.finished) > finishedKeep
		case now.Sub(rm.created) > maxRoomAge,
			rm.status == "lobby" && now.Sub(rm.activity) > lobbyIdle,
			rm.status != "lobby" && rm.host == nil && rm.online() == 0 && now.Sub(rm.activity) > activeIdle:
			rm.finish("idle")
		}
		if drop {
			rm.host.closeIf()
			for _, p := range rm.players {
				p.conn.closeIf()
			}
		}
		rm.mu.Unlock()
		if drop {
			h.mu.Lock()
			if h.rooms[rm.code] == rm {
				delete(h.rooms, rm.code)
			}
			h.mu.Unlock()
		}
	}
}

func (c *conn) closeIf() { c.closeAfterFlush(websocket.StatusNormalClosure, "ended") }

func (h *hub) get(code string) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[code]
}

func closeLiveWhere(match func(*room) bool) {
	theHub.mu.Lock()
	var rs []*room
	for _, rm := range theHub.rooms {
		if match(rm) {
			rs = append(rs, rm)
		}
	}
	theHub.mu.Unlock()
	for _, rm := range rs {
		rm.mu.Lock()
		rm.finish("closed")
		rm.mu.Unlock()
	}
}

func closeLiveForQuiz(quizID int64) { closeLiveWhere(func(r *room) bool { return r.quizID == quizID }) }
func closeLiveForHost(uid int64)    { closeLiveWhere(func(r *room) bool { return r.hostID == uid }) }

// --- HTTP ------------------------------------------------------------------------------

func wsURL(r *http.Request) string {
	if u := config.Str("QUIZ_LIVE_WS_URL", ""); u != "" {
		return u
	}
	scheme := "wss"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "ws"
	}
	return scheme + "://" + r.Host + "/api/quiz/live/ws"
}

// liveCreate: POST /api/quiz/live/sessions {quizId, share?} → 201 {code, sessionId, hostToken,
// expiresAt, wsUrl}. The host must own the quiz or be able to play it (public / share code).
func liveCreate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		QuizID int64  `json:"quizId"`
		Share  string `json:"share"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || body.QuizID <= 0 {
		bad(w, r)
		return
	}
	if !needConsent(w, r, u) {
		return
	}
	ctx := r.Context()
	m, err := loadMeta(ctx, body.QuizID, u.ID)
	if err != nil || !canPlay(m, u.ID, normCode(body.Share)) {
		notFound(w, r)
		return
	}
	var qs []Question
	var views []PlayQuestion
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if qs, err = loadFull(ctx, tx, m.ID); err != nil {
			return err
		}
		views, err = loadPlay(ctx, tx, m, nil)
		return err
	}); err != nil {
		internal(w, r, "live load", err)
		return
	}
	if len(qs) == 0 || len(qs) != len(views) {
		fail(w, r, "no_questions", http.StatusBadRequest)
		return
	}
	theHub.mu.Lock()
	theHub.ensureJanitor()
	mine := 0
	for _, rm := range theHub.rooms {
		rm.mu.Lock()
		if rm.hostID == u.ID && rm.status != "finished" {
			mine++
		}
		rm.mu.Unlock()
	}
	if len(theHub.rooms) >= maxRooms() || mine >= maxHostRooms() {
		theHub.mu.Unlock()
		fail(w, r, "live_busy", http.StatusServiceUnavailable)
		return
	}
	var code string
	for range 20 {
		c := randomCode(6, "0123456789")
		if theHub.rooms[c] == nil {
			code = c
			break
		}
	}
	if code == "" {
		theHub.mu.Unlock()
		fail(w, r, "live_busy", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	rm := &room{code: code, quizID: m.ID, hostID: u.ID, title: m.Title, timePer: m.TimePerQuestion, questions: qs,
		views: views, status: "lobby", players: map[string]*player{}, created: now, activity: now}
	theHub.rooms[code] = rm // reserve the code
	theHub.mu.Unlock()
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		// a stale open row with this code (crashed process) is closed first
		if _, err := tx.Exec(ctx, `update quiz_live_sessions set status = 'abandoned', finished_at = now()
			where code = $1 and status in ('lobby', 'active')`, code); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `insert into quiz_live_sessions (code, quiz_id, host_id) values ($1, $2, $3) returning id`,
			code, m.ID, u.ID).Scan(&rm.sessionID)
	})
	if err != nil {
		theHub.mu.Lock()
		delete(theHub.rooms, code)
		theHub.mu.Unlock()
		internal(w, r, "live create", err)
		return
	}
	tok, exp, err := hostToken(code)
	if err != nil {
		internal(w, r, "live token", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"code": code, "sessionId": rm.sessionID, "hostToken": tok,
		"expiresAt": exp, "wsUrl": wsURL(r), "joinUrl": config.SiteURL() + "/quiz/live?code=" + code,
		"questionCount": len(qs), "title": m.Title})
}

func hostToken(code string) (string, int64, error) {
	exp := time.Now().Add(tokenTTL).Unix()
	tok, err := signLive(liveClaims{Code: code, Role: "host", Exp: exp})
	return tok, exp * 1000, err
}

// liveHostToken: POST /api/quiz/live/sessions/{code}/host-token — a fresh token for the host
// (reconnects after the 2-minute token expired).
func liveHostToken(w http.ResponseWriter, r *http.Request, u *auth.User) {
	rm := theHub.get(r.PathValue("code"))
	if rm == nil {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	rm.mu.Lock()
	host, status := rm.hostID, rm.status
	rm.mu.Unlock()
	if host != u.ID {
		fail(w, r, "not_host", http.StatusForbidden)
		return
	}
	if status == "finished" {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	tok, exp, err := hostToken(rm.code)
	if err != nil {
		internal(w, r, "live token", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"hostToken": tok, "expiresAt": exp, "wsUrl": wsURL(r)})
}

// liveInfo: GET /api/quiz/live/sessions/{code} — the join screen's check.
func liveInfo(w http.ResponseWriter, r *http.Request) {
	rm := theHub.get(r.PathValue("code"))
	if rm == nil {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.status == "finished" {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"code": rm.code, "status": rm.status, "title": rm.title,
		"questionCount": len(rm.views), "players": len(rm.active()), "maxPlayers": maxPlayers()})
}

func hashKey(k string) [32]byte { return sha256.Sum256([]byte(k)) }

// liveJoin: POST /api/quiz/live/join {code, nickname, rejoinKey?} → {playerId, rejoinKey, token,
// expiresAt, wsUrl}. Signing in is optional; with a session the result is linked to the account.
func liveJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code      string `json:"code"`
		Nickname  string `json:"nickname"`
		RejoinKey string `json:"rejoinKey"`
	}
	if err := httpx.Decode(r, &body, 2<<10); err != nil {
		bad(w, r)
		return
	}
	if ok, _ := ratelimit.Allow(r.Context(), "quiz:join:"+httpx.ClientIP(r), config.Int("QUIZ_LIVE_JOINS_PER_MIN", 20), time.Minute); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	rm := theHub.get(strings.TrimSpace(body.Code))
	if rm == nil {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	var uid *int64
	if u := auth.Current(r); u != nil {
		id := u.ID
		uid = &id
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.status == "finished" {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	var p *player
	key := body.RejoinKey
	if key != "" {
		h := hashKey(key)
		for _, x := range rm.players {
			if subtle.ConstantTimeCompare(x.rejoin[:], h[:]) == 1 && !x.kicked {
				p = x
			}
		}
	}
	if p == nil {
		nick := clean(body.Nickname, false)
		if nick == "\x00" || runes(nick) < 2 || runes(nick) > 24 {
			fail(w, r, "bad_nickname", http.StatusBadRequest)
			return
		}
		if len(rm.active()) >= maxPlayers() {
			fail(w, r, "live_full", http.StatusConflict)
			return
		}
		for _, x := range rm.players {
			if !x.kicked && strings.EqualFold(x.Nickname, nick) {
				fail(w, r, "nickname_taken", http.StatusConflict)
				return
			}
		}
		key = randomCode(32, alnum)
		rm.joinSeq++
		p = &player{ID: randomCode(10, alnum), Nickname: nick, UserID: uid, rejoin: hashKey(key), joined: rm.joinSeq}
		rm.players[p.ID] = p
		rm.activity = time.Now()
		rm.broadcast(rm.lobbyMsg())
	}
	exp := time.Now().Add(tokenTTL).Unix()
	tok, err := signLive(liveClaims{Code: rm.code, Role: "player", PID: p.ID, Exp: exp})
	if err != nil {
		internal(w, r, "live token", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"playerId": p.ID, "nickname": p.Nickname, "rejoinKey": key,
		"token": tok, "expiresAt": exp * 1000, "wsUrl": wsURL(r), "status": rm.status, "title": rm.title})
}

// wsOriginOK: the Origin header must name one of ALLOWED_HOSTS (browsers always send it on
// WebSocket handshakes; a missing one is refused).
func wsOriginOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	u, err := url.Parse(o)
	if o == "" || err != nil || u.Hostname() == "" {
		return false
	}
	for _, h := range config.List("ALLOWED_HOSTS", defaultHosts) {
		if strings.EqualFold(h, u.Hostname()) {
			return true
		}
	}
	return false
}

type inMsg struct {
	Type     string          `json:"type"`
	Index    int             `json:"index"`
	Answer   json.RawMessage `json:"answer"`
	PlayerID string          `json:"playerId"`
}

// liveWS: GET /api/quiz/live/ws?token=… — the game socket.
func liveWS(w http.ResponseWriter, r *http.Request) {
	if !wsOriginOK(r) {
		httpx.Error(w, r, http.StatusForbidden, "bad_origin", auth.MsgOrigin)
		return
	}
	if ok, _ := ratelimit.Allow(r.Context(), "quiz:ws:"+httpx.ClientIP(r), config.Int("QUIZ_LIVE_WS_PER_MIN", 30), time.Minute); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	cl, ok := parseLive(r.URL.Query().Get("token"), time.Now())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "bad_token", httpx.MsgAuth)
		return
	}
	rm := theHub.get(cl.Code)
	if rm == nil {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	rm.mu.Lock()
	p := rm.players[cl.PID]
	gone := rm.status == "finished" || (cl.Role == "player" && (p == nil || p.kicked))
	rm.mu.Unlock()
	if gone {
		fail(w, r, "live_not_found", http.StatusNotFound)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // Origin checked above
	if err != nil {
		return
	}
	ws.SetReadLimit(8 << 10)
	c := &conn{ws: ws, send: make(chan []byte, sendBuffer), role: cl.Role, pid: cl.PID, done: make(chan struct{})}

	rm.mu.Lock()
	if cl.Role == "host" {
		if rm.host != nil {
			rm.host.close(websocket.StatusPolicyViolation, "replaced")
		}
		rm.host = c
	} else {
		if p.conn != nil {
			p.conn.close(websocket.StatusPolicyViolation, "replaced")
		}
		p.conn = c
	}
	rm.activity = time.Now()
	c.push(rm.stateFor(c))
	c.push(rm.lobbyMsg())
	if rm.status == "question" {
		c.push(rm.questionMsg())
	}
	if cl.Role == "player" {
		rm.host.push(rm.lobbyMsg())
	}
	rm.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.writer(ctx)
	c.reader(ctx, rm)

	rm.mu.Lock()
	if cl.Role == "host" && rm.host == c {
		rm.host = nil
	}
	if cl.Role == "player" && p.conn == c {
		p.conn = nil
		if rm.status != "finished" {
			rm.host.push(rm.lobbyMsg())
		}
	}
	rm.mu.Unlock()
	c.close(websocket.StatusNormalClosure, "")
}

func (c *conn) writer(ctx context.Context) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		case b := <-c.send:
			if b == nil {
				c.close(c.endCode, c.endReason)
				return
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				c.close(websocket.StatusGoingAway, "write failed")
				return
			}
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.close(websocket.StatusGoingAway, "ping failed")
				return
			}
		}
	}
}

func (c *conn) reader(ctx context.Context, rm *room) {
	for {
		typ, b, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		select {
		case <-c.done:
			return
		default:
		}
		now := time.Now()
		rm.mu.Lock()
		if !c.allow(now) {
			c.violations++
			c.push(out("error", map[string]string{"code": "rate_limited"}))
			if c.violations > maxViolations {
				rm.mu.Unlock()
				c.closeAfterFlush(websocket.StatusPolicyViolation, "rate limited")
				select {
				case <-c.done:
				case <-time.After(5 * time.Second):
					c.close(websocket.StatusPolicyViolation, "rate limited")
				}
				return
			}
			rm.mu.Unlock()
			continue
		}
		var m inMsg
		if typ != websocket.MessageText || json.Unmarshal(b, &m) != nil {
			c.push(out("error", map[string]string{"code": "bad_message"}))
			rm.mu.Unlock()
			continue
		}
		rm.activity = now
		rm.handle(c, m)
		rm.mu.Unlock()
	}
}

func (rm *room) handle(c *conn, m inMsg) {
	if m.Type == "ping" {
		c.push(out("pong", map[string]int64{"serverTime": time.Now().UnixMilli()}))
		return
	}
	if c.role == "host" {
		switch m.Type {
		case "start":
			if rm.status == "lobby" && len(rm.active()) == 0 {
				c.push(out("error", map[string]string{"code": "no_players"}))
				return
			}
			rm.start()
		case "next":
			rm.next()
		case "end":
			rm.finish("host")
		case "kick":
			if p := rm.players[m.PlayerID]; p != nil && !p.kicked {
				p.kicked = true
				p.conn.push(out("kicked", nil))
				p.conn.closeAfterFlush(websocket.StatusNormalClosure, "kicked")
				rm.broadcast(rm.lobbyMsg())
			}
		default:
			c.push(out("error", map[string]string{"code": "unknown_type"}))
		}
		return
	}
	p := rm.players[c.pid]
	if p == nil || p.kicked {
		return
	}
	switch m.Type {
	case "answer":
		rm.answer(p, m.Index, m.Answer)
	default:
		c.push(out("error", map[string]string{"code": "unknown_type"}))
	}
}

// liveHistory: GET /api/quiz/live/history — games the student hosted, with final results.
func liveHistory(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	type res struct {
		Rank     int    `json:"rank"`
		Nickname string `json:"nickname"`
		Score    int    `json:"score"`
		Correct  int    `json:"correct"`
	}
	type sess struct {
		ID            int64           `json:"id"`
		QuizID        int64           `json:"quizId"`
		QuizTitle     string          `json:"quizTitle"`
		Status        string          `json:"status"`
		Players       int             `json:"players"`
		QuestionStats json.RawMessage `json:"questionStats"`
		CreatedAt     time.Time       `json:"createdAt"`
		FinishedAt    *time.Time      `json:"finishedAt"`
		Results       []res           `json:"results"`
	}
	out := []*sess{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select s.id, s.quiz_id, coalesce(q.title, ''), s.status, s.players, s.question_stats,
			s.created_at, s.finished_at from quiz_live_sessions s left join quizzes q on q.id = s.quiz_id
			where s.host_id = app_user_id() order by s.id desc limit 50`)
		if err != nil {
			return err
		}
		byID := map[int64]*sess{}
		var ids []int64
		for rows.Next() {
			s := &sess{Results: []res{}}
			if err := rows.Scan(&s.ID, &s.QuizID, &s.QuizTitle, &s.Status, &s.Players, &s.QuestionStats, &s.CreatedAt, &s.FinishedAt); err != nil {
				rows.Close()
				return err
			}
			out = append(out, s)
			byID[s.ID] = s
			ids = append(ids, s.ID)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select session_id, rank, nickname, score, correct from quiz_live_results
			where session_id = any($1) order by session_id, rank`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sid int64
			var x res
			if err := rows.Scan(&sid, &x.Rank, &x.Nickname, &x.Score, &x.Correct); err != nil {
				return err
			}
			byID[sid].Results = append(byID[sid].Results, x)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "live history", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"sessions": out})
}

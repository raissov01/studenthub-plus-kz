package events

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// QR check-in (05, stage 3).
//
// The organizer screen shows https://t.me/<BOT_USERNAME>/<MINIAPP_NAME>?startapp=ci_<token>.
// Telegram allows A-Z a-z 0-9 _ - and at most 64 characters in startapp; our value is
// "ci_" + 32 base64url characters = 35.
//
// token = base64url( event_id (8 bytes) | minute window (4 bytes) | HMAC-SHA256(key, first 12 bytes)[:12] )
// key   = HMAC-SHA256(SESSION_SECRET, "events-checkin:" | events.checkin_key)
//
// events.checkin_key is 32 random bytes created by "start check-in" (restarting rotates it and
// kills every earlier token). The token of the current minute is valid; the previous one stays
// valid for GraceSeconds after the minute turns, so a QR being scanned at the change still
// works, while a screenshot is useless within 90 seconds at most.

const (
	TokenPrefix  = "ci_"
	WindowSecs   = 60
	GraceSeconds = 30
	OpensBefore  = 30 * time.Minute // check-in opens 30 min before starts_at, closes at ends_at
	ClaimTTL     = 10 * time.Minute // a scan by a signed-out person is held this long
	tokenBytes   = 24
)

var (
	MsgToken     = httpx.Msg{KK: "QR-код жарамсыз. Экрандағы жаңа кодты сканерлеңіз.", RU: "QR-код недействителен. Отсканируйте новый код с экрана."}
	MsgTokenOld  = httpx.Msg{KK: "QR-кодтың мерзімі өтті. Экрандағы жаңа кодты сканерлеңіз.", RU: "Срок QR-кода истёк. Отсканируйте новый код с экрана."}
	MsgNotOpen   = httpx.Msg{KK: "Тіркеу іс-шарадан 30 минут бұрын ашылып, біткенде жабылады.", RU: "Отметка открывается за 30 минут до начала и закрывается в конце."}
	MsgRevoked   = httpx.Msg{KK: "Ұйымдастырушы сіздің келуіңізді растамады. Оған жолығыңыз.", RU: "Организатор не подтвердил ваше присутствие. Подойдите к нему."}
	MsgNoStart   = httpx.Msg{KK: "Ұйымдастырушы тіркеуді әлі бастаған жоқ.", RU: "Организатор ещё не начал отметку."}
	MsgClaim     = httpx.Msg{KK: "Тіркеу мерзімі өтті. QR-ды қайта сканерлеңіз.", RU: "Время отметки истекло. Отсканируйте QR снова."}
	MsgNoTicket  = httpx.Msg{KK: "Бұл билет бұл іс-шараға жатпайды.", RU: "Этот билет не относится к мероприятию."}
	MsgWelcomeKK = "Қош келдің! ✅"
	MsgWelcomeRU = "Добро пожаловать! ✅"
	// Honest limitation, shown on the organizer screen and in the docs.
	NoticeKK = "Ескерту: залдағы біреу тірі QR-ды сол сәтте сырттағы досына жібере алады. Тірі санақты залдағы адамдармен салыстырып, күмәнді тіркелуді «Растамау» батырмасымен алып тастаңыз."
	NoticeRU = "Внимание: живой QR можно в тот же момент переслать другу вне зала. Сверяйте живой счётчик с людьми в зале и снимайте сомнительные отметки кнопкой «Не подтверждать»."
)

var (
	errTokenInvalid = errors.New("token invalid")
	errTokenExpired = errors.New("token expired")
)

func tokenKey(nonce []byte) []byte {
	m := hmac.New(sha256.New, []byte(config.Str("SESSION_SECRET", "")))
	m.Write([]byte("events-checkin:"))
	m.Write(nonce)
	return m.Sum(nil)
}

func tokenMAC(nonce, head []byte) []byte {
	m := hmac.New(sha256.New, tokenKey(nonce))
	m.Write(head)
	return m.Sum(nil)[:tokenBytes-12]
}

// MakeToken returns the token (without the ci_ prefix) for the minute window containing at.
func MakeToken(eventID int64, nonce []byte, at time.Time) string {
	head := make([]byte, 12, tokenBytes)
	binary.BigEndian.PutUint64(head[:8], uint64(eventID))
	binary.BigEndian.PutUint32(head[8:12], uint32(at.Unix()/WindowSecs))
	return base64.RawURLEncoding.EncodeToString(append(head, tokenMAC(nonce, head)...))
}

// tokenEvent extracts the event id without verifying anything.
func tokenEvent(tok string) (int64, []byte, bool) {
	tok = strings.TrimPrefix(strings.TrimSpace(tok), TokenPrefix)
	if len(tok) != 32 {
		return 0, nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != tokenBytes {
		return 0, nil, false
	}
	id := int64(binary.BigEndian.Uint64(raw[:8]))
	return id, raw, id > 0
}

// verifyToken checks signature and freshness against the event's current nonce.
func verifyToken(raw, nonce []byte, now time.Time) error {
	if len(nonce) == 0 || len(raw) != tokenBytes {
		return errTokenInvalid
	}
	if !hmac.Equal(raw[12:], tokenMAC(nonce, raw[:12])) {
		return errTokenInvalid
	}
	win := int64(binary.BigEndian.Uint32(raw[8:12]))
	cur := now.Unix() / WindowSecs
	switch {
	case win == cur:
		return nil
	case win == cur-1 && now.Unix()-cur*WindowSecs < GraceSeconds:
		return nil
	case win > cur:
		return errTokenInvalid
	}
	return errTokenExpired
}

func checkinOpen(ev *Event, now time.Time) bool {
	return ev.Status == "published" && !now.Before(ev.StartsAt.Add(-OpensBefore)) && now.Before(ev.EndsAt)
}

// DeepLink is the Mini App link inside the QR; WebLink is the fallback outside Telegram.
func DeepLink(token string) string {
	bot := strings.TrimPrefix(config.Str("BOT_USERNAME", ""), "@")
	app := config.Str("MINIAPP_NAME", "")
	if bot == "" {
		return WebLink(token)
	}
	if app == "" {
		return "https://t.me/" + bot + "?startapp=" + TokenPrefix + token
	}
	return "https://t.me/" + bot + "/" + app + "?startapp=" + TokenPrefix + token
}

func WebLink(token string) string {
	return config.SiteURL() + "/checkin?t=" + url.QueryEscape(TokenPrefix+token)
}

// CheckinResult is what a check-in did.
type CheckinResult struct {
	Status  string `json:"status"` // checked_in | already
	TeamNo  *int16 `json:"team_no"`
	Method  string `json:"method"`
	Message string `json:"message"`
}

// checkIn marks userID present. The event row must already be locked (load(..., true)).
// method: qr (self, via token), manual (organizer), walk_in is derived.
func checkIn(ctx context.Context, tx pgx.Tx, ev *Event, userID int64, method string, now time.Time, lang string) (CheckinResult, error) {
	res := CheckinResult{Message: MsgWelcomeKK}
	if lang == "ru" {
		res.Message = MsgWelcomeRU
	}
	if !checkinOpen(ev, now) {
		return res, refuse(http.StatusConflict, "checkin_closed", MsgNotOpen)
	}
	var status string
	var checked, revoked *time.Time
	var team *int16
	err := tx.QueryRow(ctx, `select status, checked_in_at, checkin_revoked_at, team_no from event_regs
		where event_id = $1 and user_id = $2 for update`, ev.ID, userID).Scan(&status, &checked, &revoked, &team)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return res, err
	}
	if exists && checked != nil {
		res.Status, res.TeamNo, res.Method = "already", team, method
		return res, nil
	}
	if exists && revoked != nil && method == "qr" {
		return res, refuse(http.StatusForbidden, "checkin_revoked", MsgRevoked)
	}
	registered := exists && status == "registered"
	if !registered {
		// Walk-ins: allowed by the event, or let in by the organizer (manual) when already
		// on the list (waitlist) — the organizer sees the person.
		if !ev.WalkIn && !(method == "manual" && exists) {
			return res, refuse(http.StatusForbidden, "not_registered", MsgNotReg)
		}
		if !registered && method == "qr" {
			method = "walk_in"
		}
		if !exists {
			if _, err := tx.Exec(ctx, `insert into event_regs (event_id, user_id, status, ticket) values ($1, $2, 'registered', $3)`,
				ev.ID, userID, randomToken(16)); err != nil {
				return res, err
			}
		}
	}
	if _, err := tx.Exec(ctx, `update event_regs set status = 'registered', checked_in_at = $3, checkin_method = $4,
		checkin_revoked_at = null where event_id = $1 and user_id = $2`, ev.ID, userID, now, method); err != nil {
		return res, err
	}
	if ev.TeamsEnabled {
		n, err := assignTeam(ctx, tx, ev.ID, userID)
		if err != nil {
			return res, err
		}
		team = &n
	}
	res.Status, res.TeamNo, res.Method = "checked_in", team, method
	return res, nil
}

// scan: POST /api/events/checkin {token}. Signed in -> checked in at once. Signed out (link
// opened outside Telegram) -> 202 with a claim valid for ClaimTTL; signing in and posting the
// claim to /checkin/claim completes it.
func scan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Token string `json:"token"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	id, raw, ok := tokenEvent(body.Token)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "token_invalid", MsgToken)
		return
	}
	u := auth.Current(r)
	if u != nil && u.NeedsConsent() {
		u = nil // finishes through the claim after the consent screen
	}
	now := Now()
	var res CheckinResult
	var claimID string
	var claimExp time.Time
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		ev, err := load(ctx, tx, id, true)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusBadRequest, "token_invalid", MsgToken)
		}
		if err != nil {
			return err
		}
		switch verifyToken(raw, ev.checkinKey, now) {
		case nil:
		case errTokenExpired:
			return refuse(http.StatusBadRequest, "token_expired", MsgTokenOld)
		default:
			return refuse(http.StatusBadRequest, "token_invalid", MsgToken)
		}
		if !checkinOpen(ev, now) {
			return refuse(http.StatusConflict, "checkin_closed", MsgNotOpen)
		}
		if u == nil {
			claimID, claimExp = randomToken(18), now.Add(ClaimTTL)
			_, err := tx.Exec(ctx, `insert into event_checkin_claims (id, event_id, created_at, expires_at) values ($1, $2, $3, $4)`,
				claimID, ev.ID, now, claimExp)
			return err
		}
		res, err = checkIn(ctx, tx, ev, u.ID, "qr", now, u.Lang)
		return err
	})
	if err != nil {
		writeErr(w, r, "scan", err)
		return
	}
	if u == nil {
		httpx.JSON(w, http.StatusAccepted, map[string]any{"status": "login_required", "claim": claimID, "expires_at": claimExp})
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func claim(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body struct {
		Claim string `json:"claim"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Claim == "" || len(body.Claim) > 64 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	now := Now()
	var res CheckinResult
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var eventID int64
		err := tx.QueryRow(ctx, `update event_checkin_claims set used_at = $2
			where id = $1 and used_at is null and expires_at > $2 returning event_id`, body.Claim, now).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse(http.StatusGone, "claim_expired", MsgClaim)
		}
		if err != nil {
			return err
		}
		ev, err := load(ctx, tx, eventID, true)
		if err != nil {
			return err
		}
		res, err = checkIn(ctx, tx, ev, u.ID, "qr", now, u.Lang)
		return err
	})
	if err != nil {
		writeErr(w, r, "claim", err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

type liveView struct {
	Token        string `json:"token"` // ci_...
	DeepLink     string `json:"deep_link"`
	WebLink      string `json:"web_link"`
	RotatesInSec int    `json:"rotates_in_sec"`
	Open         bool   `json:"open"`
	Registered   int    `json:"registered"`
	CheckedIn    int    `json:"checked_in"`
	Waitlist     int    `json:"waitlist"`
	NoticeKK     string `json:"notice_kk"`
	NoticeRU     string `json:"notice_ru"`
}

func liveOf(ev *Event, now time.Time) liveView {
	tok := MakeToken(ev.ID, ev.checkinKey, now)
	return liveView{
		Token: TokenPrefix + tok, DeepLink: DeepLink(tok), WebLink: WebLink(tok),
		RotatesInSec: WindowSecs - int(now.Unix()%WindowSecs), Open: checkinOpen(ev, now),
		Registered: ev.Registered, CheckedIn: ev.CheckedIn, Waitlist: ev.Waitlist, NoticeKK: NoticeKK, NoticeRU: NoticeRU,
	}
}

// startCheckin creates a fresh signing nonce (old QR codes stop working) and returns the live view.
func startCheckin(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var out liveView
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		if ev.Status != "published" {
			return refuse(http.StatusConflict, "invalid_status", MsgStatus)
		}
		nonce := make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `update events set checkin_key = $2, checkin_started_at = now() where id = $1`, ev.ID, nonce); err != nil {
			return err
		}
		ev.checkinKey = nonce
		out = liveOf(ev, Now())
		return nil
	})
	if ok {
		httpx.JSON(w, http.StatusOK, out)
	}
}

// live: the organizer screen polls this (every few seconds) for the current QR and counters.
func live(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var out liveView
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		if len(ev.checkinKey) == 0 {
			return refuse(http.StatusConflict, "checkin_not_started", MsgNoStart)
		}
		out = liveOf(ev, Now())
		return nil
	})
	if ok {
		httpx.JSON(w, http.StatusOK, out)
	}
}

// manual: offline fallback. The organizer marks a person present by user id or by the code of
// the student's e-ticket.
func manual(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		UserID int64  `json:"user_id"`
		Ticket string `json:"ticket"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || (body.UserID == 0) == (body.Ticket == "") {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var res CheckinResult
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		uid := body.UserID
		if body.Ticket != "" {
			err := tx.QueryRow(r.Context(), `select user_id from event_regs where event_id = $1 and ticket = $2`, ev.ID, body.Ticket).Scan(&uid)
			if errors.Is(err, pgx.ErrNoRows) {
				return refuse(http.StatusNotFound, "ticket_not_found", MsgNoTicket)
			}
			if err != nil {
				return err
			}
		} else {
			var n int
			if err := tx.QueryRow(r.Context(), `select count(*) from users where id = $1 and deleted_at is null`, uid).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return refuse(http.StatusNotFound, "not_registered", MsgNotReg)
			}
		}
		var err error
		res, err = checkIn(r.Context(), tx, ev, uid, "manual", Now(), httpx.Lang(r))
		return err
	})
	if ok {
		httpx.JSON(w, http.StatusOK, res)
	}
}

// revoke: "not confirmed" — removes a doubtful check-in; QR self check-in is refused afterwards.
func revoke(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		UserID int64 `json:"user_id"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.UserID == 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		if ev.Status == "finished" {
			return refuse(http.StatusConflict, "invalid_status", MsgStatus)
		}
		tag, err := tx.Exec(r.Context(), `update event_regs set checked_in_at = null, checkin_method = null, team_no = null,
			checkin_revoked_at = now() where event_id = $1 and user_id = $2 and checked_in_at is not null`, ev.ID, body.UserID)
		if err == nil && tag.RowsAffected() == 0 {
			return refuse(http.StatusNotFound, "not_checked_in", MsgNotReg)
		}
		return err
	})
	if ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

type regRow struct {
	UserID       int64      `json:"user_id"`
	Name         *string    `json:"name"`
	Username     *string    `json:"username"`
	Faculty      *string    `json:"faculty"`
	Course       *int16     `json:"course"`
	Status       string     `json:"status"`
	TicketShort  string     `json:"ticket_short"` // first 6 characters, to match the student's e-ticket by eye
	RegisteredAt time.Time  `json:"registered_at"`
	CheckedInAt  *time.Time `json:"checked_in_at"`
	Method       *string    `json:"checkin_method"`
	Revoked      bool       `json:"checkin_revoked"`
	TeamNo       *int16     `json:"team_no"`
	Feedback     *int16     `json:"feedback_score"`
}

func loadRegs(ctx context.Context, tx pgx.Tx, eventID int64) ([]regRow, error) {
	rows, err := tx.Query(ctx, `select r.user_id, u.name, u.tg_username, u.faculty, u.course, r.status, left(r.ticket, 6),
			r.registered_at, r.checked_in_at, r.checkin_method, r.checkin_revoked_at is not null, r.team_no, r.feedback_score
		from event_regs r join users u on u.id = r.user_id
		where r.event_id = $1 order by r.status, r.registered_at, r.user_id`, eventID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (regRow, error) {
		var x regRow
		return x, row.Scan(&x.UserID, &x.Name, &x.Username, &x.Faculty, &x.Course, &x.Status, &x.TicketShort,
			&x.RegisteredAt, &x.CheckedInAt, &x.Method, &x.Revoked, &x.TeamNo, &x.Feedback)
	})
}

// regs: the organizer's full list (also the offline fallback list for manual check-in).
func regs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var out []regRow
	var ev *Event
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, e *Event) error {
		ev = e
		var err error
		out, err = loadRegs(r.Context(), tx, e.ID)
		return err
	})
	if ok {
		if out == nil {
			out = []regRow{}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"registered": ev.Registered, "checked_in": ev.CheckedIn,
			"waitlist": ev.Waitlist, "regs": out})
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

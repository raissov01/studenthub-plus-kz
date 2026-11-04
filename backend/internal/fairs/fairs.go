// Package fairs is spec 10, career-fair booths, on top of events (FEATURE_FAIRS and FEATURE_EVENTS):
// an organizer lists the companies at a career event, each company gets a printable booth QR,
// students scan it to save «Мен барған стендтер», and the morning after the fair both sides get
// one summary message. Students choose which fields (name, course, faculty, Telegram) a company
// may see, as in the apply flow; with none chosen the company sees a count only.
//
// The booth QR carries a static token, so it can be printed on A5:
//
//	token = "bt_" + base64url( event_id (8) | employer_id (8) | HMAC-SHA256(key, first 16 bytes)[:12] )
//	key   = HMAC-SHA256(SESSION_SECRET, "fair-booth:" | event_exhibitors.qr_secret)
//
// Removing the exhibitor (or re-adding it, which rotates qr_secret) voids every printed copy.
package fairs

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/events"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
)

// Feature is the switch name: FEATURE_FAIRS (the events switch must be on as well).
const Feature = "fairs"

const (
	TokenPrefix = "bt_"
	tokenRaw    = 8 + 8 + 12
	// a booth scan is accepted from 30 minutes before the start until an hour after the end
	OpensBefore = events.OpensBefore
	ClosesAfter = time.Hour
)

// FairCat: only career events get exhibitors.
const FairCat = "career"

// Fields a student may share with a company at its booth.
var Fields = []string{"name", "course", "faculty", "telegram"}

// Now is the clock; Send delivers the morning summary. Tests replace both.
var (
	Now  = time.Now
	Send = func(ctx context.Context, userID int64, key, text, link string) error {
		_, err := notify.Send(ctx, userID, notify.CatEvents, text, notify.Opts{Key: key, Link: link})
		return err
	}
)

var (
	MsgNoFair     = httpx.Msg{KK: "Стенд табылмады. QR-ды қайта сканерлеңіз не ұйымдастырушыдан сұраңыз.", RU: "Стенд не найден. Отсканируйте QR ещё раз или спросите организатора."}
	MsgNotYet     = httpx.Msg{KK: "Жәрмеңке әлі басталмады.", RU: "Ярмарка ещё не началась."}
	MsgOver       = httpx.Msg{KK: "Жәрмеңке аяқталды, стендке тіркелу жабық.", RU: "Ярмарка закончилась, отметка у стенда закрыта."}
	MsgNotCareer  = httpx.Msg{KK: "Компанияларды тек «Мансап» санатындағы іс-шараға қосуға болады.", RU: "Компании можно добавить только к мероприятию категории «Карьера»."}
	MsgNoEmployer = httpx.Msg{KK: "Бұл БСН бойынша тексерілген компания жоқ.", RU: "Проверенной компании с таким БИН нет."}
	MsgBooth      = httpx.Msg{KK: "Стенд атауы 40 таңбадан аспайды.", RU: "Название стенда — не больше 40 символов."}
	MsgNotMember  = httpx.Msg{KK: "Бұл бет тексерілген компания қызметкерлеріне арналған.", RU: "Эта страница для сотрудников проверенной компании."}
)

func init() {
	me.RegisterExport("boothCheckins", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select event_id, employer_id, shared_fields, at from booth_checkins where user_id = app_user_id() order by at")
	})
}

// Routes wires the booth endpoints, all behind FEATURE_EVENTS and FEATURE_FAIRS.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, features.Require("events", features.Require(Feature, fn)))
	}
	h("GET /api/events/{id}/exhibitors", exhibitors)
	h("POST /api/events/{id}/exhibitors", auth.Write(auth.Need(addExhibitor)))
	h("DELETE /api/events/{id}/exhibitors/{employerId}", auth.Write(auth.Need(removeExhibitor)))
	h("POST /api/events/booth-checkin", auth.Write(auth.Need(boothCheckin)))
	h("GET /api/events/{id}/my-booths", auth.Need(myBooths))
	h("GET /api/employer/fairs", auth.Need(employerFairs))
	h("GET /api/employer/fairs/{id}/checkins", auth.Need(employerCheckins))
	h("GET /api/cron/fairs", cronHandler)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("fairs: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func pathInt(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// ---------- tokens ----------

func boothKey(secret []byte) []byte {
	m := hmac.New(sha256.New, []byte(config.Str("SESSION_SECRET", "")))
	m.Write([]byte("fair-booth:"))
	m.Write(secret)
	return m.Sum(nil)
}

// MakeToken is the booth's printable token (with the bt_ prefix).
func MakeToken(eventID, employerID int64, secret []byte) string {
	raw := make([]byte, tokenRaw)
	binary.BigEndian.PutUint64(raw[0:8], uint64(eventID))
	binary.BigEndian.PutUint64(raw[8:16], uint64(employerID))
	m := hmac.New(sha256.New, boothKey(secret))
	m.Write(raw[:16])
	copy(raw[16:], m.Sum(nil)[:12])
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// parseToken reads the ids without verifying anything.
func parseToken(tok string) (eventID, employerID int64, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tok, TokenPrefix))
	if err != nil || len(b) != tokenRaw || !strings.HasPrefix(tok, TokenPrefix) {
		return 0, 0, false
	}
	return int64(binary.BigEndian.Uint64(b[0:8])), int64(binary.BigEndian.Uint64(b[8:16])), true
}

// BoothURL is what the printed QR opens.
func BoothURL(token string) string { return config.SiteURL() + "/booth?b=" + url.QueryEscape(token) }

// ---------- organizer: exhibitors ----------

// Exhibitor is one company at a fair.
type Exhibitor struct {
	EmployerID int64   `json:"employerId"`
	Name       string  `json:"name"`
	Booth      string  `json:"booth"`
	OpenJobs   int     `json:"openJobs"`
	Token      *string `json:"token,omitempty"` // organizers only
	URL        *string `json:"url,omitempty"`
	Checkins   *int    `json:"checkins,omitempty"` // organizers only
}

const openJobsSQL = `(select count(*) from jobs j where j.employer_id = x.employer_id and j.status = 'published'
	and (j.expires_at is null or j.expires_at > now()))`

func loadExhibitors(ctx context.Context, tx pgx.Tx, eventID int64, manager bool) ([]Exhibitor, error) {
	rows, err := tx.Query(ctx, `select x.employer_id, e.name, x.booth, `+openJobsSQL+`, x.qr_secret,
			(select count(*) from booth_checkins b where b.event_id = x.event_id and b.employer_id = x.employer_id)
		from event_exhibitors x join employers e on e.id = x.employer_id and e.status = 'verified'
		where x.event_id = $1 order by nullif(x.booth, '') nulls last, e.name`, eventID)
	if err != nil {
		return nil, err
	}
	out := []Exhibitor{}
	for rows.Next() {
		var x Exhibitor
		var secret []byte
		var n int
		if err := rows.Scan(&x.EmployerID, &x.Name, &x.Booth, &x.OpenJobs, &secret, &n); err != nil {
			return nil, err
		}
		if manager {
			tok := MakeToken(eventID, x.EmployerID, secret)
			u := BoothURL(tok)
			x.Token, x.URL, x.Checkins = &tok, &u, &n
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// withFair loads a visible event (404 otherwise) and runs fn as the owner.
func withFair(w http.ResponseWriter, r *http.Request, u *auth.User, lock bool, fn func(tx pgx.Tx, ev *events.Event, manager bool) error) bool {
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", events.MsgNoEvent)
		return false
	}
	missing := false
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		ev, err := events.Load(r.Context(), tx, id, lock)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !events.Visible(r.Context(), u, ev)) {
			missing = true
			return nil
		}
		if err != nil {
			return err
		}
		return fn(tx, ev, events.CanManage(r.Context(), u, ev))
	})
	switch {
	case missing:
		httpx.Error(w, r, http.StatusNotFound, "not_found", events.MsgNoEvent)
	case err != nil && !errors.Is(err, errAnswered):
		fail(w, r, "event", err)
	}
	return !missing && err == nil
}

// errAnswered: fn already wrote the reply.
var errAnswered = errors.New("fairs: answered")

// exhibitors: GET /api/events/{id}/exhibitors (anyone who can see the event; organizers also get
// each booth's QR link and scan count).
func exhibitors(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	var list []Exhibitor
	if withFair(w, r, u, false, func(tx pgx.Tx, ev *events.Event, manager bool) error {
		var err error
		list, err = loadExhibitors(r.Context(), tx, ev.ID, manager)
		return err
	}) {
		httpx.JSON(w, http.StatusOK, map[string]any{"exhibitors": list})
	}
}

// addExhibitor: POST /api/events/{id}/exhibitors {bin, booth} — organizer of a career event adds
// (or renames the booth of) a verified company found by its BIN.
func addExhibitor(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		BIN   string `json:"bin"`
		Booth string `json:"booth"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || len(body.BIN) != 12 || strings.Trim(body.BIN, "0123456789") != "" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	booth := strings.Join(strings.Fields(body.Booth), " ")
	if utf8.RuneCountInString(booth) > 40 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_booth", MsgBooth)
		return
	}
	var list []Exhibitor
	ok := withFair(w, r, u, true, func(tx pgx.Tx, ev *events.Event, manager bool) error {
		ctx := r.Context()
		switch {
		case !manager:
			httpx.Error(w, r, http.StatusForbidden, "forbidden", events.MsgForeign)
			return errAnswered
		case ev.Cat != FairCat:
			httpx.Error(w, r, http.StatusBadRequest, "not_career", MsgNotCareer)
			return errAnswered
		}
		var employerID int64
		err := tx.QueryRow(ctx, `select id from employers where bin = $1 and status = 'verified'`, body.BIN).Scan(&employerID)
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, r, http.StatusNotFound, "no_employer", MsgNoEmployer)
			return errAnswered
		}
		if err != nil {
			return err
		}
		secret := make([]byte, 16)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into event_exhibitors (event_id, employer_id, booth, qr_secret, added_by)
			values ($1, $2, $3, $4, $5) on conflict (event_id, employer_id) do update set booth = excluded.booth`,
			ev.ID, employerID, booth, secret, u.ID); err != nil {
			return err
		}
		list, err = loadExhibitors(ctx, tx, ev.ID, true)
		return err
	})
	if ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"exhibitors": list})
	}
}

// removeExhibitor: DELETE /api/events/{id}/exhibitors/{employerId}; its scans go with it.
func removeExhibitor(w http.ResponseWriter, r *http.Request, u *auth.User) {
	employerID, okID := pathInt(r, "employerId")
	var list []Exhibitor
	ok := withFair(w, r, u, true, func(tx pgx.Tx, ev *events.Event, manager bool) error {
		if !manager || !okID {
			httpx.Error(w, r, http.StatusForbidden, "forbidden", events.MsgForeign)
			return errAnswered
		}
		if _, err := tx.Exec(r.Context(), `delete from event_exhibitors where event_id = $1 and employer_id = $2`, ev.ID, employerID); err != nil {
			return err
		}
		var err error
		list, err = loadExhibitors(r.Context(), tx, ev.ID, true)
		return err
	})
	if ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"exhibitors": list})
	}
}

// ---------- student: booth scan ----------

// BoothResult is the scan reply.
type BoothResult struct {
	Status     string   `json:"status"` // ok | already
	EventID    int64    `json:"eventId"`
	EventTitle string   `json:"eventTitle"`
	EmployerID int64    `json:"employerId"`
	Company    string   `json:"company"`
	Booth      string   `json:"booth"`
	OpenJobs   int      `json:"openJobs"`
	Shared     []string `json:"shared"`
}

func cleanFields(in []string) ([]string, bool) {
	out := []string{}
	for _, f := range in {
		known := false
		for _, k := range Fields {
			known = known || k == f
		}
		if !known {
			return nil, false
		}
		dup := false
		for _, o := range out {
			dup = dup || o == f
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out, true
}

// boothCheckin: POST /api/events/booth-checkin {token, share?: [fields]}. The first scan saves the
// booth; a repeat scan answers "already" and, when share is given, updates what the company sees.
func boothCheckin(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", me.MsgConsent)
		return
	}
	var body struct {
		Token string    `json:"token"`
		Share *[]string `json:"share"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || len(body.Token) > 64 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	share := []string{}
	if body.Share != nil {
		var ok bool
		if share, ok = cleanFields(*body.Share); !ok {
			httpx.Error(w, r, http.StatusBadRequest, "bad_fields", httpx.MsgBad)
			return
		}
	}
	eventID, employerID, ok := parseToken(body.Token)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "bad_token", MsgNoFair)
		return
	}
	ctx := r.Context()
	now := Now()
	var res BoothResult
	var refusal struct {
		status int
		code   string
		msg    httpx.Msg
	}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var secret []byte
		var starts, ends time.Time
		var status string
		err := tx.QueryRow(ctx, `select x.qr_secret, x.booth, c.name, ev.title_kk, coalesce(nullif(ev.title_ru, ''), ev.title_kk), ev.starts_at, ev.ends_at, ev.status::text,
				`+openJobsSQL+`
			from event_exhibitors x join employers c on c.id = x.employer_id and c.status = 'verified'
			join events ev on ev.id = x.event_id
			where x.event_id = $1 and x.employer_id = $2`, eventID, employerID).
			Scan(&secret, &res.Booth, &res.Company, &res.EventTitle, new(string), &starts, &ends, &status, &res.OpenJobs)
		if errors.Is(err, pgx.ErrNoRows) {
			refusal.status, refusal.code, refusal.msg = http.StatusNotFound, "no_booth", MsgNoFair
			return nil
		}
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(MakeToken(eventID, employerID, secret)), []byte(body.Token)) != 1 {
			refusal.status, refusal.code, refusal.msg = http.StatusNotFound, "no_booth", MsgNoFair
			return nil
		}
		switch {
		case status != "published" && status != "finished":
			refusal.status, refusal.code, refusal.msg = http.StatusConflict, "fair_closed", MsgOver
			return nil
		case now.Before(starts.Add(-OpensBefore)):
			refusal.status, refusal.code, refusal.msg = http.StatusConflict, "fair_not_started", MsgNotYet
			return nil
		case now.After(ends.Add(ClosesAfter)):
			refusal.status, refusal.code, refusal.msg = http.StatusConflict, "fair_over", MsgOver
			return nil
		}
		res.EventID, res.EmployerID = eventID, employerID
		shareBytes, _ := json.Marshal(share)
		shareJSON := string(shareBytes)
		tag, err := tx.Exec(ctx, `insert into booth_checkins (event_id, employer_id, user_id, shared_fields, at)
			values ($1, $2, $3, $4, $5) on conflict do nothing`, eventID, employerID, u.ID, shareJSON, now)
		if err != nil {
			return err
		}
		res.Status = "ok"
		if tag.RowsAffected() == 0 {
			res.Status = "already"
			if body.Share != nil {
				if _, err := tx.Exec(ctx, `update booth_checkins set shared_fields = $4 where event_id = $1 and employer_id = $2 and user_id = $3`,
					eventID, employerID, u.ID, shareJSON); err != nil {
					return err
				}
			}
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `select shared_fields from booth_checkins where event_id = $1 and employer_id = $2 and user_id = $3`,
			eventID, employerID, u.ID).Scan(&raw); err != nil {
			return err
		}
		res.Shared = []string{}
		return json.Unmarshal(raw, &res.Shared)
	})
	switch {
	case err != nil:
		fail(w, r, "booth checkin", err)
	case refusal.status != 0:
		httpx.Error(w, r, refusal.status, refusal.code, refusal.msg)
	default:
		httpx.JSON(w, http.StatusOK, res)
	}
}

// MyBooth is one booth the student visited.
type MyBooth struct {
	EmployerID int64     `json:"employerId"`
	Company    string    `json:"company"`
	Booth      string    `json:"booth"`
	OpenJobs   int       `json:"openJobs"`
	At         time.Time `json:"at"`
	Shared     []string  `json:"shared"`
}

func loadMyBooths(ctx context.Context, tx pgx.Tx, eventID, userID int64) ([]MyBooth, error) {
	rows, err := tx.Query(ctx, `select x.employer_id, c.name, x.booth, `+openJobsSQL+`, b.at, b.shared_fields
		from booth_checkins b join event_exhibitors x on x.event_id = b.event_id and x.employer_id = b.employer_id
		join employers c on c.id = x.employer_id
		where b.event_id = $1 and b.user_id = $2 order by b.at`, eventID, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MyBooth, error) {
		var m MyBooth
		var raw []byte
		if err := row.Scan(&m.EmployerID, &m.Company, &m.Booth, &m.OpenJobs, &m.At, &raw); err != nil {
			return m, err
		}
		m.Shared = []string{}
		return m, json.Unmarshal(raw, &m.Shared)
	})
}

// myBooths: GET /api/events/{id}/my-booths.
func myBooths(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var list []MyBooth
	if withFair(w, r, u, false, func(tx pgx.Tx, ev *events.Event, _ bool) error {
		var err error
		list, err = loadMyBooths(r.Context(), tx, ev.ID, u.ID)
		return err
	}) {
		httpx.JSON(w, http.StatusOK, map[string]any{"booths": list})
	}
}

// ---------- employer cabinet ----------

func employerOf(ctx context.Context, tx pgx.Tx, userID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `select m.employer_id from employer_members m join employers e on e.id = m.employer_id and e.status = 'verified'
		where m.user_id = $1`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// Fair is one fair a company exhibits at.
type Fair struct {
	EventID  int64     `json:"eventId"`
	Title    string    `json:"title"`
	TitleRU  *string   `json:"titleRu"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Place    *string   `json:"place"`
	Booth    string    `json:"booth"`
	Token    string    `json:"token"`
	URL      string    `json:"url"`
	Checkins int       `json:"checkins"`
}

// employerFairs: GET /api/employer/fairs — the caller's company's fairs with the printable QR.
func employerFairs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	var list []Fair
	member := true
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		emp, err := employerOf(ctx, tx, u.ID)
		if err != nil || emp == 0 {
			member = false
			return err
		}
		rows, err := tx.Query(ctx, `select ev.id, ev.title_kk, ev.title_ru, ev.starts_at, ev.ends_at, ev.place_text, x.booth, x.qr_secret,
				(select count(*) from booth_checkins b where b.event_id = x.event_id and b.employer_id = x.employer_id)
			from event_exhibitors x join events ev on ev.id = x.event_id and ev.status in ('published', 'finished')
			where x.employer_id = $1 and ev.ends_at > now() - interval '60 days' order by ev.starts_at desc`, emp)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Fair, error) {
			var f Fair
			var secret []byte
			if err := row.Scan(&f.EventID, &f.Title, &f.TitleRU, &f.StartsAt, &f.EndsAt, &f.Place, &f.Booth, &secret, &f.Checkins); err != nil {
				return f, err
			}
			f.Token = MakeToken(f.EventID, emp, secret)
			f.URL = BoothURL(f.Token)
			return f, nil
		})
		return err
	})
	switch {
	case err != nil:
		fail(w, r, "employer fairs", err)
	case !member:
		httpx.Error(w, r, http.StatusForbidden, "not_member", MsgNotMember)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"fairs": list})
	}
}

// Visitor is one booth scan as the company sees it: only the fields the student shared.
type Visitor struct {
	At       time.Time `json:"at"`
	Name     *string   `json:"name,omitempty"`
	Course   *int      `json:"course,omitempty"`
	Faculty  *string   `json:"faculty,omitempty"`
	Telegram *string   `json:"telegram,omitempty"`
}

// employerCheckins: GET /api/employer/fairs/{id}/checkins — shared visitors and a count of the rest.
func employerCheckins(w http.ResponseWriter, r *http.Request, u *auth.User) {
	eventID, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", events.MsgNoEvent)
		return
	}
	ctx := r.Context()
	out := map[string]any{}
	member, found := true, true
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		emp, err := employerOf(ctx, tx, u.ID)
		if err != nil || emp == 0 {
			member = false
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from event_exhibitors where event_id = $1 and employer_id = $2)`, eventID, emp).Scan(&exists); err != nil || !exists {
			found = false
			return err
		}
		rows, err := tx.Query(ctx, `select b.at, b.shared_fields, u.name, u.course, u.faculty, u.tg_username
			from booth_checkins b join users u on u.id = b.user_id and u.deleted_at is null
			where b.event_id = $1 and b.employer_id = $2 order by b.at`, eventID, emp)
		if err != nil {
			return err
		}
		visitors := []Visitor{}
		anonymous, total := 0, 0
		for rows.Next() {
			var v Visitor
			var raw []byte
			var name, faculty, tg *string
			var course *int
			if err := rows.Scan(&v.At, &raw, &name, &course, &faculty, &tg); err != nil {
				return err
			}
			total++
			var fields []string
			_ = json.Unmarshal(raw, &fields)
			if len(fields) == 0 {
				anonymous++
				continue
			}
			for _, f := range fields {
				switch f {
				case "name":
					v.Name = name
				case "course":
					v.Course = course
				case "faculty":
					v.Faculty = faculty
				case "telegram":
					v.Telegram = tg
				}
			}
			visitors = append(visitors, v)
		}
		out["total"], out["anonymous"], out["visitors"] = total, anonymous, visitors
		return rows.Err()
	})
	switch {
	case err != nil:
		fail(w, r, "employer checkins", err)
	case !member:
		httpx.Error(w, r, http.StatusForbidden, "not_member", MsgNotMember)
	case !found:
		httpx.Error(w, r, http.StatusNotFound, "not_found", events.MsgNoEvent)
	default:
		httpx.JSON(w, http.StatusOK, out)
	}
}

// ---------- the morning after ----------

// SummaryStats is what one cron run did.
type SummaryStats struct {
	Fairs     int `json:"fairs"`
	Students  int `json:"students"`
	Employers int `json:"employers"`
}

func almatyDay(t time.Time) string { return t.UTC().Add(5 * time.Hour).Format("2006-01-02") }

// Summaries sends, for every fair that ended yesterday (Almaty), one message per student listing
// the booths they visited, and one per company with its visitor count. Idempotent per recipient
// (delivery keys fair:<event>:<user> and fair:<event>:booth:<employer>), so a rerun sends nothing new.
func Summaries(ctx context.Context, now time.Time) (SummaryStats, error) {
	var st SummaryStats
	yesterday := almatyDay(now.Add(-24 * time.Hour))
	type fair struct {
		id      int64
		titleKK string
		titleRU *string
	}
	var fairs []fair
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select ev.id, ev.title_kk, ev.title_ru from events ev
			where ev.status in ('published', 'finished') and ((ev.ends_at at time zone 'Asia/Almaty')::date) = $1::date
				and exists (select 1 from event_exhibitors x where x.event_id = ev.id)`, yesterday)
		if err != nil {
			return err
		}
		fairs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (fair, error) {
			var f fair
			return f, row.Scan(&f.id, &f.titleKK, &f.titleRU)
		})
		return err
	})
	if err != nil {
		return st, err
	}
	link := func(id int64) string { return fmt.Sprintf("%s/event/%d", config.SiteURL(), id) }
	for _, f := range fairs {
		st.Fairs++
		title := func(lang string) string {
			if lang == "ru" && f.titleRU != nil && *f.titleRU != "" {
				return html.EscapeString(*f.titleRU)
			}
			return html.EscapeString(f.titleKK)
		}
		// students: booths in scan order
		type visit struct {
			user          int64
			lang, company string
			jobs          int
		}
		var visits []visit
		var booths []struct {
			employer, owner int64
			lang            string
			total, shared   int
		}
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select b.user_id, coalesce(u.lang, 'kk'), c.name, (select count(*) from jobs j where j.employer_id = c.id and j.status = 'published'
					and (j.expires_at is null or j.expires_at > now()))
				from booth_checkins b join users u on u.id = b.user_id and u.deleted_at is null join employers c on c.id = b.employer_id
				where b.event_id = $1 order by b.user_id, b.at`, f.id)
			if err != nil {
				return err
			}
			visits, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (visit, error) {
				var v visit
				return v, row.Scan(&v.user, &v.lang, &v.company, &v.jobs)
			})
			if err != nil {
				return err
			}
			rows, err = tx.Query(ctx, `select x.employer_id, coalesce(c.owner_user_id, (select min(m.user_id) from employer_members m where m.employer_id = c.id), 0),
					coalesce((select lang from users where id = c.owner_user_id), 'kk'),
					(select count(*) from booth_checkins b where b.event_id = x.event_id and b.employer_id = x.employer_id),
					(select count(*) from booth_checkins b where b.event_id = x.event_id and b.employer_id = x.employer_id and jsonb_array_length(b.shared_fields) > 0)
				from event_exhibitors x join employers c on c.id = x.employer_id where x.event_id = $1 order by x.employer_id`, f.id)
			if err != nil {
				return err
			}
			for rows.Next() {
				var b struct {
					employer, owner int64
					lang            string
					total, shared   int
				}
				if err := rows.Scan(&b.employer, &b.owner, &b.lang, &b.total, &b.shared); err != nil {
					return err
				}
				booths = append(booths, b)
			}
			return rows.Err()
		})
		if err != nil {
			return st, err
		}
		for i := 0; i < len(visits); {
			j := i
			var names []string
			for ; j < len(visits) && visits[j].user == visits[i].user; j++ {
				n := html.EscapeString(visits[j].company)
				if visits[j].jobs > 0 {
					n += fmt.Sprintf(" (%d)", visits[j].jobs)
				}
				names = append(names, n)
			}
			v := visits[i]
			var text string
			if v.lang == "ru" {
				text = fmt.Sprintf("Вчера на «%s» вы отметились у стендов: %s. В скобках — открытые вакансии. Список и ссылки: %s", title("ru"), strings.Join(names, ", "), link(f.id))
			} else {
				text = fmt.Sprintf("Кеше «%s» жәрмеңкесінде барған стендтеріңіз: %s. Жақшада — ашық вакансиялар саны. Тізім мен сілтемелер: %s", title("kk"), strings.Join(names, ", "), link(f.id))
			}
			if err := Send(ctx, v.user, fmt.Sprintf("fair:%d:%d", f.id, v.user), text, link(f.id)); err != nil {
				log.Printf("fairs: summary %d/%d: %v", f.id, v.user, err)
			} else {
				st.Students++
			}
			i = j
		}
		for _, b := range booths {
			if b.owner == 0 || b.total == 0 {
				continue
			}
			cab := config.SiteURL() + "/employer"
			var text string
			if b.lang == "ru" {
				text = fmt.Sprintf("Итоги «%s»: у вашего стенда отметились %d студентов, %d поделились контактами. Список в кабинете: %s", title("ru"), b.total, b.shared, cab)
			} else {
				text = fmt.Sprintf("«%s» қорытындысы: стендіңізде %d студент тіркелді, %d байланысын бөлісті. Тізім кабинетте: %s", title("kk"), b.total, b.shared, cab)
			}
			if err := Send(ctx, b.owner, fmt.Sprintf("fair:%d:booth:%d", f.id, b.employer), text, cab); err != nil {
				log.Printf("fairs: employer summary %d/%d: %v", f.id, b.employer, err)
			} else {
				st.Employers++
			}
		}
	}
	return st, nil
}

func cronHandler(w http.ResponseWriter, r *http.Request) {
	if !events.CronAuthorized(r) {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	st, err := Summaries(r.Context(), Now())
	if err != nil {
		fail(w, r, "cron", err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

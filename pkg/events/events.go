// Package events serves real events (prompt 05): ambassadors / university staff / admins
// publish them, students register (capacity + waitlist through the event_register RPC),
// organizers check people in with a rotating signed QR, split them into small teams, finish
// the event (certificates) and read feedback. checked_in_at is the only "verified attendance".
//
// Everything is behind the "events" feature switch. API reference: docs/api/events.md.
package events

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// Cats are the event categories (the legacy EVENTS demo list plus mentoring and other).
var Cats = []string{"career", "youth", "volunteer", "sport", "training", "conf", "holiday", "mentoring", "other"}

// ManagerRoles may create and manage events of their own university; admin manages all.
var ManagerRoles = []string{"ambassador", "university_staff"}

// ExtraManager is the extension point for prompt 07: an approved mentor may manage only
// cat = "mentoring" group meetings. TODO(07): set it from the mentors package, e.g.
//
//	events.ExtraManager = func(ctx context.Context, u *auth.User, ev *Event) bool { return ev.Cat == "mentoring" && mentors.Approved(ctx, u.ID) }
//
// It is consulted only by the Go checks; the 07 migration must widen app_can_manage_university
// (or add a policy) for mentor-created rows as well.
var ExtraManager func(ctx context.Context, u *auth.User, ev *Event) bool

// Notify is the sender (category "events": switchable, quiet hours, daily cap); tests replace it.
var Notify = notify.For(notify.CatEvents)

// NotifyUrgent sends messages that must not be muted or deferred (an event was cancelled).
var NotifyUrgent = notify.For(notify.CatModeration)

// Now is the clock; tests replace it.
var Now = time.Now

var (
	MsgConsent       = httpx.Msg{KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."}
	MsgNoEvent       = httpx.Msg{KK: "Іс-шара табылмады.", RU: "Мероприятие не найдено."}
	MsgClosed        = httpx.Msg{KK: "Бұл іс-шараға тіркелу жабық.", RU: "Регистрация на это мероприятие закрыта."}
	MsgForeign       = httpx.Msg{KK: "Тек өз университетіңіздің іс-шарасын басқара аласыз.", RU: "Можно управлять только мероприятиями своего университета."}
	MsgInvalid       = httpx.Msg{KK: "Іс-шара деректері толық емес не қате.", RU: "Данные мероприятия неполные или неверные."}
	MsgNotReg        = httpx.Msg{KK: "Сіз бұл іс-шараға тіркелмегенсіз.", RU: "Вы не зарегистрированы на это мероприятие."}
	MsgStatus        = httpx.Msg{KK: "Іс-шара күйін бұлай өзгертуге болмайды.", RU: "Так изменить статус мероприятия нельзя."}
	MsgGroupCapacity = httpx.Msg{KK: "Топтық кездесуде 5–10 орын болуы керек.", RU: "В групповой встрече должно быть 5–10 мест."}
	MsgCapacity      = httpx.Msg{KK: "Орын саны тіркелгендерден аз болмауы керек.", RU: "Мест не может быть меньше, чем уже зарегистрировано."}
	MsgNoUniv        = httpx.Msg{KK: "Профильде университетіңізді көрсетіңіз.", RU: "Укажите университет в профиле."}
	MsgNotFinish     = httpx.Msg{KK: "Пікір іс-шара біткен соң ғана қабылданады.", RU: "Отзыв принимается только после мероприятия."}
	MsgNotCame       = httpx.Msg{KK: "Пікірді тек келгендер қалдыра алады.", RU: "Отзыв могут оставить только пришедшие."}
)

// Routes wires /api/events/*, all behind the "events" switch.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.Require("events", fn)) }
	// public / student
	h("GET /api/events", list)
	h("GET /api/events/my", auth.Need(myRegs))
	h("GET /api/events/my/attendance", auth.Need(myAttendance))
	h("GET /api/events/{id}", detail)
	h("GET /api/events/{id}/attendees", attendees)
	h("GET /api/events/{id}/ics", ics)
	h("POST /api/events/{id}/register", auth.Write(auth.Need(register)))
	h("DELETE /api/events/{id}/register", auth.Write(auth.Need(unregister)))
	h("POST /api/events/{id}/feedback", auth.Write(auth.Need(feedback)))
	h("POST /api/events/checkin", auth.Write(scan))
	h("POST /api/events/checkin/claim", auth.Write(auth.Need(claim)))
	// organizers
	h("GET /api/events/manage", auth.Need(manageList))
	h("POST /api/events/create", auth.Write(auth.Need(create))) // POST /api/events is the analytics tracker
	h("PATCH /api/events/{id}", auth.Write(auth.Need(update)))
	h("POST /api/events/{id}/checkin/start", auth.Write(auth.Need(startCheckin)))
	h("GET /api/events/{id}/checkin/live", auth.Need(live))
	h("POST /api/events/{id}/checkin/manual", auth.Write(auth.Need(manual)))
	h("POST /api/events/{id}/checkin/revoke", auth.Write(auth.Need(revoke)))
	h("GET /api/events/{id}/regs", auth.Need(regs))
	h("POST /api/events/{id}/teams", auth.Write(auth.Need(splitTeams)))
	h("POST /api/events/{id}/finish", auth.Write(auth.Need(finish)))
	h("GET /api/events/{id}/attendees.csv", auth.Need(csvExport))
	h("GET /api/events/{id}/feedback", auth.Need(feedbackSummary))
	// daily job (Bearer CRON_SECRET)
	h("GET /api/cron/events", cronHandler)
}

// Event is the API shape of an event.
type Event struct {
	ID             int64      `json:"id"`
	TitleKK        string     `json:"title_kk"`
	TitleRU        *string    `json:"title_ru"`
	DescKK         *string    `json:"desc_kk"`
	DescRU         *string    `json:"desc_ru"`
	Cat            string     `json:"cat"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         time.Time  `json:"ends_at"`
	University     string     `json:"university"`
	BuildingID     *string    `json:"building_id"`
	PlaceText      *string    `json:"place_text"`
	Capacity       *int       `json:"capacity"`
	WalkIn         bool       `json:"walk_in_allowed"`
	GivesCert      bool       `json:"gives_cert"`
	TeamsEnabled   bool       `json:"teams_enabled"`
	Status         string     `json:"status"`
	CreatedBy      *int64     `json:"created_by"`
	CheckinStarted *time.Time `json:"checkin_started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	CreatedAt      time.Time  `json:"created_at"`

	Registered int    `json:"registered"`
	Waitlist   int    `json:"waitlist"`
	CheckedIn  int    `json:"checked_in"`
	SpotsLeft  *int   `json:"spots_left"` // null = unlimited
	My         *MyReg `json:"my,omitempty"`
	CanManage  bool   `json:"can_manage"`
	checkinKey []byte // never serialized
}

// MyReg is the signed-in student's registration.
type MyReg struct {
	Status      string     `json:"status"` // registered | waitlist
	Ticket      string     `json:"ticket"`
	CheckedInAt *time.Time `json:"checked_in_at"`
	TeamNo      *int16     `json:"team_no"`
	Feedback    *int16     `json:"feedback_score"`
}

const eventCols = `e.id, e.title_kk, e.title_ru, e.desc_kk, e.desc_ru, e.cat, e.starts_at, e.ends_at, e.university,
	e.building_id, e.place_text, e.capacity, e.walk_in_allowed, e.gives_cert, e.teams_enabled, e.status::text,
	e.created_by, e.checkin_started_at, e.finished_at, e.created_at, e.checkin_key,
	(select count(*) from event_regs r where r.event_id = e.id and r.status = 'registered'),
	(select count(*) from event_regs r where r.event_id = e.id and r.status = 'waitlist'),
	(select count(*) from event_regs r where r.event_id = e.id and r.checked_in_at is not null)`

func scanEvent(row pgx.Row) (*Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.TitleKK, &e.TitleRU, &e.DescKK, &e.DescRU, &e.Cat, &e.StartsAt, &e.EndsAt, &e.University,
		&e.BuildingID, &e.PlaceText, &e.Capacity, &e.WalkIn, &e.GivesCert, &e.TeamsEnabled, &e.Status,
		&e.CreatedBy, &e.CheckinStarted, &e.FinishedAt, &e.CreatedAt, &e.checkinKey,
		&e.Registered, &e.Waitlist, &e.CheckedIn)
	if err != nil {
		return nil, err
	}
	if e.Capacity != nil {
		left := max(*e.Capacity-e.Registered, 0)
		e.SpotsLeft = &left
	}
	return &e, nil
}

// load reads one event as the connection owner (counts need every registration); callers
// apply visibility with visible() / CanManage().
func load(ctx context.Context, tx pgx.Tx, id int64, lock bool) (*Event, error) {
	q := "select " + eventCols + " from events e where e.id = $1"
	if lock {
		q += " for update of e"
	}
	return scanEvent(tx.QueryRow(ctx, q, id))
}

// CanManage: admin; ambassador / university staff of the event's university; ExtraManager (07).
func CanManage(ctx context.Context, u *auth.User, ev *Event) bool {
	if u == nil {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	if slices.Contains(ManagerRoles, u.Role) && u.University != nil && *u.University == ev.University {
		return true
	}
	return ExtraManager != nil && ExtraManager(ctx, u, ev)
}

// AggregateOnly: university staff manage their university's events but never see an
// individual student's data (10-b2b: «Университет қызметкері ешқашан жеке студенттің дерегін
// көрмейді — тек жиынтық сандар»). They get counts in place of lists, an aggregate CSV, the
// feedback distribution (from 5 answers) without comments, and manual check-in by e-ticket
// code only. Ambassadors, approved mentors (their own meetings) and admins run check-in and
// keep the attendee list (05: «Келгендер тізімі + CSV экспорт — ұйымдастырушы мен admin»).
func AggregateOnly(u *auth.User) bool { return u != nil && u.Role == "university_staff" }

// MsgAggregate refuses an action that needs a student's identity to staff.
var MsgAggregate = httpx.Msg{
	KK: "Университет қызметкері студенттердің жеке деректерін көрмейді — тек жиынтық сандар. Қолмен белгілеу үшін студенттің электронды билетінің кодын енгізіңіз.",
	RU: "Сотрудник университета не видит личные данные студентов — только сводные цифры. Для ручной отметки введите код электронного билета студента.",
}

func visible(ctx context.Context, u *auth.User, ev *Event) bool {
	return ev.Status != "draft" || CanManage(ctx, u, ev)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("events: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// withEvent loads the event (404 when missing or invisible) and runs fn as the owner.
func withEvent(w http.ResponseWriter, r *http.Request, u *auth.User, manage bool, fn func(tx pgx.Tx, ev *Event) error) bool {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
		return false
	}
	var status int
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		ev, err := load(r.Context(), tx, id, manage)
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
			return nil
		}
		if err != nil {
			return err
		}
		if !visible(r.Context(), u, ev) {
			status = http.StatusNotFound
			return nil
		}
		ev.CanManage = CanManage(r.Context(), u, ev)
		if manage && !ev.CanManage {
			status = http.StatusForbidden
			return nil
		}
		return fn(tx, ev)
	})
	switch {
	case err != nil:
		var he *httpErr
		if errors.As(err, &he) {
			httpx.Error(w, r, he.status, he.code, he.msg)
			return false
		}
		fail(w, r, r.Method+" "+r.URL.Path, err)
		return false
	case status == http.StatusNotFound:
		httpx.Error(w, r, status, "not_found", MsgNoEvent)
		return false
	case status == http.StatusForbidden:
		httpx.Error(w, r, status, "forbidden", MsgForeign)
		return false
	}
	return true
}

// httpErr rolls back a transaction and becomes an API error.
type httpErr struct {
	status int
	code   string
	msg    httpx.Msg
}

func (e *httpErr) Error() string { return e.code }

func refuse(status int, code string, msg httpx.Msg) error { return &httpErr{status, code, msg} }

func writeErr(w http.ResponseWriter, r *http.Request, what string, err error) {
	var he *httpErr
	if errors.As(err, &he) {
		httpx.Error(w, r, he.status, he.code, he.msg)
		return
	}
	fail(w, r, what, err)
}

// ---------- reading ----------

func list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.Current(r)
	q := r.URL.Query()
	scope := q.Get("scope")
	univ := strings.TrimSpace(q.Get("university"))
	where := "e.status = 'published' and e.ends_at > now()"
	order := "e.starts_at"
	switch scope {
	case "", "upcoming":
	case "past":
		where, order = "(e.status = 'finished' or (e.status = 'published' and e.ends_at <= now()))", "e.starts_at desc"
	case "all":
		where = "e.status in ('published', 'finished', 'cancelled')"
	default:
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 200 {
		limit = 100
	}
	var uid int64
	if u != nil {
		uid = u.ID
	}
	out := []*Event{}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "select "+eventCols+" from events e where "+where+
			" and ($1 = '' or e.university = $1) order by "+order+" limit $2", univ, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			ev, err := scanEvent(rows)
			if err != nil {
				rows.Close()
				return err
			}
			ev.CanManage = CanManage(ctx, u, ev)
			out = append(out, ev)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if uid != 0 {
			for _, ev := range out {
				if ev.My, err = myReg(ctx, tx, ev.ID, uid); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		fail(w, r, "list", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}

func myReg(ctx context.Context, tx pgx.Tx, eventID, userID int64) (*MyReg, error) {
	var m MyReg
	err := tx.QueryRow(ctx, `select status, ticket, checked_in_at, team_no, feedback_score from event_regs
		where event_id = $1 and user_id = $2`, eventID, userID).Scan(&m.Status, &m.Ticket, &m.CheckedInAt, &m.TeamNo, &m.Feedback)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func detail(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	var out *Event
	ok := withEvent(w, r, u, false, func(tx pgx.Tx, ev *Event) error {
		if u != nil {
			var err error
			if ev.My, err = myReg(r.Context(), tx, ev.ID, u.ID); err != nil {
				return err
			}
		}
		out = ev
		return nil
	})
	if ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"event": out})
	}
}

type attendee struct {
	Name    *string `json:"name"`
	Faculty *string `json:"faculty"`
}

// attendees: the count is public; names and faculties only for registered people and organizers.
func attendees(w http.ResponseWriter, r *http.Request) {
	u := auth.Current(r)
	var count int
	var people []attendee
	ok := withEvent(w, r, u, false, func(tx pgx.Tx, ev *Event) error {
		count = ev.Registered
		if AggregateOnly(u) {
			return nil // staff: the count only, even for an event they are registered for
		}
		allowed := ev.CanManage
		if !allowed && u != nil {
			m, err := myReg(r.Context(), tx, ev.ID, u.ID)
			if err != nil {
				return err
			}
			allowed = m != nil && m.Status == "registered"
		}
		if !allowed {
			return nil
		}
		rows, err := tx.Query(r.Context(), `select u.name, u.faculty from event_regs r join users u on u.id = r.user_id
			where r.event_id = $1 and r.status = 'registered' and u.deleted_at is null order by r.registered_at`, ev.ID)
		if err != nil {
			return err
		}
		people, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (attendee, error) {
			var a attendee
			return a, row.Scan(&a.Name, &a.Faculty)
		})
		return err
	})
	if !ok {
		return
	}
	resp := map[string]any{"count": count}
	if people != nil {
		resp["attendees"] = people
	}
	httpx.JSON(w, http.StatusOK, resp)
}

type myEvent struct {
	Event *Event `json:"event"`
	MyReg
}

func myRegs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []myEvent{}
	// RLS: the student's own registrations only.
	var ids []int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select event_id from event_regs where user_id = app_user_id() order by registered_at desc limit 200`)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	if err == nil && len(ids) > 0 {
		err = db.Tx(ctx, func(tx pgx.Tx) error {
			for _, id := range ids {
				ev, err := load(ctx, tx, id, false)
				if err != nil {
					return err
				}
				m, err := myReg(ctx, tx, id, u.ID)
				if err != nil || m == nil {
					return err
				}
				ev.CanManage = CanManage(ctx, u, ev)
				out = append(out, myEvent{ev, *m})
			}
			return nil
		})
	}
	if err != nil {
		fail(w, r, "my", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"registrations": out})
}

type Attendance struct {
	EventID        int64     `json:"event_id"`
	TitleKK        string    `json:"title_kk"`
	TitleRU        *string   `json:"title_ru"`
	Cat            string    `json:"cat"`
	University     string    `json:"university"`
	StartsAt       time.Time `json:"starts_at"`
	CheckedInAt    time.Time `json:"checked_in_at"`
	HasCertificate bool      `json:"has_certificate"`
	Verified       bool      `json:"verified"` // always true: rows exist only for checked_in_at
}

// VerifiedAttendance is the portfolio (09) source: events where the organizer confirmed the
// student's presence. Runs as app_user (RLS: own rows only).
func VerifiedAttendance(ctx context.Context, userID int64) ([]Attendance, error) {
	var out []Attendance
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select event_id, title_kk, title_ru, cat, university, starts_at, checked_in_at, has_certificate
			from verified_attendance where user_id = app_user_id() order by starts_at desc`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Attendance, error) {
			a := Attendance{Verified: true}
			return a, row.Scan(&a.EventID, &a.TitleKK, &a.TitleRU, &a.Cat, &a.University, &a.StartsAt, &a.CheckedInAt, &a.HasCertificate)
		})
		return err
	})
	if out == nil {
		out = []Attendance{}
	}
	return out, err
}

func myAttendance(w http.ResponseWriter, r *http.Request, u *auth.User) {
	out, err := VerifiedAttendance(r.Context(), u.ID)
	if err != nil {
		fail(w, r, "attendance", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"attendance": out})
}

func manageList(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.Role != "admin" && !slices.Contains(ManagerRoles, u.Role) && ExtraManager == nil {
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
		return
	}
	univ := ""
	if u.Role != "admin" {
		if u.University == nil {
			httpx.JSON(w, http.StatusOK, map[string]any{"events": []*Event{}})
			return
		}
		univ = *u.University
	}
	out := []*Event{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "select "+eventCols+` from events e where ($1 = '' or e.university = $1)
			order by e.status = 'finished', e.starts_at desc limit 300`, univ)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			ev, err := scanEvent(rows)
			if err != nil {
				return err
			}
			if ev.CanManage = CanManage(ctx, u, ev); ev.CanManage {
				out = append(out, ev)
			}
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "manage", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}

// ---------- create / update ----------

type eventInput struct {
	TitleKK      *string    `json:"title_kk"`
	TitleRU      *string    `json:"title_ru"`
	DescKK       *string    `json:"desc_kk"`
	DescRU       *string    `json:"desc_ru"`
	Cat          *string    `json:"cat"`
	StartsAt     *time.Time `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	University   *string    `json:"university"`
	BuildingID   *string    `json:"building_id"`
	PlaceText    *string    `json:"place_text"`
	Capacity     *int       `json:"capacity"`
	WalkIn       *bool      `json:"walk_in_allowed"`
	GivesCert    *bool      `json:"gives_cert"`
	TeamsEnabled *bool      `json:"teams_enabled"`
	Status       *string    `json:"status"`
}

var reBuilding = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func cleanText(p *string, min, max int) (*string, bool) {
	if p == nil {
		return nil, true
	}
	s := strings.TrimSpace(*p)
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return nil, false
	}
	n := utf8.RuneCountInString(s)
	if n > max || (n < min && n > 0) || (min > 0 && n == 0) {
		return nil, false
	}
	if s == "" {
		return nil, true
	}
	return &s, true
}

// validate checks the fields present in in; for create every required field must be set.
func (in *eventInput) validate(create bool) bool {
	var ok bool
	if create && (in.TitleKK == nil || in.Cat == nil || in.StartsAt == nil || in.EndsAt == nil) {
		return false
	}
	if in.TitleKK != nil {
		if in.TitleKK, ok = cleanText(in.TitleKK, 3, 140); !ok || in.TitleKK == nil {
			return false
		}
	}
	for _, f := range []struct {
		p   **string
		max int
	}{{&in.TitleRU, 140}, {&in.DescKK, 3000}, {&in.DescRU, 3000}, {&in.PlaceText, 200}} {
		if *f.p, ok = cleanText(*f.p, 0, f.max); !ok {
			return false
		}
	}
	if in.Cat != nil && !slices.Contains(Cats, *in.Cat) {
		return false
	}
	if in.BuildingID != nil && *in.BuildingID != "" && !reBuilding.MatchString(*in.BuildingID) {
		return false
	}
	if in.Capacity != nil && (*in.Capacity < 1 || *in.Capacity > 100000) {
		return false
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		return false
	}
	if in.EndsAt != nil && in.StartsAt != nil && in.EndsAt.Sub(*in.StartsAt) > 14*24*time.Hour {
		return false
	}
	if in.Status != nil && *in.Status != "draft" && *in.Status != "published" && *in.Status != "cancelled" {
		return false
	}
	return true
}

func isRLS(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42501"
}

func create(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	// other roles get through only via ExtraManager (approved mentors, cat = mentoring); CanManage decides below
	byRole := u.Role == "admin" || slices.Contains(ManagerRoles, u.Role)
	if !byRole && ExtraManager == nil {
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
		return
	}
	var in eventInput
	if err := httpx.Decode(r, &in, 16<<10); err != nil || !in.validate(true) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_event", MsgInvalid)
		return
	}
	if in.Status != nil && *in.Status == "cancelled" {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_status", MsgStatus)
		return
	}
	if in.StartsAt.Before(Now().Add(-24 * time.Hour)) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_event", MsgInvalid)
		return
	}
	univ := ""
	if in.University != nil {
		univ = strings.TrimSpace(*in.University)
	}
	if u.Role != "admin" {
		if u.University == nil || *u.University == "" {
			httpx.Error(w, r, http.StatusForbidden, "no_university", MsgNoUniv)
			return
		}
		if univ != "" && univ != *u.University {
			httpx.Error(w, r, http.StatusForbidden, "foreign_university", MsgForeign)
			return
		}
		univ = *u.University
	}
	if univ == "" || utf8.RuneCountInString(univ) > 120 {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_event", MsgInvalid)
		return
	}
	probe := &Event{University: univ, Cat: *in.Cat}
	if !CanManage(ctx, u, probe) {
		code, msg := "foreign_university", MsgForeign
		if !byRole {
			code, msg = "forbidden", httpx.MsgForbid
		}
		httpx.Error(w, r, http.StatusForbidden, code, msg)
		return
	}
	// a mentor's group meeting is a small group: 5–10 seats (prompt 07)
	if !byRole && (in.Capacity == nil || *in.Capacity < 5 || *in.Capacity > 10) {
		httpx.Error(w, r, http.StatusBadRequest, "group_capacity", MsgGroupCapacity)
		return
	}
	status := "draft"
	if in.Status != nil {
		status = *in.Status
	}
	var id int64
	// Inserted as app_user: the events_manage_insert policy re-checks the university rule.
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `insert into events (title_kk, title_ru, desc_kk, desc_ru, cat, starts_at, ends_at, university,
				building_id, place_text, capacity, walk_in_allowed, gives_cert, teams_enabled, status, created_by)
			values ($1, $2, $3, $4, $5, $6, $7, $8, nullif($9, ''), $10, $11, $12, $13, $14, $15::event_status, app_user_id())
			returning id`,
			in.TitleKK, in.TitleRU, in.DescKK, in.DescRU, in.Cat, in.StartsAt, in.EndsAt, univ,
			deref(in.BuildingID), in.PlaceText, in.Capacity, boolOr(in.WalkIn), boolOr(in.GivesCert), boolOr(in.TeamsEnabled), status).Scan(&id)
	})
	if isRLS(err) {
		httpx.Error(w, r, http.StatusForbidden, "foreign_university", MsgForeign)
		return
	}
	if err != nil {
		fail(w, r, "create", err)
		return
	}
	var ev *Event
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		ev, err = load(ctx, tx, id, false)
		return err
	})
	if err != nil {
		fail(w, r, "create load", err)
		return
	}
	ev.CanManage = true
	httpx.JSON(w, http.StatusCreated, map[string]any{"event": ev})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolOr(p *bool) bool { return p != nil && *p }

// allowed status changes through PATCH; "finished" only via /finish.
var transitions = map[string][]string{
	"draft":     {"draft", "published", "cancelled"},
	"published": {"published", "cancelled"},
}

func update(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	var raw map[string]any
	var in eventInput
	body, err := readBody(r, 16<<10)
	if err == nil {
		err = jsonUnmarshal(body, &in)
	}
	if err == nil {
		err = jsonUnmarshal(body, &raw)
	}
	if err != nil || len(raw) == 0 || !in.validate(false) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_event", MsgInvalid)
		return
	}
	if _, ok := raw["university"]; ok {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_event", MsgInvalid) // an event never moves to another university
		return
	}
	_, capSet := raw["capacity"]
	var before *Event
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		before = ev
		if in.Status != nil && !slices.Contains(transitions[ev.Status], *in.Status) {
			return refuse(http.StatusConflict, "invalid_status", MsgStatus)
		}
		if ev.Status == "finished" || ev.Status == "cancelled" {
			return refuse(http.StatusConflict, "invalid_status", MsgStatus)
		}
		starts, ends := ev.StartsAt, ev.EndsAt
		if in.StartsAt != nil {
			starts = *in.StartsAt
		}
		if in.EndsAt != nil {
			ends = *in.EndsAt
		}
		if !ends.After(starts) {
			return refuse(http.StatusBadRequest, "invalid_event", MsgInvalid)
		}
		if capSet && in.Capacity != nil && *in.Capacity < ev.Registered {
			return refuse(http.StatusConflict, "capacity_below_registered", MsgCapacity)
		}
		return nil
	})
	if !ok {
		return
	}
	// The update itself runs as app_user so the RLS policy applies as well.
	sets := []string{"updated_at = now()"}
	args := []any{before.ID}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if in.TitleKK != nil {
		add("title_kk", *in.TitleKK)
	}
	for k, col := range map[string]string{"title_ru": "title_ru", "desc_kk": "desc_kk", "desc_ru": "desc_ru", "place_text": "place_text"} {
		if _, ok := raw[k]; ok {
			switch k {
			case "title_ru":
				add(col, in.TitleRU)
			case "desc_kk":
				add(col, in.DescKK)
			case "desc_ru":
				add(col, in.DescRU)
			case "place_text":
				add(col, in.PlaceText)
			}
		}
	}
	if in.Cat != nil {
		add("cat", *in.Cat)
	}
	if in.StartsAt != nil {
		add("starts_at", *in.StartsAt)
	}
	if in.EndsAt != nil {
		add("ends_at", *in.EndsAt)
	}
	if _, ok := raw["building_id"]; ok {
		if in.BuildingID == nil || *in.BuildingID == "" {
			add("building_id", nil)
		} else {
			add("building_id", *in.BuildingID)
		}
	}
	if capSet {
		add("capacity", in.Capacity)
	}
	if in.WalkIn != nil {
		add("walk_in_allowed", *in.WalkIn)
	}
	if in.GivesCert != nil {
		add("gives_cert", *in.GivesCert)
	}
	if in.TeamsEnabled != nil {
		add("teams_enabled", *in.TeamsEnabled)
	}
	if in.Status != nil {
		args = append(args, *in.Status)
		sets = append(sets, "status = $"+strconv.Itoa(len(args))+"::event_status")
	}
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "update events set "+strings.Join(sets, ", ")+" where id = $1", args...)
		if err == nil && tag.RowsAffected() != 1 {
			return refuse(http.StatusForbidden, "foreign_university", MsgForeign)
		}
		return err
	})
	if isRLS(err) {
		err = refuse(http.StatusForbidden, "foreign_university", MsgForeign)
	}
	if err != nil {
		writeErr(w, r, "update", err)
		return
	}
	// More seats: move people up from the waitlist. Cancelled: tell everyone.
	if capSet {
		promoted, err := promoteWaitlist(ctx, before.ID)
		if err != nil {
			log.Printf("events: promote %d: %v", before.ID, err)
		}
		go notifyPromoted(context.WithoutCancel(ctx), before, promoted)
	}
	if in.Status != nil && *in.Status == "cancelled" && before.Status != "cancelled" {
		go notifyCancelled(context.WithoutCancel(ctx), before)
	}
	var ev *Event
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		ev, err = load(ctx, tx, before.ID, false)
		return err
	})
	if err != nil {
		fail(w, r, "update load", err)
		return
	}
	ev.CanManage = true
	httpx.JSON(w, http.StatusOK, map[string]any{"event": ev})
}

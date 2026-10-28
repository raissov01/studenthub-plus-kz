package ambassador

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

// Count is an aggregate shown to ambassadors: 1–4 is reported as "< 5" so a small group
// (e.g. one faculty) never points at individual people.
type Count int

func (c Count) MarshalJSON() ([]byte, error) {
	if c > 0 && c < 5 {
		return []byte(`"< 5"`), nil
	}
	return []byte(strconv.Itoa(int(c))), nil
}

// Workload: an ambassador never gets more work in a week than the hours they promised. There
// is no task system yet, so the unit is "one useful item" (a submission or an event created)
// ≈ one hour. Available is the gate any future task assignment / nudge must check.
type Workload struct {
	HoursPerWeek  int        `json:"hours_per_week"`
	ItemsThisWeek int        `json:"items_this_week"`
	PausedUntil   *time.Time `json:"paused_until"`
	Paused        bool       `json:"paused"`
	Available     bool       `json:"available"`
}

func weekStart(now time.Time) time.Time {
	d := AlmatyToday(now)
	off := (int(d.Weekday()) + 6) % 7 // Monday = 0
	return d.AddDate(0, 0, -off)
}

func workload(ctx context.Context, tx pgx.Tx, t *Term, now time.Time) (Workload, error) {
	w := Workload{HoursPerWeek: t.HoursPerWeek, PausedUntil: t.PausedUntil}
	w.Paused = t.PausedUntil != nil && !t.PausedUntil.Before(AlmatyToday(now))
	// week starts Monday 00:00 Almaty = Sunday 19:00 UTC
	from := weekStart(now).Add(-5 * time.Hour)
	err := tx.QueryRow(ctx, `select
		(select count(*) from submissions where submitted_by = $1 and created_at >= $2 and kind in ('opportunity', 'place'))
		+ (select count(*) from events where created_by = $1 and created_at >= $2)`, t.UserID, from).Scan(&w.ItemsThisWeek)
	w.Available = !w.Paused && w.ItemsThisWeek < w.HoursPerWeek
	return w, err
}

// Available reports whether the ambassador may be given more work this week.
func Available(ctx context.Context, userID int64) (bool, error) {
	var ok bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		t, err := ActiveTerm(ctx, tx, userID, Now())
		if err != nil || t == nil {
			return err
		}
		w, err := workload(ctx, tx, t, Now())
		ok = w.Available
		return err
	})
	return ok, err
}

type resource struct {
	TitleKK string `json:"title_kk"`
	TitleRU string `json:"title_ru"`
	URL     string `json:"url"`
}

// Resources: AMBASSADOR_RESOURCES (JSON array of {title_kk,title_ru,url}) or placeholders.
func Resources() []resource {
	var out []resource
	if raw := config.Str("AMBASSADOR_RESOURCES", ""); raw != "" && json.Unmarshal([]byte(raw), &out) == nil {
		return out
	}
	return []resource{
		{"Амбассадор нұсқаулығы", "Руководство амбассадора", ""},
		{"Постер үлгілері", "Шаблоны постеров", ""},
		{"Іс-шара сценарийлері", "Сценарии мероприятий", ""},
	}
}

type teammate struct {
	Name         *string   `json:"name"`
	Username     *string   `json:"username"`
	RoleInTeam   string    `json:"role_in_team"`
	TermStart    time.Time `json:"term_start"`
	TermEnd      time.Time `json:"term_end"`
	HoursPerWeek int       `json:"hours_per_week"`
	Paused       bool      `json:"paused"`
}

func cabinet(w http.ResponseWriter, r *http.Request, u *auth.User) {
	t, ok := needTerm(w, r, u)
	if !ok {
		return
	}
	ctx := r.Context()
	now := Now()
	var wl Workload
	var refs int
	team := []teammate{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if wl, err = workload(ctx, tx, t, now); err != nil {
			return err
		}
		if refs, err = Referrals(ctx, tx, t.RefCode, u.ID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select u.name, u.tg_username, t.role_in_team, t.term_start, t.term_end, t.hours_per_week,
				coalesce(t.paused_until >= $2::date, false)
			from ambassador_terms t join users u on u.id = t.user_id and u.deleted_at is null
			where t.university = $1 and t.status = 'active' and t.term_end >= $2::date
			order by t.role_in_team = 'lead' desc, t.term_start`, t.University, AlmatyToday(now))
		if err != nil {
			return err
		}
		team, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (teammate, error) {
			var m teammate
			return m, row.Scan(&m.Name, &m.Username, &m.RoleInTeam, &m.TermStart, &m.TermEnd, &m.HoursPerWeek, &m.Paused)
		})
		return err
	})
	if err != nil {
		fail(w, r, "cabinet", err)
		return
	}
	daysLeft := int(t.TermEnd.Sub(AlmatyToday(now)).Hours() / 24)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"term":      t,
		"days_left": daysLeft,
		"renewal":   map[string]any{"ask": daysLeft <= RenewAskDays && t.RenewAnswer == nil, "answer": t.RenewAnswer},
		"referral":  map[string]any{"code": t.RefCode, "ref": "amb_" + t.RefCode, "link": RefLink(t.RefCode), "joined": refs},
		"workload":  wl,
		"team":      team,
		"resources": Resources(),
	})
}

// UniversityStats are aggregate numbers only (no names); every count goes through Count.
type UniversityStats struct {
	University     string           `json:"university"`
	Students       Count            `json:"students"`
	WeeklyActive   Count            `json:"weekly_active"`
	SavedDeadlines Count            `json:"saved_deadlines"`
	Events         Count            `json:"events"`
	Registrations  Count            `json:"registrations"`
	CheckIns       Count            `json:"check_ins"`
	ByFaculty      map[string]Count `json:"by_faculty"`
}

// Stats for one university (events: last 90 days).
func Stats(ctx context.Context, tx pgx.Tx, univ string) (UniversityStats, error) {
	s := UniversityStats{University: univ, ByFaculty: map[string]Count{}}
	err := tx.QueryRow(ctx, `select
		(select count(*) from users where university = $1 and deleted_at is null and consent_at is not null),
		(select count(*) from users where university = $1 and deleted_at is null and consent_at is not null and last_seen_at > now() - interval '7 days'),
		(select count(*) from reminders r join users u on u.id = r.user_id where u.university = $1 and u.deleted_at is null and r.status = 'active'),
		(select count(*) from events where university = $1 and status in ('published', 'finished') and starts_at > now() - interval '90 days'),
		(select count(*) from event_regs r join events e on e.id = r.event_id where e.university = $1 and r.status = 'registered' and e.starts_at > now() - interval '90 days'),
		(select count(*) from event_regs r join events e on e.id = r.event_id where e.university = $1 and r.checked_in_at is not null and e.starts_at > now() - interval '90 days')`,
		univ).Scan(&s.Students, &s.WeeklyActive, &s.SavedDeadlines, &s.Events, &s.Registrations, &s.CheckIns)
	if err != nil {
		return s, err
	}
	rows, err := tx.Query(ctx, `select coalesce(nullif(faculty, ''), '—'), count(*) from users
		where university = $1 and deleted_at is null and consent_at is not null group by 1`, univ)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		var n int
		if err := rows.Scan(&f, &n); err != nil {
			return s, err
		}
		s.ByFaculty[f] = Count(n)
	}
	return s, rows.Err()
}

// stats: an ambassador sees only their own university; admin may pass ?university=.
func stats(w http.ResponseWriter, r *http.Request, u *auth.User) {
	univ := r.URL.Query().Get("university")
	if u.Role == "admin" && univ != "" {
		// admin: any university
	} else {
		t, ok := needTerm(w, r, u)
		if !ok {
			return
		}
		if univ != "" && univ != t.University {
			httpx.Error(w, r, http.StatusForbidden, "foreign_university", MsgForeign)
			return
		}
		univ = t.University
	}
	var s UniversityStats
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		s, err = Stats(r.Context(), tx, univ)
		return err
	})
	if err != nil {
		fail(w, r, "stats", err)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

// pause: "rest week" — no tasks or nudges until the end of the current week (or ?weeks=1..2).
func pause(w http.ResponseWriter, r *http.Request, u *auth.User) {
	t, ok := needTerm(w, r, u)
	if !ok {
		return
	}
	var body struct {
		Weeks int `json:"weeks"`
	}
	_ = httpx.Decode(r, &body, 1<<10)
	if body.Weeks < 1 || body.Weeks > 2 {
		body.Weeks = 1
	}
	until := weekStart(Now()).AddDate(0, 0, 7*body.Weeks-1) // Sunday
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `update ambassador_terms set paused_until = $2 where id = $1`, t.ID, until)
		return err
	})
	if err != nil {
		fail(w, r, "pause", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"paused_until": until.Format("2006-01-02")})
}

func unpause(w http.ResponseWriter, r *http.Request, u *auth.User) {
	t, ok := needTerm(w, r, u)
	if !ok {
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `update ambassador_terms set paused_until = null where id = $1`, t.ID)
		return err
	})
	if err != nil {
		fail(w, r, "unpause", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"paused_until": nil})
}

// renew: answer to "continue next semester?". Yes -> a renewal item in the admin queue
// (approval extends term_end by one semester); no -> the term simply ends on term_end.
func renew(w http.ResponseWriter, r *http.Request, u *auth.User) {
	t, ok := needTerm(w, r, u)
	if !ok {
		return
	}
	var body struct {
		Continue *bool `json:"continue"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Continue == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var sid int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update ambassador_terms set renew_answer = $2 where id = $1`, t.ID, *body.Continue); err != nil {
			return err
		}
		if !*body.Continue {
			return nil
		}
		var pending int
		if err := tx.QueryRow(ctx, `select count(*) from submissions where kind = $1 and status = 'pending' and submitted_by = $2`,
			KindRenewal, u.ID).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return &moderation.Refusal{Code: "already_pending", Msg: MsgRenewWait}
		}
		var err error
		sid, err = moderation.Submit(ctx, tx, KindRenewal, map[string]any{"term_id": t.ID, "term_end": t.TermEnd.Format("2006-01-02"),
			"name": u.Name, "role_in_team": t.RoleInTeam}, &u.ID, t.University)
		return err
	})
	if ref, ok := err.(*moderation.Refusal); ok {
		httpx.Error(w, r, http.StatusConflict, ref.Code, ref.Msg)
		return
	}
	if err != nil {
		fail(w, r, "renew", err)
		return
	}
	resp := map[string]any{"continue": *body.Continue}
	if sid != 0 {
		resp["submission_id"] = sid
	}
	httpx.JSON(w, http.StatusOK, resp)
}

type ratingRow struct {
	Rank          int    `json:"rank"`
	University    string `json:"university"`
	WeeklyActive  Count  `json:"weekly_active"`
	EventCheckIns Count  `json:"event_check_ins"` // last 30 days
	Score         int    `json:"score"`
}

// rating: team rating between universities with an active ambassador team (not individuals),
// visible to everyone. Score = weekly active students + event check-ins in 30 days.
func rating(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []ratingRow{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `with teams as (select distinct university from ambassador_terms where status = 'active' and term_end >= $1::date)
			select t.university,
				(select count(*) from users u where u.university = t.university and u.deleted_at is null and u.consent_at is not null
					and u.last_seen_at > now() - interval '7 days'),
				(select count(*) from event_regs r join events e on e.id = r.event_id where e.university = t.university
					and r.checked_in_at > now() - interval '30 days')
			from teams t`, AlmatyToday(Now()))
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ratingRow, error) {
			var x ratingRow
			err := row.Scan(&x.University, &x.WeeklyActive, &x.EventCheckIns)
			x.Score = int(x.WeeklyActive) + int(x.EventCheckIns)
			return x, err
		})
		return err
	})
	if err != nil {
		fail(w, r, "rating", err)
		return
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].University < out[j].University
	})
	var mine *ratingRow
	u := auth.Current(r)
	for i := range out {
		out[i].Rank = i + 1
		if i > 0 && out[i].Score == out[i-1].Score {
			out[i].Rank = out[i-1].Rank
		}
		if u != nil && u.University != nil && *u.University == out[i].University {
			mine = &out[i]
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	httpx.JSON(w, http.StatusOK, map[string]any{"rating": out, "mine": mine})
}

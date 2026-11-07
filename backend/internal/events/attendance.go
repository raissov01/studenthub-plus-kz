package events

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// The attendance overview of /admin/attendance (kurs AttendancePage, adapted to QR check-in): for the events the
// caller may manage (the same rule as /api/events/manage: admin all, ambassadors and university staff their
// university, club admins their clubs), registered vs checked in per event and per week. Marking people by hand
// stays on the event's own check-in page (/organize/<id>).

type attendanceRow struct {
	ID         int64     `json:"id"`
	TitleKK    string    `json:"title_kk"`
	TitleRU    *string   `json:"title_ru"`
	Cat        string    `json:"cat"`
	University string    `json:"university"`
	ClubID     *int64    `json:"club_id"`
	Status     string    `json:"status"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Capacity   *int      `json:"capacity"`
	Registered int       `json:"registered"`
	CheckedIn  int       `json:"checked_in"`
	QR         int       `json:"qr"`
	Manual     int       `json:"manual"`
	WalkIn     int       `json:"walk_in"`
}

type attendanceWeek struct {
	Week       string `json:"week"` // Monday, Asia/Almaty
	Events     int    `json:"events"`
	Registered int    `json:"registered"`
	CheckedIn  int    `json:"checkedIn"`
}

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

func mondayOf(t time.Time) time.Time {
	t = t.In(almaty)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, almaty)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// attendanceOverview: GET /api/events/attendance?weeks=12 (4–26) → {weeks, events, totals}. Drafts, cancelled and
// sample events are left out; the list is newest first and holds only events that have started.
func attendanceOverview(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if u.Role != "admin" && !slices.Contains(ManagerRoles, u.Role) {
		// anyone else only as a club admin (their clubs' meetings); everyone else 403
		var clubAdmin bool
		err := db.Tx(r.Context(), func(tx pgx.Tx) error {
			return tx.QueryRow(r.Context(), `select exists (select 1 from club_admins where user_id = $1)`, u.ID).Scan(&clubAdmin)
		})
		if err != nil {
			fail(w, r, "attendance gate", err)
			return
		}
		if !clubAdmin || ClubManager == nil {
			httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
			return
		}
	}
	weeks, err := strconv.Atoi(r.URL.Query().Get("weeks"))
	if err != nil || weeks < 4 || weeks > 26 {
		weeks = 12
	}
	ctx := r.Context()
	now := Now()
	first := mondayOf(now).AddDate(0, 0, -7*(weeks-1))
	empty := ""
	univ := &empty
	if u.Role != "admin" {
		univ = u.University
	}
	rows := []attendanceRow{}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		q, err := tx.Query(ctx, `select e.id, e.title_kk, e.title_ru, e.cat, e.university, e.club_id, e.status::text, e.starts_at, e.ends_at, e.capacity,
				count(r.user_id) filter (where r.status = 'registered'),
				count(r.user_id) filter (where r.checked_in_at is not null),
				count(r.user_id) filter (where r.checked_in_at is not null and r.checkin_method = 'qr'),
				count(r.user_id) filter (where r.checked_in_at is not null and r.checkin_method = 'manual'),
				count(r.user_id) filter (where r.checked_in_at is not null and r.checkin_method = 'walk_in')
			from events e left join event_regs r on r.event_id = e.id
			where e.status in ('published', 'finished') and not e.is_demo and e.starts_at >= $1 and e.starts_at <= $2
				and ($3 = '' or e.university = $3 or ($4 and e.club_id is not null))
			group by e.id order by e.starts_at desc limit 1000`, first, now, univ, ClubManager != nil)
		if err != nil {
			return err
		}
		rows, err = pgx.CollectRows(q, func(row pgx.CollectableRow) (attendanceRow, error) {
			var a attendanceRow
			return a, row.Scan(&a.ID, &a.TitleKK, &a.TitleRU, &a.Cat, &a.University, &a.ClubID, &a.Status, &a.StartsAt, &a.EndsAt, &a.Capacity,
				&a.Registered, &a.CheckedIn, &a.QR, &a.Manual, &a.WalkIn)
		})
		return err
	})
	if err != nil {
		fail(w, r, "attendance", err)
		return
	}
	mine := rows[:0]
	for _, a := range rows {
		if CanManage(ctx, u, &Event{University: a.University, ClubID: a.ClubID, Cat: a.Cat}) {
			mine = append(mine, a)
		}
	}
	byWeek := make([]attendanceWeek, weeks)
	for i := range byWeek {
		byWeek[i].Week = first.AddDate(0, 0, 7*i).Format(time.DateOnly)
	}
	var reg, came int
	for _, a := range mine {
		i := int(mondayOf(a.StartsAt).Sub(first).Hours()+1) / (24 * 7)
		if i >= 0 && i < weeks {
			byWeek[i].Events++
			byWeek[i].Registered += a.Registered
			byWeek[i].CheckedIn += a.CheckedIn
		}
		reg += a.Registered
		came += a.CheckedIn
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"weeks": byWeek, "events": mine,
		"totals": map[string]any{"events": len(mine), "registered": reg, "checkedIn": came}})
}

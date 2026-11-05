// Package console serves the role-scoped side of /admin: GET /api/console/me tells the signed-in person which
// panels they have (admin, moderator, the café or print shop they work at, the clubs they run, their
// university's voice inbox, their company, their ambassador or mentor cabinet) with the few counts each panel
// card shows, and the shop workspace (today's numbers, revenue by day, the shop's Kaspi status, the team).
//
// Every number is a count over real rows; nothing is invented. Rights are re-checked on the server per
// request: the panel list only decides what the UI offers, each endpoint behind it enforces its own scope.
package console

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
)

func init() { me.HasConsole = HasConsole }

// HasConsole: any panel at all (the role, a scoped grant, or a staff / admin / member row somewhere).
func HasConsole(ctx context.Context, u *auth.User) (bool, error) {
	if u.Role == "admin" || u.Role == "university_staff" || len(u.Scopes) > 0 {
		return true, nil
	}
	var ok bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select exists (select 1 from cafe_staff where user_id = $1)
			or exists (select 1 from print_staff where user_id = $1)
			or exists (select 1 from club_admins where user_id = $1)
			or exists (select 1 from feedback_recipient_staff where user_id = $1)
			or exists (select 1 from employer_members m join employers e on e.id = m.employer_id where m.user_id = $1 and e.status <> 'closed')
			or exists (select 1 from ambassador_terms where user_id = $1 and status = 'active')
			or exists (select 1 from mentors where user_id = $1 and status in ('approved', 'paused'))`, u.ID).Scan(&ok)
	})
	return ok, err
}

// Routes wires /api/console/*.
func Routes(mux httpx.Router) {
	mux.HandleFunc("GET /api/console/me", auth.Need(myPanels))
	shopRoutes(mux)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("console: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

type shopRef struct {
	ID     int64  `json:"id"`
	NameKk string `json:"nameKk"`
	NameRu string `json:"nameRu"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
	Open   int    `json:"open"` // orders / jobs on the board now
}

type clubRef struct {
	ID       int64   `json:"id"`
	NameKk   string  `json:"nameKk"`
	NameRu   *string `json:"nameRu"`
	Status   string  `json:"status"`
	Members  int     `json:"members"`
	Requests int     `json:"requests"`
	Posts    int     `json:"pendingPosts"`
}

type recipientRef struct {
	ID         int64  `json:"id"`
	University string `json:"university"`
	NameKk     string `json:"nameKk"`
	NameRu     string `json:"nameRu"`
	Open       int    `json:"open"`
	Overdue    int    `json:"overdue"`
}

type universityRef struct {
	Name      string `json:"name"`
	Drafts    int    `json:"drafts"`
	Published int    `json:"published"`
}

type employerRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Role     string `json:"role"`
	Jobs     int    `json:"jobs"`     // published
	Pending  int    `json:"pending"`  // waiting for moderation
	NewApply int    `json:"newApply"` // applications not yet viewed
}

type moderationRef struct {
	Pending int `json:"pending"`
	Reports int `json:"reports"`
}

// Panels is the answer of GET /api/console/me.
type Panels struct {
	Admin      bool           `json:"admin"`
	Moderator  bool           `json:"moderator"`
	Blocked    bool           `json:"blocked"`
	Moderation *moderationRef `json:"moderation"`
	Cafes      []shopRef      `json:"cafes"`
	PrintShops []shopRef      `json:"printShops"`
	Clubs      []clubRef      `json:"clubs"`
	University *universityRef `json:"university"`
	Feedback   []recipientRef `json:"feedback"`
	Employer   *employerRef   `json:"employer"`
	Ambassador bool           `json:"ambassador"`
	Mentor     bool           `json:"mentor"`
}

func myPanels(w http.ResponseWriter, r *http.Request, u *auth.User) {
	p, err := PanelsFor(r.Context(), u)
	if err != nil {
		fail(w, r, "panels", err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

// PanelsFor collects the panels of u. Sections of switched-off features stay empty.
func PanelsFor(ctx context.Context, u *auth.User) (*Panels, error) {
	p := &Panels{Admin: u.Role == "admin", Moderator: u.Can(auth.ScopeModerator), Blocked: u.Blocked,
		Cafes: []shopRef{}, PrintShops: []shopRef{}, Clubs: []clubRef{}, Feedback: []recipientRef{}}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if p.Moderator {
			m := &moderationRef{}
			hidden := moderation.AdminOnlyKinds
			if p.Admin {
				hidden = []string{}
			}
			if err := tx.QueryRow(ctx, `select
					(select count(*) from submissions where status = 'pending' and kind <> all($1)),
					(select count(distinct (target_kind, target_id)) from reports where not dismissed)`, hidden).
				Scan(&m.Pending, &m.Reports); err != nil {
				return err
			}
			p.Moderation = m
		}
		if features.Enabled("cafe") {
			rows, err := tx.Query(ctx, `select c.id, c.name_kk, c.name_ru, s.role, c.active,
					(select count(*) from cafe_orders o where o.cafe_id = c.id and o.status in ('new', 'accepted', 'ready')
						and (o.payment_method = 'pickup' or o.payment_status in ('paid', 'refunded')))
				from cafe_staff s join cafes c on c.id = s.cafe_id where s.user_id = $1 order by c.id`, u.ID)
			if err != nil {
				return err
			}
			if p.Cafes, err = collectShops(rows); err != nil {
				return err
			}
		}
		if features.Enabled("print") {
			rows, err := tx.Query(ctx, `select ps.id, ps.name_kk, ps.name_ru, s.role, ps.active,
					(select count(*) from print_jobs j where j.shop_id = ps.id and j.status in ('new', 'printing', 'ready')
						and (j.payment_method = 'pickup' or j.payment_status in ('paid', 'refunded')))
				from print_staff s join print_shops ps on ps.id = s.shop_id where s.user_id = $1 order by ps.id`, u.ID)
			if err != nil {
				return err
			}
			if p.PrintShops, err = collectShops(rows); err != nil {
				return err
			}
		}
		if features.Enabled("clubs") {
			rows, err := tx.Query(ctx, `select c.id, c.name_kk, c.name_ru, c.status,
					(select count(*) from club_members m where m.club_id = c.id and m.status = 'member'),
					(select count(*) from club_members m where m.club_id = c.id and m.status = 'pending'),
					(select count(*) from club_posts cp where cp.club_id = c.id and cp.status = 'pending')
				from club_admins a join clubs c on c.id = a.club_id where a.user_id = $1 order by c.name_kk`, u.ID)
			if err != nil {
				return err
			}
			p.Clubs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (clubRef, error) {
				var c clubRef
				return c, row.Scan(&c.ID, &c.NameKk, &c.NameRu, &c.Status, &c.Members, &c.Requests, &c.Posts)
			})
			if err != nil {
				return err
			}
		}
		if u.Role == "university_staff" {
			var univ string
			err := tx.QueryRow(ctx, `select university from university_staff where user_id = $1`, u.ID).Scan(&univ)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if univ != "" {
				ref := &universityRef{Name: univ}
				if features.Enabled("surveys") {
					if err := tx.QueryRow(ctx, `select count(*) filter (where status = 'draft'), count(*) filter (where status = 'published')
						from surveys where university = $1`, univ).Scan(&ref.Drafts, &ref.Published); err != nil {
						return err
					}
				}
				p.University = ref
			}
		}
		if features.Enabled("feedback") && (u.Role == "university_staff" || u.Role == "admin") {
			rows, err := tx.Query(ctx, `select fr.id, fr.university, fr.name_kk, fr.name_ru,
					(select count(*) from feedback_tickets t where t.recipient_id = fr.id and t.status in ('new', 'in_progress')),
					(select count(*) from feedback_tickets t where t.recipient_id = fr.id and t.status in ('new', 'in_progress') and t.sla_due_at < now())
				from feedback_recipient_staff s join feedback_recipients fr on fr.id = s.recipient_id
				where s.user_id = $1 order by fr.university, fr.name_kk`, u.ID)
			if err != nil {
				return err
			}
			p.Feedback, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (recipientRef, error) {
				var f recipientRef
				return f, row.Scan(&f.ID, &f.University, &f.NameKk, &f.NameRu, &f.Open, &f.Overdue)
			})
			if err != nil {
				return err
			}
		}
		if features.Enabled("b2b") {
			var e employerRef
			err := tx.QueryRow(ctx, `select e.id, e.name, e.status, m.role,
					(select count(*) from jobs j where j.employer_id = e.id and j.status = 'published'),
					(select count(*) from jobs j where j.employer_id = e.id and j.status = 'pending'),
					(select count(*) from applications a where a.employer_id = e.id and a.status = 'sent')
				from employer_members m join employers e on e.id = m.employer_id
				where m.user_id = $1 and e.status <> 'closed' order by e.id limit 1`, u.ID).
				Scan(&e.ID, &e.Name, &e.Status, &e.Role, &e.Jobs, &e.Pending, &e.NewApply)
			switch {
			case err == nil:
				p.Employer = &e
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
		}
		if features.Enabled("ambassador") {
			if err := tx.QueryRow(ctx, `select exists (select 1 from ambassador_terms where user_id = $1 and status = 'active')`, u.ID).
				Scan(&p.Ambassador); err != nil {
				return err
			}
		}
		if features.Enabled("mentors") {
			if err := tx.QueryRow(ctx, `select exists (select 1 from mentors where user_id = $1 and status in ('approved', 'paused'))`, u.ID).
				Scan(&p.Mentor); err != nil {
				return err
			}
		}
		return nil
	})
	return p, err
}

func collectShops(rows pgx.Rows) ([]shopRef, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (shopRef, error) {
		var s shopRef
		return s, row.Scan(&s.ID, &s.NameKk, &s.NameRu, &s.Role, &s.Active, &s.Open)
	})
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

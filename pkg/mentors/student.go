package mentors

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// cleanText trims s and checks length and control characters (newlines are allowed).
func cleanText(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' {
			return "", false
		}
	}
	return s, true
}

func isAdult(u *auth.User) bool { return u.IsAdult != nil && *u.IsAdult }

// ---- summary ----

type Stats struct {
	University  string `json:"university"`
	Mentors     int    `json:"mentors"`     // approved mentors in the university
	FreeMentors int    `json:"freeMentors"` // approved one-on-one mentors with a free slot
	Waiting     int    `json:"waiting"`     // waiting requests
	// TypicalDays is the median wait from request to match over the last 180 days; null with
	// fewer than 3 matches, so the frontend says "not enough data yet" instead of guessing.
	TypicalDays *float64 `json:"typicalDays"`
}

func loadStats(ctx context.Context, tx pgx.Tx, university string) (Stats, error) {
	s := Stats{University: university}
	var samples int
	err := tx.QueryRow(ctx, `select
		(select count(*) from mentors where university = $1 and status = 'approved'),
		(select count(*) from mentors m where m.university = $1 and m.status = 'approved' and m.format <> 'group'
			and (select count(*) from mentor_pairs p where p.mentor_id = m.user_id and p.ended_at is null) < m.capacity),
		(select count(*) from mentor_requests where university = $1 and status = 'waiting'),
		(select count(*) from mentor_requests where university = $1 and matched_at > now() - interval '180 days'),
		(select round((percentile_cont(0.5) within group (order by extract(epoch from matched_at - created_at)) / 86400)::numeric, 1)::float8
			from mentor_requests where university = $1 and matched_at > now() - interval '180 days')`,
		university).Scan(&s.Mentors, &s.FreeMentors, &s.Waiting, &samples, &s.TypicalDays)
	if samples < 3 {
		s.TypicalDays = nil
	}
	return s, err
}

type requestOut struct {
	ID        int64      `json:"id"`
	Need      string     `json:"need"`
	Goal      string     `json:"goal"`
	Expect    string     `json:"expect"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	MatchedAt *time.Time `json:"matchedAt"`
}

func groupMeetings(ctx context.Context, university string) []GroupMeeting {
	out := []GroupMeeting{}
	if GroupMeetings != nil && university != "" {
		if list, err := GroupMeetings(ctx, university, 3); err == nil && list != nil {
			out = list
		}
	}
	return out
}

// summary: GET /api/mentors/summary?university= — the honest "X mentors, Y waiting, usually
// N days" answer. Signed-in students also get their own open request and mentor status.
func summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.Current(r)
	uni := strings.TrimSpace(r.URL.Query().Get("university"))
	if uni == "" && u != nil {
		uni = deref(u.University)
	}
	if utf8.RuneCountInString(uni) > 120 {
		bad(w, r)
		return
	}
	out := map[string]any{"university": nil}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if uni != "" {
			s, err := loadStats(ctx, tx, uni)
			if err != nil {
				return err
			}
			out["university"] = uni
			out["stats"] = s
		}
		if u == nil {
			return nil
		}
		mine := map[string]any{"isAdult": isAdult(u), "request": nil, "mentorStatus": nil}
		var req requestOut
		err := tx.QueryRow(ctx, `select id, need, goal_text, expect, status, created_at, matched_at from mentor_requests
			where user_id = $1 and status in ('waiting', 'matched')`, u.ID).
			Scan(&req.ID, &req.Need, &req.Goal, &req.Expect, &req.Status, &req.CreatedAt, &req.MatchedAt)
		if err == nil {
			mine["request"] = req
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `select status from mentors where user_id = $1`, u.ID).Scan(&status); err == nil {
			mine["mentorStatus"] = status
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out["me"] = mine
		return nil
	})
	if err != nil {
		internal(w, r, "summary", err)
		return
	}
	out["groupMeetings"] = groupMeetings(ctx, uni)
	httpx.JSON(w, http.StatusOK, out)
}

// ---- requests ----

func needAccount(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return false
	}
	if deref(u.University) == "" {
		fail(w, r, "university_required", http.StatusBadRequest)
		return false
	}
	return true
}

// createRequest: POST /api/mentors/requests {need, goal, expect}. Minors are always 'group':
// they get group mentoring and open office hours, never a personal pair.
func createRequest(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Need   string `json:"need"`
		Goal   string `json:"goal"`
		Expect string `json:"expect"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || !contains(Needs, body.Need) {
		bad(w, r)
		return
	}
	goal, ok := cleanText(body.Goal, 300)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_goal", httpx.MsgBad)
		return
	}
	if body.Expect == "" {
		body.Expect = "any"
	}
	if !contains([]string{"one_on_one", "group", "any"}, body.Expect) {
		bad(w, r)
		return
	}
	if !needAccount(w, r, u) {
		return
	}
	adult := isAdult(u)
	if !adult {
		body.Expect = "group"
	}
	ctx := r.Context()
	uni := deref(u.University)
	var req requestOut
	var stats Stats
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into mentor_requests (user_id, university, need, goal_text, expect)
			values ($1, $2, $3, $4, $5) returning id, need, goal_text, expect, status, created_at, matched_at`,
			u.ID, uni, body.Need, goal, body.Expect).
			Scan(&req.ID, &req.Need, &req.Goal, &req.Expect, &req.Status, &req.CreatedAt, &req.MatchedAt)
		if err != nil {
			return err
		}
		stats, err = loadStats(ctx, tx, uni)
		return err
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		fail(w, r, "request_exists", http.StatusConflict)
		return
	}
	if err != nil {
		internal(w, r, "request", err)
		return
	}
	// No free mentor (or a minor / group-only request): offer the nearest group meeting at once.
	offerGroup := !adult || body.Expect == "group" || stats.FreeMentors == 0
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"request":       req,
		"stats":         stats,
		"groupOnly":     !adult,
		"offerGroup":    offerGroup,
		"groupMeetings": groupMeetings(ctx, uni),
	})
}

// closeRequest: POST /api/mentors/requests/close — withdraw the own waiting request (RLS).
func closeRequest(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var n int64
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update mentor_requests set status = 'closed', closed_at = now()
			where user_id = app_user_id() and status = 'waiting'`)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		internal(w, r, "close request", err)
		return
	}
	if n == 0 {
		fail(w, r, "no_open_request", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- becoming a mentor ----

type profileIn struct {
	Areas         []string `json:"areas"`
	Format        string   `json:"format"`
	About         string   `json:"about"`
	Alumni        bool     `json:"alumni"`
	Capacity      *int     `json:"capacity"`
	GroupCapacity *int     `json:"groupCapacity"`
}

func validAreas(a []string) bool {
	if len(a) < 1 || len(a) > 4 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		if !contains(Needs, s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}

func validFormat(s string) bool { return contains([]string{"one_on_one", "group", "both"}, s) }

// apply: POST /api/mentors/apply — 2nd–4th year students and alumni, adults only. The row is
// stored as 'pending' and the application goes to the shared queue (kind mentor_application);
// the admin talks to the candidate before approving (docs/api/mentors.md lists the questions).
func apply(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in profileIn
	if err := httpx.Decode(r, &in, 4<<10); err != nil || !validAreas(in.Areas) {
		bad(w, r)
		return
	}
	if in.Format == "" {
		in.Format = "both"
	}
	about, ok := cleanText(in.About, 300)
	capacity, groupCap := 2, 8
	if in.Capacity != nil {
		capacity = *in.Capacity
	}
	if in.GroupCapacity != nil {
		groupCap = *in.GroupCapacity
	}
	if !ok || !validFormat(in.Format) || capacity < 1 || capacity > 5 || groupCap < 5 || groupCap > 10 {
		bad(w, r)
		return
	}
	if !needAccount(w, r, u) {
		return
	}
	course := 0
	if u.Course != nil {
		course = int(*u.Course)
	}
	if !isAdult(u) || !(in.Alumni || (course >= 2 && course <= 4)) {
		fail(w, r, "not_eligible", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	var subID int64
	code := ""
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `select status from mentors where user_id = $1 for update`, u.ID).Scan(&status)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		case status == "pending":
			code = "already_applied"
		case status == "removed":
			code = "mentor_removed"
		case status != "rejected":
			code = "already_mentor"
		}
		if code != "" {
			return nil
		}
		payload := map[string]any{
			"userId": u.ID, "name": deref(u.Name), "university": deref(u.University), "faculty": deref(u.Faculty),
			"course": u.Course, "alumni": in.Alumni, "areas": in.Areas, "format": in.Format, "about": about,
			"capacity": capacity, "groupCapacity": groupCap,
		}
		subID, err = moderation.Submit(ctx, tx, KindApplication, payload, &u.ID, deref(u.University))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into mentors (user_id, university, faculty, areas, format, capacity, group_capacity,
				about, alumni, status, submission_id)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10)
			on conflict (user_id) do update set university = excluded.university, faculty = excluded.faculty,
				areas = excluded.areas, format = excluded.format, capacity = excluded.capacity,
				group_capacity = excluded.group_capacity, about = excluded.about, alumni = excluded.alumni,
				status = 'pending', submission_id = excluded.submission_id, approved_at = null, updated_at = now()`,
			u.ID, deref(u.University), u.Faculty, in.Areas, in.Format, capacity, groupCap, about, in.Alumni, subID)
		return err
	})
	if err != nil {
		internal(w, r, "apply", err)
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusConflict)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"status": "pending", "submissionId": subID})
}

func approveApplication(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	if s.SubmittedBy == nil {
		return nil
	}
	tag, err := tx.Exec(ctx, `update mentors set status = 'approved', approved_at = now(), updated_at = now()
		where user_id = $1 and status = 'pending'`, *s.SubmittedBy)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	p, err := loadPerson(ctx, tx, *s.SubmittedBy)
	if err != nil {
		return err
	}
	direct(notify.WithKey(ctx, fmt.Sprintf("submission:%d:decision", s.ID)), p.ID, tr(p.Lang,
		"🎓 Менторлық өтініміңіз бекітілді! Студенттермен жұптасқанда хабар береміз. Профиль: "+site("/mentors/me"),
		"🎓 Ваша заявка ментора одобрена! Сообщим, когда появится пара. Профиль: "+site("/mentors/me")))
	return nil
}

func rejectApplication(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	if s.SubmittedBy == nil {
		return nil
	}
	tag, err := tx.Exec(ctx, `update mentors set status = 'rejected', updated_at = now()
		where user_id = $1 and status = 'pending'`, *s.SubmittedBy)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	p, err := loadPerson(ctx, tx, *s.SubmittedBy)
	if err != nil {
		return err
	}
	direct(notify.WithKey(ctx, fmt.Sprintf("submission:%d:decision", s.ID)), p.ID, tr(p.Lang, "Менторлық өтініміңіз әзірге қабылданбады. Себебі: ", "Заявка ментора пока не принята. Причина: ")+
		html.EscapeString(deref(s.Reason)))
	return nil
}

// ---- the mentor's own profile ----

type mentorOut struct {
	UserID        int64      `json:"userId"`
	University    string     `json:"university"`
	Areas         []string   `json:"areas"`
	Format        string     `json:"format"`
	Capacity      int        `json:"capacity"`
	GroupCapacity int        `json:"groupCapacity"`
	About         string     `json:"about"`
	Alumni        bool       `json:"alumni"`
	Status        string     `json:"status"`
	ApprovedAt    *time.Time `json:"approvedAt"`
	ActivePairs   int        `json:"activePairs"`
	Badge         bool       `json:"badge"`
}

const mentorCols = `user_id, university, areas, format, capacity, group_capacity, about, alumni, status, approved_at,
	(select count(*) from mentor_pairs p where p.mentor_id = mentors.user_id and p.ended_at is null),
	exists (select 1 from user_badges b where b.user_id = mentors.user_id and b.badge = 'mentor')`

func scanMentor(row pgx.Row) (mentorOut, error) {
	var m mentorOut
	err := row.Scan(&m.UserID, &m.University, &m.Areas, &m.Format, &m.Capacity, &m.GroupCapacity, &m.About, &m.Alumni,
		&m.Status, &m.ApprovedAt, &m.ActivePairs, &m.Badge)
	return m, err
}

// mentorMe: GET /api/mentors/me — own mentor profile (null when not a mentor). RLS-scoped.
func mentorMe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var out *mentorOut
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		m, err := scanMentor(tx.QueryRow(r.Context(), "select "+mentorCols+" from mentors where user_id = app_user_id()"))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		out = &m
		return err
	})
	if err != nil {
		internal(w, r, "mentor me", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"mentor": out})
}

// patchMentorMe: PATCH /api/mentors/me — pause/resume ("paused" / "approved") and profile
// edits. Runs as app_user: RLS lets only approved or paused mentors change their own row, so
// a pending, rejected or removed mentor cannot approve themselves.
func patchMentorMe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in struct {
		Status        *string  `json:"status"`
		Areas         []string `json:"areas"`
		Format        *string  `json:"format"`
		About         *string  `json:"about"`
		Capacity      *int     `json:"capacity"`
		GroupCapacity *int     `json:"groupCapacity"`
	}
	if err := httpx.Decode(r, &in, 4<<10); err != nil {
		bad(w, r)
		return
	}
	sets := []string{"updated_at = now()"}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if in.Status != nil {
		if *in.Status != "paused" && *in.Status != "approved" {
			bad(w, r)
			return
		}
		add("status", *in.Status)
	}
	if in.Areas != nil {
		if !validAreas(in.Areas) {
			bad(w, r)
			return
		}
		add("areas", in.Areas)
	}
	if in.Format != nil {
		if !validFormat(*in.Format) {
			bad(w, r)
			return
		}
		add("format", *in.Format)
	}
	if in.About != nil {
		s, ok := cleanText(*in.About, 300)
		if !ok {
			bad(w, r)
			return
		}
		add("about", s)
	}
	if in.Capacity != nil {
		if *in.Capacity < 1 || *in.Capacity > 5 {
			bad(w, r)
			return
		}
		add("capacity", *in.Capacity)
	}
	if in.GroupCapacity != nil {
		if *in.GroupCapacity < 5 || *in.GroupCapacity > 10 {
			bad(w, r)
			return
		}
		add("group_capacity", *in.GroupCapacity)
	}
	if len(args) == 0 {
		bad(w, r)
		return
	}
	var out *mentorOut
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		m, err := scanMentor(tx.QueryRow(r.Context(), "update mentors set "+strings.Join(sets, ", ")+
			" where user_id = app_user_id() returning "+mentorCols, args...))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		out = &m
		return err
	})
	if err != nil {
		internal(w, r, "patch mentor", err)
		return
	}
	if out == nil {
		fail(w, r, "not_editable", http.StatusConflict)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"mentor": out})
}

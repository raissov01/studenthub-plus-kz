package b2b

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/ambassador"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// PostIDPrefix marks official university records ("uni-<university_posts.id>").
const PostIDPrefix = "uni-"

// Count is an aggregate shown with the privacy floor: 1–4 people render as "< 5".
type Count = ambassador.Count

var reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func almatyToday() string { return Now().UTC().Add(5 * time.Hour).Format("2006-01-02") }

// staffUniversity returns the university an admin bound the caller to ("" = not staff).
func staffUniversity(ctx context.Context, userID int64) (string, error) {
	var univ string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select s.university from university_staff s
			join users u on u.id = s.user_id and u.deleted_at is null and u.role = 'university_staff'
			where s.user_id = $1`, userID).Scan(&univ)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return univ, err
}

func needStaff(w http.ResponseWriter, r *http.Request, u *auth.User) (string, bool) {
	univ, err := staffUniversity(r.Context(), u.ID)
	if err != nil {
		internal(w, r, "staff", err)
		return "", false
	}
	if univ == "" {
		fail(w, r, "not_staff", http.StatusForbidden)
		return "", false
	}
	return univ, true
}

func staffMe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	univ, ok := needStaff(w, r, u)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"university": univ})
}

// ---------- posts ----------

// Post is an official announcement or opportunity.
type Post struct {
	ID          int64           `json:"id"`
	University  string          `json:"university"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	Deadline    *time.Time      `json:"deadline"`
	Hidden      bool            `json:"hidden"`
	PublishedAt time.Time       `json:"publishedAt"`
}

const postCols = `id, university, kind, payload, deadline, hidden, published_at`

func scanPost(row pgx.Row) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.University, &p.Kind, &p.Payload, &p.Deadline, &p.Hidden, &p.PublishedAt)
	return p, err
}

func biList(v any) ([]content.Bi, bool) {
	if v == nil {
		return []content.Bi{}, true
	}
	arr, ok := v.([]any)
	if !ok || len(arr) > 10 {
		return nil, false
	}
	out := []content.Bi{}
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, false
		}
		kk, _ := m["kk"].(string)
		ru, _ := m["ru"].(string)
		kk, ok1 := cleanText(kk, 1, 200, false)
		ru, ok2 := cleanText(ru, 0, 200, false)
		if !ok1 || !ok2 {
			return nil, false
		}
		out = append(out, content.Bi{KK: kk, RU: ru})
	}
	return out, true
}

// cleanPost validates a post payload: an opportunity in the existing opportunity structure
// (ambassador.CleanPayload + docs / steps), or an announcement.
func cleanPost(kind string, in map[string]any) (map[string]any, string, bool) {
	today := almatyToday()
	switch kind {
	case "opportunity":
		out, err := ambassador.CleanPayload(ambassador.KindOpportunity, in, today)
		if err != nil {
			return nil, "", false
		}
		for _, k := range []string{"docs", "steps"} {
			l, ok := biList(in[k])
			if !ok {
				return nil, "", false
			}
			out[k] = l
		}
		d, _ := out["deadline"].(string)
		return out, d, true
	case "announcement":
		out := map[string]any{}
		for _, f := range []struct {
			k        string
			min, max int
			ml       bool
		}{{"title_kk", 3, 140, false}, {"title_ru", 0, 140, false}, {"body_kk", 10, 2000, true}, {"body_ru", 0, 2000, true}} {
			s, _ := in[f.k].(string)
			if v, present := in[f.k]; present && v != nil {
				if _, isStr := v.(string); !isStr {
					return nil, "", false
				}
			}
			s, ok := cleanText(s, f.min, f.max, f.ml)
			if !ok {
				return nil, "", false
			}
			out[f.k] = s
		}
		link, _ := in["url"].(string)
		link, ok := httpsURL(link, false)
		if !ok {
			return nil, "", false
		}
		out["url"] = link
		until, _ := in["until"].(string)
		if until != "" && (!reDate.MatchString(until) || !content.IsValidDate(until) || until < today) {
			return nil, "", false
		}
		out["until"] = until
		for k := range in {
			if _, known := out[k]; !known {
				return nil, "", false
			}
		}
		return out, until, true
	}
	return nil, "", false
}

func nullDate(d string) *string {
	if d == "" {
		return nil
	}
	return &d
}

// staffPosts: GET /api/university/posts — my university's posts (as app_user; RLS).
func staffPosts(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if _, ok := needStaff(w, r, u); !ok {
		return
	}
	var out []Post
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select `+postCols+` from university_posts order by published_at desc limit 300`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Post, error) { return scanPost(row) })
		return err
	})
	if err != nil {
		internal(w, r, "posts", err)
		return
	}
	if out == nil {
		out = []Post{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"posts": out})
}

// createPost: POST /api/university/posts {"kind","payload"} — published at once, no queue,
// always for the caller's own university (RLS with check).
func createPost(w http.ResponseWriter, r *http.Request, u *auth.User) {
	univ, ok := needStaff(w, r, u)
	if !ok {
		return
	}
	var body struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	if err := httpx.Decode(r, &body, 32<<10); err != nil || body.Payload == nil {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	payload, deadline, ok := cleanPost(body.Kind, body.Payload)
	if !ok {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(payload)
	var p Post
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		var err error
		p, err = scanPost(tx.QueryRow(r.Context(), `insert into university_posts (university, author_id, kind, payload, deadline)
			values ($1, app_user_id(), $2, $3::jsonb, $4::date) returning `+postCols, univ, body.Kind, string(raw), nullDate(deadline)))
		return err
	})
	if err != nil {
		internal(w, r, "create post", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"post": p})
}

// editPost: PATCH /api/university/posts/{id} {"payload"?, "hidden"?}
func editPost(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "post_not_found", http.StatusNotFound)
		return
	}
	if _, ok := needStaff(w, r, u); !ok {
		return
	}
	var body struct {
		Payload map[string]any `json:"payload"`
		Hidden  *bool          `json:"hidden"`
	}
	if err := httpx.Decode(r, &body, 32<<10); err != nil || (body.Payload == nil && body.Hidden == nil) {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	code := ""
	var p Post
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		cur, err := scanPost(tx.QueryRow(ctx, `select `+postCols+` from university_posts where id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			code = "post_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		raw, deadline := string(cur.Payload), (*string)(nil)
		if cur.Deadline != nil {
			d := cur.Deadline.Format("2006-01-02")
			deadline = &d
		}
		if body.Payload != nil {
			payload, d, ok := cleanPost(cur.Kind, body.Payload)
			if !ok {
				code = "invalid"
				return nil
			}
			b, _ := json.Marshal(payload)
			raw, deadline = string(b), nullDate(d)
		}
		hidden := cur.Hidden
		if body.Hidden != nil {
			hidden = *body.Hidden
		}
		p, err = scanPost(tx.QueryRow(ctx, `update university_posts set payload = $2::jsonb, deadline = $3::date, hidden = $4, updated_at = now()
			where id = $1 returning `+postCols, id, raw, deadline, hidden))
		return err
	})
	switch {
	case err != nil:
		internal(w, r, "edit post", err)
	case code == "post_not_found":
		fail(w, r, code, http.StatusNotFound)
	case code != "":
		fail(w, r, code, http.StatusBadRequest)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"post": p})
	}
}

func deletePost(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, r, "post_not_found", http.StatusNotFound)
		return
	}
	if _, ok := needStaff(w, r, u); !ok {
		return
	}
	var n int64
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `delete from university_posts where id = $1`, id)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		internal(w, r, "delete post", err)
		return
	}
	if n == 0 {
		fail(w, r, "post_not_found", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// officialOpportunities shapes visible opportunity posts like data/opportunities.json plus
// source = "university", official = true («Университеттен ресми»).
func officialOpportunities(ctx context.Context) ([]content.Record, error) {
	var out []content.Record
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+postCols+` from university_posts where kind = 'opportunity' and not hidden
			order by published_at desc limit 300`)
		if err != nil {
			return err
		}
		posts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Post, error) { return scanPost(row) })
		if err != nil {
			return err
		}
		for _, p := range posts {
			var m map[string]any
			if json.Unmarshal(p.Payload, &m) != nil {
				continue
			}
			s := func(k string) string { v, _ := m[k].(string); return v }
			bl := func(k string) []content.Bi { l, _ := biList(m[k]); return l }
			posted := p.PublishedAt.UTC().Add(5 * time.Hour).Format("2006-01-02")
			var deadline *string
			if d := s("deadline"); d != "" {
				deadline = &d
			}
			funded, _ := m["funded"].(bool)
			free, _ := m["free"].(bool)
			org := content.Bi{KK: s("org_kk"), RU: s("org_ru")}
			v := struct {
				content.Opportunity
				Source     string `json:"source"`
				Official   bool   `json:"official"`
				University string `json:"university"`
				Free       bool   `json:"free"`
			}{content.Opportunity{
				ID: PostIDPrefix + strconv.FormatInt(p.ID, 10), Type: s("type"), Funded: funded,
				Title: content.Bi{KK: s("title_kk"), RU: s("title_ru")}, Org: org, Spec: []string{"all"},
				Desc: content.Bi{KK: s("desc_kk"), RU: s("desc_ru")}, Docs: bl("docs"), Steps: bl("steps"),
				SourceURL: s("source_url"), PostedAt: &posted, VerifiedAt: &posted, VerifiedBy: p.University,
				Deadline: deadline, City: s("city"),
			}, "university", true, p.University, free}
			raw, err := json.Marshal(v)
			if err != nil {
				continue
			}
			out = append(out, content.RawRecord(raw, s("deadline")))
		}
		return nil
	})
	return out, err
}

// announcements: GET /api/announcements?university= — public official announcements.
func announcements(w http.ResponseWriter, r *http.Request) {
	univ := strings.TrimSpace(r.URL.Query().Get("university"))
	type item struct {
		ID          string     `json:"id"`
		University  string     `json:"university"`
		Title       content.Bi `json:"title"`
		Body        content.Bi `json:"body"`
		URL         string     `json:"url"`
		Until       *string    `json:"until"`
		PublishedAt time.Time  `json:"publishedAt"`
		Source      string     `json:"source"`
		Official    bool       `json:"official"`
	}
	out := []item{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select `+postCols+` from university_posts
			where kind = 'announcement' and not hidden and ($1 = '' or university = $1)
				and (deadline is null or deadline >= $2::date)
			order by published_at desc limit 100`, univ, almatyToday())
		if err != nil {
			return err
		}
		posts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Post, error) { return scanPost(row) })
		for _, p := range posts {
			var m map[string]string
			_ = json.Unmarshal(p.Payload, &m)
			out = append(out, item{ID: PostIDPrefix + strconv.FormatInt(p.ID, 10), University: p.University,
				Title: content.Bi{KK: m["title_kk"], RU: m["title_ru"]}, Body: content.Bi{KK: m["body_kk"], RU: m["body_ru"]},
				URL: m["url"], Until: nullDate(m["until"]), PublishedAt: p.PublishedAt, Source: "university", Official: true})
		}
		return err
	})
	if err != nil {
		internal(w, r, "announcements", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.JSON(w, http.StatusOK, map[string]any{"announcements": out})
}

// ---------- aggregate statistics ----------

// UniversityStats are aggregate numbers only; every head count goes through Count ("< 5"),
// averages need at least 5 answers. No names, no per-student rows, anywhere.
type UniversityStats struct {
	University     string           `json:"university"`
	PeriodDays     int              `json:"periodDays"`
	Students       Count            `json:"students"`
	WeeklyActive   Count            `json:"weeklyActive"`
	ByFaculty      map[string]Count `json:"byFaculty"`
	ByCourse       map[string]Count `json:"byCourse"`
	SavedDeadlines Count            `json:"savedDeadlines"`
	Events         Count            `json:"events"`
	Registrations  Count            `json:"registrations"`
	CheckIns       Count            `json:"checkIns"`
	Ratings        Count            `json:"ratings"`
	AvgRating      *float64         `json:"avgRating"` // null with fewer than 5 ratings
	Mentors        Count            `json:"mentors"`
	MentorRequests Count            `json:"mentorRequests"`
	MentorPairs    Count            `json:"mentorPairs"`
}

const statsDays = 90

// Stats computes the numbers for one university.
func Stats(ctx context.Context, univ string) (UniversityStats, error) {
	s := UniversityStats{University: univ, PeriodDays: statsDays, ByFaculty: map[string]Count{}, ByCourse: map[string]Count{}}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var avg *float64
		err := tx.QueryRow(ctx, `with st as (select id, faculty, course, last_seen_at from users
				where university = $1 and deleted_at is null and consent_at is not null and role <> 'university_staff')
			select
				(select count(*) from st),
				(select count(*) from st where last_seen_at > now() - interval '7 days'),
				(select count(*) from reminders r join st on st.id = r.user_id where r.status = 'active'),
				(select count(*) from events where university = $1 and status in ('published', 'finished') and starts_at > now() - make_interval(days => $2)),
				(select count(*) from event_regs r join events e on e.id = r.event_id where e.university = $1 and r.status = 'registered' and e.starts_at > now() - make_interval(days => $2)),
				(select count(*) from event_regs r join events e on e.id = r.event_id where e.university = $1 and r.checked_in_at is not null and e.starts_at > now() - make_interval(days => $2)),
				(select count(*) from event_regs r join events e on e.id = r.event_id where e.university = $1 and r.feedback_score is not null and e.starts_at > now() - make_interval(days => $2)),
				(select avg(r.feedback_score)::float8 from event_regs r join events e on e.id = r.event_id where e.university = $1 and r.feedback_score is not null and e.starts_at > now() - make_interval(days => $2)),
				(select count(*) from mentors where university = $1 and status = 'approved'),
				(select count(*) from mentor_requests where university = $1 and created_at > now() - make_interval(days => $2)),
				(select count(*) from mentor_pairs where university = $1 and started_at > now() - make_interval(days => $2))`,
			univ, statsDays).Scan(&s.Students, &s.WeeklyActive, &s.SavedDeadlines, &s.Events, &s.Registrations, &s.CheckIns,
			&s.Ratings, &avg, &s.Mentors, &s.MentorRequests, &s.MentorPairs)
		if err != nil {
			return err
		}
		if s.Ratings >= 5 && avg != nil {
			v := math.Round(*avg*10) / 10
			s.AvgRating = &v
		}
		rows, err := tx.Query(ctx, `select 'f', coalesce(nullif(faculty, ''), '—'), count(*) from users
				where university = $1 and deleted_at is null and consent_at is not null and role <> 'university_staff' group by 2
			union all
			select 'c', coalesce(course::text, '—'), count(*) from users
				where university = $1 and deleted_at is null and consent_at is not null and role <> 'university_staff' group by 2`, univ)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kind, key string
			var n int
			if err := rows.Scan(&kind, &key, &n); err != nil {
				return err
			}
			if kind == "f" {
				s.ByFaculty[key] = Count(n)
			} else {
				s.ByCourse[key] = Count(n)
			}
		}
		return rows.Err()
	})
	return s, err
}

func staffStats(w http.ResponseWriter, r *http.Request, u *auth.User) {
	univ, ok := needStaff(w, r, u)
	if !ok {
		return
	}
	s, err := Stats(r.Context(), univ)
	if err != nil {
		internal(w, r, "stats", err)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

func countCell(c Count) string {
	b, _ := c.MarshalJSON()
	return strings.Trim(string(b), `"`)
}

// staffStatsCSV: the same aggregate numbers as CSV (metric,value) — no row per student.
func staffStatsCSV(w http.ResponseWriter, r *http.Request, u *auth.User) {
	univ, ok := needStaff(w, r, u)
	if !ok {
		return
	}
	s, err := Stats(r.Context(), univ)
	if err != nil {
		internal(w, r, "stats csv", err)
		return
	}
	rows := [][]string{{"metric", "value"},
		{"university", s.University}, {"period_days", strconv.Itoa(s.PeriodDays)},
		{"students", countCell(s.Students)}, {"weekly_active", countCell(s.WeeklyActive)},
		{"saved_deadlines", countCell(s.SavedDeadlines)}, {"events", countCell(s.Events)},
		{"registrations", countCell(s.Registrations)}, {"check_ins", countCell(s.CheckIns)},
		{"ratings", countCell(s.Ratings)}, {"avg_rating", ""},
		{"mentors", countCell(s.Mentors)}, {"mentor_requests", countCell(s.MentorRequests)}, {"mentor_pairs", countCell(s.MentorPairs)},
	}
	if s.AvgRating != nil {
		rows[10][1] = strconv.FormatFloat(*s.AvgRating, 'f', 1, 64)
	}
	add := func(prefix string, m map[string]Count) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rows = append(rows, []string{prefix + k, countCell(m[k])})
		}
	}
	add("faculty:", s.ByFaculty)
	add("course:", s.ByCourse)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="university-stats.csv"`)
	cw := csv.NewWriter(w)
	for _, row := range rows {
		// spreadsheet formula injection guard for free-text keys (faculty names)
		for i, c := range row {
			if c != "" && strings.ContainsRune("=+-@", rune(c[0])) {
				row[i] = "'" + c
			}
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}

// ---------- admin: bind / unbind staff ----------

func adminStaffList(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	type row struct {
		UserID     int64     `json:"userId"`
		Name       *string   `json:"name"`
		Username   *string   `json:"username"`
		University string    `json:"university"`
		Role       string    `json:"role"`
		CreatedAt  time.Time `json:"createdAt"`
	}
	var out []row
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select s.user_id, u.name, u.tg_username, s.university, u.role::text, s.created_at
			from university_staff s join users u on u.id = s.user_id and u.deleted_at is null order by s.university, s.created_at`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(rw pgx.CollectableRow) (row, error) {
			var x row
			return x, rw.Scan(&x.UserID, &x.Name, &x.Username, &x.University, &x.Role, &x.CreatedAt)
		})
		return err
	})
	if err != nil {
		internal(w, r, "staff list", err)
		return
	}
	if out == nil {
		out = []row{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"staff": out})
}

// adminStaffGrant: POST /api/admin/university-staff {"userId","university"} — role
// university_staff + binding to one university (audited).
func adminStaffGrant(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	var body struct {
		UserID     int64  `json:"userId"`
		University string `json:"university"`
	}
	if err := httpx.Decode(r, &body, 2<<10); err != nil || body.UserID <= 0 {
		bad(w, r)
		return
	}
	univ, ok := cleanText(body.University, 2, 120, false)
	if !ok {
		fail(w, r, "invalid", http.StatusBadRequest)
		return
	}
	code := ""
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(r.Context(), `select role::text from users where id = $1 and deleted_at is null for update`, body.UserID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "user_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		if role == "admin" || body.UserID == admin.ID {
			code = "invalid"
			return nil
		}
		if _, err := tx.Exec(r.Context(), `insert into university_staff (user_id, university, granted_by) values ($1, $2, $3)
			on conflict (user_id) do update set university = excluded.university, granted_by = excluded.granted_by`, body.UserID, univ, admin.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `update users set role = 'university_staff', university = $2 where id = $1`, body.UserID, univ); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'role_grant', $2, 'university staff', jsonb_build_object('from', $3::text, 'to', 'university_staff', 'university', $4::text))`,
			admin.ID, body.UserID, role, univ)
		return err
	})
	switch {
	case err != nil:
		internal(w, r, "staff grant", err)
	case code == "user_not_found":
		fail(w, r, code, http.StatusNotFound)
	case code != "":
		fail(w, r, code, http.StatusBadRequest)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"userId": body.UserID, "university": univ, "role": "university_staff"})
	}
}

func adminStaffRevoke(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	id, ok := pathID(r, "userId")
	if !ok {
		fail(w, r, "user_not_found", http.StatusNotFound)
		return
	}
	found := false
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `delete from university_staff where user_id = $1`, id)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		found = true
		if _, err := tx.Exec(r.Context(), `update users set role = 'student' where id = $1 and role = 'university_staff'`, id); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'role_revoke', $2, 'university staff', jsonb_build_object('from', 'university_staff', 'to', 'student'))`, admin.ID, id)
		return err
	})
	if err != nil {
		internal(w, r, "staff revoke", err)
		return
	}
	if !found {
		fail(w, r, "user_not_found", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

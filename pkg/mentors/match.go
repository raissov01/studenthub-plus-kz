package mentors

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// scope is who is matching: an admin (any university, optional ?university= filter) or an
// ambassador (pinned to their own university, even if the request says otherwise).
type scope struct {
	actor      *auth.User
	university string // "" = all (admin only)
	pinned     bool   // ambassador: university is enforced in SQL too
}

type scopedHandler func(w http.ResponseWriter, r *http.Request, s scope)

func adminScope(h scopedHandler) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		uni := strings.TrimSpace(r.URL.Query().Get("university"))
		if utf8.RuneCountInString(uni) > 120 {
			bad(w, r)
			return
		}
		h(w, r, scope{actor: u, university: uni})
	}
}

// ambassadorScope pins the ambassador to users.university. The "#/admin is admin-only" rule
// (04) stays intact: ambassadors use /api/ambassador/mentors, never /api/admin/*.
func ambassadorScope(h scopedHandler) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		uni := deref(u.University)
		if uni == "" {
			fail(w, r, "no_university", http.StatusForbidden)
			return
		}
		if AmbassadorCanMatch != nil {
			ok, err := AmbassadorCanMatch(r.Context(), u.ID, uni)
			if err != nil {
				internal(w, r, "ambassador check", err)
				return
			}
			if !ok {
				fail(w, r, "not_lead", http.StatusForbidden)
				return
			}
		}
		h(w, r, scope{actor: u, university: uni, pinned: true})
	}
}

type waitingOut struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	Name        string    `json:"name"`
	University  string    `json:"university"`
	Faculty     string    `json:"faculty"`
	Course      *int16    `json:"course"`
	IsAdult     bool      `json:"isAdult"`
	Need        string    `json:"need"`
	Goal        string    `json:"goal"`
	Expect      string    `json:"expect"`
	CreatedAt   time.Time `json:"createdAt"`
	WaitingDays int       `json:"waitingDays"`
	// GroupOnly: a minor or a group-only request — no personal pair, offer group mentoring.
	GroupOnly bool `json:"groupOnly"`
}

// board: GET /api/{admin|ambassador}/mentors — waiting requests, oldest first, plus stats.
// No Telegram usernames here: contacts are exchanged only in the match message.
func board(w http.ResponseWriter, r *http.Request, s scope) {
	ctx := r.Context()
	out := []waitingOut{}
	var stats *Stats
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select q.id, q.user_id, coalesce(u.name, ''), q.university, coalesce(u.faculty, ''), u.course,
				coalesce(u.is_adult, false), q.need, q.goal_text, q.expect, q.created_at
			from mentor_requests q join users u on u.id = q.user_id
			where q.status = 'waiting' and u.deleted_at is null and ($1 = '' or q.university = $1)
			order by q.created_at limit 200`, s.university)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var q waitingOut
			if err := rows.Scan(&q.ID, &q.UserID, &q.Name, &q.University, &q.Faculty, &q.Course, &q.IsAdult, &q.Need, &q.Goal,
				&q.Expect, &q.CreatedAt); err != nil {
				return err
			}
			q.WaitingDays = int(time.Since(q.CreatedAt).Hours() / 24)
			q.GroupOnly = !q.IsAdult || q.Expect == "group"
			out = append(out, q)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if s.university != "" {
			st, err := loadStats(ctx, tx, s.university)
			stats = &st
			return err
		}
		return nil
	})
	if err != nil {
		internal(w, r, "board", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"university": nilIfEmpty(s.university), "requests": out, "stats": stats})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type candidateOut struct {
	UserID      int64    `json:"userId"`
	Name        string   `json:"name"`
	University  string   `json:"university"`
	Faculty     string   `json:"faculty"`
	Course      *int16   `json:"course"`
	Alumni      bool     `json:"alumni"`
	Areas       []string `json:"areas"`
	Format      string   `json:"format"`
	About       string   `json:"about"`
	Capacity    int      `json:"capacity"`
	ActivePairs int      `json:"activePairs"`
	FreeSlots   int      `json:"freeSlots"`
	PastPairs   int      `json:"pastPairs"`
	AvgScore    *float64 `json:"avgScore"` // mean mentee_score of ended pairs
	// Score is only a hint for sorting; the human decides.
	Score   float64        `json:"score"`
	Reasons map[string]any `json:"reasons"`
}

// rank orders candidates by area match, free slots, same faculty and past feedback.
func rank(c *candidateOut, need, faculty string) {
	area := contains(c.Areas, need)
	sameFac := faculty != "" && strings.EqualFold(c.Faculty, faculty)
	c.Score = float64(c.FreeSlots)
	if area {
		c.Score += 3
	}
	if sameFac {
		c.Score += 1.5
	}
	if c.AvgScore != nil {
		c.Score += (*c.AvgScore - 3) / 2 // -1 .. +1
	}
	c.Reasons = map[string]any{"areaMatch": area, "sameFaculty": sameFac, "freeSlots": c.FreeSlots, "avgScore": c.AvgScore}
}

// candidates: GET /api/{admin|ambassador}/mentors/requests/{id}/candidates — approved mentors
// of the request's university with a free one-on-one slot, best suggestion first. Full,
// paused and group-only mentors are left out ("temporarily disappear from the list").
func candidates(w http.ResponseWriter, r *http.Request, s scope) {
	id, ok := pathID(r, "id")
	if !ok {
		bad(w, r)
		return
	}
	ctx := r.Context()
	var req waitingOut
	out := []candidateOut{}
	code := ""
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `select q.id, q.user_id, q.university, coalesce(u.faculty, ''), coalesce(u.is_adult, false),
				q.need, q.expect, q.status
			from mentor_requests q join users u on u.id = q.user_id where q.id = $1`, id).
			Scan(&req.ID, &req.UserID, &req.University, &req.Faculty, &req.IsAdult, &req.Need, &req.Expect, &status)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && s.pinned && req.University != s.university) {
			code = "request_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		req.GroupOnly = !req.IsAdult || req.Expect == "group"
		if status != "waiting" || req.GroupOnly {
			return nil
		}
		rows, err := tx.Query(ctx, `select m.user_id, coalesce(u.name, ''), m.university, coalesce(u.faculty, m.faculty, ''), u.course,
				m.alumni, m.areas, m.format, m.about, m.capacity,
				(select count(*) from mentor_pairs p where p.mentor_id = m.user_id and p.ended_at is null),
				(select count(*) from mentor_pairs p where p.mentor_id = m.user_id and p.ended_at is not null),
				(select avg(mentee_score)::float8 from mentor_pairs p where p.mentor_id = m.user_id and p.mentee_score is not null)
			from mentors m join users u on u.id = m.user_id
			where m.status = 'approved' and m.format <> 'group' and m.university = $1 and m.user_id <> $2
				and u.deleted_at is null
				and (select count(*) from mentor_pairs p where p.mentor_id = m.user_id and p.ended_at is null) < m.capacity`,
			req.University, req.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c candidateOut
			if err := rows.Scan(&c.UserID, &c.Name, &c.University, &c.Faculty, &c.Course, &c.Alumni, &c.Areas, &c.Format,
				&c.About, &c.Capacity, &c.ActivePairs, &c.PastPairs, &c.AvgScore); err != nil {
				return err
			}
			c.FreeSlots = c.Capacity - c.ActivePairs
			rank(&c, req.Need, req.Faculty)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "candidates", err)
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusNotFound)
		return
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	httpx.JSON(w, http.StatusOK, map[string]any{"requestId": req.ID, "need": req.Need, "groupOnly": req.GroupOnly, "candidates": out})
}

// match: POST /api/{admin|ambassador}/mentors/match {requestId, mentorId}. mentor_match()
// enforces approval, capacity, adults-only and the ambassador's university under row locks;
// then both sides get the introduction message.
func match(w http.ResponseWriter, r *http.Request, s scope) {
	var in struct {
		RequestID int64 `json:"requestId"`
		MentorID  int64 `json:"mentorId"`
	}
	if err := httpx.Decode(r, &in, 1<<10); err != nil || in.RequestID <= 0 || in.MentorID <= 0 {
		bad(w, r)
		return
	}
	ctx := r.Context()
	var uni *string
	if s.pinned {
		uni = &s.university
	}
	var pairID int64
	var p pairRow
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select mentor_match($1, $2, $3, $4)`, in.RequestID, in.MentorID, s.actor.ID, uni).Scan(&pairID); err != nil {
			return err
		}
		var err error
		if p, err = lockPair(ctx, tx, pairID, 0); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'mentor_match', $2, null, jsonb_build_object('pairId', $3::bigint, 'mentorId', $4::bigint, 'requestId', $5::bigint))`,
			s.actor.ID, p.MenteeID, pairID, p.MentorID, in.RequestID)
		return err
	})
	if code := raised(err); code != "" {
		status := http.StatusConflict
		if code == "wrong_university" {
			status = http.StatusForbidden
		}
		fail(w, r, code, status)
		return
	}
	if err != nil {
		internal(w, r, "match", err)
		return
	}
	sendIntros(ctx, p)
	httpx.JSON(w, http.StatusCreated, map[string]any{"pairId": pairID, "mentorId": p.MentorID, "menteeId": p.MenteeID})
}

func sendIntros(ctx context.Context, p pairRow) {
	q := mustPool(ctx)
	mentor, err1 := loadPerson(ctx, q, p.MentorID)
	mentee, err2 := loadPerson(ctx, q, p.MenteeID)
	if err := errors.Join(err1, err2); err != nil {
		log.Printf("mentors: intro %d: %v", p.ID, err)
		return
	}
	// Defence in depth: mentor_match already refuses minors; never leak a username to or
	// from someone who is not a confirmed adult.
	if !mentor.IsAdult || !mentee.IsAdult {
		mentor.Username, mentee.Username = "", ""
	}
	direct(ctx, mentor.ID, introText(mentor, mentee, p.Need, true))
	direct(ctx, mentee.ID, introText(mentee, mentor, p.Need, false))
}

// ---- admin only ----

type adminPairOut struct {
	ID          int64      `json:"id"`
	University  string     `json:"university"`
	Need        string     `json:"need"`
	MentorID    int64      `json:"mentorId"`
	MentorName  string     `json:"mentorName"`
	MenteeID    int64      `json:"menteeId"`
	MenteeName  string     `json:"menteeName"`
	StartedAt   time.Time  `json:"startedAt"`
	ContinuedAt *time.Time `json:"continuedAt"`
	EndedAt     *time.Time `json:"endedAt"`
	EndReason   *string    `json:"endReason"`
	MenteeScore *int16     `json:"menteeScore"`
	MentorScore *int16     `json:"mentorScore"`
}

// adminPairs: GET /api/admin/mentors/pairs?status=active|ended&university=
func adminPairs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	uni := strings.TrimSpace(r.URL.Query().Get("university"))
	if (status != "active" && status != "ended") || utf8.RuneCountInString(uni) > 120 {
		bad(w, r)
		return
	}
	ctx := r.Context()
	out := []adminPairOut{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select p.id, p.university, p.need, p.mentor_id, coalesce(a.name, ''), p.mentee_id, coalesce(b.name, ''),
				p.started_at, p.continued_at, p.ended_at, p.end_reason, p.mentee_score, p.mentor_score
			from mentor_pairs p join users a on a.id = p.mentor_id join users b on b.id = p.mentee_id
			where (p.ended_at is null) = ($1 = 'active') and ($2 = '' or p.university = $2)
			order by p.started_at desc limit 300`, status, uni)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p adminPairOut
			if err := rows.Scan(&p.ID, &p.University, &p.Need, &p.MentorID, &p.MentorName, &p.MenteeID, &p.MenteeName,
				&p.StartedAt, &p.ContinuedAt, &p.EndedAt, &p.EndReason, &p.MenteeScore, &p.MentorScore); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "admin pairs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"pairs": out})
}

func reasonOK(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 3 && n <= 500
}

// adminEndPair: POST /api/admin/mentors/pairs/{id}/end {reason} — audited.
func adminEndPair(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok {
		bad(w, r)
		return
	}
	reason, ok := reasonOK(in.Reason)
	if !ok {
		fail(w, r, "reason_required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	code := ""
	var p pairRow
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if p, err = lockPair(ctx, tx, id, 0); err != nil {
			return err
		}
		if p.EndedAt != nil {
			code = "pair_ended"
			return nil
		}
		if err := closePair(ctx, tx, p, u.ID, "admin"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'mentor_pair_end', $2, $3, jsonb_build_object('pairId', $4::bigint))`, u.ID, p.MentorID, reason, p.ID)
		return err
	})
	if pairErr(w, r, "admin end", err) {
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusConflict)
		return
	}
	for _, id := range []int64{p.MentorID, p.MenteeID} {
		if o, err := loadPerson(ctx, mustPool(ctx), id); err == nil {
			direct(ctx, o.ID, endedText(o.Lang))
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminSetStatus: POST /api/admin/mentors/{userId}/status {status: approved|paused|removed,
// reason} — "removed" takes the mentor role away and ends the mentor's active pairs.
func adminSetStatus(w http.ResponseWriter, r *http.Request, u *auth.User) {
	uid, ok := pathID(r, "userId")
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok || !contains([]string{"approved", "paused", "removed"}, in.Status) {
		bad(w, r)
		return
	}
	reason, ok := reasonOK(in.Reason)
	if !ok {
		fail(w, r, "reason_required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var ended []pairRow
	code := ""
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var prev string
		err := tx.QueryRow(ctx, `select status from mentors where user_id = $1 for update`, uid).Scan(&prev)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "not_a_mentor"
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update mentors set status = $2, updated_at = now(),
			approved_at = case when $2 = 'approved' then coalesce(approved_at, now()) else approved_at end
			where user_id = $1`, uid, in.Status); err != nil {
			return err
		}
		if in.Status == "removed" {
			rows, err := tx.Query(ctx, `select id from mentor_pairs where mentor_id = $1 and ended_at is null`, uid)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			for _, id := range ids {
				p, err := lockPair(ctx, tx, id, 0)
				if err != nil {
					return err
				}
				if err := closePair(ctx, tx, p, u.ID, "mentor_removed"); err != nil {
					return err
				}
				ended = append(ended, p)
			}
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'mentor_status', $2, $3, jsonb_build_object('from', $4::text, 'to', $5::text))`,
			u.ID, uid, reason, prev, in.Status)
		return err
	})
	if err != nil {
		internal(w, r, "mentor status", err)
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusNotFound)
		return
	}
	for _, p := range ended {
		if o, err := loadPerson(ctx, mustPool(ctx), p.MenteeID); err == nil {
			direct(ctx, o.ID, endedText(o.Lang))
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "endedPairs": len(ended)})
}

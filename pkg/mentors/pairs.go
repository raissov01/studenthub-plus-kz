package mentors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

const (
	check3After = 21 * 24 * time.Hour
	monthAfter  = 28 * 24 * time.Hour
)

type partnerOut struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	University string  `json:"university"`
	Faculty    string  `json:"faculty"`
	Username   *string `json:"username"` // only while the pair is active (pairs are adults-only)
}

type promptOut struct {
	Kind     string     `json:"kind"`
	SentAt   *time.Time `json:"sentAt"`
	Answered bool       `json:"answered"`
}

type pairOut struct {
	ID          int64       `json:"id"`
	Role        string      `json:"role"` // my role: mentor | mentee
	Need        string      `json:"need"`
	University  string      `json:"university"`
	StartedAt   time.Time   `json:"startedAt"`
	ContinuedAt *time.Time  `json:"continuedAt"`
	EndedAt     *time.Time  `json:"endedAt"`
	EndReason   *string     `json:"endReason"`
	MyScore     *int16      `json:"myScore"`
	Partner     partnerOut  `json:"partner"`
	Prompts     []promptOut `json:"prompts"`
	// Due lists the check-ins I can answer now: check3 from week 3, month from week 4.
	Due []string `json:"due"`
}

// myPairs: GET /api/mentors/pairs — pairs where I am the mentor or the mentee. Runs as the
// owner (it shows the partner's name) with an explicit participant filter.
func myPairs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []pairOut{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select p.id, case when p.mentor_id = $1 then 'mentor' else 'mentee' end,
				p.need, p.university, p.started_at, p.continued_at, p.ended_at, p.end_reason,
				case when p.mentor_id = $1 then p.mentor_score else p.mentee_score end,
				o.id, coalesce(o.name, ''), coalesce(o.university, ''), coalesce(o.faculty, ''),
				case when p.ended_at is null and o.is_adult is true and me.is_adult is true then o.tg_username end,
				coalesce((select jsonb_agg(jsonb_build_object('kind', q.kind, 'sentAt', q.sent_at, 'answered', q.answered_at is not null))
					from mentor_pair_prompts q where q.pair_id = p.id and q.user_id = $1), '[]')
			from mentor_pairs p
			join users o on o.id = case when p.mentor_id = $1 then p.mentee_id else p.mentor_id end
			join users me on me.id = $1
			where p.mentor_id = $1 or p.mentee_id = $1
			order by p.ended_at is null desc, p.started_at desc`, u.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p pairOut
			var prompts []byte
			if err := rows.Scan(&p.ID, &p.Role, &p.Need, &p.University, &p.StartedAt, &p.ContinuedAt, &p.EndedAt, &p.EndReason,
				&p.MyScore, &p.Partner.ID, &p.Partner.Name, &p.Partner.University, &p.Partner.Faculty, &p.Partner.Username,
				&prompts); err != nil {
				return err
			}
			if err := json.Unmarshal(prompts, &p.Prompts); err != nil {
				return err
			}
			p.Due = due(p, time.Now())
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "pairs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"pairs": out})
}

func due(p pairOut, now time.Time) []string {
	out := []string{}
	if p.EndedAt != nil {
		return out
	}
	answered := map[string]bool{}
	for _, q := range p.Prompts {
		answered[q.Kind] = q.Answered
	}
	if now.Sub(p.StartedAt) >= check3After && !answered["check3"] {
		out = append(out, "check3")
	}
	if p.ContinuedAt == nil && now.Sub(p.StartedAt) >= monthAfter && !answered["month"] {
		out = append(out, "month")
	}
	return out
}

type pairRow struct {
	ID          int64
	MentorID    int64
	MenteeID    int64
	RequestID   *int64
	University  string
	Need        string
	StartedAt   time.Time
	ContinuedAt *time.Time
	EndedAt     *time.Time
}

// lockPair loads a pair FOR UPDATE; with userID > 0 it must be one of the two participants.
func lockPair(ctx context.Context, tx pgx.Tx, id, userID int64) (pairRow, error) {
	var p pairRow
	err := tx.QueryRow(ctx, `select id, mentor_id, mentee_id, request_id, university, need, started_at, continued_at, ended_at
		from mentor_pairs where id = $1 and ($2 = 0 or mentor_id = $2 or mentee_id = $2) for update`, id, userID).
		Scan(&p.ID, &p.MentorID, &p.MenteeID, &p.RequestID, &p.University, &p.Need, &p.StartedAt, &p.ContinuedAt, &p.EndedAt)
	return p, err
}

func (p pairRow) other(userID int64) int64 {
	if p.MentorID == userID {
		return p.MenteeID
	}
	return p.MentorID
}

// closePair ends an active pair and closes its request. by may be 0 (system / admin hook).
func closePair(ctx context.Context, tx pgx.Tx, p pairRow, by int64, reason string) error {
	var byArg *int64
	if by > 0 {
		byArg = &by
	}
	_, err := tx.Exec(ctx, `update mentor_pairs set ended_at = now(), end_reason = nullif($2, ''), ended_by = $3
		where id = $1 and ended_at is null`, p.ID, reason, byArg)
	if err != nil {
		return err
	}
	if p.RequestID != nil {
		_, err = tx.Exec(ctx, `update mentor_requests set status = 'closed', closed_at = now() where id = $1 and status <> 'closed'`, *p.RequestID)
	}
	return err
}

// setScore stores the caller's rating and awards the verified "mentor" badge when a mentee
// rates an ended pair 4 or 5.
func setScore(ctx context.Context, tx pgx.Tx, p pairRow, userID int64, score int) error {
	col := "mentee_score"
	if p.MentorID == userID {
		col = "mentor_score"
	}
	if _, err := tx.Exec(ctx, "update mentor_pairs set "+col+" = $2 where id = $1", p.ID, score); err != nil {
		return err
	}
	if col == "mentee_score" && score >= 4 {
		_, err := tx.Exec(ctx, `insert into user_badges (user_id, badge, verified) values ($1, 'mentor', true)
			on conflict (user_id, badge) do nothing`, p.MentorID)
		return err
	}
	return nil
}

func pairErr(w http.ResponseWriter, r *http.Request, what string, err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "pair_not_found", http.StatusNotFound)
		return true
	}
	if err != nil {
		internal(w, r, what, err)
		return true
	}
	return false
}

// checkin: POST /api/mentors/pairs/{id}/checkin
//
//	{"kind":"check3","met":true,"score":4}   week 3: did you meet, how useful (1–5)
//	{"kind":"month","continue":true}         week 4: continue after the first month?
//
// A "no" at week 3 gets a blame-free "find another mentor?" offer (offerRematch). At week 4
// the pair continues only when both said yes; one "no" ends it.
func checkin(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Kind     string `json:"kind"`
		Met      *bool  `json:"met"`
		Score    *int   `json:"score"`
		Continue *bool  `json:"continue"`
	}
	if err := httpx.Decode(r, &in, 1<<10); err != nil || !ok {
		bad(w, r)
		return
	}
	switch in.Kind {
	case "check3":
		if in.Met == nil || (in.Score != nil && (*in.Score < 1 || *in.Score > 5)) {
			bad(w, r)
			return
		}
	case "month":
		if in.Continue == nil {
			bad(w, r)
			return
		}
	default:
		bad(w, r)
		return
	}
	ctx := r.Context()
	code := ""
	resp := map[string]any{"ok": true}
	var ended *pairRow
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		p, err := lockPair(ctx, tx, id, u.ID)
		if err != nil {
			return err
		}
		if p.EndedAt != nil {
			code = "pair_ended"
			return nil
		}
		after := check3After
		if in.Kind == "month" {
			after = monthAfter
		}
		if time.Since(p.StartedAt) < after {
			code = "too_early"
			return nil
		}
		if _, err := tx.Exec(ctx, `insert into mentor_pair_prompts (pair_id, user_id, kind, met, score, continue_, answered_at)
			values ($1, $2, $3, $4, $5, $6, now())
			on conflict (pair_id, user_id, kind) do update set met = excluded.met, score = excluded.score,
				continue_ = excluded.continue_, answered_at = now()`, p.ID, u.ID, in.Kind, in.Met, in.Score, in.Continue); err != nil {
			return err
		}
		if in.Kind == "check3" {
			resp["offerRematch"] = !*in.Met || (in.Score != nil && *in.Score <= 2)
			return nil
		}
		if !*in.Continue {
			resp["ended"] = true
			ended = &p
			return closePair(ctx, tx, p, u.ID, "first_month_done")
		}
		var yes int
		if err := tx.QueryRow(ctx, `select count(*) from mentor_pair_prompts where pair_id = $1 and kind = 'month' and continue_`, p.ID).Scan(&yes); err != nil {
			return err
		}
		if yes == 2 {
			resp["continued"] = true
			_, err = tx.Exec(ctx, `update mentor_pairs set continued_at = now() where id = $1`, p.ID)
		}
		return err
	})
	if pairErr(w, r, "checkin", err) {
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusConflict)
		return
	}
	if ended != nil {
		other, err := loadPerson(ctx, mustPool(ctx), ended.other(u.ID))
		if err == nil {
			direct(ctx, other.ID, endedText(other.Lang))
		}
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// endPair: POST /api/mentors/pairs/{id}/end {reason?, score?, rematch?} — either side may end
// the pair; the reason is optional. rematch (mentee only) opens a new waiting request.
func endPair(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Reason  string `json:"reason"`
		Score   *int   `json:"score"`
		Rematch bool   `json:"rematch"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok || (in.Score != nil && (*in.Score < 1 || *in.Score > 5)) {
		bad(w, r)
		return
	}
	reason, ok := cleanText(in.Reason, 300)
	if !ok {
		bad(w, r)
		return
	}
	ctx := r.Context()
	code := ""
	var p pairRow
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		p, err = lockPair(ctx, tx, id, u.ID)
		if err != nil {
			return err
		}
		if p.EndedAt != nil {
			code = "pair_ended"
			return nil
		}
		if err := closePair(ctx, tx, p, u.ID, reason); err != nil {
			return err
		}
		if in.Score != nil {
			if err := setScore(ctx, tx, p, u.ID, *in.Score); err != nil {
				return err
			}
		}
		if in.Rematch && p.MenteeID == u.ID {
			_, err = tx.Exec(ctx, `insert into mentor_requests (user_id, university, need, goal_text, expect)
				select user_id, university, need, goal_text, 'one_on_one' from mentor_requests where id = $1`, p.RequestID)
		}
		return err
	})
	if pairErr(w, r, "end pair", err) {
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusConflict)
		return
	}
	if other, err := loadPerson(ctx, mustPool(ctx), p.other(u.ID)); err == nil {
		direct(ctx, other.ID, endedText(other.Lang))
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// scorePair: POST /api/mentors/pairs/{id}/score {score} — feedback after the pair ended.
func scorePair(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Score int `json:"score"`
	}
	if err := httpx.Decode(r, &in, 1<<10); err != nil || !ok || in.Score < 1 || in.Score > 5 {
		bad(w, r)
		return
	}
	ctx := r.Context()
	code := ""
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		p, err := lockPair(ctx, tx, id, u.ID)
		if err != nil {
			return err
		}
		if p.EndedAt == nil {
			code = "pair_active"
			return nil
		}
		return setScore(ctx, tx, p, u.ID, in.Score)
	})
	if pairErr(w, r, "score", err) {
		return
	}
	if code != "" {
		fail(w, r, code, http.StatusConflict)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

var reportReasons = []string{"disrespect", "money", "unsafe_meeting", "no_show", "other"}

// reportPair: POST /api/mentors/pairs/{id}/report {reason, text, endPair} — either side
// reports the other to the shared queue (kind mentor_report); admins get a direct message.
// endPair also closes the pair at once.
func reportPair(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Reason  string `json:"reason"`
		Text    string `json:"text"`
		EndPair bool   `json:"endPair"`
	}
	if err := httpx.Decode(r, &in, 4<<10); err != nil || !ok || !contains(reportReasons, in.Reason) {
		bad(w, r)
		return
	}
	text, ok := cleanText(in.Text, 500)
	if !ok {
		bad(w, r)
		return
	}
	ctx := r.Context()
	var subID int64
	var p pairRow
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		p, err = lockPair(ctx, tx, id, u.ID)
		if err != nil {
			return err
		}
		payload := map[string]any{"pairId": p.ID, "reporterId": u.ID, "againstId": p.other(u.ID),
			"reporterRole": map[bool]string{true: "mentor", false: "mentee"}[p.MentorID == u.ID],
			"reason":       in.Reason, "text": text, "pairEnded": in.EndPair || p.EndedAt != nil}
		subID, err = moderation.Submit(ctx, tx, KindReport, payload, &u.ID, p.University)
		if err != nil {
			return err
		}
		if in.EndPair && p.EndedAt == nil {
			return closePair(ctx, tx, p, u.ID, "report")
		}
		return nil
	})
	if pairErr(w, r, "report", err) {
		return
	}
	notifyAdmins(ctx, fmt.Sprintf("⚠️ Менторлық жұпқа шағым #%d (жұп %d, себеп: %s). Кезек: %s",
		subID, p.ID, html.EscapeString(in.Reason), site("/#/admin/queue")))
	httpx.JSON(w, http.StatusCreated, map[string]any{"submissionId": subID})
}

type reportPayload struct {
	PairID     int64 `json:"pairId"`
	ReporterID int64 `json:"reporterId"`
}

// approveReport (the report is upheld): the pair is ended. Removing the mentor role is a
// separate, explicit admin action (POST /api/admin/mentors/{userId}/status).
func approveReport(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var pl reportPayload
	if err := json.Unmarshal(s.Payload, &pl); err != nil {
		return err
	}
	p, err := lockPair(ctx, tx, pl.PairID, 0)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.EndedAt == nil {
		if err := closePair(ctx, tx, p, 0, "report_upheld"); err != nil {
			return err
		}
		for _, id := range []int64{p.MentorID, p.MenteeID} {
			if id == pl.ReporterID {
				continue
			}
			if o, err := loadPerson(ctx, tx, id); err == nil {
				direct(ctx, o.ID, endedText(o.Lang))
			}
		}
	}
	if rep, err := loadPerson(ctx, tx, pl.ReporterID); err == nil {
		direct(ctx, rep.ID, tr(rep.Lang, "Шағымыңыз қаралды, шара қолданылды. Рақмет, бұл қауіпсіздік үшін маңызды.",
			"Ваша жалоба рассмотрена, меры приняты. Спасибо, это важно для безопасности."))
	}
	return nil
}

func rejectReport(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	var pl reportPayload
	if err := json.Unmarshal(s.Payload, &pl); err != nil {
		return err
	}
	if rep, err := loadPerson(ctx, tx, pl.ReporterID); err == nil {
		direct(ctx, rep.ID, tr(rep.Lang, "Шағымыңыз қаралды. Әзірге шара қолданылмады. Себебі: ",
			"Ваша жалоба рассмотрена. Пока меры не приняты. Причина: ")+html.EscapeString(deref(s.Reason)))
	}
	return nil
}

func mustPool(ctx context.Context) interface {
	QueryRow(context.Context, string, ...any) pgx.Row
} {
	p, err := db.Pool(ctx)
	if err != nil {
		return errRower{err}
	}
	return p
}

type errRower struct{ err error }

func (e errRower) QueryRow(context.Context, string, ...any) pgx.Row { return errRow(e) }

type errRow errRower

func (e errRow) Scan(...any) error { return e.err }

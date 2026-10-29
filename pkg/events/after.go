package events

import (
	"context"
	"crypto/subtle"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/cronrun"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

func init() {
	me.RegisterExport("eventRegistrations", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select event_id, status, registered_at, checked_in_at, checkin_method, team_no,
			feedback_score, feedback_text, feedback_at from event_regs where user_id = app_user_id() order by registered_at`)
	})
	me.RegisterExport("eventCertificates", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select event_id, issued_at from event_certificates where user_id = app_user_id() order by issued_at`)
	})
	// event_regs / event_certificates cascade with the account; events.created_by is set null.
}

// finish: status -> finished; with gives_cert every checked-in person gets a certificate.
func finish(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var certs int64
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		if ev.Status != "published" {
			return refuse(http.StatusConflict, "invalid_status", MsgStatus)
		}
		if Now().Before(ev.StartsAt) {
			return refuse(http.StatusConflict, "not_started", MsgStatus)
		}
		if _, err := tx.Exec(r.Context(), `update events set status = 'finished', finished_at = now(), checkin_key = null,
			updated_at = now() where id = $1`, ev.ID); err != nil {
			return err
		}
		if !ev.GivesCert {
			return nil
		}
		tag, err := tx.Exec(r.Context(), `insert into event_certificates (event_id, user_id)
			select event_id, user_id from event_regs where event_id = $1 and checked_in_at is not null
			on conflict do nothing`, ev.ID)
		certs = tag.RowsAffected()
		return err
	})
	if ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"status": "finished", "certificates": certs})
	}
}

// csvCell neutralises spreadsheet formulas.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func csvExport(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if AggregateOnly(u) {
		csvAggregate(w, r, u)
		return
	}
	var rows []regRow
	var ev *Event
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, e *Event) error {
		ev = e
		var err error
		rows, err = loadRegs(r.Context(), tx, e.ID)
		return err
	})
	if !ok {
		return
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	ts := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	num := func(p *int16) string {
		if p == nil {
			return ""
		}
		return fmt.Sprint(*p)
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d-attendees.csv"`, ev.ID))
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM: Excel opens UTF-8 correctly
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"user_id", "name", "username", "faculty", "course", "status", "registered_at", "checked_in_at",
		"checkin_method", "team_no", "feedback_score"})
	for _, x := range rows {
		_ = cw.Write([]string{fmt.Sprint(x.UserID), csvCell(str(x.Name)), csvCell(str(x.Username)), csvCell(str(x.Faculty)),
			num(x.Course), x.Status, x.RegisteredAt.UTC().Format(time.RFC3339), ts(x.CheckedInAt), str(x.Method),
			num(x.TeamNo), num(x.Feedback)})
	}
	cw.Flush()
}

// minGroup: a number about fewer people than this is shown as "< 5" to university staff
// (10-b2b: 5 адамнан аз топ ешқашан көрсетілмейді).
const minGroup = 5

// groupCount marshals like ambassador.Count / b2b.Count: 1–4 becomes "< 5".
type groupCount int

func (c groupCount) MarshalJSON() ([]byte, error) {
	if c > 0 && c < minGroup {
		return []byte(`"< 5"`), nil
	}
	return []byte(fmt.Sprint(int(c))), nil
}

func groupCell(n int) string {
	if n > 0 && n < minGroup {
		return "< 5"
	}
	return fmt.Sprint(n)
}

// csvAggregate is the university staff's attendees.csv: metric,value rows, no row per
// student, small groups as "< 5" (10-b2b: «CSV экспорт тек жиынтық сандармен»).
func csvAggregate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var ev *Event
	var cancelled, revoked, ratings int
	var avg *float64
	byFaculty := map[string]int{}
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, e *Event) error {
		ev = e
		err := tx.QueryRow(r.Context(), `select count(*) filter (where r.status = 'cancelled'),
				count(*) filter (where r.checkin_revoked_at is not null and r.checked_in_at is null),
				count(r.feedback_score), avg(r.feedback_score)::float8
			from event_regs r where r.event_id = $1`, e.ID).Scan(&cancelled, &revoked, &ratings, &avg)
		if err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `select coalesce(nullif(u.faculty, ''), '—'), count(*) from event_regs r
			join users u on u.id = r.user_id where r.event_id = $1 and r.checked_in_at is not null group by 1`, e.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f string
			var n int
			if err := rows.Scan(&f, &n); err != nil {
				return err
			}
			byFaculty[f] = n
		}
		return rows.Err()
	})
	if !ok {
		return
	}
	out := [][]string{{"metric", "value"},
		{"event_id", fmt.Sprint(ev.ID)}, {"registered", groupCell(ev.Registered)}, {"waitlist", groupCell(ev.Waitlist)},
		{"checked_in", groupCell(ev.CheckedIn)}, {"cancelled", groupCell(cancelled)}, {"checkin_revoked", groupCell(revoked)},
		{"ratings", groupCell(ratings)}, {"avg_rating", ""}}
	if ratings >= minGroup && avg != nil {
		out[len(out)-1][1] = fmt.Sprintf("%.1f", *avg)
	}
	facs := make([]string, 0, len(byFaculty))
	for f := range byFaculty {
		facs = append(facs, f)
	}
	sort.Strings(facs)
	small := 0
	for _, f := range facs {
		if n := byFaculty[f]; n < minGroup {
			small += n // 10-032: a faculty with 1–4 people is not named
			continue
		}
		out = append(out, []string{csvCell("checked_in_faculty:" + f), groupCell(byFaculty[f])})
	}
	if small > 0 {
		out = append(out, []string{"checked_in_faculty:other", groupCell(small)})
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d-summary.csv"`, ev.ID))
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.WriteAll(out)
}

func feedback(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
		return
	}
	var body struct {
		Score int    `json:"score"`
		Text  string `json:"text"`
	}
	text := ""
	if err := httpx.Decode(r, &body, 4<<10); err == nil {
		text = strings.TrimSpace(body.Text)
	}
	if body.Score < 1 || body.Score > 5 || utf8.RuneCountInString(text) > 500 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var ended bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select status = 'finished' or ends_at <= now() from events where id = $1 and status <> 'draft'`, id).Scan(&ended)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
		return
	}
	if err != nil {
		fail(w, r, "feedback", err)
		return
	}
	if !ended {
		httpx.Error(w, r, http.StatusConflict, "not_finished", MsgNotFinish)
		return
	}
	// as app_user: the policy allows updating only one's own checked-in registration
	var n int64
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update event_regs set feedback_score = $2, feedback_text = nullif($3, ''), feedback_at = now()
			where event_id = $1 and user_id = app_user_id()`, id, body.Score, text)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		fail(w, r, "feedback", err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, http.StatusForbidden, "not_attended", MsgNotCame)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

type fbText struct {
	Score int       `json:"score"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// feedbackSummary: anonymous results for the organizer. University staff get the
// distribution and average only from 5 answers and never the free-text comments (a
// comment is one student's own words; 10-b2b allows aggregates only).
func feedbackSummary(w http.ResponseWriter, r *http.Request, u *auth.User) {
	dist := make([]int, 5)
	var texts []fbText
	var count int
	var avg *float64
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		rows, err := tx.Query(r.Context(), `select feedback_score, coalesce(feedback_text, ''), feedback_at from event_regs
			where event_id = $1 and feedback_score is not null order by feedback_at desc`, ev.ID)
		if err != nil {
			return err
		}
		texts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (fbText, error) {
			var f fbText
			return f, row.Scan(&f.Score, &f.Text, &f.At)
		})
		return err
	})
	if !ok {
		return
	}
	sum := 0
	withText := []fbText{}
	for _, f := range texts {
		dist[f.Score-1]++
		sum += f.Score
		count++
		if f.Text != "" {
			withText = append(withText, f)
		}
	}
	if count > 0 {
		a := float64(sum) / float64(count)
		avg = &a
	}
	if AggregateOnly(u) {
		var d []int
		if count >= minGroup {
			d = dist
		} else {
			avg = nil
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"count": groupCount(count), "average": avg, "distribution": d,
			"comments": []fbText{}, "aggregate_only": true})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"count": count, "average": avg, "distribution": dist, "comments": withText})
}

// ics: "add to calendar" file.
func ics(w http.ResponseWriter, r *http.Request) {
	var ev *Event
	if !withEvent(w, r, auth.Current(r), false, func(tx pgx.Tx, e *Event) error { ev = e; return nil }) {
		return
	}
	esc := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
	}
	title, desc := ev.TitleKK, ""
	if httpx.Lang(r) == "ru" && ev.TitleRU != nil {
		title = *ev.TitleRU
	}
	if ev.DescKK != nil {
		desc = *ev.DescKK
	}
	if httpx.Lang(r) == "ru" && ev.DescRU != nil {
		desc = *ev.DescRU
	}
	loc := ""
	if ev.PlaceText != nil {
		loc = *ev.PlaceText
	}
	f := "20060102T150405Z"
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Student Hub//Events//KK", "CALSCALE:GREGORIAN", "METHOD:PUBLISH",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:event-%d@studenthub", ev.ID),
		"DTSTAMP:" + Now().UTC().Format(f),
		"DTSTART:" + ev.StartsAt.UTC().Format(f),
		"DTEND:" + ev.EndsAt.UTC().Format(f),
		"SUMMARY:" + esc(title),
		"DESCRIPTION:" + esc(desc),
		"LOCATION:" + esc(loc),
		"URL:" + eventLink(ev.ID),
		"END:VEVENT", "END:VCALENDAR",
	}
	for i, l := range lines {
		lines[i] = foldICS(l)
	}
	body := strings.Join(lines, "\r\n") + "\r\n"
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d.ics"`, ev.ID))
	_, _ = w.Write([]byte(body))
}

// ---------- daily job ----------

// DailyStats is what Daily did.
type DailyStats struct {
	Reminders     int `json:"reminders"`
	ReminderSkips int `json:"reminder_skips"` // the student already got a message today
	FeedbackAsks  int `json:"feedback_asks"`
	ClaimsPruned  int `json:"claims_pruned"`
}

func almatyDay(t time.Time) string { return t.UTC().Add(5 * time.Hour).Format("2006-01-02") }

type dueMsg struct {
	eventID, userID int64
	lang, title     string
	titleRU         *string
	starts          time.Time
}

// Daily sends (1) the reminder one day before an event and (2) the one-question feedback ask
// the day after, never more than one message per student per day (bot_daily_sends), and prunes
// stale check-in claims. The bot's daily cron (or GET /api/cron/events) calls it once a day.
func Daily(ctx context.Context, now time.Time) (DailyStats, error) {
	var st DailyStats
	today := almatyDay(now)
	tomorrow := almatyDay(now.Add(24 * time.Hour))
	collect := func(q string, args ...any) ([]dueMsg, error) {
		var out []dueMsg
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, q, args...)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueMsg, error) {
				var m dueMsg
				return m, row.Scan(&m.eventID, &m.userID, &m.lang, &m.title, &m.titleRU, &m.starts)
			})
			return err
		})
		return out, err
	}
	people := `join users u on u.id = r.user_id and u.deleted_at is null`
	reminders, err := collect(`select r.event_id, r.user_id, u.lang, e.title_kk, e.title_ru, e.starts_at
		from event_regs r join events e on e.id = r.event_id `+people+`
		where e.status = 'published' and r.status = 'registered' and r.reminded_at is null
			and ((e.starts_at at time zone 'Asia/Almaty')::date) = $1::date`, tomorrow)
	if err != nil {
		return st, err
	}
	asks, err := collect(`select r.event_id, r.user_id, u.lang, e.title_kk, e.title_ru, e.starts_at
		from event_regs r join events e on e.id = r.event_id `+people+`
		where e.status in ('published', 'finished') and r.checked_in_at is not null and r.feedback_asked_at is null
			and r.feedback_score is null
			and ((e.ends_at at time zone 'Asia/Almaty')::date) < $1::date
			and e.ends_at > $2::timestamptz - interval '3 days'`, today, now)
	if err != nil {
		return st, err
	}
	process := func(list []dueMsg, mark string, text func(m dueMsg) string) (sent, skipped int) {
		for _, m := range list {
			key := fmt.Sprintf("event:%d:%s", m.eventID, mark)
			status, err := notify.DigestAs(notify.WithKey(ctx, key), m.userID, notify.CatEvents, now, text(m))
			if err != nil {
				log.Printf("events: daily %d/%d: %v", m.eventID, m.userID, err)
				continue
			}
			if !notify.DigestAccepted(status) {
				skipped++
				continue
			}
			err = db.Tx(ctx, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, "update event_regs set "+mark+" = $3 where event_id = $1 and user_id = $2", m.eventID, m.userID, now)
				return err
			})
			if err != nil {
				log.Printf("events: daily mark %d/%d: %v", m.eventID, m.userID, err)
				continue
			}
			sent++
		}
		return
	}
	title := func(m dueMsg) string {
		if m.lang == "ru" && m.titleRU != nil {
			return html.EscapeString(*m.titleRU)
		}
		return html.EscapeString(m.title)
	}
	st.Reminders, st.ReminderSkips = process(reminders, "reminded_at", func(m dueMsg) string {
		at := m.starts.UTC().Add(5 * time.Hour).Format("15:04")
		if m.lang == "ru" {
			return fmt.Sprintf("📅 Завтра в %s — «%s». Ваш билет: %s", at, title(m), eventLink(m.eventID))
		}
		return fmt.Sprintf("📅 Ертең %s-де — «%s». Билетіңіз: %s", at, title(m), eventLink(m.eventID))
	})
	st.FeedbackAsks, _ = process(asks, "feedback_asked_at", func(m dueMsg) string {
		if m.lang == "ru" {
			return fmt.Sprintf("Как прошло «%s»? Оцените 1–5 и напишите, что понравилось / чего не хватило (по желанию): %s",
				title(m), eventLink(m.eventID))
		}
		return fmt.Sprintf("«%s» қалай өтті? 1–5 баға беріп, не ұнағанын / не жетіспегенін жазыңыз (міндетті емес): %s",
			title(m), eventLink(m.eventID))
	})
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from event_checkin_claims where expires_at < $1::timestamptz - interval '1 day'`, now)
		st.ClaimsPruned = int(tag.RowsAffected())
		return err
	})
	return st, err
}

// CronAuthorized: Authorization: Bearer <CRON_SECRET> (same secret as the bot cron).
func CronAuthorized(r *http.Request) bool {
	secret := config.Str("CRON_SECRET", "")
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return secret != "" && ok && subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1
}

func cronHandler(w http.ResponseWriter, r *http.Request) {
	if !CronAuthorized(r) {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	st, err := Daily(r.Context(), Now())
	if err != nil {
		fail(w, r, "cron", err)
		return
	}
	if err := cronrun.Success(r.Context(), "/api/cron/events"); err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "cron_record_failed", httpx.MsgInternal)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

// foldICS splits a content line into 75-octet pieces (RFC 5545 §3.1) without breaking a UTF-8
// character; continuation lines start with a single space.
func foldICS(line string) string {
	if len(line) <= 75 {
		return line
	}
	var b strings.Builder
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = 74 // the leading space counts toward the next line's 75 octets
	}
	b.WriteString(line)
	return b.String()
}

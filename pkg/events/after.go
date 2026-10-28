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
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
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

// feedbackSummary: anonymous results for the organizer.
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
	body := strings.Join([]string{
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
		"END:VEVENT", "END:VCALENDAR", "",
	}, "\r\n")
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

// claimDay applies the bot's hard rule: at most one message per student per Almaty day.
func claimDay(ctx context.Context, tx pgx.Tx, userID int64, day string) (bool, error) {
	tag, err := tx.Exec(ctx, `insert into bot_daily_sends (user_id, day) values ($1, $2::date) on conflict do nothing`, userID, day)
	return tag.RowsAffected() == 1, err
}

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
	people := `join users u on u.id = r.user_id and u.deleted_at is null and not u.bot_inactive
		and (u.paused_until is null or u.paused_until < $2)`
	reminders, err := collect(`select r.event_id, r.user_id, u.lang, e.title_kk, e.title_ru, e.starts_at
		from event_regs r join events e on e.id = r.event_id `+people+`
		where e.status = 'published' and r.status = 'registered' and r.reminded_at is null
			and ((e.starts_at at time zone 'Asia/Almaty')::date) = $1::date`, tomorrow, now)
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
			var claimed bool
			err := db.Tx(ctx, func(tx pgx.Tx) error {
				var err error
				if claimed, err = claimDay(ctx, tx, m.userID, today); err != nil || !claimed {
					return err
				}
				_, err = tx.Exec(ctx, "update event_regs set "+mark+" = now() where event_id = $1 and user_id = $2", m.eventID, m.userID)
				return err
			})
			if err != nil {
				log.Printf("events: daily %d/%d: %v", m.eventID, m.userID, err)
				continue
			}
			if !claimed {
				skipped++
				continue
			}
			if err := Notify(ctx, m.userID, text(m)); err != nil {
				log.Printf("events: daily notify %d: %v", m.userID, err)
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
	httpx.JSON(w, http.StatusOK, st)
}

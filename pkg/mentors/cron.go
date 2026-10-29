package mentors

import (
	"context"
	"crypto/subtle"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/cronrun"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// monthGrace: after the week-4 question, a pair without two "yes" answers ends after this.
const monthGrace = 7 * 24 * time.Hour

// CronResult counts what one daily run did.
type CronResult struct {
	Check3Sent   int `json:"check3Sent"`
	MonthSent    int `json:"monthSent"`
	Postponed    int `json:"postponed"` // the student already had a bot message today
	MonthExpired int `json:"monthExpired"`
	// 07-017: waiting mentees told about a new group meeting of their university
	MeetingNotices   int `json:"meetingNotices"`
	MeetingPostponed int `json:"meetingPostponed"`
}

// cronHandler: GET /api/cron/mentors with "Authorization: Bearer $CRON_SECRET". Run it after
// the bot's /api/cron so deadline reminders keep priority in the one-message-a-day budget.
func cronHandler(w http.ResponseWriter, r *http.Request) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	secret := config.Str("CRON_SECRET", "")
	if !ok || secret == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) != 1 {
		httpx.JSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
		return
	}
	res, err := RunCron(r.Context(), time.Now())
	if err != nil {
		internal(w, r, "cron", err)
		return
	}
	if err := cronrun.Success(r.Context(), "/api/cron/mentors"); err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "cron_record_failed", httpx.MsgInternal)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

type duePrompt struct {
	pairID, userID int64
	lang           string
}

// RunCron sends the week-3 check-in and the week-4 "continue?" question (digest type, so the
// one-bot-message-per-day rule applies: a student who already got a message today is asked
// on a later run) and ends first-month pairs that did not get two "yes" answers in time.
func RunCron(ctx context.Context, now time.Time) (*CronResult, error) {
	res := &CronResult{}
	for _, kind := range []string{"check3", "month"} {
		after := check3After
		if kind == "month" {
			after = monthAfter
		}
		var list []duePrompt
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select p.id, u.id, u.lang
				from mentor_pairs p
				join users u on u.id in (p.mentor_id, p.mentee_id)
				where p.ended_at is null and p.started_at <= $1::timestamptz - make_interval(secs => $2::float8)
					and ($3 = 'check3' or p.continued_at is null)
					and u.deleted_at is null
					and not exists (select 1 from mentor_pair_prompts q
						where q.pair_id = p.id and q.user_id = u.id and q.kind = $3 and (q.sent_at is not null or q.answered_at is not null))
				order by p.started_at`, now, after.Seconds(), kind)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var d duePrompt
				if err := rows.Scan(&d.pairID, &d.userID, &d.lang); err != nil {
					return err
				}
				list = append(list, d)
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			text := check3Text(d.lang)
			if kind == "month" {
				text = monthText(d.lang)
			}
			status, err := notify.DigestAs(notify.WithKey(ctx, fmt.Sprintf("mentor-pair:%d:%s", d.pairID, kind)), d.userID, notify.CatMentors, now, text)
			if err != nil {
				log.Printf("mentors cron: %s %d: %v", kind, d.userID, err)
				continue
			}
			if !notify.DigestAccepted(status) {
				res.Postponed++
				continue
			}
			col := "check3_at"
			if kind == "month" {
				col = "month_at"
				res.MonthSent++
			} else {
				res.Check3Sent++
			}
			err = db.Tx(ctx, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `insert into mentor_pair_prompts (pair_id, user_id, kind, sent_at) values ($1, $2, $3, $4)
					on conflict (pair_id, user_id, kind) do update set sent_at = coalesce(mentor_pair_prompts.sent_at, excluded.sent_at)`,
					d.pairID, d.userID, kind, now); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, "update mentor_pairs set "+col+" = coalesce("+col+", $2) where id = $1", d.pairID, now)
				return err
			})
			if err != nil {
				return nil, err
			}
		}
	}
	if err := meetingNotices(ctx, now, res); err != nil {
		return nil, err
	}
	// first month over, question asked a week ago, not both "yes": the pair ends quietly
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id from mentor_pairs
			where ended_at is null and continued_at is null and month_at <= $1::timestamptz - make_interval(secs => $2::float8)`, now, monthGrace.Seconds())
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
			if err := closePair(ctx, tx, p, 0, "first_month_done"); err != nil {
				return err
			}
			res.MonthExpired++
		}
		return nil
	})
	return res, err
}

type meetingNotice struct {
	eventID, userID int64
	lang, title     string
	startsAt        time.Time
	place           string
}

var almaty = time.FixedZone("Asia/Almaty", 5*3600)

func meetingText(m meetingNotice) string {
	when := m.startsAt.In(almaty).Format("02.01 15:04")
	place := ""
	if m.place != "" {
		place = ", " + html.EscapeString(m.place)
	}
	return tr(m.lang,
		"🎓 Сенің университетіңде топтық менторлық кездесу ашылды: «"+html.EscapeString(m.title)+"» — "+when+place+
			". Жеке ментор күтіп жүргенде осыған қатысып көр (орын саны шектеулі): "+site("/event/"+strconv.FormatInt(m.eventID, 10)),
		"🎓 В вашем университете открыта групповая менторская встреча: «"+html.EscapeString(m.title)+"» — "+when+place+
			". Пока ждёте личного ментора, приходите на неё (мест немного): "+site("/event/"+strconv.FormatInt(m.eventID, 10)))
}

// meetingNotices (07-017: «Күтіп тұрған студенттерге сай топтық кездесу ашылса — digest
// хабарлама»): every student with a waiting mentor request hears once about each published,
// not yet started group meeting (cat = mentoring) of their university that still has seats
// and that they have not registered for. It is a digest-type message in the "mentors"
// category: at most one bot message a day (DigestAs), so a student gets one meeting per run
// and the rest on later runs. A notice is recorded when the message went out or cannot go
// out by the bot at all (category off, no Telegram) — the inbox has it either way.
func meetingNotices(ctx context.Context, now time.Time, res *CronResult) error {
	var list []meetingNotice
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select e.id, r.user_id, u.lang,
				case when u.lang = 'ru' then coalesce(nullif(e.title_ru, ''), e.title_kk) else e.title_kk end,
				e.starts_at, coalesce(e.place_text, '')
			from events e
			join mentor_requests r on r.university = e.university and r.status = 'waiting'
			join users u on u.id = r.user_id and u.deleted_at is null
			where e.cat = 'mentoring' and e.status = 'published' and e.starts_at > $1
				and (e.capacity is null or e.capacity > (select count(*) from event_regs g where g.event_id = e.id and g.status = 'registered'))
				and not exists (select 1 from event_regs g where g.event_id = e.id and g.user_id = r.user_id)
				and not exists (select 1 from mentor_meeting_notices n where n.event_id = e.id and n.user_id = r.user_id)
			order by r.user_id, e.starts_at, e.id`, now)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (meetingNotice, error) {
			var m meetingNotice
			return m, row.Scan(&m.eventID, &m.userID, &m.lang, &m.title, &m.startsAt, &m.place)
		})
		return err
	})
	if err != nil {
		return err
	}
	for _, m := range list {
		st, err := notify.DigestAs(notify.WithKey(notify.WithLink(ctx, "/event/"+strconv.FormatInt(m.eventID, 10)), fmt.Sprintf("mentor-meeting:%d", m.eventID)), m.userID, notify.CatMentors, now, meetingText(m))
		if err != nil {
			log.Printf("mentors cron: meeting %d to %d: %v", m.eventID, m.userID, err)
			continue
		}
		if !notify.DigestAccepted(st) {
			res.MeetingPostponed++
			continue
		}
		res.MeetingNotices++
		err = db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `insert into mentor_meeting_notices (event_id, user_id, sent_at) values ($1, $2, $3) on conflict do nothing`,
				m.eventID, m.userID, now)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

package b2b

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/events"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// DailyStats is what one run did.
type DailyStats struct {
	EmployerReminders int `json:"employerReminders"` // companies reminded about unopened applications
	NoResponse        int `json:"noResponse"`        // applications moved to no_response
	StudentsTold      int `json:"studentsTold"`      // "the company did not answer in 14 days" messages sent
	StudentsDeferred  int `json:"studentsDeferred"`  // had a bot message today already; retried tomorrow
	JobsExpired       int `json:"jobsExpired"`
	LowRateWarnings   int `json:"lowRateWarnings"` // admins told about companies answering < 30%
}

// Daily is the anti-ghosting job (prompt 10, stage 4):
//  1. applications not opened for 5 days -> one reminder per company (direct, to every member);
//  2. applications without an answer for 14 days -> no_response, the student gets an honest
//     message under the one-bot-message-per-day rule (notify.Digest; retried on later runs);
//  3. published jobs past expires_at -> expired;
//  4. companies whose response rate fell below 30% -> admins are told (once per 30 days).
func Daily(ctx context.Context, now time.Time) (DailyStats, error) {
	var st DailyStats
	type remind struct {
		employer int64
		n        int
		members  []int64
	}
	var reminders []remind
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `with due as (
				update applications a set reminded_at = $1
				from employers e
				where e.id = a.employer_id and e.status = 'verified' and a.status = 'sent' and a.reminded_at is null
					and a.created_at <= $1::timestamptz - make_interval(days => $2)
				returning a.employer_id)
			select employer_id, count(*) from due group by employer_id`, now, RemindAfterDays)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r remind
			if err := rows.Scan(&r.employer, &r.n); err != nil {
				rows.Close()
				return err
			}
			reminders = append(reminders, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range reminders {
			if reminders[i].members, err = memberIDs(ctx, tx, reminders[i].employer); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return st, err
	}
	for _, r := range reminders {
		n := strconv.Itoa(r.n)
		for _, m := range r.members {
			tell(ctx, m, "⏰ "+n+" өтінім 5 күннен бері қаралмады. Студенттер жауап күтіп отыр: "+siteLink("/employer"),
				"⏰ "+n+" откликов не открыты уже 5 дней. Студенты ждут ответа: "+siteLink("/employer"))
		}
		st.EmployerReminders++
	}

	err = db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update applications set status = 'no_response', updated_at = now()
			where status in ('sent', 'viewed') and created_at <= $1::timestamptz - make_interval(days => $2)`, now, NoResponseDays)
		st.NoResponse = int(tag.RowsAffected())
		if err != nil {
			return err
		}
		tag, err = tx.Exec(ctx, `update jobs set status = 'expired' where status = 'published' and expires_at <= $1`, now)
		st.JobsExpired = int(tag.RowsAffected())
		return err
	})
	if err != nil {
		return st, err
	}

	// students: one honest message per application, at most one bot message per day
	type due struct {
		id, user       int64
		lang, title, c string
	}
	var list []due
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select a.id, a.user_id, u.lang, j.title_kk, e.name from applications a
			join users u on u.id = a.user_id and u.deleted_at is null and not u.bot_inactive
			join jobs j on j.id = a.job_id join employers e on e.id = a.employer_id
			where a.status = 'no_response' and a.student_notified_at is null order by a.id limit 500`)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
			var d due
			return d, row.Scan(&d.id, &d.user, &d.lang, &d.title, &d.c)
		})
		return err
	})
	if err != nil {
		return st, err
	}
	told := map[int64]bool{}
	for _, d := range list {
		if told[d.user] {
			st.StudentsDeferred++
			continue
		}
		text := pick(d.lang,
			"«"+esc(d.c)+"» компаниясы «"+esc(d.title)+"» бойынша 14 күнде жауап бермеді. Басқа вакансияларды қара: "+siteLink("/jobs"),
			"Компания «"+esc(d.c)+"» не ответила по вакансии «"+esc(d.title)+"» за 14 дней. Посмотрите другие вакансии: "+siteLink("/jobs"))
		res, err := Digest(ctx, d.user, now, text)
		if err != nil {
			log.Printf("b2b: no_response notify %d: %v", d.user, err)
			continue
		}
		told[d.user] = true
		if res != notify.DigestSent {
			st.StudentsDeferred++
			continue
		}
		st.StudentsTold++
		_ = db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update applications set student_notified_at = now() where id = $1`, d.id)
			return err
		})
	}

	// low response rate -> admins
	var ids []int64
	names := map[int64]string{}
	var admins []int64
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, name from employers where status = 'verified'
			and (low_rate_warned_at is null or low_rate_warned_at < $1::timestamptz - interval '30 days')`, now)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var n string
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
			names[id] = n
		}
		rows.Close()
		admins, err = adminIDs(ctx, tx)
		return err
	})
	if err != nil {
		return st, err
	}
	stats, err := ResponseStatsFor(ctx, ids)
	if err != nil {
		return st, err
	}
	for id, s := range stats {
		if !s.Low() {
			continue
		}
		st.LowRateWarnings++
		for _, a := range admins {
			_ = Notify(ctx, a, "⚠️ «"+esc(names[id])+"» өтінімдердің "+strconv.Itoa(*s.Rate)+"%-ына ғана жауап береді / отвечает лишь на "+
				strconv.Itoa(*s.Rate)+"% откликов. "+siteLink("/admin/employers"))
		}
		_ = db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update employers set low_rate_warned_at = $2 where id = $1`, id, now)
			return err
		})
	}
	return st, nil
}

// cronHandler: GET /api/cron/b2b with Authorization: Bearer <CRON_SECRET>.
func cronHandler(w http.ResponseWriter, r *http.Request) {
	if !events.CronAuthorized(r) {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	st, err := Daily(r.Context(), Now())
	if err != nil {
		internal(w, r, "cron", err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

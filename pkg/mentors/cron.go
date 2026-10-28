package mentors

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
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
			status, err := notify.DigestAs(ctx, d.userID, notify.CatMentors, now, text)
			if err != nil {
				log.Printf("mentors cron: %s %d: %v", kind, d.userID, err)
				continue
			}
			if status != notify.DigestSent {
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

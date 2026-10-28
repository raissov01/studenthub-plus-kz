package market

import (
	"context"
	"crypto/subtle"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/cronrun"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// orphanAge: uploaded photos never attached to an item are removed after this.
const orphanAge = 24 * time.Hour

type CronResult struct {
	Expired       int `json:"expired"`
	Reminded      int `json:"reminded"`
	Postponed     int `json:"postponed"` // digest not sent: the student already had a bot message today
	OrphanPhotos  int `json:"orphanPhotos"`
	SeasonSent    int `json:"seasonSent"`
	SeasonPending int `json:"seasonPending"`
}

// cronHandler: GET /api/cron/market with "Authorization: Bearer $CRON_SECRET", daily, after
// the bot's /api/cron (deadline reminders keep priority in the one-message-a-day budget).
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
	if err := cronrun.Success(r.Context(), "/api/cron/market"); err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "cron_record_failed", httpx.MsgInternal)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func tr(lang, kk, ru string) string {
	if lang == "ru" {
		return ru
	}
	return kk
}

// RunCron closes items after 30 days (photos deleted), sends the "3 days left" digest,
// removes unattached uploads and, in MARKET_SEASON, sends the seasonal digest once per
// student per season.
func RunCron(ctx context.Context, now time.Time) (*CronResult, error) {
	res := &CronResult{}

	// 1. expiry
	var files []string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `update market_items set status = 'expired'
			where status in ('active', 'reserved') and expires_at <= $1 returning id`, now)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, id := range ids {
			names, err := dropPhotos(ctx, tx, id)
			if err != nil {
				return err
			}
			files = append(files, names...)
		}
		res.Expired = len(ids)
		return nil
	})
	if err != nil {
		return nil, err
	}
	removeFiles(files)

	// 2. "3 days left: still giving it away? extend / close" — one digest per owner
	type due struct {
		user  int64
		lang  string
		items []int64
		title string
	}
	var list []due
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select i.user_id, u.lang, array_agg(i.id order by i.expires_at), min(i.title)
			from market_items i join users u on u.id = i.user_id
			where i.status in ('active', 'reserved') and i.reminded_at is null
				and i.expires_at <= $1::timestamptz + interval '3 days' and u.deleted_at is null
			group by i.user_id, u.lang`, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.user, &d.lang, &d.items, &d.title); err != nil {
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
		link := config.SiteURL() + "/market/mine"
		t := html.EscapeString(d.title)
		text := tr(d.lang, "🎁 «"+t+"» жарнамасының мерзімі 3 күннен кейін бітеді. Әлі бересің бе? Ұзарту немесе жабу: "+link,
			"🎁 Объявление «"+t+"» закроется через 3 дня. Ещё отдаёшь? Продлить или закрыть: "+link)
		if len(d.items) > 1 {
			text = tr(d.lang, "🎁 Маркеттегі заттарыңның мерзімі 3 күннен кейін бітеді. Ұзарту немесе жабу: "+link,
				"🎁 Твои объявления в маркете закроются через 3 дня. Продлить или закрыть: "+link)
		}
		st, err := notify.DigestAs(ctx, d.user, notify.CatMarket, now, text)
		if err != nil {
			log.Printf("market cron: remind %d: %v", d.user, err)
			continue
		}
		if st != notify.DigestSent {
			res.Postponed++
			continue
		}
		res.Reminded++
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update market_items set reminded_at = $2 where id = any($1)`, d.items, now)
			return err
		}); err != nil {
			return nil, err
		}
	}

	// 3. unattached uploads
	files = nil
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `delete from market_photos where item_id is null and created_at < $1 returning name`, now.Add(-orphanAge))
		if err != nil {
			return err
		}
		files, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, err
	}
	res.OrphanPhotos = len(files)
	removeFiles(files)

	// 4. seasonal digest, once per student per season (batch per run; the rest follow later)
	if s := features.MarketSeason(now); s != nil && s.Active {
		var users []struct {
			id   int64
			lang string
		}
		err = db.Tx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select u.id, u.lang from users u
				where u.deleted_at is null and u.consent_at is not null and not u.bot_inactive
					and not exists (select 1 from market_season_sends s where s.season = $1 and s.user_id = u.id)
				order by u.id limit $2`, s.Key(), config.Int("MARKET_SEASON_BATCH", 1000))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var u struct {
					id   int64
					lang string
				}
				if err := rows.Scan(&u.id, &u.lang); err != nil {
					return err
				}
				users = append(users, u)
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
		link := config.SiteURL() + "/market/new"
		for _, u := range users {
			text := tr(u.lang, "📦 Көшіп жатырсың ба? Керек емес заттарыңды жаңа студенттерге тегін бер: "+link,
				"📦 Переезжаешь? Отдай ненужные вещи новым студентам бесплатно: "+link)
			st, err := notify.DigestAs(ctx, u.id, notify.CatMarket, now, text)
			if err != nil {
				log.Printf("market cron: season %d: %v", u.id, err)
				continue
			}
			if st != notify.DigestSent {
				res.SeasonPending++
				continue
			}
			res.SeasonSent++
			if err := db.Tx(ctx, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `insert into market_season_sends (season, user_id) values ($1, $2) on conflict do nothing`, s.Key(), u.id)
				return err
			}); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

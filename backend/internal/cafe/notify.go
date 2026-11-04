package cafe

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
)

// Order updates go through notify.Order (transactional: no quiet hours or daily cap, never deferred). The
// student and the staff also see everything in the app, so a message that cannot be sent is only logged.

// Send is replaced by tests.
var Send = notify.Order

type button struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

func markup(rows ...[]button) json.RawMessage {
	var kb [][]button
	for _, r := range rows {
		if len(r) > 0 {
			kb = append(kb, r)
		}
	}
	raw, _ := json.Marshal(map[string]any{"inline_keyboard": kb})
	return raw
}

func pick(lang, kk, ru string) string {
	if lang == "ru" {
		return ru
	}
	return kk
}

// boardButton opens the staff board. Telegram accepts only https URL buttons.
func boardButton(lang string) []button {
	site := config.SiteURL()
	if !strings.HasPrefix(site, "https://") {
		return nil
	}
	return []button{{Text: pick(lang, "Тапсырыстар тақтасы", "Доска заказов"), URL: site + "/cafe/staff"}}
}

func hhmm(o *Order) string { return o.PickupAt.In(almaty).Format("15:04") }

func itemsText(o *Order, lang string) string {
	var b strings.Builder
	for _, l := range o.Items {
		name := pick(lang, l.NameKK, l.NameRU)
		fmt.Fprintf(&b, "\n%d × %s", l.Qty, html.EscapeString(name))
		for _, op := range l.Options {
			fmt.Fprintf(&b, " · %s", html.EscapeString(pick(lang, op.NameKK, op.NameRU)))
		}
	}
	return b.String()
}

type recipient struct {
	id   int64
	lang string
}

func staffOf(ctx context.Context, cafeID int64, managersOnly bool) ([]recipient, error) {
	var out []recipient
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select u.id, u.lang from cafe_staff s join users u on u.id = s.user_id
			where s.cafe_id = $1 and u.deleted_at is null and (not $2 or s.role = 'manager') order by u.id`, cafeID, managersOnly)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (recipient, error) {
			var r recipient
			return r, row.Scan(&r.id, &r.lang)
		})
		return err
	})
	return out, err
}

func orderByID(ctx context.Context, id int64) (*Order, error) {
	var o *Order
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		o, err = loadOrder(ctx, tx, id)
		return err
	})
	return o, err
}

// notifyStaffNew: a new order the counter can make now (pay-at-pickup at once, Kaspi once paid), with an
// «Accept» button handled by the bot (bot.CafeAction).
func notifyStaffNew(ctx context.Context, orderID int64) {
	o, err := orderByID(ctx, orderID)
	if err != nil || o.Status != "new" {
		return
	}
	staff, err := staffOf(ctx, o.CafeID, false)
	if err != nil {
		log.Printf("cafe: staff of %d: %v", o.CafeID, err)
		return
	}
	for _, s := range staff {
		pay := pick(s.lang, "кассада төлейді", "оплата на кассе")
		if o.PaymentMethod == "kaspi" {
			pay = pick(s.lang, "Kaspi арқылы төленді", "оплачено через Kaspi")
		}
		text := fmt.Sprintf("<b>%s №%s</b> · %s%s\n%s: %s · %s",
			pick(s.lang, "Жаңа тапсырыс", "Новый заказ"), o.PickupCode, html.EscapeString(pick(s.lang, o.CafeNameKK, o.CafeNameRU)),
			itemsText(o, s.lang), pick(s.lang, "Алу уақыты", "Выдача"), hhmm(o), tenge(o.Total)+", "+pay)
		if o.Note != "" {
			text += "\n" + pick(s.lang, "Ескертпе: ", "Комментарий: ") + html.EscapeString(o.Note)
		}
		opts := notify.Opts{Link: "/cafe/staff", Key: fmt.Sprintf("cafe:new:%d", o.ID),
			ReplyMarkup: markup([]button{{Text: pick(s.lang, "Қабылдау", "Принять"), CallbackData: fmt.Sprintf("cafe:a%d", o.ID)}}, boardButton(s.lang))}
		if _, err := Send(ctx, s.id, text, opts); err != nil {
			log.Printf("cafe: notify staff %d of order %d: %v", s.id, o.ID, err)
		}
	}
}

// notifyStudent tells the student about their order: accepted, ready, declined, refunded, refund_manual.
func notifyStudent(ctx context.Context, orderID int64, kind string) {
	o, err := orderByID(ctx, orderID)
	if err != nil || o.userID == nil {
		return
	}
	var lang string
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select lang from users where id = $1 and deleted_at is null`, *o.userID).Scan(&lang)
	})
	if err != nil {
		return
	}
	cafe := html.EscapeString(pick(lang, o.CafeNameKK, o.CafeNameRU))
	place := html.EscapeString(pick(lang, o.PlaceKK, o.PlaceRU))
	var text string
	switch kind {
	case "accepted":
		text = pick(lang, fmt.Sprintf("<b>Тапсырыс қабылданды</b> · №%s\n%s, %s дайын болады.", o.PickupCode, cafe, hhmm(o)),
			fmt.Sprintf("<b>Заказ принят</b> · №%s\n%s, будет готов к %s.", o.PickupCode, cafe, hhmm(o)))
	case "ready":
		where := cafe
		if place != "" {
			where += ", " + place
		}
		text = pick(lang, fmt.Sprintf("<b>Дайын — алып кетіңіз</b> · №%s\n%s. Кодты кассада айтыңыз.", o.PickupCode, where),
			fmt.Sprintf("<b>Готово — заберите</b> · №%s\n%s. Назовите код на кассе.", o.PickupCode, where))
	case "declined":
		text = pick(lang, fmt.Sprintf("<b>Кофейня тапсырысты қабылдамады</b> · №%s\n%s.", o.PickupCode, cafe),
			fmt.Sprintf("<b>Кофейня отклонила заказ</b> · №%s\n%s.", o.PickupCode, cafe))
		if o.CancelReason != nil && *o.CancelReason != "" {
			text += "\n" + html.EscapeString(*o.CancelReason)
		}
	case "refunded":
		text = pick(lang, fmt.Sprintf("Тапсырыс №%s ақшасы Kaspi-ге қайтарылды: %s.", o.PickupCode, tenge(o.Total)),
			fmt.Sprintf("Деньги за заказ №%s возвращены в Kaspi: %s.", o.PickupCode, tenge(o.Total)))
	case "refund_manual":
		text = pick(lang, fmt.Sprintf("Тапсырыс №%s ақшасын (%s) кофейня өзі қайтарады. Сұрақ болса — %s кассасына.", o.PickupCode, tenge(o.Total), cafe),
			fmt.Sprintf("Деньги за заказ №%s (%s) вернёт сама кофейня. Вопросы — на кассе %s.", o.PickupCode, tenge(o.Total), cafe))
	default:
		return
	}
	opts := notify.Opts{Link: fmt.Sprintf("/cafe/orders?id=%d", o.ID), Key: fmt.Sprintf("cafe:%s:%d", kind, o.ID)}
	if _, err := Send(ctx, *o.userID, text, opts); err != nil {
		log.Printf("cafe: notify student of order %d (%s): %v", o.ID, kind, err)
	}
}

// notifyManagers: things a café manager must act on (the Kaspi session stopped, a refund to do by hand, an
// amount that does not match).
func notifyManagers(ctx context.Context, cafeID int64, kind string, orderID int64) {
	staff, err := staffOf(ctx, cafeID, true)
	if err != nil {
		return
	}
	code := ""
	if orderID != 0 {
		if o, err := orderByID(ctx, orderID); err == nil {
			code = o.PickupCode
		}
	}
	for _, s := range staff {
		var text string
		switch kind {
		case "session_broken":
			text = pick(s.lang, "<b>Kaspi байланысы үзілді.</b> Kaspi арқылы төлем уақытша өшірілді. Әкімші кассир нөмірімен қайта кіруі керек; оған дейін кассада төлеу жұмыс істейді.",
				"<b>Связь с Kaspi прервалась.</b> Оплата через Kaspi временно отключена. Администратор должен заново войти номером кассира; до этого работает оплата на кассе.")
		case "refund_manual":
			text = pick(s.lang, fmt.Sprintf("<b>Ақшаны қолмен қайтару керек:</b> тапсырыс №%s болдырылмады, ал Kaspi қайтаруды автоматты жасамады. Kaspi Pay қосымшасында қайтарыңыз да, тақтада «Қайтарылды» деп белгілеңіз.", code),
				fmt.Sprintf("<b>Нужен ручной возврат:</b> заказ №%s отменён, а Kaspi не вернул деньги автоматически. Верните в Kaspi Pay и отметьте «Возвращено» на доске.", code))
		case "amount_mismatch":
			text = pick(s.lang, fmt.Sprintf("<b>Kaspi сомасы сәйкес емес:</b> тапсырыс №%s. Төлемді Kaspi Pay-де тексеріңіз.", code),
				fmt.Sprintf("<b>Сумма Kaspi не совпадает:</b> заказ №%s. Проверьте платёж в Kaspi Pay.", code))
		default:
			return
		}
		opts := notify.Opts{Link: "/cafe/staff", Key: fmt.Sprintf("cafe:%s:%d:%d", kind, cafeID, orderID)}
		if kind == "session_broken" {
			// once per breakage, not per order
			opts.Key = fmt.Sprintf("cafe:%s:%d:%s", kind, cafeID, Now().In(almaty).Format("2006-01-02T15"))
		}
		if _, err := Send(ctx, s.id, text, opts); err != nil {
			log.Printf("cafe: notify manager %d (%s): %v", s.id, kind, err)
		}
	}
}

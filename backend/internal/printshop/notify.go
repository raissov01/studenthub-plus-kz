package printshop

import (
	"context"
	"fmt"
	"html"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

// Job updates are transactional (notify.OrderIn with CatPrint: no quiet hours or daily cap, never deferred). The
// student and the staff also see everything in the app, so a message that cannot be sent is only logged. File
// names and contents never go into a message: only the code, the count of files and the price.

// Send is replaced by tests.
var Send = func(ctx context.Context, userID int64, text string, opts notify.Opts) (notify.Result, error) {
	return notify.OrderIn(ctx, userID, notify.CatPrint, text, opts)
}

func pick(lang, kk, ru string) string { return shopkit.Pick(lang, kk, ru) }

func boardButton(lang string) []shopkit.Button {
	return shopkit.LinkButton(pick(lang, "Кезек тақтасы", "Доска очереди"), "/print/staff")
}

type recipient struct {
	id   int64
	lang string
}

func staffOf(ctx context.Context, shopID int64, managersOnly bool) ([]recipient, error) {
	var out []recipient
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select u.id, u.lang from print_staff s join users u on u.id = s.user_id
			where s.shop_id = $1 and u.deleted_at is null and (not $2 or s.role = 'manager') order by u.id`, shopID, managersOnly)
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

func jobByID(ctx context.Context, id int64) (*Job, error) {
	var j *Job
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		j, err = loadJob(ctx, tx, id)
		return err
	})
	return j, err
}

func summary(j *Job, lang string) string {
	files, other := 0, 0
	for _, l := range j.Lines {
		if l.Kind == "file" {
			files++
		} else if l.Kind != "minimum" {
			other++
		}
	}
	s := pick(lang, fmt.Sprintf("%d файл", files), fmt.Sprintf("файлов: %d", files))
	if other > 0 {
		s += pick(lang, fmt.Sprintf(", %d тауар/қызмет", other), fmt.Sprintf(", товаров/услуг: %d", other))
	}
	return s
}

func when(j *Job, lang string) string {
	if j.PickupAt == nil {
		return pick(lang, "мүмкіндігінше тез", "как можно скорее")
	}
	return j.PickupAt.In(shopkit.Almaty).Format("15:04")
}

// notifyStaffNew: a job entered the queue (pay at the counter at once, Kaspi once paid) or waits for a price check.
func notifyStaffNew(ctx context.Context, jobID int64) {
	j, err := jobByID(ctx, jobID)
	if err != nil || j.Status != "new" {
		return
	}
	staff, err := staffOf(ctx, j.ShopID, false)
	if err != nil {
		log.Printf("print: staff of %d: %v", j.ShopID, err)
		return
	}
	for _, s := range staff {
		head := pick(s.lang, "Жаңа тапсырыс", "Новый заказ")
		if j.PriceState == "review" {
			head = pick(s.lang, "Бағаны тексеру керек", "Нужно проверить цену")
		}
		pay := pick(s.lang, "кассада төлейді", "оплата на кассе")
		if j.PaymentMethod == "kaspi" {
			pay = pick(s.lang, "Kaspi", "Kaspi")
			if j.paymentStatus == "paid" {
				pay = pick(s.lang, "Kaspi арқылы төленді", "оплачено через Kaspi")
			}
		}
		text := fmt.Sprintf("<b>%s №%s</b> · %s\n%s · %s · %s: %s",
			head, j.PickupCode, html.EscapeString(pick(s.lang, j.ShopNameKK, j.ShopNameRU)), summary(j, s.lang),
			shopkit.Tenge(j.Total)+", "+pay, pick(s.lang, "Алу", "Выдача"), when(j, s.lang))
		var rows [][]shopkit.Button
		if workable(j) {
			rows = append(rows, []shopkit.Button{{Text: pick(s.lang, "Басып жатырмын", "Печатаю"), CallbackData: fmt.Sprintf("print:p%d", j.ID)}})
		}
		rows = append(rows, boardButton(s.lang))
		opts := notify.Opts{Link: "/print/staff", Key: fmt.Sprintf("print:new:%d:%s", j.ID, j.paymentStatus), ReplyMarkup: shopkit.Markup(rows...)}
		if _, err := Send(ctx, s.id, text, opts); err != nil {
			log.Printf("print: notify staff %d of job %d: %v", s.id, j.ID, err)
		}
	}
}

// notifyStaffPrice: the student answered a new price.
func notifyStaffPrice(ctx context.Context, jobID int64, accepted bool) {
	j, err := jobByID(ctx, jobID)
	if err != nil {
		return
	}
	staff, err := staffOf(ctx, j.ShopID, false)
	if err != nil {
		return
	}
	for _, s := range staff {
		text := pick(s.lang, fmt.Sprintf("<b>№%s:</b> студент жаңа бағаны (%s) қабылдады.", j.PickupCode, shopkit.Tenge(j.Total)),
			fmt.Sprintf("<b>№%s:</b> студент принял новую цену (%s).", j.PickupCode, shopkit.Tenge(j.Total)))
		if !accepted {
			text = pick(s.lang, fmt.Sprintf("<b>№%s:</b> студент жаңа бағадан бас тартты, тапсырыс болдырылмады.", j.PickupCode),
				fmt.Sprintf("<b>№%s:</b> студент отказался от новой цены, заказ отменён.", j.PickupCode))
		}
		opts := notify.Opts{Link: "/print/staff", Key: fmt.Sprintf("print:answer:%d:%v", j.ID, accepted), ReplyMarkup: shopkit.Markup(boardButton(s.lang))}
		if _, err := Send(ctx, s.id, text, opts); err != nil {
			log.Printf("print: notify staff %d of price answer %d: %v", s.id, j.ID, err)
		}
	}
}

// notifyStudent: ready, price_proposed, price_confirmed, declined, refunded, refund_manual.
func notifyStudent(ctx context.Context, jobID int64, kind string) {
	j, err := jobByID(ctx, jobID)
	if err != nil || j.userID == nil {
		return
	}
	var lang string
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select lang from users where id = $1 and deleted_at is null`, *j.userID).Scan(&lang)
	})
	if err != nil {
		return
	}
	shop := html.EscapeString(pick(lang, j.ShopNameKK, j.ShopNameRU))
	place := html.EscapeString(pick(lang, j.PlaceKK, j.PlaceRU))
	var text string
	switch kind {
	case "ready":
		where := shop
		if place != "" {
			where += ", " + place
		}
		text = pick(lang, fmt.Sprintf("<b>Дайын — алып кетіңіз</b> · №%s\n%s. Кодты айтыңыз.", j.PickupCode, where),
			fmt.Sprintf("<b>Готово — заберите</b> · №%s\n%s. Назовите код.", j.PickupCode, where))
		if j.PaymentMethod == "pickup" {
			text += pick(lang, fmt.Sprintf("\nТөлем кассада: %s.", shopkit.Tenge(j.Total)), fmt.Sprintf("\nОплата на кассе: %s.", shopkit.Tenge(j.Total)))
		}
	case "price_proposed":
		if j.ProposedTotal == nil {
			return
		}
		reason := ""
		if j.AdjustReason != nil {
			reason = "\n" + html.EscapeString(*j.AdjustReason)
		}
		text = pick(lang, fmt.Sprintf("<b>Жаңа баға: %s</b> · №%s (бұрын %s)%s\nҚабылдасаңыз, тапсырыс кезекке тұрады.", shopkit.Tenge(*j.ProposedTotal), j.PickupCode, shopkit.Tenge(j.Total), reason),
			fmt.Sprintf("<b>Новая цена: %s</b> · №%s (было %s)%s\nПримите её, и заказ встанет в очередь.", shopkit.Tenge(*j.ProposedTotal), j.PickupCode, shopkit.Tenge(j.Total), reason))
	case "price_confirmed":
		text = pick(lang, fmt.Sprintf("<b>Баға расталды: %s</b> · №%s\nЕнді Kaspi арқылы төлей аласыз.", shopkit.Tenge(j.Total), j.PickupCode),
			fmt.Sprintf("<b>Цена подтверждена: %s</b> · №%s\nТеперь можно оплатить через Kaspi.", shopkit.Tenge(j.Total), j.PickupCode))
	case "declined":
		text = pick(lang, fmt.Sprintf("<b>Полиграфия тапсырысты қабылдамады</b> · №%s\n%s.", j.PickupCode, shop),
			fmt.Sprintf("<b>Полиграфия отклонила заказ</b> · №%s\n%s.", j.PickupCode, shop))
		if j.CancelReason != nil && *j.CancelReason != "" {
			text += "\n" + html.EscapeString(*j.CancelReason)
		}
	case "refunded":
		text = pick(lang, fmt.Sprintf("Тапсырыс №%s ақшасы Kaspi-ге қайтарылды: %s.", j.PickupCode, shopkit.Tenge(j.Total)),
			fmt.Sprintf("Деньги за заказ №%s возвращены в Kaspi: %s.", j.PickupCode, shopkit.Tenge(j.Total)))
	case "refund_manual":
		text = pick(lang, fmt.Sprintf("Тапсырыс №%s ақшасын (%s) полиграфия өзі қайтарады. Сұрақ болса — %s.", j.PickupCode, shopkit.Tenge(j.Total), shop),
			fmt.Sprintf("Деньги за заказ №%s (%s) вернёт сама полиграфия. Вопросы — %s.", j.PickupCode, shopkit.Tenge(j.Total), shop))
	default:
		return
	}
	opts := notify.Opts{Link: fmt.Sprintf("/print/jobs?id=%d", j.ID), Key: fmt.Sprintf("print:%s:%d", kind, j.ID)}
	if kind == "price_proposed" && j.ProposedTotal != nil {
		opts.Key = fmt.Sprintf("print:%s:%d:%d", kind, j.ID, *j.ProposedTotal)
	}
	if _, err := Send(ctx, *j.userID, text, opts); err != nil {
		log.Printf("print: notify student of job %d (%s): %v", j.ID, kind, err)
	}
}

// notifyManagers: the Kaspi session stopped, a refund to do by hand, an amount that does not match.
func notifyManagers(ctx context.Context, shopID int64, kind string, jobID int64) {
	staff, err := staffOf(ctx, shopID, true)
	if err != nil {
		return
	}
	code := ""
	if jobID != 0 {
		if j, err := jobByID(ctx, jobID); err == nil {
			code = j.PickupCode
		}
	}
	for _, s := range staff {
		var text string
		switch kind {
		case "session_broken":
			text = pick(s.lang, "<b>Kaspi байланысы үзілді.</b> Kaspi арқылы төлем уақытша өшірілді. Әкімші кассир нөмірімен қайта кіруі керек; оған дейін кассада төлеу жұмыс істейді.",
				"<b>Связь с Kaspi прервалась.</b> Оплата через Kaspi временно отключена. Администратор должен заново войти номером кассира; до этого работает оплата на кассе.")
		case "refund_manual":
			text = pick(s.lang, fmt.Sprintf("<b>Ақшаны қолмен қайтару керек:</b> тапсырыс №%s болдырылмады, ал Kaspi қайтаруды автоматты жасамады. Kaspi Pay-де қайтарыңыз да, тақтада «Қайтарылды» деп белгілеңіз.", code),
				fmt.Sprintf("<b>Нужен ручной возврат:</b> заказ №%s отменён, а Kaspi не вернул деньги автоматически. Верните в Kaspi Pay и отметьте «Возвращено» на доске.", code))
		case "amount_mismatch":
			text = pick(s.lang, fmt.Sprintf("<b>Kaspi сомасы сәйкес емес:</b> тапсырыс №%s. Төлемді Kaspi Pay-де тексеріңіз.", code),
				fmt.Sprintf("<b>Сумма Kaspi не совпадает:</b> заказ №%s. Проверьте платёж в Kaspi Pay.", code))
		default:
			return
		}
		opts := notify.Opts{Link: "/print/staff", Key: fmt.Sprintf("print:%s:%d:%d", kind, shopID, jobID)}
		if kind == "session_broken" {
			opts.Key = fmt.Sprintf("print:%s:%d:%s", kind, shopID, Now().In(shopkit.Almaty).Format("2006-01-02T15"))
		}
		if _, err := Send(ctx, s.id, text, opts); err != nil {
			log.Printf("print: notify manager %d (%s): %v", s.id, kind, err)
		}
	}
}

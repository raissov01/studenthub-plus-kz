package mentors

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// person is what a message needs to know about a user.
type person struct {
	ID         int64
	Name       string
	Username   string
	University string
	Faculty    string
	Lang       string
	IsAdult    bool
}

func loadPerson(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (person, error) {
	p := person{ID: id}
	var name, username, uni, fac *string
	var adult *bool
	err := q.QueryRow(ctx, `select name, tg_username, university, faculty, lang, is_adult from users where id = $1`, id).
		Scan(&name, &username, &uni, &fac, &p.Lang, &adult)
	p.Name, p.Username, p.University, p.Faculty = deref(name), deref(username), deref(uni), deref(fac)
	p.IsAdult = adult != nil && *adult
	if p.Name == "" {
		p.Name = "Student Hub"
	}
	return p, err
}

func tr(lang, kk, ru string) string {
	if lang == "ru" {
		return ru
	}
	return kk
}

var needNames = map[string][2]string{
	"adaptation": {"бейімделу (жатақхана, құжаттар, университет қалай жұмыс істейді)", "адаптация (общежитие, документы, как устроен университет)"},
	"study":      {"оқу", "учёба"},
	"career":     {"мансап", "карьера"},
	"life":       {"тұрмыс", "быт"},
}

func needName(lang, need string) string {
	n, ok := needNames[need]
	if !ok {
		return need
	}
	return tr(lang, n[0], n[1])
}

// Etiquette rules go to both sides in the first message (05-КЕЗЕҢ).
func etiquette(lang string) string {
	return tr(lang,
		"<b>Әдеп ережелері</b>\n• Бір-біріңізді құрметтеңіз.\n• Ақша, қарыз немесе жеке қаржылық дерек (карта, ЖСН, құпиясөз) сұрамаңыз және бермеңіз.\n• Кездесулер тек университет аумағында, адам көп жерде немесе онлайн өтеді — ересектер арасында да.\n• Бірдеңе ыңғайсыз болса — сайттағы «Шағымдану» батырмасы, жұпты кез келген уақытта жабуға болады.",
		"<b>Правила</b>\n• Уважайте друг друга.\n• Не просите и не передавайте деньги, займы или личные финансовые данные (карта, ИИН, пароли).\n• Встречи — только на территории университета, в людном месте или онлайн, в том числе между взрослыми.\n• Если что-то не так — кнопка «Пожаловаться» на сайте, пару можно закрыть в любой момент.")
}

func mention(p person) string {
	if p.Username == "" {
		return ""
	}
	return "@" + html.EscapeString(p.Username)
}

// introText is the match message for `to` about `other`. Usernames are exchanged only here,
// after a match, and pairs exist only between adults (mentor_match refuses minors).
func introText(to, other person, need string, toIsMentor bool) string {
	l := to.Lang
	var b strings.Builder
	if toIsMentor {
		b.WriteString(tr(l, "🎓 Сізге менти табылды!\n\n", "🎓 У вас новый менти!\n\n"))
	} else {
		b.WriteString(tr(l, "🎓 Сізге ментор табылды!\n\n", "🎓 Для вас нашёлся ментор!\n\n"))
	}
	fmt.Fprintf(&b, "%s: <b>%s</b>\n", tr(l, "Аты", "Имя"), html.EscapeString(other.Name))
	if other.University != "" {
		uni := other.University
		if other.Faculty != "" {
			uni += ", " + other.Faculty
		}
		fmt.Fprintf(&b, "%s: %s\n", tr(l, "Университет", "Университет"), html.EscapeString(uni))
	}
	fmt.Fprintf(&b, "%s: %s\n", tr(l, "Бағыты", "Направление"), needName(l, need))
	switch {
	case other.Username != "":
		fmt.Fprintf(&b, "Telegram: %s\n", mention(other))
	case to.Username != "":
		b.WriteString(tr(l, "Серіктесіңізде Telegram username жоқ — біз оған сіздің username-іңізді жібердік, ол өзі жазады.\n",
			"У собеседника нет username в Telegram — мы отправили ему ваш username, он напишет сам.\n"))
	default:
		b.WriteString(tr(l, "Екеуіңізде де Telegram username жоқ. Telegram баптауларында username орнатыңыз да, ботқа жазыңыз — таныстырамыз.\n",
			"Ни у кого из вас нет username в Telegram. Установите username в настройках Telegram и напишите боту — мы вас познакомим.\n"))
	}
	b.WriteString(tr(l, "\n<b>Алғашқы қадам:</b> 15 минуттық танысу туралы келісіңдер.\n\n",
		"\n<b>Первый шаг:</b> договоритесь о 15-минутном знакомстве.\n\n"))
	b.WriteString(etiquette(l))
	return b.String()
}

func site(path string) string { return config.SiteURL() + path }

func check3Text(lang string) string {
	return tr(lang,
		"🎓 Жұптарыңызға 3 апта болды. Кездестіңдер ме? Пайдасы бар ма (1–5)? Жауап беру: "+site("/#/mentors/me"),
		"🎓 Вашей паре 3 недели. Встретились? Есть ли польза (1–5)? Ответить: "+site("/#/mentors/me"))
}

func monthText(lang string) string {
	return tr(lang,
		"🎓 Алғашқы ай аяқталды. Жалғастырамыз ба? Екеуіңіз де «иә» десеңіз ғана жұп жалғасады: "+site("/#/mentors/me"),
		"🎓 Первый месяц позади. Продолжаем? Пара продолжится, только если вы оба ответите «да»: "+site("/#/mentors/me"))
}

func endedText(lang string) string {
	return tr(lang,
		"🎓 Менторлық жұбыңыз жабылды. Рақмет! Қажет болса, жаңа сұраныс қалдыра аласыз: "+site("/#/mentors"),
		"🎓 Ваша менторская пара закрыта. Спасибо! При необходимости можно оставить новую заявку: "+site("/#/mentors"))
}

// direct sends an immediate message and only logs failures: a match or decision must not be
// rolled back because Telegram is unreachable.
func direct(ctx context.Context, userID int64, text string) {
	if err := notify.Direct(ctx, userID, text); err != nil {
		log.Printf("mentors: notify %d: %v", userID, err)
	}
}

// notifyAdmins tells every admin about a new report (06: admins hear about the first report).
func notifyAdmins(ctx context.Context, text string) {
	p, err := db.Pool(ctx)
	if err != nil {
		return
	}
	rows, err := p.Query(ctx, `select id from users where role = 'admin' and deleted_at is null`)
	if err != nil {
		log.Printf("mentors: admins: %v", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return
	}
	for _, id := range ids {
		direct(ctx, id, text)
	}
}

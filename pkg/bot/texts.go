package bot

// Every user-facing bot text, Kazakh and Russian. HTML parse mode: callers escape titles.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

var months = map[string][]string{
	"kk": {"қаңтар", "ақпан", "наурыз", "сәуір", "мамыр", "маусым", "шілде", "тамыз", "қыркүйек", "қазан", "қараша", "желтоқсан"},
	"ru": {"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"},
}

// fmtDate: '2026-12-07' -> '7 желтоқсан' / '7 декабря'; the year is added only when it is not today's.
func fmtDate(iso, lang, today string) string {
	t, ok := parseISO(iso)
	if !ok {
		return iso
	}
	base := fmt.Sprintf("%d %s", t.Day(), months[lang][t.Month()-1])
	if today != "" && strconv.Itoa(t.Year()) != today[:4] {
		return fmt.Sprintf("%s %d", base, t.Year())
	}
	return base
}

func ruDays(n int) string {
	a, b := n%10, n%100
	if a == 1 && b != 11 {
		return "день"
	}
	if a >= 2 && a <= 4 && (b < 12 || b > 14) {
		return "дня"
	}
	return "дней"
}

func joinWith(list []string, and string) string {
	if len(list) < 2 {
		return strings.Join(list, "")
	}
	return strings.Join(list[:len(list)-1], ", ") + " " + and + " " + list[len(list)-1]
}

func intsToStrings(days []int) []string {
	out := make([]string, len(days))
	for i, d := range days {
		out[i] = strconv.Itoa(d)
	}
	return out
}

type option struct{ key, kk, ru string }

func (o option) label(lang string) string {
	if lang == "ru" {
		return o.ru
	}
	return o.kk
}

var cities = []option{
	{"astana", "Астана", "Астана"}, {"almaty", "Алматы", "Алматы"},
	{"shymkent", "Шымкент", "Шымкент"}, {"karaganda", "Қарағанды", "Караганда"},
	{"aktobe", "Ақтөбе", "Актобе"}, {"taraz", "Тараз", "Тараз"},
	{"turkistan", "Түркістан", "Туркестан"}, {"pavlodar", "Павлодар", "Павлодар"},
	{"oskemen", "Өскемен", "Усть-Каменогорск"}, {"semey", "Семей", "Семей"},
	{"kostanay", "Қостанай", "Костанай"}, {"atyrau", "Атырау", "Атырау"},
	{"aktau", "Ақтау", "Актау"}, {"oral", "Орал", "Уральск"},
	{"kyzylorda", "Қызылорда", "Кызылорда"}, {"petropavl", "Петропавл", "Петропавловск"},
	{"taldykorgan", "Талдықорған", "Талдыкорган"}, {"kokshetau", "Көкшетау", "Кокшетау"},
	{"other", "Басқа", "Другой"},
}

var dirs = []option{
	{"econ", "Экономика", "Экономика"}, {"acc", "Есеп және аудит", "Учёт и аудит"},
	{"it", "Ақпараттық жүйелер", "Информационные системы"}, {"agro", "Агрономия", "Агрономия"},
	{"vet", "Ветеринария", "Ветеринария"}, {"eng", "Құрылыс және инженерия", "Строительство и инженерия"},
	{"energy", "Энергетика", "Энергетика"}, {"other", "Басқа мамандық", "Другая специальность"},
}

func hasOption(list []option, key string) bool {
	for _, o := range list {
		if o.key == key {
			return true
		}
	}
	return false
}

type statsData struct{ Users, Active, Reminders, Submitted int64 }

type texts struct {
	NoMoney, Greeting, LangSet                        string
	BtnSite, BtnOpen, BtnDone, BtnSkip, BtnDelete     string
	BtnRemind                                         string
	Confirm                                           func(title, date string, days []int) string
	Already                                           func(title, date string) string
	NotFound                                          string
	Expired                                           func(title, date string) string
	NoDeadline                                        func(title string) string
	TooLate                                           func(title, date string) string
	TooMany, LoadFail, ReminderHead, ReminderHeadMany string
	DaysLeft                                          func(n int) string
	Docs, ReminderFoot                                string
	MoreInMy                                          func(n int) string
	DoneToast, SkipToast, Gone, MyEmpty, MyHead       string
	MyPaused                                          func(date string) string
	Deleted                                           string
	Paused                                            func(date string) string
	Resumed, NotPaused, StopAsk, StopYes, StopNo      string
	StopDone, StopCancel, Help                        string
	Stats                                             func(s statsData) string
	AskCity, AskDir, DigestSaved, DigestHead          string
	DigestDeadline                                    func(date string) string
	DigestFoot                                        string
	RemindSaved                                       func(title, date string) string
	// /settings (F8): category switches and quiet hours
	SettingsHead  func(quietOn bool, start, end string, dailyCap int) string
	CatLabel      map[notify.Category]string
	BtnQuiet      func(on bool, start, end string) string
	SettingsSaved string
}

var allTexts = map[string]*texts{
	"kk": {
		NoMoney:   "Бот ешқашан ақша сұрамайды. Біреу Student Hub атынан ақша сұраса — бізге хабарла.",
		Greeting:  "Сәлем! Мен Student Hub ботымын.\n\nМенің бір ғана жұмысым бар: сайтта сақтаған мүмкіндігіңнің мерзімін өткізіп алмауыңа көмектесу. Сайтта мүмкіндікті ашып, «Еске салу» батырмасын бас — қалғанын өзім істеймін.\n\nТілді таңда:",
		LangSet:   "Жақсы, енді қазақша жазамын.",
		BtnSite:   "Сайтты ашу",
		BtnOpen:   "Ашу",
		BtnDone:   "Тапсырдым ✅",
		BtnSkip:   "Қажет емес",
		BtnDelete: "Өшіру",
		BtnRemind: "Еске салу",
		Confirm: func(title, date string, days []int) string {
			if len(days) == 0 {
				return fmt.Sprintf("«%s» мерзімі — %s.", title, date)
			}
			return fmt.Sprintf("«%s» мерзімі — %s. %s күн қалғанда еске саламын ✅", title, date, joinWith(intsToStrings(days), "және"))
		},
		Already: func(title, date string) string {
			return fmt.Sprintf("«%s» бұрыннан тізіміңде бар, мерзімі — %s. Уақытында еске саламын.", title, date)
		},
		NotFound: "Кешір, бұл мүмкіндікті таба алмадым — мүмкін, ол сайттан алынып тасталған. Сайттан басқа ұсыныстарды қарап көр.",
		Expired: func(title, date string) string {
			return fmt.Sprintf("«%s» қабылдауы %s күні аяқталып қойыпты. Келесі жолы міндетті түрде үлгересің — сайтта жаңа мүмкіндіктер көп.", title, date)
		},
		NoDeadline: func(title string) string {
			return fmt.Sprintf("«%s» үшін нақты мерзім жоқ, сондықтан еске салатын күн жоқ. Толығырақ сайттан қара.", title)
		},
		TooLate: func(title, date string) string {
			return fmt.Sprintf("«%s» мерзімі — %s, яғни өте жақын. Еске салуға уақыт қалмады — мүмкіндігің болса, бүгін-ақ тапсыр.", title, date)
		},
		TooMany:          "Тізімде мүмкіндік тым көп. /my арқылы керегі жоқтарын өшір де, қайталап көр.",
		LoadFail:         "Сайттан мәлімет алу мүмкін болмады. Сәлден соң қайталап көр.",
		ReminderHead:     "Сәлем! Сақтаған мүмкіндігіңнің мерзімі жақындап қалды:",
		ReminderHeadMany: "Сәлем! Сақтаған мүмкіндіктеріңнің мерзімі жақындап қалды:",
		DaysLeft: func(n int) string {
			if n == 1 {
				return "Ертең — соңғы күн"
			}
			return fmt.Sprintf("%d күн қалды", n)
		},
		Docs:         "Дайындайтын құжаттар",
		ReminderFoot: "Үлгересің! Тапсырып қойсаң, «Тапсырдым ✅» бас — бұдан әрі мазаламаймын.",
		MoreInMy:     func(n int) string { return fmt.Sprintf("Тағы %d мүмкіндік — /my", n) },
		DoneToast:    "Керемет! Сәттілік тілеймін. Бұл туралы енді еске салмаймын.",
		SkipToast:    "Түсіндім, бұл туралы енді еске салмаймын.",
		Gone:         "Бұл еске салу бұрын тоқтатылған.",
		MyEmpty:      "Сақталған еске салулар жоқ. Сайтта мүмкіндікті ашып, «Еске салу» батырмасын бас.",
		MyHead:       "Сенің еске салуларың:",
		MyPaused: func(date string) string {
			return fmt.Sprintf("Еске салулар %s дейін кідіртілген. Қайта қосу: /resume", date)
		},
		Deleted: "Өшірілді.",
		Paused: func(date string) string {
			return fmt.Sprintf("Жарайды, 7 күнге кідірттім. %s бастап қайта еске саламын. Ертерек қайтару: /resume", date)
		},
		Resumed:    "Еске салулар қайта қосылды.",
		NotPaused:  "Еске салулар қазір қосулы тұр.",
		StopAsk:    "Барлық деректеріңді (тіл, сақталған еске салулар, баптаулар) біржола өшірейін бе? Бұл әрекетті қайтару мүмкін емес.",
		StopYes:    "Иә, бәрін өшір",
		StopNo:     "Жоқ, қалсын",
		StopDone:   "Барлық деректерің өшірілді. Қайта керек болсаң, /start жаз.",
		StopCancel: "Жарайды, ештеңе өшірілген жоқ.",
		Help:       "Мен сайтта сақтаған мүмкіндіктеріңнің мерзімін еске саламын: 7, 3 және 1 күн қалғанда, күніне ең көбі бір хабарлама.\n\n/my — сақталған тізім\n/pause — 7 күнге кідірту\n/resume — қайта қосу\n/settings — хабарлама баптаулары\n/stop — барлық деректерді өшіру\n/help — анықтама",
		Stats: func(s statsData) string {
			return fmt.Sprintf("Студенттер: %d (белсенді: %d)\nБелсенді еске салулар: %d\nСоңғы 7 күнде «Тапсырдым»: %d", s.Users, s.Active, s.Reminders, s.Submitted)
		},
		AskCity:        "Аптасына бір рет саған сай жаңа мүмкіндіктерді жіберіп тұрайын. Қай қаладасың?",
		AskDir:         "Бағытың қандай?",
		DigestSaved:    "Рақмет! Бейсенбі сайын 2–3 жаңа мүмкіндік жіберемін.",
		DigestHead:     "Осы аптаның саған сай жаңа мүмкіндіктері:",
		DigestDeadline: func(date string) string { return "мерзімі — " + date },
		DigestFoot:     "Ұнағанын «Еске салу» арқылы сақта — мерзімін өзім қадағалаймын.",
		RemindSaved: func(title, date string) string {
			return fmt.Sprintf("«%s» сақталды, мерзімі — %s.", title, date)
		},
		SettingsHead: func(quietOn bool, start, end string, dailyCap int) string {
			quiet := "өшірулі"
			if quietOn {
				quiet = start + "–" + end + " (Астана уақыты)"
			}
			return fmt.Sprintf("<b>Хабарлама баптаулары</b>\n\nҚайсысын алғың келетінін таңда — батырманы басу қосады не өшіреді.\n\nТыныш уақыт: %s. Бұл уақытта хабарлама жібермеймін, кейінге қалдырамын.\nКүніне ең көбі: %d тікелей хабарлама және 1 дайджест.\nМодерацияны да өшіруге болады; сайттағы жәшікке бәрі түседі.", quiet, dailyCap)
		},
		CatLabel: map[notify.Category]string{
			notify.CatDeadlines: "Мерзім еске салулары", notify.CatDigest: "Апталық топтама",
			notify.CatEvents: "Іс-шаралар", notify.CatMentors: "Менторлар",
			notify.CatMarket: "Маркет", notify.CatJobs: "Жұмыс ұсыныстары",
			notify.CatCommunity: "Қауымдастық және бот жауаптары", notify.CatModeration: "Модерация",
		},
		BtnQuiet: func(on bool, start, end string) string {
			if on {
				return "Тыныш уақыт: " + start + "–" + end
			}
			return "Тыныш уақыт: өшірулі"
		},
		SettingsSaved: "Сақталды.",
	},
	"ru": {
		NoMoney:   "Бот никогда не просит денег. Если кто-то просит деньги от имени Student Hub — сообщите нам.",
		Greeting:  "Здравствуйте! Я бот Student Hub.\n\nУ меня одна задача: чтобы вы не пропустили дедлайн возможности, сохранённой на сайте. Откройте возможность на сайте и нажмите «Напомнить» — остальное я сделаю сам.\n\nВыберите язык:",
		LangSet:   "Хорошо, теперь пишу по-русски.",
		BtnSite:   "Открыть сайт",
		BtnOpen:   "Открыть",
		BtnDone:   "Подал(а) ✅",
		BtnSkip:   "Не нужно",
		BtnDelete: "Удалить",
		BtnRemind: "Напомнить",
		Confirm: func(title, date string, days []int) string {
			if len(days) == 0 {
				return fmt.Sprintf("Дедлайн «%s» — %s.", title, date)
			}
			return fmt.Sprintf("Дедлайн «%s» — %s. Напомню за %s %s ✅", title, date, joinWith(intsToStrings(days), "и"), ruDays(days[len(days)-1]))
		},
		Already: func(title, date string) string {
			return fmt.Sprintf("«%s» уже в вашем списке, дедлайн — %s. Напомню вовремя.", title, date)
		},
		NotFound: "Извините, не нашёл эту возможность — возможно, её убрали с сайта. Посмотрите другие предложения на сайте.",
		Expired: func(title, date string) string {
			return fmt.Sprintf("Приём на «%s» закончился %s. В следующий раз обязательно успеете — на сайте много новых возможностей.", title, date)
		},
		NoDeadline: func(title string) string {
			return fmt.Sprintf("У «%s» нет точного дедлайна, поэтому напоминать не о чем. Подробности — на сайте.", title)
		},
		TooLate: func(title, date string) string {
			return fmt.Sprintf("Дедлайн «%s» — %s, это совсем скоро. На напоминания времени не осталось — если можете, подайте уже сегодня.", title, date)
		},
		TooMany:          "В списке слишком много возможностей. Удалите ненужные через /my и попробуйте снова.",
		LoadFail:         "Не удалось получить данные с сайта. Попробуйте чуть позже.",
		ReminderHead:     "Здравствуйте! Дедлайн сохранённой возможности приближается:",
		ReminderHeadMany: "Здравствуйте! Дедлайны сохранённых возможностей приближаются:",
		DaysLeft: func(n int) string {
			if n == 1 {
				return "Завтра — последний день"
			}
			return fmt.Sprintf("Осталось %d %s", n, ruDays(n))
		},
		Docs:         "Подготовьте документы",
		ReminderFoot: "Вы успеете! Если уже подали, нажмите «Подал(а) ✅» — больше не побеспокою.",
		MoreInMy:     func(n int) string { return fmt.Sprintf("Ещё %d — /my", n) },
		DoneToast:    "Отлично! Удачи. Больше не буду напоминать об этом.",
		SkipToast:    "Понял, больше не буду напоминать об этом.",
		Gone:         "Это напоминание уже остановлено.",
		MyEmpty:      "Сохранённых напоминаний нет. Откройте возможность на сайте и нажмите «Напомнить».",
		MyHead:       "Ваши напоминания:",
		MyPaused: func(date string) string {
			return fmt.Sprintf("Напоминания на паузе до %s. Включить снова: /resume", date)
		},
		Deleted: "Удалено.",
		Paused: func(date string) string {
			return fmt.Sprintf("Хорошо, пауза на 7 дней. Снова начну напоминать с %s. Включить раньше: /resume", date)
		},
		Resumed:    "Напоминания снова включены.",
		NotPaused:  "Напоминания сейчас включены.",
		StopAsk:    "Удалить все ваши данные (язык, сохранённые напоминания, настройки) навсегда? Это действие нельзя отменить.",
		StopYes:    "Да, удалить всё",
		StopNo:     "Нет, оставить",
		StopDone:   "Все ваши данные удалены. Если понадоблюсь — напишите /start.",
		StopCancel: "Хорошо, ничего не удалено.",
		Help:       "Я напоминаю о дедлайнах возможностей, сохранённых на сайте: за 7, 3 и 1 день, не больше одного сообщения в день.\n\n/my — сохранённый список\n/pause — пауза на 7 дней\n/resume — включить снова\n/settings — настройки уведомлений\n/stop — удалить все данные\n/help — справка",
		Stats: func(s statsData) string {
			return fmt.Sprintf("Студенты: %d (активные: %d)\nАктивные напоминания: %d\n«Подал(а)» за 7 дней: %d", s.Users, s.Active, s.Reminders, s.Submitted)
		},
		AskCity:        "Раз в неделю могу присылать подходящие вам новые возможности. В каком вы городе?",
		AskDir:         "Ваше направление?",
		DigestSaved:    "Спасибо! По четвергам буду присылать 2–3 новые возможности.",
		DigestHead:     "Новые возможности этой недели для вас:",
		DigestDeadline: func(date string) string { return "дедлайн — " + date },
		DigestFoot:     "Понравившееся сохраните кнопкой «Напомнить» — за дедлайном прослежу сам.",
		RemindSaved: func(title, date string) string {
			return fmt.Sprintf("«%s» сохранено, дедлайн — %s.", title, date)
		},
		SettingsHead: func(quietOn bool, start, end string, dailyCap int) string {
			quiet := "выключены"
			if quietOn {
				quiet = start + "–" + end + " (время Астаны)"
			}
			return fmt.Sprintf("<b>Настройки уведомлений</b>\n\nВыберите, что хотите получать — нажатие на кнопку включает или выключает.\n\nТихие часы: %s. В это время сообщения не приходят, я отложу их.\nНе больше %d прямых сообщений и 1 дайджеста в день.\nМодерацию тоже можно отключить; всё останется во входящих на сайте.", quiet, dailyCap)
		},
		CatLabel: map[notify.Category]string{
			notify.CatDeadlines: "Напоминания о дедлайнах", notify.CatDigest: "Подборка недели",
			notify.CatEvents: "Мероприятия", notify.CatMentors: "Менторы",
			notify.CatMarket: "Маркет", notify.CatJobs: "Вакансии",
			notify.CatCommunity: "Сообщество и ответы бота", notify.CatModeration: "Модерация",
		},
		BtnQuiet: func(on bool, start, end string) string {
			if on {
				return "Тихие часы: " + start + "–" + end
			}
			return "Тихие часы: выкл"
		},
		SettingsSaved: "Сохранено.",
	},
}

func txt(lang string) *texts {
	if t, ok := allTexts[lang]; ok {
		return t
	}
	return allTexts["kk"]
}

var langButtons = [][]Button{{{Text: "Қазақша", CallbackData: "lang:kk"}, {Text: "Русский", CallbackData: "lang:ru"}}}

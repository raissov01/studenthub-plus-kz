package mail

import (
	"html"
	"strings"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
)

// Kinds of code email. The code itself is only ever in the body, never in the subject
// (subjects show up in notification previews and mail logs).
const (
	VerifyEmail   = "verify_email"
	ResetPassword = "reset_password"
	ChangeEmail   = "change_email"
)

type text struct{ subject, lead, ignore string }

var codeTexts = map[string]map[string]text{
	VerifyEmail: {
		"kk": {"Student Hub: email-ді растаңыз", "Student Hub-қа тіркелуді аяқтау үшін осы кодты енгізіңіз:", "Егер сіз тіркелмеген болсаңыз, бұл хатты елемеңіз."},
		"ru": {"Student Hub: подтвердите email", "Чтобы завершить регистрацию в Student Hub, введите этот код:", "Если вы не регистрировались, просто проигнорируйте это письмо."},
	},
	ResetPassword: {
		"kk": {"Student Hub: құпиясөзді қалпына келтіру", "Жаңа құпиясөз орнату үшін осы кодты енгізіңіз:", "Егер сіз сұрамаған болсаңыз, бұл хатты елемеңіз — құпиясөзіңіз өзгермейді."},
		"ru": {"Student Hub: восстановление пароля", "Чтобы задать новый пароль, введите этот код:", "Если вы не запрашивали сброс, проигнорируйте письмо — пароль не изменится."},
	},
	ChangeEmail: {
		"kk": {"Student Hub: email-ді аккаунтқа қосу", "Бұл адресті Student Hub аккаунтыңызға қосу үшін осы кодты енгізіңіз:", "Егер сіз сұрамаған болсаңыз, бұл хатты елемеңіз."},
		"ru": {"Student Hub: привязка email к аккаунту", "Чтобы привязать этот адрес к аккаунту Student Hub, введите код:", "Если вы этого не запрашивали, проигнорируйте письмо."},
	},
}

var expiresIn = map[string]string{"kk": "Код 15 минут жарамды. Оны ешкімге айтпаңыз.", "ru": "Код действует 15 минут. Никому его не сообщайте."}

// Code builds the email that carries a 6-digit code.
func Code(lang, kind, to, code string) Message {
	lang = pick(lang)
	t := codeTexts[kind][lang]
	plain := t.lead + "\n\n" + code + "\n\n" + expiresIn[lang] + "\n" + t.ignore + "\n\n— Student Hub\n" + config.SiteURL()
	body := `<p style="margin:0 0 16px">` + html.EscapeString(t.lead) + `</p>
<p style="margin:0 0 16px;font:700 32px/1.2 ui-monospace,Menlo,Consolas,monospace;letter-spacing:6px;padding:16px;border:1px solid #d9d9e3;border-radius:8px;text-align:center">` + html.EscapeString(code) + `</p>
<p style="margin:0 0 8px;color:#555">` + html.EscapeString(expiresIn[lang]) + `</p>
<p style="margin:0;color:#555">` + html.EscapeString(t.ignore) + `</p>`
	return Message{To: to, Subject: t.subject, Text: plain, HTML: wrap(lang, body), Tag: kind}
}

// Notices: emails without a code that keep the API from telling whether an address is
// registered (the requester sees the same "check your inbox" answer either way).
const (
	AccountExists   = "account_exists"   // someone tried to sign up with an address that has an account
	EmailInUse      = "email_in_use"     // someone tried to attach this address to another account
	PasswordChanged = "password_changed" // after a reset or a change
)

var noticeTexts = map[string]map[string]text{
	AccountExists: {
		"kk": {"Student Hub: сізде аккаунт бар", "Осы адреспен Student Hub-та тіркелу сұралды, бірақ бұл email-де аккаунт әлдеқашан бар. Кіру бетінде email мен құпиясөзді енгізіңіз немесе «Құпиясөзді ұмыттым» батырмасын басыңыз.", "Егер бұл сіз болмасаңыз, хатты елемеңіз — аккаунтыңызға ешнәрсе өзгерген жоқ."},
		"ru": {"Student Hub: у вас уже есть аккаунт", "Кто-то попытался зарегистрироваться в Student Hub с этим адресом, но аккаунт с этим email уже есть. Войдите с email и паролем или нажмите «Забыли пароль?».", "Если это были не вы, проигнорируйте письмо — в аккаунте ничего не изменилось."},
	},
	EmailInUse: {
		"kk": {"Student Hub: email басқа аккаунтқа қосылмады", "Бұл адресті басқа Student Hub аккаунтына қосу сұралды. Адрес сіздің аккаунтыңызда тұрғандықтан, ол қосылмады.", "Егер бұл сіз болсаңыз, алдымен осы адреспен кіріп, аккаунттардың бірін өшіріңіз. Болмаса, хатты елемеңіз."},
		"ru": {"Student Hub: email не привязан к другому аккаунту", "Кто-то попросил привязать этот адрес к другому аккаунту Student Hub. Адрес уже используется вашим аккаунтом, поэтому привязки не было.", "Если это были вы, войдите с этим адресом и удалите один из аккаунтов. Иначе просто проигнорируйте письмо."},
	},
	PasswordChanged: {
		"kk": {"Student Hub: құпиясөз өзгерді", "Student Hub аккаунтыңыздың құпиясөзі өзгертілді. Басқа құрылғылардағы сессиялар жабылды.", "Егер бұл сіз болмасаңыз, бірден «Құпиясөзді ұмыттым» арқылы жаңа құпиясөз орнатыңыз."},
		"ru": {"Student Hub: пароль изменён", "Пароль вашего аккаунта Student Hub изменён. Сессии на других устройствах завершены.", "Если это были не вы, сразу задайте новый пароль через «Забыли пароль?»."},
	},
}

// Notice builds a code-less notice email.
func Notice(lang, kind, to string) Message {
	lang = pick(lang)
	t := noticeTexts[kind][lang]
	plain := t.lead + "\n\n" + t.ignore + "\n\n— Student Hub\n" + config.SiteURL()
	body := `<p style="margin:0 0 16px">` + html.EscapeString(t.lead) + `</p>
<p style="margin:0;color:#555">` + html.EscapeString(t.ignore) + `</p>`
	return Message{To: to, Subject: t.subject, Text: plain, HTML: wrap(lang, body), Tag: kind}
}

func pick(lang string) string {
	if strings.ToLower(lang) == "ru" {
		return "ru"
	}
	return "kk"
}

func wrap(lang, body string) string {
	site := html.EscapeString(config.SiteURL())
	return `<!doctype html><html lang="` + lang + `"><head><meta charset="utf-8"></head>
<body style="margin:0;padding:24px;background:#ffffff;color:#1a1a2e;font:15px/1.5 -apple-system,'Segoe UI',Roboto,Arial,sans-serif">
<div style="max-width:520px;margin:0 auto">
<p style="margin:0 0 20px;font-weight:700">Student Hub</p>
` + body + `
<p style="margin:24px 0 0;font-size:12px;color:#888"><a href="` + site + `" style="color:#888">` + site + `</a></p>
</div></body></html>`
}

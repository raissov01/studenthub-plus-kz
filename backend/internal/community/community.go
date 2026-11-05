// Package community holds three student-to-student features, each behind its own switch and
// documented in docs/api/community.md:
//
//   - lostfound (FEATURE_LOSTFOUND) «Табылған заттар»: lost / found posts with a private claim;
//     contacts open only after the poster confirms, the return needs both sides' mark.
//   - courseqa (FEATURE_COURSEQA) «Пән бойынша сұрақ-жауап»: questions per subject board,
//     answers, upvotes, an accepted answer, anonymous-to-classmates posting, thread locks.
//   - roommates (FEATURE_ROOMMATES) «Пәтерлес»: listings with a strict «only my gender» option,
//     matching and a mutual-consent contact reveal (the rules of the Bagdar bot's /paterles).
//
// Shared rules: writing needs a signed-in account with consent; free text is length-limited and
// may not carry contact data (phones, @handles, links) where contact must wait for consent;
// complaints go through the shared /api/reports rule (moderation.OnReport) and an auto-hidden
// item lands in the shared admin queue. Contacts are read from users only by the server, only
// for the two parties of a confirmed claim / accepted request.
package community

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cronrun"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/points"
)

// Feature switches.
const (
	FeatureLostFound = "lostfound"
	FeatureCourseQA  = "courseqa"
	FeatureRoommates = "roommates"
)

// Points reasons (internal/points earning table): small, capped, server-verified.
const (
	ReasonLostFoundReturn = "lostfound_return" // the finder, once both sides confirmed the return
	ReasonQAAccepted      = "qa_accepted"      // the author of an accepted answer (not the asker)
)

// Report target kinds (reports.target_kind) and the shared queue kind for auto-hidden items.
const (
	TargetLostFound = "lostfound_post"
	TargetQuestion  = "qa_question"
	TargetAnswer    = "qa_answer"
	TargetRoommate  = "roommate_listing"
	KindHidden      = "community_hidden" // an item hidden by reports, waiting for the admin's decision
)

func init() {
	points.Rules[ReasonLostFoundReturn] = points.Rule{XP: 20, Coins: 5, PerDay: 1, PerWeek: 2}
	points.Rules[ReasonQAAccepted] = points.Rule{XP: 10, Coins: 2, PerDay: 3, PerWeek: 10}
	registerReports()
	registerExports()
	cronrun.OnDaily(Archive)
}

// Routes wires all three features; each answers 404 while its switch is off.
func Routes(mux httpx.Router) {
	lostFoundRoutes(mux)
	qaRoutes(mux)
	roommateRoutes(mux)
}

func handle(mux httpx.Router, feature, pattern string, fn http.HandlerFunc) {
	mux.Handle(pattern, features.RequireFunc(feature, fn))
}

// write is a signed-in, origin-checked, rate-limited mutation.
func write(fn auth.Handler) http.HandlerFunc { return auth.Write(auth.Need(fn)) }

// admin is a moderation mutation: an admin or a moderator (admin_grants).
func admin(fn auth.Handler) http.HandlerFunc {
	return auth.Write(auth.NeedScope(fn, auth.ScopeModerator))
}

// ---------- errors ----------

var msgs = map[string]httpx.Msg{
	"consent_required": {KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."},
	"no_university":    {KK: "Профильде университетіңізді көрсетіңіз.", RU: "Укажите университет в профиле."},
	"no_contact":       {KK: "Байланыс ашылғанда сізге жаза алуы үшін Telegram username не расталған email керек.", RU: "Нужен username в Telegram или подтверждённый email, чтобы с вами могли связаться."},
	"has_contact":      {KK: "Мәтінге телефон, @username не сілтеме жазбаңыз: байланыс екі жақ келіскенде ғана ашылады.", RU: "Не пишите в тексте телефон, @username или ссылку: контакты открываются только по согласию обеих сторон."},
	"has_number":       {KK: "Құжат не карта нөмірін жарияламаңыз (6+ цифр қатар). Иесі оны өзі айтады.", RU: "Не публикуйте номер документа или карты (6+ цифр подряд). Владелец назовёт его сам."},
	"bad_text":         {KK: "Мәтін тым қысқа не тым ұзын.", RU: "Текст слишком короткий или слишком длинный."},
	"daily_limit":      {KK: "Бүгінгі шекке жеттіңіз. Ертең жалғастырыңыз.", RU: "Достигнут дневной лимит. Продолжите завтра."},
	"not_found":        httpx.MsgNotFound,
	"own_item":         {KK: "Өз жазбаңызға бұл әрекет жасалмайды.", RU: "Это действие нельзя сделать со своей записью."},
	"already":          {KK: "Сіз бұны бұрын жібергенсіз.", RU: "Вы уже отправляли это."},
	"closed":           {KK: "Бұл жазба жабылған.", RU: "Эта запись закрыта."},
	"bad_state":        {KK: "Бұл әрекет қазір мүмкін емес.", RU: "Это действие сейчас недоступно."},
	"forbidden":        httpx.MsgForbid,
	// lost & found
	"sensitive_photo": {KK: "Құжат пен картаның фотосын жарияламаңыз.", RU: "Не публикуйте фото документов и карт."},
	"handed_required": {KK: "Табылған құжат не картаны деканатқа не күзетке тапсырып, қайсысы екенін белгілеңіз.", RU: "Найденный документ или карту сдайте в деканат или охрану и отметьте, куда."},
	"bad_photo":       {KK: "Фото табылмады — қайта жүктеңіз.", RU: "Фото не найдено — загрузите заново."},
	"bad_date":        {KK: "Күні соңғы 90 күн ішінде болсын.", RU: "Дата должна быть в пределах последних 90 дней."},
	// Q&A
	"honesty":   {KK: "Академиялық адалдық ережесін растаңыз.", RU: "Подтвердите правило академической честности."},
	"locked":    {KK: "Тақырып модератормен жабылған: жаңа жауап қабылданбайды.", RU: "Тема закрыта модератором: новые ответы не принимаются."},
	"bad_tags":  {KK: "Тегтер: 5-ке дейін, әрқайсысы 2–24 әріп не цифр.", RU: "Теги: до 5, каждый 2–24 буквы или цифры."},
	"bad_scope": {KK: "Факультет бойынша жариялау үшін профильде факультетті көрсетіңіз.", RU: "Чтобы публиковать для факультета, укажите факультет в профиле."},
	// roommates
	"one_active":   {KK: "Сізде белсенді хабарландыру бар. Жаңасын жасау үшін алдымен оны жабыңыз.", RU: "У вас уже есть активное объявление. Закройте его, чтобы создать новое."},
	"need_gender":  {KK: "«Тек өз жынысым» шарты үшін жынысыңызды белгілеңіз (профильде көрсетілмейді).", RU: "Для условия «только мой пол» укажите пол (в профиле не показывается)."},
	"bad_budget":   {KK: "Бюджет: 5 000 – 1 000 000 ₸, ең азы ең көбінен аспасын.", RU: "Бюджет: 5 000 – 1 000 000 ₸, минимум не больше максимума."},
	"bad_move_in":  {KK: "Көшу күні бүгіннен бастап 6 ай ішінде болсын.", RU: "Дата заезда — в ближайшие 6 месяцев."},
	"reason_short": {KK: "Себебін жазыңыз (3–300 таңба).", RU: "Укажите причину (3–300 символов)."},
}

func fail(w http.ResponseWriter, r *http.Request, code string, status int) {
	m, ok := msgs[code]
	if !ok {
		m = httpx.MsgBad
	}
	httpx.Error(w, r, status, code, m)
}

func internalErr(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("community: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func internalLog(what string, err error) { log.Printf("community: %s: %v", what, err) }

func bad(w http.ResponseWriter, r *http.Request) { fail(w, r, "bad_request", http.StatusBadRequest) }
func notFound(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
}

// codeErr carries a user-facing error code out of a transaction.
type codeErr struct {
	code   string
	status int
}

func (e *codeErr) Error() string { return "community: " + e.code }

func refuse(code string, status int) error { return &codeErr{code, status} }

// respond maps a transaction error to the HTTP answer; true when one was written.
func respond(w http.ResponseWriter, r *http.Request, what string, err error) bool {
	if err == nil {
		return false
	}
	var ce *codeErr
	switch {
	case errors.As(err, &ce):
		fail(w, r, ce.code, ce.status)
	case errors.Is(err, pgx.ErrNoRows):
		notFound(w, r)
	default:
		internalErr(w, r, what, err)
	}
	return true
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------- writers ----------

// writer checks what every write needs: consent and (for scoped features) a university.
func writer(w http.ResponseWriter, r *http.Request, u *auth.User, needUniversity bool) bool {
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return false
	}
	if needUniversity && strings.TrimSpace(deref(u.University)) == "" {
		fail(w, r, "no_university", http.StatusForbidden)
		return false
	}
	return true
}

// ---------- text rules ----------

var (
	// the Bagdar rule (services.has_contacts) plus e-mail addresses and messenger names
	contactRe = regexp.MustCompile(`(?i)@[a-z0-9_]{4,}|t\.me/|wa\.me/|https?://|www\.|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}|\b[a-z0-9-]+\.(kz|ru|com|me|org|net|io)\b`)
	phoneRe   = regexp.MustCompile(`\+?\d{7,}`)
	handleRe  = regexp.MustCompile(`(?i)(^|[^a-z0-9._%+-])@[a-z0-9_]{4,}`)
	squeezeRe = regexp.MustCompile(`[\s\-().]`)
	digitsRe  = regexp.MustCompile(`\d{6,}`)
)

// HasContact reports a phone number, @handle, e-mail or link in free text, which would bypass
// «contacts open only when both sides agree».
func HasContact(s string) bool {
	if contactRe.MatchString(s) {
		return true
	}
	return phoneRe.MatchString(squeezeRe.ReplaceAllString(s, ""))
}

// HasPhoneOrHandle is the Q&A rule: links to study material are fine, personal contacts are not.
func HasPhoneOrHandle(s string) bool {
	if handleRe.MatchString(s) {
		return true
	}
	return phoneRe.MatchString(squeezeRe.ReplaceAllString(s, ""))
}

// HasLongNumber reports 6+ digits in a row (ignoring spaces, dashes, dots, brackets): an ID,
// card or student-ticket number that a finder must not publish.
func HasLongNumber(s string) bool {
	return digitsRe.MatchString(squeezeRe.ReplaceAllString(s, ""))
}

// cleanText trims and checks the rune length; control characters other than newlines fail.
func cleanText(s string, min, max int) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < min || n > max {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' {
			return "", false
		}
	}
	return s, true
}

// oneLine is cleanText for single-line fields: whitespace runs collapse to one space.
func oneLine(s string, min, max int) (string, bool) {
	return cleanText(strings.Join(strings.Fields(s), " "), min, max)
}

// NormKey is the board key of a subject or faculty name: lower case, ё→е, letters and digits
// only, single spaces. «Математика-1», «  математика 1 » and «МАТЕМАТИКА 1» share a board.
func NormKey(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		} else {
			space = true
		}
	}
	out := b.String()
	for utf8.RuneCountInString(out) > 80 {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return strings.TrimSpace(out)
}

// ---------- days and limits ----------

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

// Today is the Asia/Almaty date (YYYY-MM-DD); replaced in tests.
var Now = time.Now

func today() time.Time {
	a := Now().In(almaty)
	return time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
}

// countToday counts the rows a user created since Almaty midnight (q must take $1 = user,
// $2 = the start of the day).
func countToday(ctx context.Context, tx pgx.Tx, q string, userID int64) (int, error) {
	a := Now().In(almaty)
	start := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, almaty)
	var n int
	err := tx.QueryRow(ctx, q, userID, start).Scan(&n)
	return n, err
}

func limit(name string, def int) int { return config.Int(name, def) }

// ---------- contacts ----------

// Contact is what the two parties of a confirmed claim / accepted request see of each other.
type Contact struct {
	Name        string  `json:"name"`
	Username    *string `json:"username"`
	TelegramURL *string `json:"telegramUrl"`
	Email       *string `json:"email"`
}

// hasChannel: the account can be reached after a reveal (Telegram @username or verified e-mail).
func hasChannel(ctx context.Context, tx pgx.Tx, userID int64) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `select coalesce(tg_username, '') <> '' or (email is not null and email_verified_at is not null)
		from users where id = $1 and deleted_at is null`, userID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// loadContact reads one user's contact. Callers must have checked the consent state first.
func loadContact(ctx context.Context, userID int64) (*Contact, error) {
	var c Contact
	var verified bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select coalesce(name, ''), nullif(tg_username, ''), email, email_verified_at is not null
			from users where id = $1 and deleted_at is null`, userID).Scan(&c.Name, &c.Username, &c.Email, &verified)
	})
	if err != nil {
		return nil, err
	}
	if !verified {
		c.Email = nil
	}
	if c.Username != nil {
		u := "https://t.me/" + *c.Username
		c.TelegramURL = &u
	}
	return &c, nil
}

// ---------- notifications ----------

// Notify is the sender (community category); tests replace it.
var Notify = notify.For(notify.CatCommunity)

func tr(lang, kk, ru string) string {
	if lang == "ru" {
		return ru
	}
	return kk
}

// tell sends a short message with a site link, in the recipient's language, in the background.
func tell(ctx context.Context, userID int64, key, link string, text func(lang string) string) {
	ctx = context.WithoutCancel(ctx)
	go func() {
		lang := "kk"
		if p, err := db.Pool(ctx); err == nil {
			_ = p.QueryRow(ctx, `select lang from users where id = $1`, userID).Scan(&lang)
		}
		body := text(lang) + "\n" + html.EscapeString(config.SiteURL()+link)
		c := notify.WithLink(notify.WithKey(ctx, key), link)
		if err := Notify(c, userID, body); err != nil {
			log.Printf("community: notify %d %s: %v", userID, key, err)
		}
	}()
}

// ---------- reports and moderation ----------

type target struct {
	table, title string
	feature      string
}

var targets = map[string]target{
	TargetLostFound: {"lostfound_posts", "title", FeatureLostFound},
	TargetQuestion:  {"qa_questions", "title", FeatureCourseQA},
	TargetAnswer:    {"qa_answers", "body", FeatureCourseQA},
	TargetRoommate:  {"roommate_listings", "district", FeatureRoommates},
}

func registerReports() {
	for kind, t := range targets {
		moderation.OnReport(kind, moderation.ReportTarget{
			Exists: func(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
				n, err := strconv.ParseInt(id, 10, 64)
				if err != nil || !features.Enabled(t.feature) {
					return false, nil
				}
				var ok bool
				err = tx.QueryRow(ctx, `select exists (select 1 from `+t.table+` where id = $1 and status = 'active')`, n).Scan(&ok)
				return ok, err
			},
			SetHidden: func(ctx context.Context, tx pgx.Tx, id string, hidden bool) error {
				return setHidden(ctx, tx, kind, id, hidden)
			},
		})
	}
	moderation.OnApprove(KindHidden, func(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
		k, id := hiddenTarget(s)
		t, ok := targets[k]
		if !ok {
			return nil
		}
		// upheld: stays hidden for good (an admin decision, not reversible by a report dismissal)
		_, err := tx.Exec(ctx, `update `+t.table+` set hidden_reason = 'admin' where id = $1 and status = 'hidden'`, id)
		return err
	})
	moderation.OnReject(KindHidden, func(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
		k, id := hiddenTarget(s)
		t, ok := targets[k]
		if !ok {
			return nil
		}
		if _, err := tx.Exec(ctx, `update reports set dismissed = true where target_kind = $1 and target_id = $2 and not dismissed`,
			k, strconv.FormatInt(id, 10)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update `+t.table+` set status = 'active', hidden_reason = null where id = $1 and status = 'hidden'`, id)
		return err
	})
	moderation.Notice(KindHidden, func(s moderation.Submission, approved bool, lang string) string {
		if approved {
			return tr(lang, "Шағымдардан кейін жазбаңыз жасырылды: ол қауымдастық ережелеріне сай емес.",
				"После жалоб ваша запись скрыта: она не соответствует правилам сообщества.")
		}
		return tr(lang, "Жазбаңыз тексерілді және қайта көрінеді.", "Ваша запись проверена и снова видна.")
	})
}

func hiddenTarget(s moderation.Submission) (string, int64) {
	var p struct {
		Kind string `json:"kind"`
		ID   int64  `json:"id"`
	}
	_ = json.Unmarshal(s.Payload, &p)
	return p.Kind, p.ID
}

// setHidden hides an item by reports (and queues it for the admin with its real author, even
// when it was posted anonymously) or shows it again after a dismissal.
func setHidden(ctx context.Context, tx pgx.Tx, kind, rawID string, hidden bool) error {
	t, ok := targets[kind]
	if !ok {
		return nil
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return nil
	}
	if !hidden {
		_, err := tx.Exec(ctx, `update `+t.table+` set status = 'active', hidden_reason = null where id = $1 and status = 'hidden' and hidden_reason = 'reports'`, id)
		return err
	}
	var author int64
	var text, uni string
	q := `update ` + t.table + ` set status = 'hidden', hidden_reason = 'reports' where id = $1 and status = 'active'
		returning user_id, left(` + t.title + `, 200), %s`
	uniCol := "university"
	if kind == TargetAnswer {
		uniCol = "(select university from qa_questions q where q.id = question_id)"
	}
	err = tx.QueryRow(ctx, fmt.Sprintf(q, uniCol), id).Scan(&author, &text, &uni)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = moderation.Submit(ctx, tx, KindHidden, map[string]any{"kind": kind, "id": id, "excerpt": text}, &author, uni)
	return err
}

// adminModerate: POST /api/admin/<feature>/moderate {kind?, id, hidden, reason} — hide or show
// an item; recorded in audit_log.
func adminModerate(kinds ...string) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, u *auth.User) {
		var body struct {
			Kind   string `json:"kind"`
			ID     int64  `json:"id"`
			Hidden bool   `json:"hidden"`
			Reason string `json:"reason"`
		}
		if err := httpx.Decode(r, &body, 4<<10); err != nil || body.ID <= 0 {
			bad(w, r)
			return
		}
		switch body.Kind {
		case "":
			body.Kind = kinds[0]
		case "question":
			body.Kind = TargetQuestion
		case "answer":
			body.Kind = TargetAnswer
		}
		known := false
		for _, k := range kinds {
			known = known || k == body.Kind
		}
		reason, ok := oneLine(body.Reason, 3, 300)
		if !known {
			bad(w, r)
			return
		}
		if !ok {
			fail(w, r, "reason_short", http.StatusBadRequest)
			return
		}
		t := targets[body.Kind]
		err := db.Tx(r.Context(), func(tx pgx.Tx) error {
			var tag interface{ RowsAffected() int64 }
			var err error
			if body.Hidden {
				tag, err = tx.Exec(r.Context(), `update `+t.table+` set status = 'hidden', hidden_reason = 'admin' where id = $1 and status in ('active', 'hidden')`, body.ID)
			} else {
				tag, err = tx.Exec(r.Context(), `update `+t.table+` set status = 'active', hidden_reason = null where id = $1 and status = 'hidden'`, body.ID)
			}
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return pgx.ErrNoRows
			}
			action := "community_hide"
			if !body.Hidden {
				action = "community_restore"
			}
			_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, reason, meta)
				values ($1, $2, $3, jsonb_build_object('kind', $4::text, 'id', $5::bigint))`, u.ID, action, reason, body.Kind, body.ID)
			return err
		})
		if respond(w, r, "moderate", err) {
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "hidden": body.Hidden})
	}
}

// ---------- daily job ----------

// Archive closes what is 30 days old: lost & found posts become 'archived', roommate listings
// 'closed'. It runs inside the main daily cron (cronrun.OnDaily); reads also filter expires_at,
// so a late cron never shows a stale item.
func Archive(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `update lostfound_posts set status = 'archived', updated_at = now() where status = 'active' and expires_at <= now()`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update roommate_listings set status = 'closed' where status = 'active' and expires_at <= now()`); err != nil {
		return err
	}
	// photos uploaded but never attached to a post (a day is enough to finish the form)
	rows, err := tx.Query(ctx, `delete from lostfound_photos where post_id is null and created_at < now() - interval '1 day' returning name`)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		if photoRe.MatchString(n) {
			if err := os.Remove(filepath.Join(PhotoDir(), n)); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("community: orphan photo %s: %v", n, err)
			}
		}
	}
	return nil
}

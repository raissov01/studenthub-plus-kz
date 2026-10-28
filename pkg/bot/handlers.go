package bot

// Webhook update handling: /start remind_<id>, commands, button callbacks.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db/q"
)

// Update is the subset of a Telegram update the bot reads.
type Update struct {
	UpdateID      int64              `json:"update_id"`
	Message       *Message           `json:"message"`
	CallbackQuery *CallbackQuery     `json:"callback_query"`
	MyChatMember  *ChatMemberUpdated `json:"my_chat_member"`
}

type TgUser struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	LanguageCode string `json:"language_code"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Message struct {
	MessageID   int64   `json:"message_id"`
	From        *TgUser `json:"from"`
	Chat        Chat    `json:"chat"`
	Text        string  `json:"text"`
	ReplyMarkup *markup `json:"reply_markup"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *TgUser  `json:"from"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

type ChatMemberUpdated struct {
	Chat          Chat    `json:"chat"`
	From          *TgUser `json:"from"`
	NewChatMember struct {
		Status string `json:"status"`
	} `json:"new_chat_member"`
}

// session is one update being handled for one student.
type session struct {
	b       *Bot
	q       *q.Queries
	c       *content
	user    q.User
	chatID  int64
	deleted bool
}

func (s *session) lang() string { return s.user.Lang }
func (s *session) L() *texts    { return txt(s.user.Lang) }
func (s *session) today() string {
	return localDate(s.b.now())
}

var ruLike = regexp.MustCompile(`(?i)^(ru|be|uk)`)

// HandleUpdate processes one webhook update.
func (b *Bot) HandleUpdate(ctx context.Context, up *Update) error {
	if err := b.ready(); err != nil {
		return err
	}
	qs := q.New(b.Pool)
	if up.MyChatMember != nil {
		return b.onChatMember(ctx, qs, up.MyChatMember)
	}
	msg, cq := up.Message, up.CallbackQuery
	var from *TgUser
	if msg != nil {
		from = msg.From
	} else if cq != nil {
		from = cq.From
	}
	if from == nil || from.IsBot || from.ID == 0 {
		return nil
	}
	if msg != nil && msg.Chat.Type != "private" {
		return nil // the bot only talks in private chats
	}

	lang := "kk"
	if ruLike.MatchString(from.LanguageCode) {
		lang = "ru"
	}
	user, err := qs.BotTouchUser(ctx, q.BotTouchUserParams{TelegramID: from.ID, Lang: lang})
	if err != nil {
		return err
	}
	noteSent := user.SafetyNoteSent
	s := &session{b: b, q: qs, c: b.newContent(), user: user, chatID: from.ID}
	if msg != nil {
		err = s.onMessage(ctx, msg)
	} else if cq != nil {
		err = s.onCallback(ctx, cq)
	}
	if err != nil || s.deleted { // /stop confirmed: nothing may be written back
		return err
	}
	if !noteSent {
		if err := qs.BotMarkSafetyNote(ctx, user.ID); err != nil {
			return err
		}
		return b.send(ctx, s.chatID, s.L().NoMoney, nil)
	}
	return nil
}

func (b *Bot) onChatMember(ctx context.Context, qs *q.Queries, upd *ChatMemberUpdated) error {
	if upd.Chat.Type != "private" || upd.From == nil {
		return nil
	}
	user, err := qs.BotUserByTelegram(ctx, upd.From.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !user.BotStartedAt.Valid) {
		return nil
	}
	if err != nil {
		return err
	}
	st := upd.NewChatMember.Status
	return qs.BotSetInactive(ctx, q.BotSetInactiveParams{ID: user.ID, BotInactive: st == "kicked" || st == "left"})
}

var cmdRe = regexp.MustCompile(`(?i)^/([a-z]+)(?:@\w+)?(?:\s+(.*))?$`)
var remindRe = regexp.MustCompile(`^remind_(.+)$`)

func (s *session) siteButton(lang string) [][]Button {
	return [][]Button{{{Text: txt(lang).BtnSite, URL: s.b.Cfg.SiteURL + "/"}}}
}

func (s *session) send(ctx context.Context, text string, kb [][]Button) error {
	return s.b.send(ctx, s.chatID, text, kb)
}

func (s *session) pausedUntil() string { return pauseISO(s.user.PausedUntil) }

func (s *session) onMessage(ctx context.Context, msg *Message) error {
	var cmd, arg string
	if m := cmdRe.FindStringSubmatch(strings.TrimSpace(msg.Text)); m != nil {
		cmd, arg = strings.ToLower(m[1]), strings.TrimSpace(m[2])
	}
	L := s.L()
	today := s.today()

	switch cmd {
	case "start":
		if rm := remindRe.FindStringSubmatch(arg); rm != nil {
			return s.startRemind(ctx, rm[1])
		}
		return s.send(ctx, L.Greeting, append(append([][]Button{}, langButtons...), s.siteButton(s.lang())...))
	case "my":
		return s.sendMy(ctx)
	case "pause":
		until := addDays(today, 7)
		if err := s.q.BotSetPausedUntil(ctx, q.BotSetPausedUntilParams{ID: s.user.ID, PausedUntil: pgPause(until)}); err != nil {
			return err
		}
		s.user.PausedUntil = pgPause(until)
		return s.send(ctx, L.Paused(fmtDate(until, s.lang(), today)), nil)
	case "resume":
		p := s.pausedUntil()
		was := p != "" && p > today
		if err := s.q.BotSetPausedUntil(ctx, q.BotSetPausedUntilParams{ID: s.user.ID}); err != nil {
			return err
		}
		if was {
			return s.send(ctx, L.Resumed, nil)
		}
		return s.send(ctx, L.NotPaused, nil)
	case "stop":
		return s.send(ctx, L.StopAsk, [][]Button{{{Text: L.StopYes, CallbackData: "stop:yes"}, {Text: L.StopNo, CallbackData: "stop:no"}}})
	case "stats":
		if s.b.Cfg.Admins[strconv.FormatInt(s.chatID, 10)] {
			st, err := s.b.Stats(ctx, s.b.now())
			if err != nil {
				return err
			}
			return s.send(ctx, L.Stats(st), nil)
		}
		return s.send(ctx, L.Help, nil) // non-admins see nothing special
	default:
		return s.send(ctx, L.Help, s.siteButton(s.lang()))
	}
}

// Stats is the admin summary: bot students, active ones, live reminders, "submitted" in 7 days.
func (b *Bot) Stats(ctx context.Context, now time.Time) (statsData, error) {
	qs := q.New(b.Pool)
	var st statsData
	u, err := qs.BotCountUsers(ctx)
	if err != nil {
		return st, err
	}
	st.Users, st.Active = u.Users, u.Active
	if st.Reminders, err = qs.BotCountLiveReminders(ctx, pgDate(localDate(now))); err != nil {
		return st, err
	}
	since := now.Add(-7 * 24 * time.Hour)
	st.Submitted, err = qs.BotCountEvents(ctx, q.BotCountEventsParams{Kind: "submitted", At: pgTime(since)})
	return st, err
}

type saved struct {
	text string
	ok   bool
}

// saveReminder stores a reminder and returns the reply text. Shared by /start remind_<id>
// and the digest "remind" button.
func (s *session) saveReminder(ctx context.Context, id string, short bool) (saved, error) {
	lang, L, today := s.lang(), s.L(), s.today()
	if !idRe.MatchString(id) {
		return saved{text: L.NotFound}, nil
	}
	opp, err := s.c.find(ctx, id)
	if err != nil {
		return saved{text: L.LoadFail}, nil
	}
	if opp == nil {
		return saved{text: L.NotFound}, nil
	}
	title := esc(opp.Title.pick(lang))
	if !isISODate(opp.Deadline) {
		return saved{text: L.NoDeadline(title)}, nil
	}
	date := fmtDate(opp.Deadline, lang, today)
	left := daysBetween(today, opp.Deadline)
	if left < 0 {
		return saved{text: L.Expired(title, date)}, nil
	}
	var days []int
	for _, d := range remindDays {
		if d < left {
			days = append(days, d)
		}
	}
	if len(days) == 0 {
		return saved{text: L.TooLate(title, date)}, nil
	}
	existing, err := s.q.BotUserReminders(ctx, s.user.ID)
	if err != nil {
		return saved{}, err
	}
	for _, r := range existing {
		if r.OpportunityID == id {
			return saved{text: L.Already(title, date), ok: true}, nil
		}
	}
	if len(existing) >= maxReminders {
		return saved{text: L.TooMany}, nil
	}
	err = s.q.BotUpsertReminder(ctx, q.BotUpsertReminderParams{
		UserID: s.user.ID, OpportunityID: id,
		Title: opp.Title.pick("kk"), TitleRu: pgText(opp.Title.pick("ru")),
		Deadline: pgDate(opp.Deadline),
	})
	if err != nil {
		return saved{}, err
	}
	if short {
		return saved{text: L.RemindSaved(title, date), ok: true}, nil
	}
	return saved{text: L.Confirm(title, date, days), ok: true}, nil
}

func (s *session) startRemind(ctx context.Context, id string) error {
	r, err := s.saveReminder(ctx, id, false)
	if err != nil {
		return err
	}
	kb := s.siteButton(s.lang())
	if r.ok {
		kb = [][]Button{{{Text: s.L().BtnOpen, URL: s.b.oppURL(id)}}}
	}
	if err := s.send(ctx, r.text, kb); err != nil {
		return err
	}
	if r.ok && s.b.Cfg.Digest && !s.user.DigestAsked {
		return s.askCity(ctx)
	}
	return nil
}

func optionRows(list []option, perRow int, lang, prefix string) [][]Button {
	var rows [][]Button
	for i := 0; i < len(list); i += perRow {
		var row []Button
		for _, o := range list[i:min(i+perRow, len(list))] {
			row = append(row, Button{Text: o.label(lang), CallbackData: prefix + o.key})
		}
		rows = append(rows, row)
	}
	return rows
}

func (s *session) askCity(ctx context.Context) error {
	if err := s.q.BotSetDigestAsked(ctx, s.user.ID); err != nil {
		return err
	}
	s.user.DigestAsked = true
	return s.send(ctx, s.L().AskCity, optionRows(cities, 3, s.lang(), "city:"))
}

func reminderTitle(r q.Reminder) Text { return Text{KK: r.Title, RU: text(r.TitleRu)} }

func (s *session) sendMy(ctx context.Context) error {
	lang, L, today := s.lang(), s.L(), s.today()
	list, err := s.q.BotUserReminders(ctx, s.user.ID)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return s.send(ctx, L.MyEmpty, s.siteButton(lang))
	}
	var lines []string
	var kb [][]Button
	for i, r := range list {
		lines = append(lines, fmt.Sprintf("%d. <b>%s</b> — %s", i+1, esc(reminderTitle(r).pick(lang)), fmtDate(isoOf(r.Deadline), lang, today)))
		kb = append(kb, []Button{
			{Text: fmt.Sprintf("%d · %s", i+1, L.BtnOpen), URL: s.b.oppURL(r.OpportunityID)},
			{Text: fmt.Sprintf("%d · %s", i+1, L.BtnDelete), CallbackData: "del:" + r.OpportunityID},
		})
	}
	if p := s.pausedUntil(); p != "" && p > today {
		lines = append(lines, "", L.MyPaused(fmtDate(p, lang, today)))
	}
	return s.send(ctx, L.MyHead+"\n\n"+strings.Join(lines, "\n"), kb)
}

// withoutOpp drops the buttons that belong to one opportunity from a message keyboard.
func withoutOpp(m *markup, id string) [][]Button {
	out := [][]Button{}
	if m == nil {
		return out
	}
	suffix := "/opportunity/" + url.PathEscape(id)
	for _, row := range m.InlineKeyboard {
		var keep []Button
		for _, btn := range row {
			if parts := strings.Split(btn.CallbackData, ":"); btn.CallbackData != "" && len(parts) > 1 && parts[1] == id {
				continue
			}
			if btn.URL != "" && strings.HasSuffix(btn.URL, suffix) {
				continue
			}
			keep = append(keep, btn)
		}
		if len(keep) > 0 {
			out = append(out, keep)
		}
	}
	return out
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

func plain(s string, max int) string {
	s = tagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("&lt;", "<", "&gt;", ">").Replace(s)
	s = strings.ReplaceAll(s, "&amp;", "&")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

func (s *session) onCallback(ctx context.Context, cq *CallbackQuery) error {
	L := s.L()
	parts := strings.Split(cq.Data, ":")
	action, value := parts[0], ""
	if len(parts) > 1 {
		value = parts[1]
	}
	msg := cq.Message
	answer := func(text string, alert bool) error {
		p := map[string]any{"callback_query_id": cq.ID}
		if text != "" {
			p["text"], p["show_alert"] = text, alert
		}
		_, _ = s.b.tg(ctx, "answerCallbackQuery", p) // a stale callback must not fail the update
		return nil
	}
	setKeyboard := func(kb [][]Button) {
		if msg == nil {
			return
		}
		if kb == nil {
			kb = [][]Button{}
		}
		_, _ = s.b.tg(ctx, "editMessageReplyMarkup", map[string]any{
			"chat_id": msg.Chat.ID, "message_id": msg.MessageID, "reply_markup": markup{kb},
		})
	}
	var current *markup
	if msg != nil {
		current = msg.ReplyMarkup
	}

	switch action {
	case "lang":
		if value != "kk" && value != "ru" {
			return answer("", false)
		}
		if err := s.q.BotSetLang(ctx, q.BotSetLangParams{ID: s.user.ID, Lang: value}); err != nil {
			return err
		}
		s.user.Lang = value
		T := txt(value)
		_ = answer(T.LangSet, false)
		return s.send(ctx, T.LangSet+"\n\n"+T.Help, s.siteButton(value))
	case "done", "skip":
		status, kind, toast := "submitted", "submitted", L.DoneToast
		if action == "skip" {
			status, kind, toast = "dismissed", "dismissed", L.SkipToast
		}
		n, err := s.q.BotFinishReminder(ctx, q.BotFinishReminderParams{UserID: s.user.ID, OpportunityID: value, Status: status})
		if err != nil {
			return err
		}
		if n > 0 {
			if err := s.q.BotLogEvent(ctx, q.BotLogEventParams{Kind: kind, UserID: pgInt8(s.user.ID)}); err != nil {
				return err
			}
		}
		setKeyboard(withoutOpp(current, value))
		if n > 0 {
			return answer(toast, true)
		}
		return answer(L.Gone, false)
	case "del":
		if _, err := s.q.BotDeleteReminder(ctx, q.BotDeleteReminderParams{UserID: s.user.ID, OpportunityID: value}); err != nil {
			return err
		}
		setKeyboard(withoutOpp(current, value))
		return answer(L.Deleted, false)
	case "stop":
		if value == "yes" {
			if err := s.b.DeleteUserData(ctx, s.user.ID); err != nil {
				return err
			}
			s.deleted = true
			_ = answer("", false)
			if msg != nil {
				return s.b.edit(ctx, msg.Chat.ID, msg.MessageID, L.StopDone, nil)
			}
			return s.send(ctx, L.StopDone, nil)
		}
		_ = answer("", false)
		if msg != nil {
			return s.b.edit(ctx, msg.Chat.ID, msg.MessageID, L.StopCancel, nil)
		}
		return nil
	case "city":
		if !hasOption(cities, value) {
			return answer("", false)
		}
		if err := s.q.BotSetDigestCity(ctx, q.BotSetDigestCityParams{ID: s.user.ID, DigestCity: pgText(value)}); err != nil {
			return err
		}
		_ = answer("", false)
		rows := optionRows(dirs, 2, s.lang(), "dir:")
		if msg != nil {
			return s.b.edit(ctx, msg.Chat.ID, msg.MessageID, L.AskDir, rows)
		}
		return s.send(ctx, L.AskDir, rows)
	case "dir":
		if !hasOption(dirs, value) {
			return answer("", false)
		}
		if err := s.q.BotSetDigestDirection(ctx, q.BotSetDigestDirectionParams{ID: s.user.ID, DigestDirection: pgText(value)}); err != nil {
			return err
		}
		_ = answer("", false)
		if msg != nil {
			return s.b.edit(ctx, msg.Chat.ID, msg.MessageID, L.DigestSaved, nil)
		}
		return s.send(ctx, L.DigestSaved, nil)
	case "rem":
		r, err := s.saveReminder(ctx, value, true)
		if err != nil {
			return err
		}
		if r.ok && current != nil {
			var kb [][]Button
			for _, row := range current.InlineKeyboard {
				var keep []Button
				for _, btn := range row {
					if btn.CallbackData != "rem:"+value {
						keep = append(keep, btn)
					}
				}
				if len(keep) > 0 {
					kb = append(kb, keep)
				}
			}
			setKeyboard(kb)
		}
		return answer(plain(r.text, 190), true)
	default:
		return answer("", false)
	}
}

// DeleteUserData is /stop: reminders, day claims and the bot state go for real. A user
// known only to the bot is deleted entirely; a site account keeps its row (the account
// deletion flow owns that) with every bot column cleared. Events lose the user link.
func (b *Bot) DeleteUserData(ctx context.Context, userID int64) error {
	return pgx.BeginFunc(ctx, b.Pool, func(t pgx.Tx) error {
		qs := q.New(t)
		if err := qs.BotDeleteUserReminders(ctx, userID); err != nil {
			return err
		}
		if err := qs.BotDeleteUserSends(ctx, userID); err != nil {
			return err
		}
		if err := qs.BotAnonymizeEvents(ctx, pgInt8(userID)); err != nil {
			return err
		}
		n, err := qs.BotDeleteBotOnlyUser(ctx, userID)
		if err != nil || n > 0 {
			return err
		}
		return qs.BotClearUser(ctx, userID)
	})
}

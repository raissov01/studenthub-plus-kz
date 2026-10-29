package bot

// Daily job: deadline reminders (7/3/1 days left) and the optional weekly digest.
// Producers persist digest items; pkg/notify combines and dispatches at most one/day.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/db/q"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

const (
	maxItems  = 8  // per message: readable and under Telegram's 4096 chars
	maxDocs   = 6  //
	parallel  = 10 // well under Telegram's ~30 msg/s
	keepSeen  = 200
	claimDays = 3 // day claims older than this are pruned
)

// CronResult is the JSON summary of one run.
type CronResult struct {
	Date        string `json:"date"`
	Reminders   int    `json:"reminders"`
	Digests     int    `json:"digests"`
	AlreadySent int    `json:"alreadySent"`
	Blocked     int    `json:"blocked"`
	Failed      int    `json:"failed"`
	Cleaned     int    `json:"cleaned"`
}

type dueItem struct {
	OppID    string
	Deadline string
	Left     int
	Title    Text
	Docs     []Text
}

func (b *Bot) reminderMessage(items []dueItem, lang, today string) (string, [][]Button) {
	L := txt(lang)
	shown := items[:min(len(items), maxItems)]
	many := len(shown) > 1
	head := L.ReminderHead
	if many {
		head = L.ReminderHeadMany
	}
	parts := []string{head, ""}
	kb := [][]Button{}
	for i, it := range shown {
		n, p := "", ""
		if many {
			n, p = fmt.Sprintf("%d. ", i+1), fmt.Sprintf("%d · ", i+1)
		}
		parts = append(parts, fmt.Sprintf("%s<b>%s</b>", n, esc(it.Title.pick(lang))))
		parts = append(parts, fmt.Sprintf("%s (%s)", L.DaysLeft(it.Left), fmtDate(it.Deadline, lang, today)))
		var docs []string
		for _, d := range it.Docs {
			if v := d.pick(lang); v != "" {
				docs = append(docs, v)
			}
		}
		if len(docs) > 0 {
			extra := ""
			if len(docs) > maxDocs {
				extra = fmt.Sprintf(" +%d", len(docs)-maxDocs)
			}
			parts = append(parts, fmt.Sprintf("%s: %s%s", L.Docs, esc(strings.Join(docs[:min(len(docs), maxDocs)], ", ")), extra))
		}
		parts = append(parts, "")
		kb = append(kb, []Button{{Text: p + L.BtnOpen, URL: b.oppURL(it.OppID)}})
		kb = append(kb, []Button{
			{Text: p + L.BtnDone, CallbackData: "done:" + it.OppID},
			{Text: p + L.BtnSkip, CallbackData: "skip:" + it.OppID},
		})
	}
	if len(items) > len(shown) {
		parts = append(parts, L.MoreInMy(len(items)-len(shown)), "")
	}
	parts = append(parts, L.ReminderFoot)
	return strings.Join(parts, "\n"), kb
}

func (b *Bot) digestMessage(opps []Opp, lang, today string) (string, [][]Button) {
	L := txt(lang)
	parts := []string{L.DigestHead, ""}
	kb := [][]Button{}
	for i, o := range opps {
		dl := ""
		if isISODate(o.Deadline) {
			dl = " — " + L.DigestDeadline(fmtDate(o.Deadline, lang, today))
		}
		parts = append(parts, fmt.Sprintf("%d. <b>%s</b>%s", i+1, esc(o.Title.pick(lang)), dl))
		kb = append(kb, []Button{
			{Text: fmt.Sprintf("%d · %s", i+1, L.BtnOpen), URL: b.oppURL(o.ID)},
			{Text: fmt.Sprintf("%d · %s", i+1, L.BtnRemind), CallbackData: "rem:" + o.ID},
		})
	}
	parts = append(parts, "", L.DigestFoot)
	return strings.Join(parts, "\n"), kb
}

// pickDigest: up to 3 opportunities posted in the last 7 days that fit the student's city
// and direction, not seen in an earlier digest and not already saved.
func pickDigest(u q.User, opps []Opp, saved map[string]bool, today string) []Opp {
	seen := map[string]bool{}
	for _, id := range u.DigestSeen {
		seen[id] = true
	}
	city, dir := text(u.DigestCity), text(u.DigestDirection)
	var out []Opp
	for _, o := range opps {
		if o.ID == "" || seen[o.ID] || saved[o.ID] {
			continue
		}
		if !isISODate(o.PostedAt) {
			continue
		}
		if age := daysBetween(o.PostedAt, today); age < 0 || age > 7 {
			continue
		}
		if isISODate(o.Deadline) && daysBetween(today, o.Deadline) <= 1 {
			continue
		}
		if o.City != "" && o.City != "remote" && city != "other" && o.City != city {
			continue
		}
		if o.Spec != nil && len(o.Spec) > 0 && !slices.Contains(o.Spec, "all") && dir != "other" && !slices.Contains(o.Spec, dir) {
			continue
		}
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PostedAt > out[j].PostedAt })
	return out[:min(len(out), 3)]
}

// enqueueDigest preserves the bot's action keyboard in the shared durable queue.
func (b *Bot) enqueueDigest(ctx context.Context, u q.User, category notify.Category, key string, now time.Time, msg string, kb [][]Button) (string, error) {
	markup, err := json.Marshal(markup{kb})
	if err != nil {
		return "", err
	}
	return b.dispatcher().Digest(ctx, u.ID, category, msg, notify.Opts{Now: now, Key: key, ReplyMarkup: markup})
}

// inChunks runs fn over list, parallel at a time; the first error wins.
func inChunks(list []q.User, fn func(q.User) error) error {
	for i := 0; i < len(list); i += parallel {
		var wg sync.WaitGroup
		errs := make([]error, 0)
		var mu sync.Mutex
		for _, u := range list[i:min(i+parallel, len(list))] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := fn(u); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if len(errs) > 0 {
			return errs[0]
		}
	}
	return nil
}

// RunCron is the daily job for the Almaty day of now.
func (b *Bot) RunCron(ctx context.Context, now time.Time) (*CronResult, error) {
	if err := b.ready(); err != nil {
		return nil, err
	}
	qs := q.New(b.Pool)
	today := localDate(now)
	res := &CronResult{Date: today}
	var mu sync.Mutex
	count := func(status string) {
		mu.Lock()
		defer mu.Unlock()
		switch status {
		case "sent", "queued":
			res.Reminders++
		case "claimed":
			res.AlreadySent++
		case "blocked":
			res.Blocked++
		case "failed":
			res.Failed++
		case "cleaned":
			res.Cleaned++
		}
	}

	opps, err := b.newContent().load(ctx)
	contentOK := err == nil
	if err != nil {
		log.Printf("cron content: %v", err)
	}
	byID := map[string]*Opp{}
	for i := range opps {
		byID[opps[i].ID] = &opps[i]
	}

	if err := qs.BotPruneDays(ctx, pgDate(addDays(today, -claimDays))); err != nil {
		return nil, err
	}
	users, err := qs.BotListUsers(ctx)
	if err != nil {
		return nil, err
	}
	all, err := qs.BotActiveReminders(ctx)
	if err != nil {
		return nil, err
	}
	byUser := map[int64][]q.Reminder{}
	for _, r := range all {
		byUser[r.UserID] = append(byUser[r.UserID], r)
	}

	// 1. Deadline reminders.
	err = inChunks(users, func(u q.User) error {
		var due []dueItem
		for _, r := range byUser[u.ID] {
			deadline := isoOf(r.Deadline)
			opp := byID[r.OpportunityID]
			if opp != nil && isISODate(opp.Deadline) && opp.Deadline != deadline {
				deadline = opp.Deadline // the site moved the deadline: follow it
				if err := qs.BotSetReminderDeadline(ctx, q.BotSetReminderDeadlineParams{
					UserID: u.ID, OpportunityID: r.OpportunityID, Deadline: pgDate(deadline),
				}); err != nil {
					return err
				}
			}
			if contentOK && opp != nil && !isISODate(opp.Deadline) {
				continue
			}
			left := daysBetween(today, deadline)
			if left < 0 {
				if _, err := qs.BotDeleteReminder(ctx, q.BotDeleteReminderParams{UserID: u.ID, OpportunityID: r.OpportunityID}); err != nil {
					return err
				}
				count("cleaned")
				continue
			}
			if slices.Contains(remindDays, left) {
				it := dueItem{OppID: r.OpportunityID, Deadline: deadline, Left: left, Title: reminderTitle(r)}
				if opp != nil {
					it.Title, it.Docs = opp.Title, opp.Docs
				}
				due = append(due, it)
			}
		}
		if len(due) == 0 {
			return nil
		}
		sort.SliceStable(due, func(i, j int) bool { return due[i].Deadline < due[j].Deadline })
		msg, kb := b.reminderMessage(due, u.Lang, today)
		status, err := b.enqueueDigest(ctx, u, notify.CatDeadlines, "bot:deadlines:"+today, now, msg, kb)
		count(status)
		return err
	})
	if err != nil {
		return nil, err
	}

	// 2. Weekly digest (off unless DIGEST_ENABLED): Thursday, or the next free day that week.
	if b.Cfg.Digest && contentOK && weekday(today) >= 4 {
		week := weekStart(today)
		fresh, err := qs.BotListUsers(ctx) // re-read: step 1 may have marked users inactive
		if err != nil {
			return nil, err
		}
		err = inChunks(fresh, func(u q.User) error {
			if text(u.DigestCity) == "" || text(u.DigestDirection) == "" || isoOf(u.DigestWeek) == week {
				return nil
			}
			saved := map[string]bool{}
			for _, r := range byUser[u.ID] {
				saved[r.OpportunityID] = true
			}
			list := pickDigest(u, opps, saved, today)
			if len(list) == 0 {
				return nil
			}
			msg, kb := b.digestMessage(list, u.Lang, today)
			status, err := b.enqueueDigest(ctx, u, notify.CatDigest, "bot:weekly:"+week, now, msg, kb)
			if err != nil {
				return err
			}
			switch status {
			case "sent", "queued", "off":
				seen := append([]string{}, u.DigestSeen...)
				for _, o := range list {
					seen = append(seen, o.ID)
				}
				if len(seen) > keepSeen {
					seen = seen[len(seen)-keepSeen:]
				}
				if err := qs.BotSetDigestSent(ctx, q.BotSetDigestSentParams{ID: u.ID, DigestWeek: pgDate(week), DigestSeen: seen}); err != nil {
					return err
				}
				mu.Lock()
				if status != notify.DigestOff {
					res.Digests++
				}
				mu.Unlock()
			case "claimed":
			default:
				count(status)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

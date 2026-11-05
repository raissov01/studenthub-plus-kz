package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
)

// Collector runs one collection pass (cmd/feeds-collect, every 30 minutes).
type Collector struct {
	Sources  []Source
	Fetchers map[string]Fetcher // by kind; a kind without a reader is reported per source
	AI       *AI                // nil: collect only, posts wait for a run with AI
	Now      func() time.Time
	Logf     func(format string, a ...any)

	DailyTokens  int           // FEEDS_AI_DAILY_LIMIT
	AutoMin      float64       // FEEDS_AUTOPUBLISH_MIN_CONFIDENCE, 0 = off
	UndatedTTL   int           // FEEDS_UNDATED_TTL_DAYS
	MaxAgeDays   int           // posts older than this are stored but never sent to AI
	MaxAIPerRun  int           // AI calls per run at most
	SourceDelay  time.Duration // pause between sources (politeness)
	Channel      string        // FEEDS_PUBLISH_CHANNEL
	PostChannel  func(ctx context.Context, chat, text string) error
	ChannelBatch int
}

// Settings reads the FEEDS_* env.
func (c *Collector) Settings() {
	c.DailyTokens = config.Int("FEEDS_AI_DAILY_LIMIT", 150000)
	c.AutoMin = 0
	if v := config.Str("FEEDS_AUTOPUBLISH_MIN_CONFIDENCE", ""); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil && f > 0 && f <= 1 {
			c.AutoMin = f
		}
	}
	c.UndatedTTL = config.Int("FEEDS_UNDATED_TTL_DAYS", 45)
	c.MaxAgeDays = config.Int("FEEDS_MAX_POST_AGE_DAYS", 30)
	c.MaxAIPerRun = config.Int("FEEDS_MAX_AI_PER_RUN", 60)
	c.SourceDelay = time.Duration(config.Int("FEEDS_SOURCE_DELAY_MS", 1500)) * time.Millisecond
	c.Channel = config.Str("FEEDS_PUBLISH_CHANNEL", "")
	c.ChannelBatch = 5
}

// Report is the summary of one run (printed as JSON by the command; no post text, no secrets).
type Report struct {
	Sources    int      `json:"sources"`
	Fetched    int      `json:"fetched"`
	New        int      `json:"new"`
	Skipped    int      `json:"skipped"`
	AICalls    int      `json:"ai_calls"`
	Tokens     int      `json:"tokens"`
	Irrelevant int      `json:"irrelevant"`
	Duplicates int      `json:"duplicates"`
	Failed     int      `json:"failed"`
	Pending    int      `json:"pending"`
	Published  int      `json:"published"`
	Expired    int      `json:"expired"`
	Channel    int      `json:"channel_posts"`
	BudgetHit  bool     `json:"budget_hit"`
	Errors     []string `json:"errors,omitempty"`
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Collector) logf(f string, a ...any) {
	if c.Logf != nil {
		c.Logf(f, a...)
	}
}

func (c *Collector) fetch(ctx context.Context, s Source) ([]Post, error) {
	f := c.Fetchers[s.Kind]
	if f == nil {
		return nil, fmt.Errorf("no reader for kind %s (tg_mtproto needs FEEDS_TG_SESSION + TELEGRAM_APP_ID/HASH)", s.Kind)
	}
	posts, err := f.Fetch(ctx, s)
	for i := range posts {
		posts[i].SourceID = s.ID
	}
	return posts, err
}

// DryRun fetches every enabled source and prints one JSON line per post with the prefilter
// verdict. No database, no AI.
func (c *Collector) DryRun(ctx context.Context, w io.Writer, limit int) Report {
	var rep Report
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, s := range c.Sources {
		if !s.Enabled {
			continue
		}
		if rep.Sources > 0 {
			sleepCtx(ctx, c.SourceDelay)
		}
		rep.Sources++
		posts, err := c.fetch(ctx, s)
		if err != nil {
			rep.Errors = append(rep.Errors, s.ID+": "+err.Error())
			continue
		}
		if limit > 0 && len(posts) > limit {
			posts = posts[len(posts)-limit:]
		}
		rep.Fetched += len(posts)
		for _, p := range posts {
			ok, why := Prefilter(p, s)
			if !ok {
				rep.Skipped++
			}
			text := p.Text
			if utf8.RuneCountInString(text) > 280 {
				text = string([]rune(text)[:280]) + "…"
			}
			_ = enc.Encode(map[string]any{"source": s.ID, "id": p.ExternalID, "url": p.URL, "posted_at": p.PostedAt,
				"links": p.Links, "prefilter": ok, "skip_reason": why, "text": text})
		}
	}
	return rep
}

// Run: fetch every enabled source, store new posts, structure waiting posts with AI, expire old
// items and post newly published ones to the channel.
func (c *Collector) Run(ctx context.Context) (Report, error) {
	var rep Report
	var sources []Source
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		sources, err = SyncSources(ctx, tx, c.Sources)
		return err
	}); err != nil {
		return rep, fmt.Errorf("sync sources: %w", err)
	}
	now := c.now()
	first := true
	for _, s := range sources {
		if !s.Enabled {
			continue
		}
		if !first {
			sleepCtx(ctx, c.SourceDelay)
		}
		first = false
		rep.Sources++
		posts, fetchErr := c.fetch(ctx, s)
		newPosts := 0
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			for _, p := range posts {
				status, note := "new", ""
				if p.PostedAt != nil && c.MaxAgeDays > 0 && now.Sub(*p.PostedAt) > time.Duration(c.MaxAgeDays)*24*time.Hour {
					status, note = "skipped", "older than FEEDS_MAX_POST_AGE_DAYS"
				} else if ok, why := Prefilter(p, s); !ok {
					status, note = "skipped", why
				}
				_, inserted, err := InsertPost(ctx, tx, p, status, note)
				if err != nil {
					return err
				}
				if inserted {
					newPosts++
					if status == "skipped" {
						rep.Skipped++
					}
				}
			}
			return MarkRun(ctx, tx, s.ID, newPosts, fetchErr)
		})
		rep.Fetched += len(posts)
		rep.New += newPosts
		if fetchErr != nil {
			rep.Errors = append(rep.Errors, s.ID+": "+fetchErr.Error())
			c.logf("feeds: %s: %v", s.ID, fetchErr)
		}
		if err != nil {
			return rep, fmt.Errorf("store %s: %w", s.ID, err)
		}
	}

	if c.AI != nil && c.AI.Key != "" {
		if err := c.process(ctx, &rep); err != nil {
			return rep, err
		}
	}

	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		n, err := ExpireItems(ctx, tx, c.now(), c.UndatedTTL)
		rep.Expired = n
		return err
	}); err != nil {
		return rep, fmt.Errorf("expire: %w", err)
	}
	if c.Channel != "" && c.PostChannel != nil {
		n, err := c.postChannel(ctx)
		rep.Channel = n
		if err != nil {
			rep.Errors = append(rep.Errors, "channel: "+err.Error())
		}
	}
	return rep, nil
}

func (c *Collector) process(ctx context.Context, rep *Report) error {
	var waiting []storedPost
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		waiting, err = waitingPosts(ctx, tx, max(c.MaxAIPerRun, 1))
		return err
	}); err != nil {
		return err
	}
	for _, sp := range waiting {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var tokens int
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			_, tokens, _, err = UsageToday(ctx, tx, c.now())
			return err
		}); err != nil {
			return err
		}
		if c.DailyTokens > 0 && tokens >= c.DailyTokens {
			rep.BudgetHit = true
			c.logf("feeds: daily AI budget reached (%d tokens), %d posts wait for tomorrow", tokens, len(waiting))
			return nil
		}
		if err := c.processOne(ctx, sp, rep); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) processOne(ctx context.Context, sp storedPost, rep *Report) error {
	now := c.now()
	res, usage, aiErr := c.AI.Classify(ctx, sp.Post, sp.Source, now)
	rep.AICalls++
	rep.Tokens += usage.Total()
	c.logf("feeds: ai post %d (%s): prompt=%d completion=%d err=%v", sp.ID, sp.Source.ID, usage.Prompt, usage.Completion, aiErr)
	return db.Tx(ctx, func(tx pgx.Tx) error {
		if err := addUsage(ctx, tx, now, usage, aiErr != nil); err != nil {
			return err
		}
		if errors.Is(aiErr, ErrUpstream) {
			rep.Failed++
			return nil // stays "new": retried next run, within the budget
		}
		if aiErr != nil {
			rep.Failed++
			return setPost(ctx, tx, sp.ID, "failed", aiErr.Error())
		}
		r, err := Validate(res, sp.Post, now)
		if errors.Is(err, ErrIrrelevant) {
			rep.Irrelevant++
			return setPost(ctx, tx, sp.ID, "irrelevant", res.Reason)
		}
		if err != nil {
			rep.Failed++
			return setPost(ctx, tx, sp.ID, "failed", "validation: "+err.Error())
		}
		hash := Simhash(sp.Text)
		dup, why, err := Duplicate(ctx, tx, URLKey(r.ApplyURL), NormTitle(firstNonEmpty(r.TitleRU, r.TitleKK)), r.Deadline, hash, len(strings.Fields(sp.Text)))
		if err != nil {
			return err
		}
		if dup != 0 {
			rep.Duplicates++
			if err := addExtraSource(ctx, tx, dup, ExtraSource{SourceID: sp.Source.ID, URL: sp.URL}); err != nil {
				return err
			}
			return setPost(ctx, tx, sp.ID, "duplicate", fmt.Sprintf("item %d: %s", dup, why))
		}
		status := Decide(r.Confidence, sp.Source.Trust, c.AutoMin)
		if Expired(r.Deadline, r.EventDate, now, AlmatyToday(now), 0, now) {
			status = StatusExpired
		}
		if _, err := InsertItem(ctx, tx, NewItem{PostID: sp.ID, Source: sp.Source, Result: r, Status: status, Simhash: hash,
			Model: c.AI.Model, Tokens: usage.Total(), PostedAt: sp.PostedAt}); err != nil {
			return err
		}
		switch status {
		case StatusPublished:
			rep.Published++
		case StatusPending:
			rep.Pending++
		}
		return setPost(ctx, tx, sp.ID, "processed", "")
	})
}

// ---------- Telegram channel ----------

// ChannelText is the channel post of a published item (HTML, escaped).
func ChannelText(r Result, sourceName, siteURL string, id int64) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString("<b>" + e(r.TitleKK) + "</b>\n")
	if r.SummaryKK != "" {
		b.WriteString(e(r.SummaryKK) + "\n")
	}
	if r.Deadline != "" {
		b.WriteString("\nМерзімі: " + e(r.Deadline) + "\n")
	}
	if r.ApplyURL != "" {
		b.WriteString("Өтінім: " + e(r.ApplyURL) + "\n")
	}
	b.WriteString("\n" + e(siteURL) + "/opportunity/" + IDPrefix + fmt.Sprint(id) + "\n")
	b.WriteString("<i>ИИ қорытындысы, түпнұсқасын тексеріңіз</i>: " + e(sourceName) + " " + e(r.SourceURL))
	return b.String()
}

func (c *Collector) postChannel(ctx context.Context) (int, error) {
	type row struct {
		id   int64
		data []byte
		name string
	}
	var rows []row
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `select i.id, i.data, s.name from feed_items i join feed_sources s on s.id = i.source_id
			where i.status = 'published' and i.channel_posted_at is null and i.published_at > now() - interval '2 days'
			order by i.published_at limit $1`, max(c.ChannelBatch, 1))
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.id, &r.data, &r.name); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return rs.Err()
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		var res Result
		_ = json.Unmarshal(r.data, &res)
		// mark first: a crash after Telegram accepted the post must not post it twice
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `update feed_items set channel_posted_at = now() where id = $1`, r.id)
			return err
		}); err != nil {
			return n, err
		}
		if err := c.PostChannel(ctx, c.Channel, ChannelText(res, r.name, config.SiteURL(), r.id)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

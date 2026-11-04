package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
)

// SyncSources upserts the registry into feed_sources and returns the sources with the
// database's enabled switch (the file decides it only for a source seen for the first time).
func SyncSources(ctx context.Context, tx pgx.Tx, list []Source) ([]Source, error) {
	out := make([]Source, 0, len(list))
	for _, s := range list {
		var enabled bool
		err := tx.QueryRow(ctx, `insert into feed_sources (id, kind, handle, url, name, lang, category, scope, trust, enabled)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			on conflict (id) do update set kind = excluded.kind, handle = excluded.handle, url = excluded.url, name = excluded.name,
				lang = excluded.lang, category = excluded.category, scope = excluded.scope, trust = excluded.trust, updated_at = now()
			returning enabled`, s.ID, s.Kind, s.Handle, s.URL, s.Name, s.Lang, s.Category, s.Scope, s.Trust, s.Enabled).Scan(&enabled)
		if err != nil {
			return nil, err
		}
		s.Enabled = enabled
		out = append(out, s)
	}
	return out, nil
}

// MarkRun records one source's run.
func MarkRun(ctx context.Context, tx pgx.Tx, id string, newPosts int, runErr error) error {
	if runErr != nil {
		_, err := tx.Exec(ctx, `update feed_sources set last_run_at = now(), last_error = $2, last_new = 0 where id = $1`, id, clip(runErr.Error(), 300))
		return err
	}
	_, err := tx.Exec(ctx, `update feed_sources set last_run_at = now(), last_ok_at = now(), last_error = null, last_new = $2 where id = $1`, id, newPosts)
	return err
}

// InsertPost stores a post once per (source, external id); inserted is false for a known post.
func InsertPost(ctx context.Context, tx pgx.Tx, p Post, status, note string) (id int64, inserted bool, err error) {
	links, _ := json.Marshal(nonNil(p.Links))
	err = tx.QueryRow(ctx, `insert into feed_posts (source_id, external_id, url, text, links, posted_at, simhash, status, note)
		values ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, nullif($9, ''))
		on conflict (source_id, external_id) do nothing returning id`,
		p.SourceID, p.ExternalID, p.URL, p.Text, string(links), p.PostedAt, int64(Simhash(p.Text)), status, note).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// storedPost is a post waiting for AI, with its source.
type storedPost struct {
	ID int64
	Post
	Source Source
}

// waitingPosts: posts not yet processed, oldest first.
func waitingPosts(ctx context.Context, tx pgx.Tx, limit int) ([]storedPost, error) {
	rows, err := tx.Query(ctx, `select p.id, p.source_id, p.external_id, p.url, p.text, p.links, p.posted_at,
			s.id, s.kind, s.handle, s.url, s.name, s.lang, s.category, s.scope, s.trust
		from feed_posts p join feed_sources s on s.id = p.source_id
		where p.status = 'new' and s.enabled order by p.fetched_at, p.id limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedPost
	for rows.Next() {
		var sp storedPost
		var links []byte
		if err := rows.Scan(&sp.ID, &sp.SourceID, &sp.ExternalID, &sp.URL, &sp.Text, &links, &sp.PostedAt,
			&sp.Source.ID, &sp.Source.Kind, &sp.Source.Handle, &sp.Source.URL, &sp.Source.Name, &sp.Source.Lang,
			&sp.Source.Category, &sp.Source.Scope, &sp.Source.Trust); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(links, &sp.Links)
		out = append(out, sp)
	}
	return out, rows.Err()
}

func setPost(ctx context.Context, tx pgx.Tx, id int64, status, note string) error {
	_, err := tx.Exec(ctx, `update feed_posts set status = $2, note = nullif($3, '') where id = $1`, id, status, clip(note, 300))
	return err
}

// ---------- AI usage ----------

// UsageToday returns today's (Asia/Almaty) calls and tokens.
func UsageToday(ctx context.Context, q pgx.Tx, now time.Time) (calls, tokens, failures int, err error) {
	err = q.QueryRow(ctx, `select coalesce(sum(calls), 0), coalesce(sum(prompt_tokens + completion_tokens), 0), coalesce(sum(failures), 0)
		from feed_ai_usage where day = $1::date`, AlmatyToday(now)).Scan(&calls, &tokens, &failures)
	return
}

func addUsage(ctx context.Context, tx pgx.Tx, now time.Time, u Usage, failed bool) error {
	f := 0
	if failed {
		f = 1
	}
	_, err := tx.Exec(ctx, `insert into feed_ai_usage (day, calls, prompt_tokens, completion_tokens, failures) values ($1::date, 1, $2, $3, $4)
		on conflict (day) do update set calls = feed_ai_usage.calls + 1, prompt_tokens = feed_ai_usage.prompt_tokens + excluded.prompt_tokens,
			completion_tokens = feed_ai_usage.completion_tokens + excluded.completion_tokens, failures = feed_ai_usage.failures + excluded.failures`,
		AlmatyToday(now), u.Prompt, u.Completion, f)
	return err
}

// ---------- dedupe ----------

// Duplicate finds an existing item for the same opportunity: the same apply link, the same
// normalized title with the same deadline, or a near-identical post text (simhash). Items of
// the last 120 days count; rejected ones too (a rejected post must not come back via another
// channel).
func Duplicate(ctx context.Context, tx pgx.Tx, urlKey, titleNorm, deadline string, simhash uint64, words int) (int64, string, error) {
	var id int64
	if urlKey != "" {
		err := tx.QueryRow(ctx, `select id from feed_items where canonical_url = $1 and created_at > now() - interval '120 days' order by id limit 1`, urlKey).Scan(&id)
		if err == nil {
			return id, "same link", nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, "", err
		}
	}
	if deadline != "" && titleNorm != "" {
		err := tx.QueryRow(ctx, `select id from feed_items where title_norm = $1 and deadline = $2::date order by id limit 1`, titleNorm, deadline).Scan(&id)
		if err == nil {
			return id, "same title and deadline", nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, "", err
		}
	}
	if simhash == 0 || words < 12 {
		return 0, "", nil
	}
	rows, err := tx.Query(ctx, `select id, simhash from feed_items where simhash <> 0 and created_at > now() - interval '120 days' order by id`)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var h int64
		if err := rows.Scan(&id, &h); err != nil {
			return 0, "", err
		}
		if Hamming(uint64(h), simhash) <= NearDuplicate {
			return id, "near-identical text", nil
		}
	}
	return 0, "", rows.Err()
}

// ExtraSource is another post announcing an item.
type ExtraSource struct {
	SourceID string `json:"source_id"`
	URL      string `json:"url"`
}

func addExtraSource(ctx context.Context, tx pgx.Tx, itemID int64, e ExtraSource) error {
	raw, _ := json.Marshal([]ExtraSource{e})
	_, err := tx.Exec(ctx, `update feed_items set extra_sources = extra_sources || $2::jsonb
		where id = $1 and not extra_sources @> $2::jsonb`, itemID, string(raw))
	return err
}

// ---------- items ----------

// NewItem is what the collector stores for a validated post.
type NewItem struct {
	PostID   int64
	Source   Source
	Result   Result
	Status   string
	Simhash  uint64
	Model    string
	Tokens   int
	PostedAt *time.Time
}

// QueuePayload is the moderation queue payload of a pending item (flat, so the admin queue
// lists every field; the admin may edit the text fields before approving).
func QueuePayload(itemID int64, s Source, r Result) map[string]any {
	return map[string]any{
		"feed_item_id": itemID, "source": s.Name, "trust": s.Trust, "category": r.Category,
		"title_kk": r.TitleKK, "title_ru": r.TitleRU, "summary_kk": r.SummaryKK, "summary_ru": r.SummaryRU,
		"organizer": r.Organizer, "deadline": r.Deadline, "event_date": r.EventDate, "eligibility": r.Eligibility,
		"funding": r.Funding, "online": r.Online, "city": r.City, "country": r.Country,
		"apply_url": r.ApplyURL, "source_url": r.SourceURL, "confidence": r.Confidence, "ai_reason": r.Reason,
		"ai_notes": r.Notes,
	}
}

// InsertItem stores the item; a pending one also enters the moderation queue.
func InsertItem(ctx context.Context, tx pgx.Tx, it NewItem) (int64, error) {
	r := it.Result
	data, _ := json.Marshal(r)
	var published *time.Time
	if it.Status == StatusPublished {
		now := time.Now().UTC()
		published = &now
	}
	var id int64
	err := tx.QueryRow(ctx, `insert into feed_items (post_id, source_id, status, category, data, title_norm, canonical_url,
			deadline, event_date, confidence, simhash, auto, ai_model, ai_tokens, published_at)
		values ($1, $2, $3, $4, $5::jsonb, $6, $7, nullif($8, '')::date, nullif($9, '')::date, $10, $11, $12, $13, $14, $15)
		returning id`, it.PostID, it.Source.ID, it.Status, r.Category, string(data), NormTitle(firstNonEmpty(r.TitleRU, r.TitleKK)),
		URLKey(r.ApplyURL), r.Deadline, r.EventDate, r.Confidence, int64(it.Simhash), it.Status == StatusPublished,
		it.Model, it.Tokens, published).Scan(&id)
	if err != nil {
		return 0, err
	}
	if it.Status == StatusPending {
		sid, err := moderation.Submit(ctx, tx, SubmissionKind, QueuePayload(id, it.Source, r), nil, "")
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `update feed_items set submission_id = $2 where id = $1`, id, sid); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// ExpireItems hides items whose deadline / event date passed (or undated ones older than
// ttlDays); a pending one leaves the moderation queue with an automatic reason.
func ExpireItems(ctx context.Context, tx pgx.Tx, now time.Time, ttlDays int) (int, error) {
	today := AlmatyToday(now)
	rows, err := tx.Query(ctx, `select id, coalesce(deadline::text, ''), coalesce(event_date::text, ''), created_at, submission_id, status
		from feed_items where status in ('pending', 'published')`)
	if err != nil {
		return 0, err
	}
	type row struct {
		id     int64
		sub    *int64
		status string
	}
	var expired []row
	for rows.Next() {
		var r row
		var d, e string
		var created time.Time
		if err := rows.Scan(&r.id, &d, &e, &created, &r.sub, &r.status); err != nil {
			rows.Close()
			return 0, err
		}
		if Expired(d, e, created, today, ttlDays, now) {
			expired = append(expired, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, r := range expired {
		if _, err := tx.Exec(ctx, `update feed_items set status = 'expired' where id = $1`, r.id); err != nil {
			return 0, err
		}
		if r.status == StatusPending && r.sub != nil {
			if _, err := tx.Exec(ctx, `update submissions set status = 'rejected', reason = 'Мерзімі өтті (автоматты) / Срок истёк (автоматически)', reviewed_at = now()
				where id = $1 and status = 'pending'`, *r.sub); err != nil {
				return 0, err
			}
		}
	}
	return len(expired), nil
}

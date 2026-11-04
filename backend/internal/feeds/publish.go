package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/content"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
)

// Source marker of feed records in /api/content.
const ContentSource = "feed"

// AINote is the label every published feed item carries.
var AINote = content.Bi{KK: "ИИ қорытындысы, түпнұсқасын тексеріңіз", RU: "Краткое изложение ИИ, проверьте оригинал"}

func init() {
	content.RegisterExtra("feeds", func(ctx context.Context) (map[string][]content.Record, error) {
		if !features.Enabled(Feature) {
			return nil, nil
		}
		if _, err := db.Pool(ctx); errors.Is(err, db.ErrNoDB) {
			return nil, nil
		}
		recs, err := Published(ctx)
		if err != nil {
			return nil, err
		}
		return map[string][]content.Record{"opportunities": recs}, nil
	})
	moderation.OnApprove(SubmissionKind, approve)
	moderation.OnReject(SubmissionKind, reject)
	moderation.Editor(SubmissionKind, edit)
}

// FeedMeta is the extra block of a feed record: where it came from and what the AI read.
type FeedMeta struct {
	Category     string       `json:"category"`
	SourceID     string       `json:"source_id"`
	SourceName   string       `json:"source_name"`
	OriginalURL  string       `json:"original_url"`
	ApplyURL     string       `json:"apply_url"`
	EventDate    string       `json:"event_date"`
	Online       bool         `json:"online"`
	Eligibility  string       `json:"eligibility"`
	Funding      string       `json:"funding"`
	Country      string       `json:"country"`
	Reviewed     bool         `json:"reviewed"` // an admin approved it (false: published automatically)
	Note         content.Bi   `json:"note"`
	ExtraSources []ExtraLabel `json:"extra_sources"`
	Summary      content.Bi   `json:"summary"`
	Confidence   float64      `json:"confidence"`
}

// ExtraLabel is another channel that announced the same opportunity.
type ExtraLabel struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// record is a feed item in the opportunity shape plus source and feed meta.
type record struct {
	*content.Opportunity
	Source string   `json:"source"`
	Feed   FeedMeta `json:"feed"`
}

// Record builds the /api/content opportunity for a published item.
func Record(id int64, r Result, sourceID, sourceName string, publishedAt time.Time, auto bool, extras []ExtraLabel) (json.RawMessage, string) {
	posted := AlmatyToday(publishedAt)
	var deadline *string
	if r.Deadline != "" {
		d := r.Deadline
		deadline = &d
	}
	ru := func(s, fb string) string {
		if s == "" {
			return fb
		}
		return s
	}
	o := &content.Opportunity{
		ID:        IDPrefix + strconv.FormatInt(id, 10),
		Type:      OppType(r.Category),
		Funded:    r.Funding == "fully" || r.Funding == "partly",
		Title:     content.Bi{KK: r.TitleKK, RU: ru(r.TitleRU, r.TitleKK)},
		Org:       content.Bi{KK: r.Organizer, RU: r.Organizer},
		Spec:      []string{"all"},
		Desc:      content.Bi{KK: r.SummaryKK, RU: ru(r.SummaryRU, r.SummaryKK)},
		Docs:      []content.Bi{},
		Steps:     []content.Bi{},
		SourceURL: r.SourceURL,
		PostedAt:  &posted,
		Contact:   content.Contact{Site: httpsOnly(r.ApplyURL)},
		Deadline:  deadline,
		City:      CityCode(r.City, r.Online),
	}
	if extras == nil {
		extras = []ExtraLabel{}
	}
	raw, _ := json.Marshal(record{o, ContentSource, FeedMeta{
		Category: r.Category, SourceID: sourceID, SourceName: sourceName, OriginalURL: r.SourceURL, ApplyURL: r.ApplyURL,
		EventDate: r.EventDate, Online: r.Online, Eligibility: r.Eligibility, Funding: r.Funding, Country: r.Country,
		Reviewed: !auto, Note: AINote, ExtraSources: extras, Summary: o.Desc, Confidence: r.Confidence,
	}})
	return raw, r.Deadline
}

func httpsOnly(u string) string {
	if strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}

// Published returns the live feed records, newest first.
func Published(ctx context.Context) ([]content.Record, error) {
	var out []content.Record
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		names := map[string]string{}
		srows, err := tx.Query(ctx, `select id, name from feed_sources`)
		if err != nil {
			return err
		}
		for srows.Next() {
			var id, name string
			if err := srows.Scan(&id, &name); err != nil {
				srows.Close()
				return err
			}
			names[id] = name
		}
		srows.Close()
		rows, err := tx.Query(ctx, `select id, data, source_id, coalesce(published_at, created_at), auto, extra_sources
			from feed_items where status = 'published' order by published_at desc nulls last, id desc limit 500`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var data, extra []byte
			var sid string
			var at time.Time
			var auto bool
			if err := rows.Scan(&id, &data, &sid, &at, &auto, &extra); err != nil {
				return err
			}
			var r Result
			if err := json.Unmarshal(data, &r); err != nil {
				log.Printf("feeds: item %d: %v", id, err)
				continue
			}
			var es []ExtraSource
			_ = json.Unmarshal(extra, &es)
			labels := []ExtraLabel{}
			for _, e := range es {
				labels = append(labels, ExtraLabel{Name: names[e.SourceID], URL: e.URL})
			}
			raw, deadline := Record(id, r, sid, names[sid], at, auto, labels)
			out = append(out, content.RawRecord(raw, deadline))
		}
		return rows.Err()
	})
	return out, err
}

// ---------- moderation hooks ----------

var msgExpired = httpx.Msg{KK: "Бұл мүмкіндіктің мерзімі өтіп кеткен.", RU: "Срок этой возможности уже истёк."}

func itemID(p json.RawMessage) (int64, error) {
	var v struct {
		ID int64 `json:"feed_item_id"`
	}
	if err := json.Unmarshal(p, &v); err != nil || v.ID <= 0 {
		return 0, errors.New("feeds: payload without feed_item_id")
	}
	return v.ID, nil
}

// editable fields of the queue payload, copied into the item on approval
var editable = []string{"category", "title_kk", "title_ru", "summary_kk", "summary_ru", "organizer", "deadline",
	"event_date", "eligibility", "funding", "apply_url"}

func approve(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id, err := itemID(s.Payload)
	if err != nil {
		return err
	}
	var data []byte
	var status string
	if err := tx.QueryRow(ctx, `select data, status from feed_items where id = $1 for update`, id).Scan(&data, &status); err != nil {
		return err
	}
	var r map[string]any
	_ = json.Unmarshal(data, &r)
	var p map[string]any
	_ = json.Unmarshal(s.Payload, &p)
	for _, k := range editable {
		if v, ok := p[k]; ok {
			r[k] = v
		}
	}
	str := func(k string) string { v, _ := r[k].(string); return v }
	today := AlmatyToday(time.Now())
	if d := str("deadline"); d != "" && d < today {
		return &moderation.Refusal{Code: "expired", Msg: msgExpired}
	}
	raw, _ := json.Marshal(r)
	title := firstNonEmpty(str("title_ru"), str("title_kk"))
	_, err = tx.Exec(ctx, `update feed_items set status = 'published', published_at = now(), auto = false, data = $2::jsonb,
			category = $3, title_norm = $4, canonical_url = $5, deadline = nullif($6, '')::date, event_date = nullif($7, '')::date
		where id = $1`, id, string(raw), str("category"), NormTitle(title), URLKey(str("apply_url")), str("deadline"), str("event_date"))
	return err
}

func reject(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id, err := itemID(s.Payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `update feed_items set status = 'rejected' where id = $1`, id)
	return err
}

// fieldError: the field name is the error text (moderation shows it).
type fieldError string

func (e fieldError) Error() string { return string(e) }

// edit validates the admin's corrections of a pending item: plain text fields, a category from
// the list, dates YYYY-MM-DD not in the past, an http(s) apply link. Other payload keys (source,
// confidence, the AI's notes) cannot be changed.
func edit(old, in map[string]any, today string) (map[string]any, error) {
	next := map[string]any{}
	for k, v := range old {
		next[k] = v
	}
	limits := map[string]int{"title_kk": 160, "title_ru": 160, "summary_kk": 700, "summary_ru": 700, "organizer": 160, "eligibility": 400}
	for _, k := range editable {
		v, ok := in[k]
		if !ok {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			return nil, fieldError(k)
		}
		s = strings.TrimSpace(s)
		switch k {
		case "category":
			if !slices.Contains(Categories, s) {
				return nil, fieldError(k)
			}
		case "funding":
			if !slices.Contains(Fundings, s) {
				return nil, fieldError(k)
			}
		case "deadline", "event_date":
			if s != "" {
				if _, err := time.Parse("2006-01-02", s); err != nil || (k == "deadline" && s < today) {
					return nil, fieldError(k)
				}
			}
		case "apply_url":
			if s != "" {
				u, err := url.Parse(s)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return nil, fieldError(k)
				}
			}
		default:
			if clip(s, limits[k]) != s {
				return nil, fieldError(k)
			}
		}
		next[k] = s
	}
	if t, _ := next["title_kk"].(string); t == "" {
		return nil, fieldError("title_kk")
	}
	return next, nil
}

// Unpublish takes a published item off the site (admin); it stays in the table as rejected.
func Unpublish(ctx context.Context, tx pgx.Tx, id, actor int64) error {
	tag, err := tx.Exec(ctx, `update feed_items set status = 'rejected', decided_by = $2 where id = $1 and status = 'published'`, id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, meta) values ($1, 'feed_unpublish', jsonb_build_object('feed_item', $2::bigint))`, actor, id)
	return err
}

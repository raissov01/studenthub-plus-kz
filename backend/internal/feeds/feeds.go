// Package feeds is the automatic opportunity feed (docs/feeds.md): posts from Telegram channels
// and websites are collected (cmd/feeds-collect), cheaply prefiltered, structured by one AI call,
// validated against the post text, deduplicated and either sent to the moderation queue (kind
// feed_item) or, for trusted sources above FEEDS_AUTOPUBLISH_MIN_CONFIDENCE, published at once.
// Published items enter /api/content as opportunities "feed-<id>" (source = "feed"), so search,
// the opportunity lists, the home carousel and calendar deadlines show them like any other record.
// Everything is behind FEATURE_FEEDS.
//
// Readers live in subpackages (telegram: public t.me/s pages; telegram/mtproto: optional user
// session; web: RSS/Atom and HTML lists) and are wired by cmd/feeds-collect, so the API binary
// never links the MTProto client.
package feeds

import (
	"context"
	"time"
)

// Feature is the switch name (FEATURE_FEEDS).
const Feature = "feeds"

// SubmissionKind is the moderation queue kind of a pending item.
const SubmissionKind = "feed_item"

// IDPrefix is the /api/content id prefix of published items.
const IDPrefix = "feed-"

// Source kinds.
const (
	KindTGPublic  = "tg_public"
	KindTGMTProto = "tg_mtproto"
	KindRSS       = "rss"
	KindHTML      = "html"
)

// Categories the AI may return.
var Categories = []string{"hackathon", "startup", "grant", "scholarship", "internship", "mobility", "exchange",
	"competition", "conference", "volunteer", "course", "other"}

// Source is one registry entry (backend/feeds/sources.yaml).
type Source struct {
	ID       string `yaml:"id" json:"id"`
	Kind     string `yaml:"kind" json:"kind"`
	Handle   string `yaml:"handle,omitempty" json:"handle"`
	URL      string `yaml:"url,omitempty" json:"url"`
	Name     string `yaml:"name" json:"name"`
	Lang     string `yaml:"lang" json:"lang"`
	Category string `yaml:"category" json:"category"`
	Scope    string `yaml:"scope" json:"scope"`
	Trust    string `yaml:"trust" json:"trust"`
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	// tg_mtproto without a public username: the channel id from the reading account's dialogs
	ChannelID int64     `yaml:"channel_id,omitempty" json:"channel_id,omitempty"`
	Account   string    `yaml:"account,omitempty" json:"account,omitempty"` // tg_mtproto: which Telegram account reads it (documentation only)
	Note      string    `yaml:"note,omitempty" json:"note,omitempty"`
	Select    Selectors `yaml:"select,omitempty" json:"select,omitempty"` // html only
	// Relevant marks a source whose every post is on topic (a scholarship site); the keyword
	// prefilter is skipped for it.
	Relevant bool `yaml:"relevant,omitempty" json:"relevant,omitempty"`
}

// Selectors pick items out of an HTML list page (see web.ParseHTMLList for the syntax).
type Selectors struct {
	Item  string `yaml:"item,omitempty" json:"item,omitempty"`   // one element per post
	Title string `yaml:"title,omitempty" json:"title,omitempty"` // inside item
	Link  string `yaml:"link,omitempty" json:"link,omitempty"`   // inside item; its href, else the first a[href]
	Date  string `yaml:"date,omitempty" json:"date,omitempty"`   // inside item; datetime attr or text
	Text  string `yaml:"text,omitempty" json:"text,omitempty"`   // inside item; default: the whole item text
}

// Post is one collected post, before any AI.
type Post struct {
	SourceID   string     `json:"source_id"`
	ExternalID string     `json:"external_id"`
	URL        string     `json:"url"`
	Text       string     `json:"text"`
	Links      []string   `json:"links"`
	PostedAt   *time.Time `json:"posted_at,omitempty"`
}

// Fetcher reads the latest posts of a source (newest last or first; order does not matter).
type Fetcher interface {
	Fetch(ctx context.Context, s Source) ([]Post, error)
}

// FetcherFunc adapts a function.
type FetcherFunc func(ctx context.Context, s Source) ([]Post, error)

func (f FetcherFunc) Fetch(ctx context.Context, s Source) ([]Post, error) { return f(ctx, s) }

// AlmatyToday is the date in Asia/Almaty (UTC+5) as YYYY-MM-DD.
func AlmatyToday(now time.Time) string { return now.UTC().Add(5 * time.Hour).Format("2006-01-02") }

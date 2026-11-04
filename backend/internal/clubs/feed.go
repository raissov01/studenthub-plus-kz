package clubs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
)

// FeedItem is a news item from the opportunity feed (Telegram / RSS) that belongs to a club.
//
// Hook for the feed module: when it classifies an item as category "club" and knows the club,
// it calls AttachFeedItem inside its own transaction (as the connection owner):
//
//	id, created, err := clubs.AttachFeedItem(ctx, tx, clubID, clubs.FeedItem{Ref: "tg:kazatzu_media/812",
//		BodyKK: text, URL: "https://t.me/kazatzu_media/812", Publish: false})
//
// Ref is unique per club, so re-importing the same item is a no-op (created = false). With
// Publish = false the post waits in the shared moderation queue (kind club_post, no author);
// Publish = true is for items the feed already moderated. No dependency on the feed's code.
type FeedItem struct {
	Ref     string // the feed's own stable id, ≤ 200 chars
	BodyKK  string // 3–2000 chars
	BodyRU  string // optional
	URL     string // optional https:// link to the original
	Publish bool
	At      time.Time // original publication time; zero = now
}

var ErrFeedItem = errors.New("clubs: bad feed item")

// AttachFeedItem stores a feed item as club news. Returns the club_posts id.
func AttachFeedItem(ctx context.Context, tx pgx.Tx, clubID int64, it FeedItem) (int64, bool, error) {
	ref := strings.TrimSpace(it.Ref)
	kk, ok1 := cleanText(it.BodyKK, 3, 2000)
	ru, ok2 := cleanText(it.BodyRU, 3, 2000)
	url := strings.TrimSpace(it.URL)
	if ref == "" || len(ref) > 200 || !ok1 || kk == "" || !ok2 || (url != "" && !reHTTPS.MatchString(url)) {
		return 0, false, ErrFeedItem
	}
	var univ, name string
	if err := tx.QueryRow(ctx, `select university, name_kk from clubs where id = $1`, clubID).Scan(&univ, &name); err != nil {
		return 0, false, err
	}
	status := "pending"
	var at any
	if it.Publish {
		status = "published"
		at = time.Now()
		if !it.At.IsZero() {
			at = it.At
		}
	}
	var id int64
	err := tx.QueryRow(ctx, `insert into club_posts (club_id, body_kk, body_ru, link_url, status, source, source_ref, published_at)
		values ($1, $2, nullif($3, ''), nullif($4, ''), $5, 'feed', $6, $7)
		on conflict (club_id, source_ref) do nothing returning id`, clubID, kk, ru, url, status, ref, at).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `select id from club_posts where club_id = $1 and source_ref = $2`, clubID, ref).Scan(&id)
		return id, false, err
	}
	if err != nil {
		return 0, false, err
	}
	if !it.Publish {
		if _, err := moderation.Submit(ctx, tx, KindPost, map[string]any{
			"post_id": id, "club_id": clubID, "club": name, "body_kk": kk, "body_ru": ru, "link_url": url, "source": "feed",
		}, nil, univ); err != nil {
			return 0, false, err
		}
	}
	return id, true, nil
}

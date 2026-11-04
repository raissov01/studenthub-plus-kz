// Package telegram reads public Telegram channels through their web preview
// (https://t.me/s/<username>): no account, no API keys, no joining. Only channels that enabled
// the preview are readable this way; the rest need the optional MTProto reader
// (telegram/mtproto, a user session).
package telegram

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds/htmlx"
	"golang.org/x/net/html"
)

// Public is the t.me/s reader.
type Public struct {
	Web  *feeds.Web
	Base string // https://t.me (tests point it at a local server)
}

// Fetch reads the latest page (about 20 posts) of the channel.
func (p *Public) Fetch(ctx context.Context, s feeds.Source) ([]feeds.Post, error) {
	base := p.Base
	if base == "" {
		base = "https://t.me"
	}
	body, err := p.Web.Get(ctx, base+"/s/"+url.PathEscape(s.Handle))
	if err != nil {
		return nil, err
	}
	posts, err := ParsePreview(body, s.Handle)
	if err != nil {
		return nil, err
	}
	for i := range posts {
		posts[i].SourceID = s.ID
	}
	return posts, nil
}

// ErrNoPreview: the page has no message list (private channel, preview disabled, wrong name).
var ErrNoPreview = fmt.Errorf("no public preview (t.me/s shows no posts)")

// ParsePreview extracts the posts of a t.me/s page. Service messages ("Channel created",
// pinned) and posts without text are skipped; the post id is the number in data-post.
func ParsePreview(body []byte, handle string) ([]feeds.Post, error) {
	doc, err := htmlx.Parse(body)
	if err != nil {
		return nil, err
	}
	msgs := htmlx.FindAll(doc, func(n *html.Node) bool {
		return htmlx.HasClass(n, "tgme_widget_message") && htmlx.Attr(n, "data-post") != ""
	}, true)
	if len(msgs) == 0 {
		if htmlx.First(doc, htmlx.ByClass("tgme_channel_history")) == nil {
			return nil, ErrNoPreview
		}
		return nil, nil
	}
	var out []feeds.Post
	for _, m := range msgs {
		if htmlx.HasClass(m, "service_message") {
			continue
		}
		dp := htmlx.Attr(m, "data-post") // handle/123
		slash := strings.LastIndexByte(dp, '/')
		if slash < 0 {
			continue
		}
		id := dp[slash+1:]
		textNode := htmlx.First(m, func(n *html.Node) bool {
			return htmlx.HasClass(n, "tgme_widget_message_text") && htmlx.HasClass(n, "js-message_text")
		})
		text := ""
		var links []string
		if textNode != nil {
			text = htmlx.Text(textNode)
			links = htmlx.Links(textNode)
		}
		// link preview card and inline buttons carry the application link more often than the text
		for _, cls := range []string{"tgme_widget_message_link_preview", "tgme_widget_message_inline_button"} {
			for _, a := range htmlx.FindAll(m, htmlx.ByClass(cls), true) {
				if h := htmlx.Attr(a, "href"); h != "" {
					links = append(links, h)
				}
				if cls == "tgme_widget_message_link_preview" {
					if t := htmlx.Text(a); t != "" {
						text += "\n\n[link preview] " + t
					}
				}
			}
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		var posted *time.Time
		if tm := htmlx.First(m, func(n *html.Node) bool { return n.Data == "time" && htmlx.Attr(n, "datetime") != "" }); tm != nil {
			if t, err := time.Parse(time.RFC3339, htmlx.Attr(tm, "datetime")); err == nil {
				t = t.UTC()
				posted = &t
			}
		}
		out = append(out, feeds.Post{
			ExternalID: id,
			URL:        "https://t.me/" + dp,
			Text:       text,
			Links:      dedupe(links),
			PostedAt:   posted,
		})
	}
	return out, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ChannelTitle returns the channel title of a t.me/s page ("" when absent).
func ChannelTitle(body []byte) string {
	doc, err := htmlx.Parse(body)
	if err != nil {
		return ""
	}
	if n := htmlx.First(doc, htmlx.ByClass("tgme_channel_info_header_title")); n != nil {
		return htmlx.Text(n)
	}
	return ""
}

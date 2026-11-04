// Package web reads websites for the opportunity feed: RSS 2.0 / Atom feeds and HTML list pages
// with per-source selectors (sources.yaml select:).
package web

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds/htmlx"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

// RSS reads RSS 2.0 and Atom feeds.
type RSS struct{ Web *feeds.Web }

func (r *RSS) Fetch(ctx context.Context, s feeds.Source) ([]feeds.Post, error) {
	body, err := r.Web.Get(ctx, s.URL)
	if err != nil {
		return nil, err
	}
	return ParseFeed(body, s.URL)
}

// HTMLList reads a list page.
type HTMLList struct{ Web *feeds.Web }

func (h *HTMLList) Fetch(ctx context.Context, s feeds.Source) ([]feeds.Post, error) {
	body, err := h.Web.Get(ctx, s.URL)
	if err != nil {
		return nil, err
	}
	return ParseHTMLList(body, s.URL, s.Select)
}

type rssDoc struct {
	XMLName xml.Name
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	Entries []atomEntry `xml:"entry"`
	Items   []rssItem   `xml:"item"` // RSS 1.0 (RDF) puts items at the top level
}

type rssItem struct {
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	GUID       string   `xml:"guid"`
	PubDate    string   `xml:"pubDate"`
	DCDate     string   `xml:"http://purl.org/dc/elements/1.1/ date"`
	Desc       string   `xml:"description"`
	Content    string   `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Categories []string `xml:"category"`
}

type atomEntry struct {
	Title string `xml:"title"`
	ID    string `xml:"id"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
}

var dateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02", "02.01.2006", "2 January 2006", "January 2, 2006"}

// ParseDate tries the usual feed and page date formats.
func ParseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// ParseFeed parses RSS 2.0, RSS 1.0 or Atom. HTML in descriptions becomes plain text; its links
// are kept.
func ParseFeed(body []byte, base string) ([]feeds.Post, error) {
	var d rssDoc
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.CharsetReader = charset.NewReaderLabel
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	var out []feeds.Post
	items := append(d.Channel.Items, d.Items...)
	for _, it := range items {
		html := it.Content
		if strings.TrimSpace(html) == "" {
			html = it.Desc
		}
		text, links := htmlText(html, base)
		link := resolve(base, strings.TrimSpace(it.Link))
		id := strings.TrimSpace(it.GUID)
		if id == "" {
			id = link
		}
		date := ParseDate(it.PubDate)
		if date == nil {
			date = ParseDate(it.DCDate)
		}
		out = append(out, post(id, link, strings.TrimSpace(it.Title), text, links, date))
	}
	for _, e := range d.Entries {
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = resolve(base, l.Href)
				break
			}
		}
		body := e.Content
		if strings.TrimSpace(body) == "" {
			body = e.Summary
		}
		text, links := htmlText(body, base)
		date := ParseDate(e.Published)
		if date == nil {
			date = ParseDate(e.Updated)
		}
		id := strings.TrimSpace(e.ID)
		if id == "" {
			id = link
		}
		out = append(out, post(id, link, strings.TrimSpace(e.Title), text, links, date))
	}
	if len(out) == 0 && d.XMLName.Local != "rss" && d.XMLName.Local != "feed" && d.XMLName.Local != "RDF" {
		return nil, errors.New("not an RSS/Atom document")
	}
	return out, nil
}

func post(id, link, title, text string, links []string, date *time.Time) feeds.Post {
	full := title
	if text != "" && !strings.HasPrefix(text, title) {
		full = strings.TrimSpace(title + "\n\n" + text)
	} else if text != "" {
		full = text
	}
	if link != "" {
		links = append([]string{link}, links...)
	}
	return feeds.Post{ExternalID: shortID(id), URL: link, Text: full, Links: uniq(links), PostedAt: date}
}

// shortID keeps ids short and stable: long guids/URLs are hashed.
func shortID(id string) string {
	if len(id) <= 120 {
		return id
	}
	h := sha1.Sum([]byte(id))
	return "sha1:" + hex.EncodeToString(h[:])
}

func htmlText(s, base string) (string, []string) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	doc, err := html.Parse(strings.NewReader("<div>" + s + "</div>"))
	if err != nil {
		return htmlx.Clean(s), nil
	}
	var links []string
	for _, l := range htmlx.Links(doc) {
		links = append(links, resolve(base, l))
	}
	return htmlx.Text(doc), links
}

func resolve(base, ref string) string {
	if ref == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s != "" && !seen[s] && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ParseHTMLList picks posts out of a list page: sel.Item selects one element per post; inside
// it sel.Title, sel.Link (href; default the first link), sel.Date (datetime attribute or text)
// and sel.Text (default: the item's whole text). The post id is its absolute link.
func ParseHTMLList(body []byte, base string, sel feeds.Selectors) ([]feeds.Post, error) {
	doc, err := htmlx.Parse(body)
	if err != nil {
		return nil, err
	}
	items := htmlx.Select(doc, sel.Item)
	if len(items) == 0 {
		return nil, errors.New("selector " + sel.Item + " matched nothing (page layout changed?)")
	}
	var out []feeds.Post
	for _, it := range items {
		var link string
		if sel.Link != "" {
			if n := htmlx.SelectFirst(it, sel.Link); n != nil {
				link = htmlx.Attr(n, "href")
			}
		}
		if link == "" && it.DataAtom == atom.A {
			link = htmlx.Attr(it, "href")
		}
		if link == "" {
			if ls := htmlx.Links(it); len(ls) > 0 {
				link = ls[0]
			}
		}
		link = resolve(base, link)
		if link == "" {
			continue
		}
		title := ""
		if sel.Title != "" {
			if n := htmlx.SelectFirst(it, sel.Title); n != nil {
				title = htmlx.Text(n)
			}
		}
		text := ""
		if sel.Text != "" {
			if n := htmlx.SelectFirst(it, sel.Text); n != nil {
				text = htmlx.Text(n)
			}
		} else {
			text = htmlx.Text(it)
		}
		var date *time.Time
		if sel.Date != "" {
			if n := htmlx.SelectFirst(it, sel.Date); n != nil {
				if dt := htmlx.Attr(n, "datetime"); dt != "" {
					date = ParseDate(dt)
				} else {
					date = ParseDate(htmlx.Text(n))
				}
			}
		}
		var links []string
		for _, l := range htmlx.Links(it) {
			links = append(links, resolve(base, l))
		}
		out = append(out, post(link, link, title, text, links, date))
	}
	return out, nil
}

package web

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseRSS(t *testing.T) {
	posts, err := ParseFeed(read(t, "rss_sample.xml"), "https://opps.example.org/feed/")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d", len(posts))
	}
	p := posts[0]
	if p.ExternalID != "https://opps.example.org/?p=179857" || p.URL != "https://opps.example.org/summer-research-2027/" {
		t.Errorf("id/url %q %q", p.ExternalID, p.URL)
	}
	if !strings.HasPrefix(p.Text, "Summer Research Internship Program 2027\n\nDeadline: November 30, 2026") {
		t.Errorf("text %q", p.Text)
	}
	if !slices.Contains(p.Links, "https://apply.example.org/csr") || p.Links[0] != p.URL {
		t.Errorf("links %v", p.Links)
	}
	if p.PostedAt == nil || p.PostedAt.Format("2006-01-02") != "2026-11-04" {
		t.Errorf("date %v", p.PostedAt)
	}
	q := posts[1]
	if q.ExternalID != "https://opps.example.org/fellowship-2027/" || q.PostedAt == nil || !strings.Contains(q.Text, "Fellowship & Grant 2027\n\nFully funded.\nOpen to all") {
		t.Errorf("second %+v", q)
	}
}

func TestParseAtom(t *testing.T) {
	posts, err := ParseFeed(read(t, "atom_sample.xml"), "https://example.org/feed.atom")
	if err != nil || len(posts) != 1 {
		t.Fatalf("%v %v", posts, err)
	}
	p := posts[0]
	if p.URL != "https://example.org/events/42" || p.ExternalID != "tag:example.org,2026:42" {
		t.Errorf("%+v", p)
	}
	if !slices.Contains(p.Links, "https://reg.example.org/42") || !strings.Contains(p.Text, "Registration until 25.11.2026") {
		t.Errorf("%+v", p)
	}
	if p.PostedAt == nil || p.PostedAt.UTC().Hour() != 4 {
		t.Errorf("date %v", p.PostedAt)
	}
}

func TestParseFeedRejectsHTML(t *testing.T) {
	if _, err := ParseFeed([]byte("<html><body>Cloudflare</body></html>"), "https://x.example"); err == nil {
		t.Fatal("an HTML page must not parse as a feed")
	}
}

func TestParseHTMLList(t *testing.T) {
	sel := feeds.Selectors{Item: "div.news-list div.item", Title: ".title", Link: "a.image", Date: ".date", Text: ".in-blk"}
	posts, err := ParseHTMLList(read(t, "html_list_sample.html"), "https://enic.example.kz/novosti", sel)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d: %+v", len(posts), posts)
	}
	p := posts[0]
	if p.URL != "https://enic.example.kz/event/292" || p.ExternalID != p.URL {
		t.Errorf("url %q", p.URL)
	}
	if !strings.HasPrefix(p.Text, "Erasmus+ academic mobility call\n\n") || !strings.Contains(p.Text, "until 15 December") {
		t.Errorf("text %q", p.Text)
	}
	if p.PostedAt == nil || p.PostedAt.Format("2006-01-02") != "2026-09-30" {
		t.Errorf("date %v", p.PostedAt)
	}
	if posts[1].URL != "https://other.example.kz/event/291" || posts[1].PostedAt != nil {
		t.Errorf("second %+v", posts[1])
	}
	if _, err := ParseHTMLList(read(t, "html_list_sample.html"), "https://x", feeds.Selectors{Item: "article.post"}); err == nil {
		t.Error("a selector that matches nothing is an error (layout changed)")
	}
}

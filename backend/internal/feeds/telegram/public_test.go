package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
)

func TestParsePreview(t *testing.T) {
	b, err := os.ReadFile("testdata/tme_s_sample.html")
	if err != nil {
		t.Fatal(err)
	}
	posts, err := ParsePreview(b, "example_opps")
	if err != nil {
		t.Fatal(err)
	}
	// service message and the photo-only post are skipped
	if len(posts) != 2 {
		t.Fatalf("got %d posts, want 2: %+v", len(posts), posts)
	}
	p := posts[0]
	if p.ExternalID != "122" || p.URL != "https://t.me/example_opps/122" {
		t.Errorf("id/url: %q %q", p.ExternalID, p.URL)
	}
	if p.PostedAt == nil || p.PostedAt.Format("2006-01-02T15:04") != "2026-11-04T11:41" {
		t.Errorf("date: %v", p.PostedAt)
	}
	if !strings.Contains(p.Text, "Startup Cup 2026: чемпионат стартапов в Алматы\n\n28 ноября") || !strings.Contains(p.Text, "Приём заявок до 20 ноября") {
		t.Errorf("text with line breaks: %q", p.Text)
	}
	if !slices.Contains(p.Links, "https://example.org/apply?slug=cup-2026") || slices.Contains(p.Links, "?q=%23Startups") {
		t.Errorf("links: %v", p.Links)
	}
	q := posts[1]
	if strings.Contains(q.Text, "чемпионат стартапов\n") || !strings.HasPrefix(q.Text, "Erasmus+ grant for master students") {
		t.Errorf("reply quote must not leak into the text: %q", q.Text)
	}
	for _, want := range []string{"https://erasmus.example.eu/call-2027", "https://erasmus.example.eu/apply"} {
		if !slices.Contains(q.Links, want) {
			t.Errorf("missing %s in %v", want, q.Links)
		}
	}
	if !strings.Contains(q.Text, "[link preview] Erasmus+") {
		t.Errorf("link preview text: %q", q.Text)
	}
	if ChannelTitle(b) != "Example Opportunities" {
		t.Errorf("title %q", ChannelTitle(b))
	}
}

func TestParsePreviewWithoutHistory(t *testing.T) {
	if _, err := ParsePreview([]byte(`<html><body><div class="tgme_page_title">Private</div></body></html>`), "x"); err != ErrNoPreview {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchUsesUserAgentAndPath(t *testing.T) {
	b, _ := os.ReadFile("testdata/tme_s_sample.html")
	var ua, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, path = r.UserAgent(), r.URL.Path
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	w := feeds.NewWeb()
	w.MinGap, w.IgnoreRobots = 0, true
	posts, err := (&Public{Web: w, Base: srv.URL}).Fetch(context.Background(), feeds.Source{ID: "tg-x", Handle: "example_opps"})
	if err != nil || len(posts) != 2 || posts[0].SourceID != "tg-x" {
		t.Fatalf("posts %v err %v", posts, err)
	}
	if path != "/s/example_opps" || !strings.HasPrefix(ua, "StudentHubBot/1.0") {
		t.Errorf("path %q ua %q", path, ua)
	}
}

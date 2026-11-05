package mtproto

import (
	"context"
	"slices"
	"testing"

	"github.com/gotd/td/session"
	"github.com/gotd/td/tg"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
)

func TestEntityLinksUseUTF16Offsets(t *testing.T) {
	text := "🎓 Грант: example.org/apply и сайт"
	// "🎓" is 2 UTF-16 units; "🎓 Грант: " = 2+1+6+1 = 10 units; "example.org/apply" = 17
	ents := []tg.MessageEntityClass{
		&tg.MessageEntityURL{Offset: 10, Length: 17},
		&tg.MessageEntityTextURL{Offset: 30, Length: 4, URL: "https://site.example.kz/"},
		&tg.MessageEntityURL{Offset: 100, Length: 5}, // out of range: ignored
	}
	got := EntityLinks(text, ents)
	if !slices.Equal(got, []string{"https://example.org/apply", "https://site.example.kz/"}) {
		t.Errorf("%v", got)
	}
}

func TestPostsPermalinks(t *testing.T) {
	msgs := []tg.MessageClass{
		&tg.Message{ID: 5, Message: "Стажировка для студентов", Date: 1793800000,
			ReplyMarkup: &tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: []tg.KeyboardButtonClass{&tg.KeyboardButtonURL{Text: "Apply", URL: "https://jobs.example.kz/1"}}}}}},
		&tg.Message{ID: 6, Message: "   "},
		&tg.MessageService{ID: 7},
	}
	posts := Posts(msgs, feeds.Source{ID: "tg-x"}, &tg.Channel{ID: 1698158864})
	if len(posts) != 1 || posts[0].URL != "https://t.me/c/1698158864/5" || posts[0].Links[0] != "https://jobs.example.kz/1" || posts[0].PostedAt == nil {
		t.Fatalf("%+v", posts)
	}
	posts = Posts(msgs[:1], feeds.Source{ID: "tg-x"}, &tg.Channel{ID: 1, Username: "open_chan"})
	if posts[0].URL != "https://t.me/open_chan/5" {
		t.Errorf("%s", posts[0].URL)
	}
}

func TestMemStorageNeverTouchesDisk(t *testing.T) {
	m := &memStorage{}
	if _, err := m.LoadSession(context.Background()); err != session.ErrNotFound {
		t.Errorf("empty: %v", err)
	}
	_ = m.StoreSession(context.Background(), []byte("x"))
	if b, _ := m.LoadSession(context.Background()); string(b) != "x" {
		t.Errorf("%q", b)
	}
}

func TestNewNeedsEverything(t *testing.T) {
	if _, err := New("", 1, "h"); err != ErrNotConfigured {
		t.Error(err)
	}
	if _, err := New("/nonexistent/session", 1, "h"); err == nil {
		t.Error("missing file accepted")
	}
}

// Package mtproto is the optional Telegram reader for channels that have no public web preview.
// It logs in with an existing USER session (gotd/td, the same session files as the tools in
// ~/my-new_project/backend: cmd/tg-resolve-chan, cmd/tg-chan-read) and only reads: it resolves
// a public username (contacts.resolveUsername, which does not join the channel) or finds a
// channel the account already follows in its dialog list, then reads messages.getHistory.
// It never joins, subscribes, sends or marks anything as read.
//
// The session file is a credential (it IS the logged-in account): keep it 0600 outside git; it
// is read once into memory and never written back or logged. Only cmd/feeds-collect links this
// package, so the API binary does not carry the MTProto client.
package mtproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
)

// Reader reads tg_mtproto sources with one session.
type Reader struct {
	AppID   int
	AppHash string
	Session []byte // the session file's bytes (never logged)
	Limit   int    // messages per channel (default 30)
	MaxWait time.Duration

	mu sync.Mutex
}

// ErrNotConfigured: FEEDS_TG_SESSION / TELEGRAM_APP_ID / TELEGRAM_APP_HASH missing.
var ErrNotConfigured = errors.New("mtproto reader is not configured")

// New reads the session file; path "/dev/stdin" (or "-") reads standard input, which lets the
// host timer pass a root-only file into the container without mounting it.
func New(path string, appID int, appHash string) (*Reader, error) {
	if path == "" || appID == 0 || appHash == "" {
		return nil, ErrNotConfigured
	}
	var b []byte
	var err error
	if path == "-" || path == "/dev/stdin" {
		b, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read session: %w", errors.Unwrap(err))
	}
	if len(b) == 0 {
		return nil, errors.New("session file is empty")
	}
	return &Reader{AppID: appID, AppHash: appHash, Session: b, Limit: 30, MaxWait: 60 * time.Second}, nil
}

// memStorage hands gotd the session and keeps any refreshed copy in memory only, so the file
// on disk is never modified (and a read-only mount works).
type memStorage struct {
	mu   sync.Mutex
	data []byte
}

func (m *memStorage) LoadSession(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.data) == 0 {
		return nil, session.ErrNotFound
	}
	return append([]byte(nil), m.data...), nil
}

func (m *memStorage) StoreSession(_ context.Context, data []byte) error {
	m.mu.Lock()
	m.data = append([]byte(nil), data...)
	m.mu.Unlock()
	return nil
}

// Fetch reads the latest messages of one channel.
func (r *Reader) Fetch(ctx context.Context, s feeds.Source) ([]feeds.Post, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	client := telegram.NewClient(r.AppID, r.AppHash, telegram.Options{
		SessionStorage: &memStorage{data: r.Session},
		NoUpdates:      true,
	})
	var out []feeds.Post
	err := client.Run(ctx, func(ctx context.Context) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized {
			return errors.New("session is not authorized (log in again with the tools in my-new_project)")
		}
		api := client.API()
		ch, err := r.channel(ctx, api, s)
		if err != nil {
			return err
		}
		peer := &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
		limit := r.Limit
		if limit <= 0 {
			limit = 30
		}
		var hist tg.MessagesMessagesClass
		for attempt := 0; ; attempt++ {
			hist, err = api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: limit})
			if d, ok := tgerr.AsFloodWait(err); ok && attempt == 0 && d <= r.MaxWait {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(d + time.Second):
				}
				continue
			}
			break
		}
		if err != nil {
			return err
		}
		out = Posts(messages(hist), s, ch)
		return nil
	})
	return out, err
}

func (r *Reader) channel(ctx context.Context, api *tg.Client, s feeds.Source) (*tg.Channel, error) {
	if s.Handle != "" {
		res, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: s.Handle})
		if err != nil {
			return nil, fmt.Errorf("resolve @%s: %w", s.Handle, err)
		}
		for _, c := range res.Chats {
			if ch, ok := c.(*tg.Channel); ok && strings.EqualFold(username(ch), s.Handle) {
				return ch, nil
			}
		}
		return nil, fmt.Errorf("@%s is not a channel", s.Handle)
	}
	// no username: the account must already follow it; look it up in the dialog list
	offsetDate, offsetID := 0, 0
	var offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}
	for page := 0; page < 10; page++ {
		res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetDate: offsetDate, OffsetID: offsetID, OffsetPeer: offsetPeer, Limit: 100})
		if err != nil {
			return nil, err
		}
		var chats []tg.ChatClass
		var msgs []tg.MessageClass
		var dialogs []tg.DialogClass
		switch v := res.(type) {
		case *tg.MessagesDialogs:
			chats, msgs, dialogs = v.Chats, v.Messages, v.Dialogs
		case *tg.MessagesDialogsSlice:
			chats, msgs, dialogs = v.Chats, v.Messages, v.Dialogs
		}
		for _, c := range chats {
			if ch, ok := c.(*tg.Channel); ok && ch.ID == s.ChannelID {
				return ch, nil
			}
		}
		if len(dialogs) < 100 || len(msgs) == 0 {
			break
		}
		last := msgs[len(msgs)-1]
		if m, ok := last.(*tg.Message); ok {
			offsetDate, offsetID = m.Date, m.ID
		} else {
			break
		}
		if ch, ok := chats[len(chats)-1].(*tg.Channel); ok {
			offsetPeer = &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
		}
	}
	return nil, fmt.Errorf("channel %d is not in this account's dialogs", s.ChannelID)
}

func username(ch *tg.Channel) string {
	if ch.Username != "" {
		return ch.Username
	}
	for _, u := range ch.Usernames {
		if u.Active {
			return u.Username
		}
	}
	return ""
}

func messages(h tg.MessagesMessagesClass) []tg.MessageClass {
	switch v := h.(type) {
	case *tg.MessagesMessages:
		return v.Messages
	case *tg.MessagesMessagesSlice:
		return v.Messages
	case *tg.MessagesChannelMessages:
		return v.Messages
	}
	return nil
}

// Posts converts messages: text with URL entities, text links, URL buttons and a web page
// preview as links; the permalink is t.me/<handle>/<id> (t.me/c/<id>/<msg> without a username).
func Posts(msgs []tg.MessageClass, s feeds.Source, ch *tg.Channel) []feeds.Post {
	var out []feeds.Post
	for _, raw := range msgs {
		m, ok := raw.(*tg.Message)
		if !ok || strings.TrimSpace(m.Message) == "" {
			continue
		}
		links := EntityLinks(m.Message, m.Entities)
		if wp, ok := m.Media.(*tg.MessageMediaWebPage); ok {
			if p, ok := wp.Webpage.(*tg.WebPage); ok {
				links = append(links, p.URL)
			}
		}
		if rm, ok := m.ReplyMarkup.(*tg.ReplyInlineMarkup); ok {
			for _, row := range rm.Rows {
				for _, b := range row.Buttons {
					if u, ok := b.(*tg.KeyboardButtonURL); ok {
						links = append(links, u.URL)
					}
				}
			}
		}
		link := ""
		if h := username(ch); h != "" {
			link = "https://t.me/" + h + "/" + strconv.Itoa(m.ID)
		} else {
			link = "https://t.me/c/" + strconv.FormatInt(ch.ID, 10) + "/" + strconv.Itoa(m.ID)
		}
		t := time.Unix(int64(m.Date), 0).UTC()
		out = append(out, feeds.Post{SourceID: s.ID, ExternalID: strconv.Itoa(m.ID), URL: link, Text: m.Message, Links: links, PostedAt: &t})
	}
	return out
}

// EntityLinks returns the URLs of url and text_url entities (offsets are UTF-16 units).
func EntityLinks(text string, ents []tg.MessageEntityClass) []string {
	u16 := utf16.Encode([]rune(text))
	var out []string
	for _, e := range ents {
		switch v := e.(type) {
		case *tg.MessageEntityTextURL:
			out = append(out, v.URL)
		case *tg.MessageEntityURL:
			if v.Offset >= 0 && v.Offset+v.Length <= len(u16) {
				s := string(utf16.Decode(u16[v.Offset : v.Offset+v.Length]))
				if !strings.Contains(s, "://") {
					s = "https://" + s
				}
				out = append(out, s)
			}
		}
	}
	return out
}

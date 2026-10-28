// Package notify is the one way the server tells a user something: every message is first
// written to the in-site inbox (notifications, inbox.go), then sent through the bot under
// the user's preferences. Users who blocked the bot are skipped and marked; email-only
// accounts (no telegram_id) only get the inbox row.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

var client = &http.Client{Timeout: 10 * time.Second}

// Direct sends a transactional message (category moderation) right away: see DirectAs.
func Direct(ctx context.Context, userID int64, text string) error {
	return DirectAs(ctx, userID, CatModeration, text)
}

// DirectAs writes the in-site inbox row (category c) and sends text (HTML parse mode —
// escape user input) to the user's Telegram chat at once, ignoring preferences, quiet hours
// and the cap. The Telegram part is skipped when no bot is configured, so features keep
// working in tests, and when the account has no Telegram (signed up by email and never
// linked it) or blocked the bot — the inbox row is written either way.
func DirectAs(ctx context.Context, userID int64, c Category, text string) error {
	p, err := db.Pool(ctx)
	if err != nil {
		return err
	}
	recordLogged(ctx, p, userID, c, text, false)
	return deliver(ctx, userID, text)
}

// DirectFor adapts DirectAs to func(ctx, userID, text) error for a fixed category.
func DirectFor(c Category) func(ctx context.Context, userID int64, text string) error {
	return func(ctx context.Context, userID int64, text string) error { return DirectAs(ctx, userID, c, text) }
}

// deliver is the Telegram part only (no inbox row): Send, Flush and Digest use it after
// writing the inbox themselves.
func deliver(ctx context.Context, userID int64, text string) error {
	token := config.Str("BOT_TOKEN", "")
	if token == "" {
		return nil
	}
	p, err := db.Pool(ctx)
	if err != nil {
		return err
	}
	var tgID *int64
	var inactive bool
	if err := p.QueryRow(ctx, `select telegram_id, bot_inactive from users where id = $1 and deleted_at is null`, userID).Scan(&tgID, &inactive); err != nil {
		return err
	}
	if inactive || tgID == nil {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"chat_id": *tgID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true})
	base := config.Str("TELEGRAM_API_BASE", "https://api.telegram.org")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusForbidden {
		_, err = p.Exec(ctx, `update users set bot_inactive = true where id = $1`, userID)
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram sendMessage: %s", res.Status)
	}
	return nil
}

// HasTelegram reports whether the account has a Telegram chat the bot could write to.
// Send uses it so email-only accounts never fill the outbox or use up a daily cap.
func HasTelegram(ctx context.Context, q Querier, userID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `select exists (select 1 from users where id = $1 and telegram_id is not null)`, userID).Scan(&ok)
	return ok, err
}

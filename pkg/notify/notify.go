// Package notify sends a "direct" Telegram message to a user through the bot (moderation
// decisions, reports, mentor matches). Users who blocked the bot are skipped and marked.
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

// Direct sends text (HTML parse mode — escape user input) to the user's Telegram chat.
// It returns nil without sending when no bot is configured, so features keep working in tests.
func Direct(ctx context.Context, userID int64, text string) error {
	token := config.Str("BOT_TOKEN", "")
	if token == "" {
		return nil
	}
	p, err := db.Pool(ctx)
	if err != nil {
		return err
	}
	var tgID int64
	var inactive bool
	if err := p.QueryRow(ctx, `select telegram_id, bot_inactive from users where id = $1 and deleted_at is null`, userID).Scan(&tgID, &inactive); err != nil {
		return err
	}
	if inactive {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"chat_id": tgID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true})
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

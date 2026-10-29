// Package notify is the only sendMessage path: persist the inbox, queue delivery,
// and reserve the user's shared Almaty-day budget before contacting Telegram.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

var client = &http.Client{Timeout: 10 * time.Second}

// Dispatcher also lets the bot use its existing pool and configured Telegram client.
// Every instance shares the same database budgets, outbox and idempotency keys.
type Dispatcher struct {
	Pool           *pgxpool.Pool
	Token, APIBase string
	Client         *http.Client
}

func dispatcher(ctx context.Context) (*Dispatcher, error) {
	p, err := db.Pool(ctx)
	return &Dispatcher{Pool: p, Token: config.Str("BOT_TOKEN", ""), APIBase: config.Str("TELEGRAM_API_BASE", "https://api.telegram.org"), Client: client}, err
}

// Direct and DirectAs are compatibility adapters, not budget exemptions.
func Direct(ctx context.Context, userID int64, text string) error {
	return DirectAs(ctx, userID, CatModeration, text)
}
func DirectAs(ctx context.Context, userID int64, c Category, text string) error {
	_, err := Send(ctx, userID, c, text, Opts{})
	return err
}
func DirectFor(c Category) func(context.Context, int64, string) error { return For(c) }

// HasTelegram excludes deleted, unlinked and inactive accounts; they still get inbox rows.
func HasTelegram(ctx context.Context, q Querier, userID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `select exists (select 1 from users where id = $1 and telegram_id is not null and not bot_inactive and deleted_at is null)`, userID).Scan(&ok)
	return ok, err
}

type delivery struct {
	text        string
	markup      json.RawMessage
	chatID      int64
	ids         []int64
	reservation int64
	kind        string
}

type telegramResult struct {
	sent, rejected, blocked bool
	err                     error
}

// transmit never retries an ambiguous outcome: Telegram has no idempotency token.
// Only an explicit ok:false response is safe to retry. Reservations stay consumed
// for timeouts, broken responses and a crash after Telegram accepted a message.
func (d *Dispatcher) transmit(ctx context.Context, m delivery) telegramResult {
	body, err := json.Marshal(map[string]any{"chat_id": m.chatID, "text": m.text, "parse_mode": "HTML", "disable_web_page_preview": true, "reply_markup": m.markup})
	if err != nil {
		return telegramResult{rejected: true, err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := d.APIBase
	if base == "" {
		base = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/bot"+d.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return telegramResult{rejected: true, err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	c := d.Client
	if c == nil {
		c = client
	}
	res, err := c.Do(req)
	if err != nil {
		// net/http wraps failures in a URL containing the bot token. Never log it.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return telegramResult{err: fmt.Errorf("telegram transport: %w", err)}
	}
	defer res.Body.Close()
	var response struct {
		OK          *bool  `json:"ok"`
		Code        int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil || response.OK == nil {
		return telegramResult{err: fmt.Errorf("telegram: ambiguous response (%s)", res.Status)}
	}
	if *response.OK {
		return telegramResult{sent: true}
	}
	return telegramResult{rejected: true, blocked: response.Code == 403, err: fmt.Errorf("telegram: %d %s", response.Code, response.Description)}
}

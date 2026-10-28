package bot

// Minimal Telegram Bot API client on net/http.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Button is one inline keyboard button: either a URL or callback data.
type Button struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

type markup struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}

// TgError is a Bot API error response.
type TgError struct {
	Method      string
	Code        int
	Description string
}

func (e *TgError) Error() string {
	return fmt.Sprintf("Telegram %s failed: %d %s", e.Method, e.Code, e.Description)
}

func isBlocked(err error) bool {
	var te *TgError
	return errors.As(err, &te) && te.Code == 403
}

type tgResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (b *Bot) tg(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return b.tgCall(ctx, method, params, true)
}

func (b *Bot) tgCall(ctx context.Context, method string, params any, retry bool) (json.RawMessage, error) {
	if b.Cfg.Token == "" {
		return nil, errors.New("BOT_TOKEN is not set")
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.Cfg.APIBase+"/bot"+b.Cfg.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Telegram %s: %w", method, err)
	}
	defer res.Body.Close()
	var data tgResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		data = tgResponse{ErrorCode: res.StatusCode, Description: "bad response"}
	}
	if data.OK {
		return data.Result, nil
	}
	if wait := data.Parameters.RetryAfter; data.ErrorCode == 429 && retry && wait > 0 && wait <= 5 {
		select {
		case <-time.After(time.Duration(wait) * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return b.tgCall(ctx, method, params, false)
	}
	return nil, &TgError{Method: method, Code: data.ErrorCode, Description: data.Description}
}

type sendParams struct {
	ChatID             int64          `json:"chat_id"`
	MessageID          int64          `json:"message_id,omitempty"`
	Text               string         `json:"text"`
	ParseMode          string         `json:"parse_mode"`
	LinkPreviewOptions map[string]any `json:"link_preview_options"`
	ReplyMarkup        *markup        `json:"reply_markup,omitempty"`
}

// send posts an HTML message; keyboard nil means no reply_markup at all.
func (b *Bot) send(ctx context.Context, chatID int64, text string, keyboard [][]Button) error {
	p := sendParams{ChatID: chatID, Text: text, ParseMode: "HTML", LinkPreviewOptions: map[string]any{"is_disabled": true}}
	if keyboard != nil {
		p.ReplyMarkup = &markup{keyboard}
	}
	_, err := b.tg(ctx, "sendMessage", p)
	return err
}

// edit replaces a message's text; the keyboard is always replaced (empty removes it).
func (b *Bot) edit(ctx context.Context, chatID, messageID int64, text string, keyboard [][]Button) error {
	if keyboard == nil {
		keyboard = [][]Button{}
	}
	p := sendParams{ChatID: chatID, MessageID: messageID, Text: text, ParseMode: "HTML",
		LinkPreviewOptions: map[string]any{"is_disabled": true}, ReplyMarkup: &markup{keyboard}}
	_, err := b.tg(ctx, "editMessageText", p)
	return err
}

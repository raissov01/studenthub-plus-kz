package server

import (
	"context"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/bot"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Café pre-ordering «Кофе / Кофейня» (docs/cafe.md): menu, orders, staff board, admin, the kaspi-pos webhook;
// FEATURE_CAFE. The staff Telegram buttons (Accept / Ready) are bot callbacks handled by the café package.
func init() {
	register(func(mux httpx.Router) { cafe.Routes(mux) })
	bot.CafeAction = cafe.BotAction
}

// StartBackground starts the in-process jobs of the API: the café payment poller (the backstop for a lost
// Kaspi webhook, pending → paid/failed, unpaid Kaspi orders cancelled). It does nothing while FEATURE_CAFE
// is off. cmd/server calls it once.
func StartBackground(ctx context.Context) {
	cafe.StartPoller(ctx, time.Duration(config.Int("CAFE_POLL_SECONDS", 15))*time.Second)
}

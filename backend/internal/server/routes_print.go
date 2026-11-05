package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/bot"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/printshop"
)

// Print / copy shop «Полиграфия» (docs/print.md): price list, uploads, jobs, the operator's queue board, admin;
// FEATURE_PRINT. Kaspi payments reuse the café's sidecar and webhook (internal/shopkit). The operator's Telegram
// buttons (Printing / Ready) are bot callbacks handled by the print package.
func init() {
	register(func(mux httpx.Router) { printshop.Routes(mux) })
	bot.PrintAction = printshop.BotAction
}

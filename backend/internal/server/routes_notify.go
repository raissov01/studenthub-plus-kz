package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
)

// F8 notification settings (GET/PUT /api/me/notifications) and the hourly outbox flush
// (GET /api/cron/notify).
func init() { register(func(mux httpx.Router) { notify.Routes(mux) }) }

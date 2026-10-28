package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// F8 notification settings (GET/PUT /api/me/notifications) and the hourly outbox flush
// (GET /api/cron/notify).
func init() { register(func(mux *http.ServeMux) { notify.Routes(mux) }) }

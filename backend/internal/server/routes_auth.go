package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/admin"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
)

// Accounts: Telegram login, sessions, /api/me, admin, feature switches and /api/config.
func init() {
	register(func(mux httpx.Router) {
		auth.Routes(mux)
		me.Routes(mux)
		admin.Routes(mux)
		features.Routes(mux)
	})
}

package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/admin"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
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

package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/webpush"
)

// Web Push subscriptions (POST/DELETE /api/me/push, POST /api/me/push/test), FEATURE_WEBPUSH.
func init() { register(func(mux httpx.Router) { webpush.Routes(mux) }) }

package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/bot"
)

func init() { register(func(mux httpx.Router) { bot.Routes(mux) }) }

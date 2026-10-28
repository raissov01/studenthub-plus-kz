package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/bot"
)

func init() { register(func(mux httpx.Router) { bot.Routes(mux) }) }

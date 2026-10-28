package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/bot"
)

func init() { register(func(mux *http.ServeMux) { bot.Routes(mux) }) }

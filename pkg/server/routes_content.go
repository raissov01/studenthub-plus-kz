package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/chat"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
)

func init() { register(func(mux httpx.Router) { content.Routes(mux); chat.Routes(mux) }) }

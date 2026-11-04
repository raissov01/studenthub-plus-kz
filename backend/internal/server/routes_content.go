package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/chat"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/content"
)

func init() { register(func(mux httpx.Router) { content.Routes(mux); chat.Routes(mux) }) }

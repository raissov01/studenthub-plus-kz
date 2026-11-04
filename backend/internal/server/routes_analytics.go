package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/analytics"
)

func init() { register(func(mux httpx.Router) { analytics.Routes(mux) }) }

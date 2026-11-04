package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/launch"
)

func init() { register(func(mux httpx.Router) { launch.Routes(mux) }) }

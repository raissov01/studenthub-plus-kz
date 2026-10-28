package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/analytics"
)

func init() { register(func(mux httpx.Router) { analytics.Routes(mux) }) }
